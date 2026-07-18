package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

type cacheActionPayload struct {
	ScenarioTemplateID string      `json:"scenarioTemplateId"`
	Request            ActionInput `json:"request"`
}

func (r *Repository) EnqueueCacheAction(
	ctx context.Context,
	userID uint64,
	labID string,
	input ActionInput,
	now time.Time,
) (ActionResult, error) {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ActionResult{}, fmt.Errorf("begin cache action: %w", err)
	}
	defer tx.Rollback()
	if existing, stored, findErr := findCacheAction(ctx, tx, input.OperationID); findErr == nil {
		if existing.LabID != labID || existing.RequestedBy != userID || !reflect.DeepEqual(stored, input) {
			return ActionResult{}, ErrOperationConflict
		}
		return ActionResult{Operation: existing, Existing: true}, nil
	} else if !errors.Is(findErr, sql.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("find cache action: %w", findErr)
	}
	session, err := scanSession(tx.QueryRowContext(ctx, `
		SELECT id, user_id, course_id, scenario_type, scenario_template_id,
			status, balancing_mode, redis_enabled, started_at,
			last_effective_action_at, terminated_at, termination_reason,
			created_at, updated_at
		FROM lab_sessions WHERE id = ? FOR UPDATE`, labID))
	if errors.Is(err, sql.ErrNoRows) {
		return ActionResult{}, ErrNotFound
	}
	if err != nil {
		return ActionResult{}, fmt.Errorf("lock cache lab: %w", err)
	}
	if session.UserID != userID {
		return ActionResult{}, ErrNotOwned
	}
	if session.Status != StatusRunning {
		return ActionResult{}, ErrNotRunning
	}
	if session.ScenarioType != "multi_level_cache" {
		return ActionResult{}, ErrActionNotAllowed
	}
	if input.TargetInstanceID != nil {
		var count int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM lab_instances
			WHERE lab_id = ? AND instance_name = ? AND status = 'running'`,
			labID, *input.TargetInstanceID,
		).Scan(&count); err != nil {
			return ActionResult{}, fmt.Errorf("validate cache instance: %w", err)
		}
		if count != 1 {
			return ActionResult{}, ErrInstanceNotFound
		}
	}
	if err := ensureNoPendingTopologyAction(ctx, tx, labID); err != nil {
		return ActionResult{}, err
	}
	payload, err := json.Marshal(cacheActionPayload{
		ScenarioTemplateID: session.ScenarioTemplateID, Request: input,
	})
	if err != nil {
		return ActionResult{}, fmt.Errorf("encode cache action: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO lab_operations (
			operation_id, lab_id, requested_by, action, target_instance_id,
			status, payload_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?)`,
		input.OperationID, labID, userID, input.ActionType,
		input.TargetInstanceID, payload, now, now,
	)
	if err != nil {
		if duplicateKey(err) {
			return ActionResult{}, ErrOperationConflict
		}
		return ActionResult{}, fmt.Errorf("insert cache action: %w", err)
	}
	databaseID, err := result.LastInsertId()
	if err != nil {
		return ActionResult{}, fmt.Errorf("read cache action id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ActionResult{}, fmt.Errorf("commit cache action: %w", err)
	}
	return ActionResult{Operation: CreatedOperation{
		ID: uint64(databaseID), OperationID: input.OperationID, LabID: labID,
		RequestedBy: userID, Action: input.ActionType,
		TargetInstanceID: input.TargetInstanceID, Status: "pending", SubmittedAt: now,
	}}, nil
}

func findCacheAction(
	ctx context.Context,
	tx *sql.Tx,
	operationID string,
) (CreatedOperation, ActionInput, error) {
	operation, err := findActionByOperationID(ctx, tx, operationID)
	if err != nil {
		return CreatedOperation{}, ActionInput{}, err
	}
	var body []byte
	if err := tx.QueryRowContext(ctx, "SELECT payload_json FROM lab_operations WHERE id = ?", operation.ID).Scan(&body); err != nil {
		return CreatedOperation{}, ActionInput{}, err
	}
	var payload cacheActionPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return CreatedOperation{}, ActionInput{}, err
	}
	return operation, payload.Request, nil
}
