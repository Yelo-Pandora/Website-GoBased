package operation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"website-gobased/services/platform-api/internal/lab"
)

// Repository stores and leases persistent lab operations.
type Repository struct {
	database *sql.DB
}

// NewRepository returns a MySQL-backed operation repository.
func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

// Claim leases the oldest pending or expired operation to one worker.
func (r *Repository) Claim(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseExpires time.Time,
) (Record, error) {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Record{}, fmt.Errorf("begin claim operation transaction: %w", err)
	}
	defer tx.Rollback()

	var record Record
	var payload []byte
	err = tx.QueryRowContext(ctx, `
		SELECT
			id, operation_id, lab_id, requested_by, action, payload_json,
			status, attempt_count, created_at
		FROM lab_operations
		WHERE status = 'pending'
		   OR (
			status IN ('claimed', 'running')
			AND lease_expires_at <= ?
		   )
		ORDER BY created_at, id
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, now).Scan(
		&record.ID,
		&record.OperationID,
		&record.LabID,
		&record.RequestedBy,
		&record.Action,
		&payload,
		&record.Status,
		&record.AttemptCount,
		&record.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNoPending
	}
	if err != nil {
		return Record{}, fmt.Errorf("select operation for claim: %w", err)
	}
	if len(payload) > 0 {
		record.Payload = append(json.RawMessage(nil), payload...)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE lab_operations
		SET
			status = 'claimed',
			lease_owner = ?,
			lease_expires_at = ?,
			attempt_count = attempt_count + 1,
			updated_at = ?
		WHERE id = ?`, owner, leaseExpires, now, record.ID)
	if err != nil {
		return Record{}, fmt.Errorf("claim operation: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return Record{}, fmt.Errorf("commit claim operation: %w", err)
	}
	record.Status = StatusClaimed
	record.LeaseOwner = owner
	record.LeaseExpires = leaseExpires
	record.AttemptCount++
	return record, nil
}

// MarkRunning records that a claimed operation started external work.
func (r *Repository) MarkRunning(
	ctx context.Context,
	record Record,
	now time.Time,
	leaseExpires time.Time,
) error {
	result, err := r.database.ExecContext(ctx, `
		UPDATE lab_operations
		SET status = 'running', lease_expires_at = ?, updated_at = ?
		WHERE id = ? AND status = 'claimed' AND lease_owner = ?`,
		leaseExpires,
		now,
		record.ID,
		record.LeaseOwner,
	)
	if err != nil {
		return fmt.Errorf("mark operation running: %w", err)
	}
	return requireOneRow(result)
}

// CompleteProvision atomically completes CREATE_LAB and its session transition.
func (r *Repository) CompleteProvision(
	ctx context.Context,
	record Record,
	resultValue any,
	errorCode string,
	errorMessage string,
	now time.Time,
) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin complete operation transaction: %w", err)
	}
	defer tx.Rollback()

	status := StatusSucceeded
	targetLabStatus := lab.StatusRunning
	var resultJSON []byte
	if errorCode == "" {
		resultJSON, err = json.Marshal(resultValue)
		if err != nil {
			return fmt.Errorf("encode operation result: %w", err)
		}
	} else {
		status = StatusFailed
		targetLabStatus = lab.StatusFailed
	}
	operationResult, err := tx.ExecContext(ctx, `
		UPDATE lab_operations
		SET
			status = ?,
			result_json = ?,
			error_code = NULLIF(?, ''),
			error_message = NULLIF(?, ''),
			lease_owner = NULL,
			lease_expires_at = NULL,
			completed_at = ?,
			updated_at = ?
		WHERE id = ? AND status = 'running' AND lease_owner = ?`,
		status,
		nullJSON(resultJSON),
		errorCode,
		errorMessage,
		now,
		now,
		record.ID,
		record.LeaseOwner,
	)
	if err != nil {
		return fmt.Errorf("complete provision operation: %w", err)
	}
	if err := requireOneRow(operationResult); err != nil {
		return err
	}

	labResult, err := tx.ExecContext(ctx, `
		UPDATE lab_sessions
		SET
			status = ?,
			started_at = CASE WHEN ? = 'Running' THEN ? ELSE started_at END,
			last_effective_action_at = CASE
				WHEN ? = 'Running' THEN ?
				ELSE last_effective_action_at
			END,
			updated_at = ?
		WHERE id = ? AND status = 'Preparing'`,
		targetLabStatus,
		targetLabStatus,
		now,
		targetLabStatus,
		now,
		now,
		record.LabID,
	)
	if err != nil {
		return fmt.Errorf("transition provisioned lab: %w", err)
	}
	if err := requireOneRow(labResult); err != nil {
		return lab.ErrStateConflict
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit complete operation: %w", err)
	}
	return nil
}

func requireOneRow(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected operation rows: %w", err)
	}
	if rowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

func nullJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
