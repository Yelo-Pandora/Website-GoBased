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
	resultValue ProvisionResult,
	errorCode string,
	errorMessage string,
	now time.Time,
) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin complete operation transaction: %w", err)
	}
	defer tx.Rollback()

	status := StatusFailed
	var resultJSON []byte
	if errorCode == "" {
		if err := persistProvisionResult(ctx, tx, record, resultValue, now); err != nil {
			return err
		}
		status = StatusSucceeded
		resultJSON, err = json.Marshal(resultValue)
		if err != nil {
			return fmt.Errorf("encode provision result: %w", err)
		}
	}
	if err := completeOperationRecord(
		ctx, tx, record, status, resultJSON, errorCode, errorMessage, now,
	); err != nil {
		return err
	}
	if errorCode == "" {
		labResult, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, mysql_db_name = ?, mysql_db_user = ?,
				redis_enabled = ?, started_at = ?, last_effective_action_at = ?,
				updated_at = ?
			WHERE id = ? AND status = ?`,
			lab.StatusRunning,
			resultValue.DatabaseName,
			resultValue.DatabaseUser,
			resultValue.Redis != nil,
			now,
			now,
			now,
			record.LabID,
			lab.StatusPreparing,
		)
		if err != nil {
			return fmt.Errorf("transition provisioned lab: %w", err)
		}
		if err := requireOneRow(labResult); err != nil {
			return lab.ErrStateConflict
		}
	} else {
		labResult, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, updated_at = ?
			WHERE id = ? AND status = ?`,
			lab.StatusFailed,
			now,
			record.LabID,
			lab.StatusPreparing,
		)
		if err != nil {
			return fmt.Errorf("fail provisioned lab: %w", err)
		}
		if err := requireOneRow(labResult); err != nil {
			return lab.ErrStateConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit complete operation: %w", err)
	}
	return nil
}

// CompleteReset atomically completes RESET_LAB and restores persisted resources.
func (r *Repository) CompleteReset(
	ctx context.Context,
	record Record,
	resultValue ProvisionResult,
	errorCode string,
	errorMessage string,
	now time.Time,
) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin complete reset transaction: %w", err)
	}
	defer tx.Rollback()
	status := StatusFailed
	var resultJSON []byte
	if errorCode == "" {
		if err := persistProvisionResult(ctx, tx, record, resultValue, now); err != nil {
			return err
		}
		status = StatusSucceeded
		resultJSON, err = json.Marshal(resultValue)
		if err != nil {
			return fmt.Errorf("encode reset result: %w", err)
		}
	}
	if err := completeOperationRecord(
		ctx, tx, record, status, resultJSON, errorCode, errorMessage, now,
	); err != nil {
		return err
	}
	if errorCode == "" {
		labResult, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, mysql_db_name = ?, mysql_db_user = ?,
				redis_enabled = ?, last_effective_action_at = ?, updated_at = ?
			WHERE id = ? AND status IN (?, ?)`,
			lab.StatusRunning,
			resultValue.DatabaseName,
			resultValue.DatabaseUser,
			resultValue.Redis != nil,
			now,
			now,
			record.LabID,
			lab.StatusRunning,
			lab.StatusExpiring,
		)
		if err != nil {
			return fmt.Errorf("complete reset lab transition: %w", err)
		}
		if err := requireOneRow(labResult); err != nil {
			return lab.ErrStateConflict
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE lab_instances
			SET status = 'unknown', container_id = NULL, updated_at = ?
			WHERE lab_id = ?`, now, record.LabID); err != nil {
			return fmt.Errorf("mark reset instances unknown: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE lab_resources
			SET status = 'unknown', external_id = NULL, updated_at = ?
			WHERE lab_id = ?`, now, record.LabID); err != nil {
			return fmt.Errorf("mark reset resources unknown: %w", err)
		}
		labResult, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, updated_at = ?
			WHERE id = ? AND status IN (?, ?)`,
			lab.StatusFailed,
			now,
			record.LabID,
			lab.StatusRunning,
			lab.StatusExpiring,
		)
		if err != nil {
			return fmt.Errorf("fail reset lab transition: %w", err)
		}
		if err := requireOneRow(labResult); err != nil {
			return lab.ErrStateConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit complete reset: %w", err)
	}
	return nil
}

// CompleteDestroy atomically completes DESTROY_LAB and its terminal transition.
func (r *Repository) CompleteDestroy(
	ctx context.Context,
	record Record,
	resultValue any,
	errorCode string,
	errorMessage string,
	now time.Time,
) error {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin complete destroy transaction: %w", err)
	}
	defer tx.Rollback()
	status := StatusFailed
	var resultJSON []byte
	if errorCode == "" {
		status = StatusSucceeded
		resultJSON, err = json.Marshal(resultValue)
		if err != nil {
			return fmt.Errorf("encode destroy result: %w", err)
		}
	}
	if err := completeOperationRecord(
		ctx, tx, record, status, resultJSON, errorCode, errorMessage, now,
	); err != nil {
		return err
	}
	if errorCode == "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE lab_instances
			SET status = 'terminated', container_id = NULL, updated_at = ?
			WHERE lab_id = ?`, now, record.LabID); err != nil {
			return fmt.Errorf("terminate lab instances: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE lab_resources
			SET status = 'deleted', external_id = NULL, updated_at = ?
			WHERE lab_id = ?`, now, record.LabID); err != nil {
			return fmt.Errorf("delete persisted lab resources: %w", err)
		}
		labResult, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, terminated_at = ?, termination_reason = 'user_requested',
				updated_at = ?
			WHERE id = ? AND status = ?`,
			lab.StatusTerminated,
			now,
			now,
			record.LabID,
			lab.StatusTerminating,
		)
		if err != nil {
			return fmt.Errorf("complete destroy lab transition: %w", err)
		}
		if err := requireOneRow(labResult); err != nil {
			return lab.ErrStateConflict
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE lab_instances
			SET status = 'unknown', container_id = NULL, updated_at = ?
			WHERE lab_id = ?`, now, record.LabID); err != nil {
			return fmt.Errorf("mark destroy instances unknown: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE lab_resources
			SET status = 'unknown', external_id = NULL, updated_at = ?
			WHERE lab_id = ?`, now, record.LabID); err != nil {
			return fmt.Errorf("mark destroy resources unknown: %w", err)
		}
		labResult, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, termination_reason = 'destroy_failed', updated_at = ?
			WHERE id = ? AND status = ?`,
			lab.StatusFailed,
			now,
			record.LabID,
			lab.StatusTerminating,
		)
		if err != nil {
			return fmt.Errorf("fail destroy lab transition: %w", err)
		}
		if err := requireOneRow(labResult); err != nil {
			return lab.ErrStateConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit complete destroy: %w", err)
	}
	return nil
}

func completeOperationRecord(
	ctx context.Context,
	tx *sql.Tx,
	record Record,
	status string,
	resultJSON []byte,
	errorCode string,
	errorMessage string,
	now time.Time,
) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE lab_operations
		SET
			status = ?, result_json = ?,
			error_code = NULLIF(?, ''), error_message = NULLIF(?, ''),
			lease_owner = NULL, lease_expires_at = NULL,
			completed_at = ?, updated_at = ?
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
		return fmt.Errorf("complete lab operation: %w", err)
	}
	return requireOneRow(result)
}

func persistProvisionResult(
	ctx context.Context,
	tx *sql.Tx,
	record Record,
	result ProvisionResult,
	now time.Time,
) error {
	if result.LabID != record.LabID || result.DatabaseName == "" ||
		result.DatabaseUser == "" || result.NetworkName == "" ||
		result.NetworkID == "" || len(result.Instances) == 0 {
		return errors.New("orchestrator provision result is incomplete")
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM lab_resources WHERE lab_id = ?", record.LabID); err != nil {
		return fmt.Errorf("replace lab resources: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM lab_instances WHERE lab_id = ?", record.LabID); err != nil {
		return fmt.Errorf("replace lab instances: %w", err)
	}
	for _, instance := range result.Instances {
		if instance.InstanceName == "" || instance.ContainerID == "" ||
			instance.ContainerName == "" || instance.MemoryLimitMB <= 0 ||
			instance.PerformancePercent <= 0 || instance.EffectiveCapacity < 0 ||
			instance.CurrentWeight <= 0 {
			return errors.New("orchestrator instance result is incomplete")
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO lab_instances (
				lab_id, instance_name, container_id, status, cpu_limit_cores,
				memory_limit_mb, performance_percent, effective_capacity,
				current_weight, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				container_id = VALUES(container_id), status = VALUES(status),
				cpu_limit_cores = VALUES(cpu_limit_cores),
				memory_limit_mb = VALUES(memory_limit_mb),
				performance_percent = VALUES(performance_percent),
				effective_capacity = VALUES(effective_capacity),
				current_weight = VALUES(current_weight), updated_at = VALUES(updated_at)`,
			record.LabID,
			instance.InstanceName,
			instance.ContainerID,
			valueOrDefault(instance.Status, "running"),
			instance.CPULimitCores,
			instance.MemoryLimitMB,
			instance.PerformancePercent,
			instance.EffectiveCapacity,
			instance.CurrentWeight,
			now,
			now,
		)
		if err != nil {
			return fmt.Errorf("persist lab instance %s: %w", instance.InstanceName, err)
		}
	}
	resources := []struct {
		typ      string
		name     string
		external string
		metadata any
	}{
		{typ: "mysql-database", name: result.DatabaseName, metadata: map[string]any{
			"databaseUser": result.DatabaseUser,
		}},
		{typ: "mysql-user", name: result.DatabaseUser},
		{typ: "docker-network", name: result.NetworkName, external: result.NetworkID},
		{typ: "nginx-fragment", name: record.LabID},
	}
	if result.Redis != nil {
		resources = append(resources, struct {
			typ      string
			name     string
			external string
			metadata any
		}{
			typ: "session-redis", name: result.Redis.ContainerName,
			external: result.Redis.ContainerID,
		})
	}
	for _, resource := range resources {
		metadata, err := json.Marshal(resource.metadata)
		if err != nil {
			return fmt.Errorf("encode resource metadata: %w", err)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO lab_resources (
				lab_id, resource_type, resource_name, external_id, status,
				metadata_json, created_at, updated_at
			) VALUES (?, ?, ?, NULLIF(?, ''), 'ready', NULLIF(?, 'null'), ?, ?)
			ON DUPLICATE KEY UPDATE
				external_id = VALUES(external_id), status = VALUES(status),
				metadata_json = VALUES(metadata_json), updated_at = VALUES(updated_at)`,
			record.LabID,
			resource.typ,
			resource.name,
			resource.external,
			metadata,
			now,
			now,
		)
		if err != nil {
			return fmt.Errorf("persist lab resource %s/%s: %w", resource.typ, resource.name, err)
		}
	}
	return nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// CompleteFailure fails an operation without applying an action-specific transition.
func (r *Repository) CompleteFailure(
	ctx context.Context,
	record Record,
	errorCode string,
	errorMessage string,
	now time.Time,
) error {
	result, err := r.database.ExecContext(ctx, `
		UPDATE lab_operations
		SET
			status = 'failed',
			error_code = ?,
			error_message = ?,
			lease_owner = NULL,
			lease_expires_at = NULL,
			completed_at = ?,
			updated_at = ?
		WHERE id = ? AND status = 'running' AND lease_owner = ?`,
		errorCode,
		errorMessage,
		now,
		now,
		record.ID,
		record.LeaseOwner,
	)
	if err != nil {
		return fmt.Errorf("fail operation: %w", err)
	}
	return requireOneRow(result)
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
