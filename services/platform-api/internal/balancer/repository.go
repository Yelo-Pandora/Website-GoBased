package balancer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Repository persists internal adaptive weight operations.
type Repository struct {
	database *sql.DB
}

// NewRepository returns a MySQL-backed adaptive repository.
func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

// ListAdaptiveLabs returns running application-cluster labs in adaptive mode.
func (r *Repository) ListAdaptiveLabs(ctx context.Context) ([]Lab, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT
			s.id, s.user_id, i.instance_name, i.status,
			i.effective_capacity, i.current_weight
		FROM lab_sessions AS s
		JOIN lab_instances AS i ON i.lab_id = s.id
		WHERE s.scenario_type = 'application_cluster'
		  AND s.balancing_mode = 'adaptive'
		  AND s.status IN ('Running', 'Expiring')
		ORDER BY s.id, i.instance_name`)
	if err != nil {
		return nil, fmt.Errorf("list adaptive labs: %w", err)
	}
	defer rows.Close()
	var labs []Lab
	for rows.Next() {
		var labID string
		var userID uint64
		var instance Instance
		if err := rows.Scan(
			&labID,
			&userID,
			&instance.ID,
			&instance.Status,
			&instance.EffectiveCapacity,
			&instance.CurrentWeight,
		); err != nil {
			return nil, fmt.Errorf("scan adaptive lab: %w", err)
		}
		if len(labs) == 0 || labs[len(labs)-1].ID != labID {
			labs = append(labs, Lab{ID: labID, UserID: userID})
		}
		labs[len(labs)-1].Instances = append(labs[len(labs)-1].Instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate adaptive labs: %w", err)
	}
	return labs, nil
}

// EnqueueAdjustment validates current topology and inserts one internal operation.
func (r *Repository) EnqueueAdjustment(
	ctx context.Context,
	adjustment Adjustment,
	now time.Time,
) (Operation, error) {
	if adjustment.OperationID == "" || adjustment.LabID == "" ||
		adjustment.RequestedBy == 0 || adjustment.TopologyFingerprint == "" ||
		len(adjustment.Weights) == 0 {
		return Operation{}, ErrStale
	}
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Operation{}, fmt.Errorf("begin adaptive adjustment: %w", err)
	}
	defer tx.Rollback()
	var userID uint64
	var status string
	var scenarioType string
	var mode string
	err = tx.QueryRowContext(ctx, `
		SELECT user_id, status, scenario_type, balancing_mode
		FROM lab_sessions
		WHERE id = ?
		FOR UPDATE`, adjustment.LabID).Scan(&userID, &status, &scenarioType, &mode)
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, ErrStale
	}
	if err != nil {
		return Operation{}, fmt.Errorf("lock adaptive lab: %w", err)
	}
	if userID != adjustment.RequestedBy || scenarioType != "application_cluster" ||
		mode != "adaptive" || (status != "Running" && status != "Expiring") {
		return Operation{}, ErrStale
	}
	var pendingID uint64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM lab_operations
		WHERE lab_id = ?
		  AND status IN ('pending', 'claimed', 'running', 'compensating')
		LIMIT 1`, adjustment.LabID).Scan(&pendingID)
	if err == nil {
		return Operation{}, ErrBusy
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, fmt.Errorf("check adaptive operation queue: %w", err)
	}
	instances, err := lockInstances(ctx, tx, adjustment.LabID)
	if err != nil {
		return Operation{}, err
	}
	if TopologyFingerprint(instances) != adjustment.TopologyFingerprint {
		return Operation{}, ErrStale
	}
	if err := validateAdjustmentWeights(instances, adjustment.Weights); err != nil {
		return Operation{}, err
	}
	if WeightsEqual(instances, adjustment.Weights) {
		return Operation{}, ErrNoChange
	}
	payload, err := encodeAdjustmentPayload(adjustment)
	if err != nil {
		return Operation{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO lab_operations (
			operation_id, lab_id, requested_by, action, status,
			payload_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?)`,
		adjustment.OperationID,
		adjustment.LabID,
		adjustment.RequestedBy,
		ActionApplyWeights,
		payload,
		now,
		now,
	)
	if err != nil {
		return Operation{}, fmt.Errorf("insert adaptive adjustment: %w", err)
	}
	if _, err := result.LastInsertId(); err != nil {
		return Operation{}, fmt.Errorf("read adaptive adjustment id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Operation{}, fmt.Errorf("commit adaptive adjustment: %w", err)
	}
	return Operation{OperationID: adjustment.OperationID, Status: "pending"}, nil
}

// FindAdjustment returns the persisted status of one internal adjustment.
func (r *Repository) FindAdjustment(
	ctx context.Context,
	operationID string,
) (Operation, error) {
	var operation Operation
	var errorCode sql.NullString
	var errorMessage sql.NullString
	var resultJSON []byte
	err := r.database.QueryRowContext(ctx, `
		SELECT operation_id, status, error_code, error_message, result_json
		FROM lab_operations
		WHERE operation_id = ? AND action = ?`,
		operationID,
		ActionApplyWeights,
	).Scan(
		&operation.OperationID,
		&operation.Status,
		&errorCode,
		&errorMessage,
		&resultJSON,
	)
	if err != nil {
		return Operation{}, fmt.Errorf("find adaptive adjustment: %w", err)
	}
	operation.ErrorCode = errorCode.String
	operation.ErrorMessage = errorMessage.String
	if len(resultJSON) > 0 {
		var result struct {
			Skipped bool `json:"skipped"`
		}
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			return Operation{}, fmt.Errorf("decode adaptive adjustment result: %w", err)
		}
		operation.Skipped = result.Skipped
	}
	return operation, nil
}

func lockInstances(ctx context.Context, tx *sql.Tx, labID string) ([]Instance, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT instance_name, status, effective_capacity, current_weight
		FROM lab_instances
		WHERE lab_id = ?
		ORDER BY instance_name
		FOR UPDATE`, labID)
	if err != nil {
		return nil, fmt.Errorf("lock adaptive instances: %w", err)
	}
	defer rows.Close()
	var instances []Instance
	for rows.Next() {
		var instance Instance
		if err := rows.Scan(
			&instance.ID,
			&instance.Status,
			&instance.EffectiveCapacity,
			&instance.CurrentWeight,
		); err != nil {
			return nil, fmt.Errorf("scan adaptive instance: %w", err)
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate adaptive instances: %w", err)
	}
	return instances, nil
}

func validateAdjustmentWeights(instances []Instance, weights []Weight) error {
	if len(instances) != len(weights) {
		return ErrStale
	}
	values := make(map[string]int, len(weights))
	for _, weight := range weights {
		if weight.InstanceID == "" || weight.Weight < 1 || weight.Weight > 100 ||
			values[weight.InstanceID] != 0 {
			return ErrStale
		}
		values[weight.InstanceID] = weight.Weight
	}
	for _, instance := range instances {
		if values[instance.ID] == 0 {
			return ErrStale
		}
	}
	return nil
}

func encodeAdjustmentPayload(adjustment Adjustment) ([]byte, error) {
	type server struct {
		InstanceName string `json:"instanceName"`
		Weight       int    `json:"weight"`
	}
	payload := struct {
		ExpectedMode        string   `json:"expectedMode"`
		TopologyFingerprint string   `json:"topologyFingerprint"`
		Servers             []server `json:"servers"`
		Request             struct {
			Weights []Weight `json:"weights"`
		} `json:"request"`
	}{
		ExpectedMode:        "adaptive",
		TopologyFingerprint: adjustment.TopologyFingerprint,
		Servers:             make([]server, 0, len(adjustment.Weights)),
	}
	payload.Request.Weights = append([]Weight(nil), adjustment.Weights...)
	for _, weight := range adjustment.Weights {
		payload.Servers = append(payload.Servers, server{
			InstanceName: weight.InstanceID,
			Weight:       weight.Weight,
		})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode adaptive adjustment: %w", err)
	}
	return body, nil
}
