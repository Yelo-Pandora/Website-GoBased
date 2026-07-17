package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	createLabAction                   = "CREATE_LAB"
	resetLabAction                    = "RESET_LAB"
	destroyLabAction                  = "DESTROY_LAB"
	globalAdmissionLockName           = "platform:global-admission"
	globalAdmissionLockTimeoutSeconds = 5
	globalAdmissionReleaseTimeout     = 5 * time.Second
)

var activeStatuses = []Status{
	StatusPreparing,
	StatusRunning,
	StatusExpiring,
	StatusTerminating,
}

// Repository stores persistent lab sessions and their initial operations.
type Repository struct {
	database *sql.DB
}

// NewRepository returns a MySQL-backed lab repository.
func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

// Create atomically enforces admission rules and inserts a lab and operation.
func (r *Repository) Create(
	ctx context.Context,
	userID uint64,
	courseID uint64,
	operationID string,
	labID string,
	quota Quota,
	now time.Time,
) (CreateResult, error) {
	connection, err := r.database.Conn(ctx)
	if err != nil {
		return CreateResult{}, fmt.Errorf("reserve create lab connection: %w", err)
	}
	defer connection.Close()

	tx, err := connection.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return CreateResult{}, fmt.Errorf("begin create lab transaction: %w", err)
	}
	globalAdmissionLocked := false
	defer func() {
		_ = tx.Rollback()
		if globalAdmissionLocked {
			releaseCtx, cancel := context.WithTimeout(
				context.WithoutCancel(ctx),
				globalAdmissionReleaseTimeout,
			)
			_ = releaseGlobalAdmission(releaseCtx, connection)
			cancel()
		}
	}()

	existing, err := findExistingCreate(ctx, tx, operationID)
	if err == nil {
		if existing.Operation.RequestedBy != userID ||
			existing.Operation.Action != createLabAction ||
			existing.Session.CourseID != courseID {
			return CreateResult{}, ErrOperationConflict
		}
		existing.Existing = true
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, err
	}

	if err := lockUser(ctx, tx, userID); err != nil {
		return CreateResult{}, err
	}
	if err := lockGlobalAdmission(ctx, tx); err != nil {
		return CreateResult{}, err
	}
	globalAdmissionLocked = true
	existing, err = findExistingCreate(ctx, tx, operationID)
	if err == nil {
		if existing.Operation.RequestedBy != userID ||
			existing.Operation.Action != createLabAction ||
			existing.Session.CourseID != courseID {
			return CreateResult{}, ErrOperationConflict
		}
		existing.Existing = true
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, err
	}
	var courseSlug string
	var courseStatus string
	if err := tx.QueryRowContext(
		ctx,
		"SELECT slug, status FROM courses WHERE id = ?",
		courseID,
	).Scan(&courseSlug, &courseStatus); errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, ErrCourseNotFound
	} else if err != nil {
		return CreateResult{}, fmt.Errorf("load lab course: %w", err)
	}
	scenario, ok := scenarioForCourse(courseSlug, courseStatus)
	if !ok {
		return CreateResult{}, ErrLabUnavailable
	}

	activeLabID, err := findActiveLabID(ctx, tx, userID)
	if err == nil && activeLabID != "" {
		return CreateResult{}, ErrAlreadyActive
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, err
	}
	usage, err := loadUsage(ctx, tx)
	if err != nil {
		return CreateResult{}, err
	}
	if err := quota.AdmitCreate(usage, Reservation{
		TemporaryContainers: scenario.TemporaryContainers,
		Instances:           scenario.InitialInstances,
	}); err != nil {
		return CreateResult{}, err
	}

	session := Session{
		ID:                 labID,
		UserID:             userID,
		CourseID:           courseID,
		ScenarioType:       scenario.Type,
		ScenarioTemplateID: scenario.TemplateID,
		Status:             StatusPreparing,
		BalancingMode:      "fixed",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO lab_sessions (
			id, user_id, course_id, scenario_type, scenario_template_id,
			status, balancing_mode, redis_enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'fixed', FALSE, ?, ?)`,
		session.ID,
		session.UserID,
		session.CourseID,
		session.ScenarioType,
		session.ScenarioTemplateID,
		session.Status,
		now,
		now,
	); err != nil {
		if duplicateKey(err) {
			return CreateResult{}, ErrOperationConflict
		}
		return CreateResult{}, fmt.Errorf("insert lab session: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"courseId":           courseID,
		"scenarioType":       scenario.Type,
		"scenarioTemplateId": scenario.TemplateID,
		"initialInstances":   scenario.InitialInstances,
		"redisRequired":      scenario.RedisRequired,
	})
	if err != nil {
		return CreateResult{}, fmt.Errorf("encode create lab payload: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO lab_operations (
			operation_id, lab_id, requested_by, action, status,
			payload_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?)`,
		operationID,
		labID,
		userID,
		createLabAction,
		payload,
		now,
		now,
	)
	if err != nil {
		if duplicateKey(err) {
			return CreateResult{}, ErrOperationConflict
		}
		return CreateResult{}, fmt.Errorf("insert create lab operation: %w", err)
	}
	operationDatabaseID, err := result.LastInsertId()
	if err != nil {
		return CreateResult{}, fmt.Errorf("read create lab operation id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO course_progress (user_id, course_id, last_lab_id)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE last_lab_id = VALUES(last_lab_id)`,
		userID,
		courseID,
		labID,
	); err != nil {
		return CreateResult{}, fmt.Errorf("update course last lab: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CreateResult{}, fmt.Errorf("commit create lab transaction: %w", err)
	}
	releaseCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		globalAdmissionReleaseTimeout,
	)
	err = releaseGlobalAdmission(releaseCtx, connection)
	cancel()
	if err != nil {
		return CreateResult{}, err
	}
	globalAdmissionLocked = false
	return CreateResult{
		Session: session,
		Operation: CreatedOperation{
			ID:          uint64(operationDatabaseID),
			OperationID: operationID,
			LabID:       labID,
			RequestedBy: userID,
			Action:      createLabAction,
			Status:      "pending",
			SubmittedAt: now,
		},
	}, nil
}

// FindOwned returns one lab and distinguishes missing from foreign ownership.
func (r *Repository) FindOwned(
	ctx context.Context,
	labID string,
	userID uint64,
) (Session, error) {
	session, err := scanSession(r.database.QueryRowContext(ctx, `
		SELECT
			id, user_id, course_id, scenario_type, scenario_template_id,
			status, balancing_mode, redis_enabled, started_at,
			last_effective_action_at, terminated_at, termination_reason,
			created_at, updated_at
		FROM lab_sessions
		WHERE id = ?`, labID))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if session.UserID != userID {
		return Session{}, ErrNotOwned
	}
	return session, nil
}

// EnqueueAction persists one serialized reset or destroy operation.
func (r *Repository) EnqueueAction(
	ctx context.Context,
	userID uint64,
	labID string,
	operationID string,
	action string,
	payload any,
	now time.Time,
) (ActionResult, error) {
	connection, err := r.database.Conn(ctx)
	if err != nil {
		return ActionResult{}, fmt.Errorf("reserve lab action connection: %w", err)
	}
	defer connection.Close()
	tx, err := connection.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ActionResult{}, fmt.Errorf("begin lab action transaction: %w", err)
	}
	defer tx.Rollback()

	existing, err := findActionByOperationID(ctx, tx, operationID)
	if err == nil {
		return matchingActionResult(existing, userID, labID, action)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("find existing lab action: %w", err)
	}

	session, err := scanSession(tx.QueryRowContext(ctx, `
		SELECT
			id, user_id, course_id, scenario_type, scenario_template_id,
			status, balancing_mode, redis_enabled, started_at,
			last_effective_action_at, terminated_at, termination_reason,
			created_at, updated_at
		FROM lab_sessions
		WHERE id = ?
		FOR UPDATE`, labID))
	if errors.Is(err, sql.ErrNoRows) {
		return ActionResult{}, ErrNotFound
	}
	if err != nil {
		return ActionResult{}, fmt.Errorf("lock lab action session: %w", err)
	}
	if session.UserID != userID {
		return ActionResult{}, ErrNotOwned
	}
	existing, err = findActionByOperationID(ctx, tx, operationID)
	if err == nil {
		return matchingActionResult(existing, userID, labID, action)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("recheck existing lab action: %w", err)
	}
	if action != resetLabAction && action != destroyLabAction {
		return ActionResult{}, ErrInvalidRequest
	}
	if action == resetLabAction {
		if session.Status != StatusRunning && session.Status != StatusExpiring {
			return ActionResult{}, ErrNotRunning
		}
		payload = map[string]any{"scenarioTemplateId": session.ScenarioTemplateID}
	} else if session.Status != StatusPreparing &&
		session.Status != StatusRunning &&
		session.Status != StatusExpiring &&
		session.Status != StatusFailed {
		return ActionResult{}, ErrNotRunning
	}
	terminationReason := "user_requested"
	if action == destroyLabAction {
		if value, ok := payload.(map[string]any); ok {
			if candidate, ok := value["reason"].(string); ok && validTerminationReason(candidate) {
				terminationReason = candidate
			}
		}
	}
	var pendingID uint64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM lab_operations
		WHERE lab_id = ?
		  AND status IN ('pending', 'claimed', 'running', 'compensating')
		LIMIT 1`, labID).Scan(&pendingID); err == nil {
		return ActionResult{}, ErrBusy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("check lab action queue: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return ActionResult{}, fmt.Errorf("encode lab action payload: %w", err)
	}
	if action == destroyLabAction {
		result, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, termination_reason = ?, updated_at = ?
			WHERE id = ? AND status IN (?, ?, ?, ?)`,
			StatusTerminating,
			terminationReason,
			now,
			labID,
			StatusPreparing,
			StatusRunning,
			StatusExpiring,
			StatusFailed,
		)
		if err != nil {
			return ActionResult{}, fmt.Errorf("mark lab terminating: %w", err)
		}
		if err := requireSingleRow(result); err != nil {
			return ActionResult{}, ErrStateConflict
		}
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO lab_operations (
			operation_id, lab_id, requested_by, action, status,
			payload_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?)`,
		operationID,
		labID,
		userID,
		action,
		payloadJSON,
		now,
		now,
	)
	if err != nil {
		if duplicateKey(err) {
			return ActionResult{}, ErrOperationConflict
		}
		return ActionResult{}, fmt.Errorf("insert lab action: %w", err)
	}
	operationDatabaseID, err := result.LastInsertId()
	if err != nil {
		return ActionResult{}, fmt.Errorf("read lab action id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ActionResult{}, fmt.Errorf("commit lab action: %w", err)
	}
	return ActionResult{Operation: CreatedOperation{
		ID:          uint64(operationDatabaseID),
		OperationID: operationID,
		LabID:       labID,
		RequestedBy: userID,
		Action:      action,
		Status:      "pending",
		SubmittedAt: now,
	}}, nil
}

// FindSnapshot returns the complete persisted state for an owned lab.
func (r *Repository) FindSnapshot(
	ctx context.Context,
	labID string,
	userID uint64,
) (Snapshot, error) {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("begin lab snapshot transaction: %w", err)
	}
	defer tx.Rollback()
	session, err := scanSession(tx.QueryRowContext(ctx, `
		SELECT
			id, user_id, course_id, scenario_type, scenario_template_id,
			status, balancing_mode, redis_enabled, started_at,
			last_effective_action_at, terminated_at, termination_reason,
			created_at, updated_at
		FROM lab_sessions
		WHERE id = ?`, labID))
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("load lab snapshot session: %w", err)
	}
	if session.UserID != userID {
		return Snapshot{}, ErrNotOwned
	}
	snapshot := Snapshot{
		Lab:       session,
		Resources: []Resource{},
		Topology:  Topology{Instances: []Instance{}},
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT instance_name, container_id, status, cpu_limit_cores,
			memory_limit_mb, performance_percent, effective_capacity, current_weight
		FROM lab_instances
		WHERE lab_id = ?
		ORDER BY instance_name`, labID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load lab instances: %w", err)
	}
	for rows.Next() {
		var instance Instance
		var containerID sql.NullString
		if err := rows.Scan(
			&instance.Name,
			&containerID,
			&instance.Status,
			&instance.CPULimitCores,
			&instance.MemoryLimitMB,
			&instance.PerformancePercent,
			&instance.EffectiveCapacity,
			&instance.CurrentWeight,
		); err != nil {
			rows.Close()
			return Snapshot{}, fmt.Errorf("scan lab instance: %w", err)
		}
		instance.ID = instance.Name
		if containerID.Valid {
			instance.ContainerID = containerID.String
		}
		snapshot.Topology.Instances = append(snapshot.Topology.Instances, instance)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Snapshot{}, fmt.Errorf("iterate lab instances: %w", err)
	}
	rows.Close()

	resourceRows, err := tx.QueryContext(ctx, `
		SELECT resource_type, resource_name, external_id, status, metadata_json
		FROM lab_resources
		WHERE lab_id = ?
		ORDER BY resource_type, resource_name`, labID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load lab resources: %w", err)
	}
	for resourceRows.Next() {
		var resource Resource
		var externalID sql.NullString
		var metadata []byte
		if err := resourceRows.Scan(
			&resource.Type,
			&resource.Name,
			&externalID,
			&resource.Status,
			&metadata,
		); err != nil {
			resourceRows.Close()
			return Snapshot{}, fmt.Errorf("scan lab resource: %w", err)
		}
		if externalID.Valid {
			resource.ExternalID = externalID.String
		}
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &resource.Metadata); err != nil {
				resourceRows.Close()
				return Snapshot{}, fmt.Errorf("decode lab resource metadata: %w", err)
			}
		}
		snapshot.Resources = append(snapshot.Resources, resource)
		if resource.Type == "session-redis" {
			snapshot.Topology.Redis = &ResourceStatus{Status: resource.Status}
		}
		if resource.Type == "nginx-fragment" {
			snapshot.Topology.Gateway.Status = resource.Status
		}
	}
	if err := resourceRows.Err(); err != nil {
		resourceRows.Close()
		return Snapshot{}, fmt.Errorf("iterate lab resources: %w", err)
	}
	resourceRows.Close()
	if snapshot.Topology.Gateway.Status == "" {
		snapshot.Topology.Gateway.Status = "unknown"
	}

	var operation OperationSnapshot
	var targetInstanceID sql.NullString
	var completedAt sql.NullTime
	var errorCode sql.NullString
	var errorMessage sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT operation_id, action, target_instance_id, status, created_at,
			completed_at, error_code, error_message
		FROM lab_operations
		WHERE lab_id = ? AND action <> 'APPLY_ADAPTIVE_WEIGHTS'
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, labID).Scan(
		&operation.OperationID,
		&operation.Action,
		&targetInstanceID,
		&operation.Status,
		&operation.SubmittedAt,
		&completedAt,
		&errorCode,
		&errorMessage,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return Snapshot{}, fmt.Errorf("commit lab snapshot: %w", err)
		}
		return snapshot, nil
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("load latest lab operation: %w", err)
	}
	if targetInstanceID.Valid {
		value := targetInstanceID.String
		operation.TargetInstanceID = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		operation.CompletedAt = &value
	}
	if errorCode.Valid || errorMessage.Valid {
		snapshotError := OperationError{Code: errorCode.String, Message: errorMessage.String}
		operation.Error = &snapshotError
	}
	snapshot.LatestOperation = &operation
	if err := tx.Commit(); err != nil {
		return Snapshot{}, fmt.Errorf("commit lab snapshot: %w", err)
	}
	return snapshot, nil
}

func findActionByOperationID(
	ctx context.Context,
	tx *sql.Tx,
	operationID string,
) (CreatedOperation, error) {
	var operation CreatedOperation
	var targetInstanceID sql.NullString
	var completedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT id, operation_id, lab_id, requested_by, action,
			target_instance_id, status, created_at, completed_at
		FROM lab_operations
		WHERE operation_id = ?`, operationID).Scan(
		&operation.ID,
		&operation.OperationID,
		&operation.LabID,
		&operation.RequestedBy,
		&operation.Action,
		&targetInstanceID,
		&operation.Status,
		&operation.SubmittedAt,
		&completedAt,
	)
	if err != nil {
		return CreatedOperation{}, err
	}
	if targetInstanceID.Valid {
		value := targetInstanceID.String
		operation.TargetInstanceID = &value
	}
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		operation.CompletedAt = &value
	}
	return operation, nil
}

func matchingActionResult(
	operation CreatedOperation,
	userID uint64,
	labID string,
	action string,
) (ActionResult, error) {
	if operation.LabID != labID || operation.RequestedBy != userID || operation.Action != action {
		return ActionResult{}, ErrOperationConflict
	}
	return ActionResult{Operation: operation, Existing: true}, nil
}

func findExistingCreate(
	ctx context.Context,
	tx *sql.Tx,
	operationID string,
) (CreateResult, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			s.id, s.user_id, s.course_id, s.scenario_type,
			s.scenario_template_id, s.status, s.balancing_mode,
			s.redis_enabled, s.started_at, s.last_effective_action_at,
			s.terminated_at, s.termination_reason, s.created_at, s.updated_at,
			o.id, o.operation_id, o.requested_by, o.action, o.status,
			o.created_at, o.completed_at
		FROM lab_operations AS o
		JOIN lab_sessions AS s ON s.id = o.lab_id
		WHERE o.operation_id = ?`, operationID)
	var result CreateResult
	var startedAt sql.NullTime
	var lastEffectiveActionAt sql.NullTime
	var terminatedAt sql.NullTime
	var terminationReason sql.NullString
	var completedAt sql.NullTime
	if err := row.Scan(
		&result.Session.ID,
		&result.Session.UserID,
		&result.Session.CourseID,
		&result.Session.ScenarioType,
		&result.Session.ScenarioTemplateID,
		&result.Session.Status,
		&result.Session.BalancingMode,
		&result.Session.RedisEnabled,
		&startedAt,
		&lastEffectiveActionAt,
		&terminatedAt,
		&terminationReason,
		&result.Session.CreatedAt,
		&result.Session.UpdatedAt,
		&result.Operation.ID,
		&result.Operation.OperationID,
		&result.Operation.RequestedBy,
		&result.Operation.Action,
		&result.Operation.Status,
		&result.Operation.SubmittedAt,
		&completedAt,
	); err != nil {
		return CreateResult{}, err
	}
	applyNullableSessionFields(
		&result.Session,
		startedAt,
		lastEffectiveActionAt,
		terminatedAt,
		terminationReason,
	)
	result.Operation.LabID = result.Session.ID
	if completedAt.Valid {
		value := completedAt.Time.UTC()
		result.Operation.CompletedAt = &value
	}
	return result, nil
}

func lockUser(ctx context.Context, tx *sql.Tx, userID uint64) error {
	var id uint64
	if err := tx.QueryRowContext(
		ctx,
		"SELECT id FROM users WHERE id = ? FOR UPDATE",
		userID,
	).Scan(&id); err != nil {
		return fmt.Errorf("lock lab owner: %w", err)
	}
	return nil
}

func lockGlobalAdmission(ctx context.Context, tx *sql.Tx) error {
	var acquired sql.NullInt64
	if err := tx.QueryRowContext(
		ctx,
		"SELECT GET_LOCK(?, ?)",
		globalAdmissionLockName,
		globalAdmissionLockTimeoutSeconds,
	).Scan(&acquired); err != nil {
		return fmt.Errorf("lock global lab admission: %w", err)
	}
	if !acquired.Valid {
		return errors.New("lock global lab admission returned null")
	}
	if acquired.Int64 != 1 {
		return errors.New("lock global lab admission timed out")
	}
	return nil
}

func releaseGlobalAdmission(ctx context.Context, connection *sql.Conn) error {
	var released sql.NullInt64
	if err := connection.QueryRowContext(
		ctx,
		"SELECT RELEASE_LOCK(?)",
		globalAdmissionLockName,
	).Scan(&released); err != nil {
		return fmt.Errorf("release global lab admission: %w", err)
	}
	if !released.Valid {
		return errors.New("release global lab admission lock was not owned")
	}
	if released.Int64 != 1 {
		return errors.New("release global lab admission lock failed")
	}
	return nil
}

func findActiveLabID(
	ctx context.Context,
	tx *sql.Tx,
	userID uint64,
) (string, error) {
	return scanString(tx.QueryRowContext(ctx, `
		SELECT id
		FROM lab_sessions
		WHERE user_id = ?
		  AND status IN (?, ?, ?, ?)
		LIMIT 1`,
		userID,
		activeStatuses[0],
		activeStatuses[1],
		activeStatuses[2],
		activeStatuses[3],
	))
}

func loadUsage(ctx context.Context, tx *sql.Tx) (Usage, error) {
	var usage Usage
	if err := tx.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(
				CASE
					WHEN scenario_type IN ('multi_level_cache', 'cache_failures')
						THEN 2
					ELSE 1
				END
			), 0)
		FROM lab_sessions
		WHERE status IN (?, ?, ?, ?)`,
		activeStatuses[0],
		activeStatuses[1],
		activeStatuses[2],
		activeStatuses[3],
	).Scan(&usage.ActiveLabs, &usage.TemporaryContainers); err != nil {
		return Usage{}, fmt.Errorf("load lab capacity usage: %w", err)
	}
	return usage, nil
}

func scanString(row *sql.Row) (string, error) {
	var value string
	if err := row.Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

func scanSession(row *sql.Row) (Session, error) {
	var session Session
	var startedAt sql.NullTime
	var lastEffectiveActionAt sql.NullTime
	var terminatedAt sql.NullTime
	var terminationReason sql.NullString
	if err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.CourseID,
		&session.ScenarioType,
		&session.ScenarioTemplateID,
		&session.Status,
		&session.BalancingMode,
		&session.RedisEnabled,
		&startedAt,
		&lastEffectiveActionAt,
		&terminatedAt,
		&terminationReason,
		&session.CreatedAt,
		&session.UpdatedAt,
	); err != nil {
		return Session{}, err
	}
	applyNullableSessionFields(
		&session,
		startedAt,
		lastEffectiveActionAt,
		terminatedAt,
		terminationReason,
	)
	return session, nil
}

func applyNullableSessionFields(
	session *Session,
	startedAt sql.NullTime,
	lastEffectiveActionAt sql.NullTime,
	terminatedAt sql.NullTime,
	terminationReason sql.NullString,
) {
	if startedAt.Valid {
		value := startedAt.Time.UTC()
		session.StartedAt = &value
	}
	if lastEffectiveActionAt.Valid {
		value := lastEffectiveActionAt.Time.UTC()
		session.LastEffectiveActionAt = &value
	}
	if terminatedAt.Valid {
		value := terminatedAt.Time.UTC()
		session.TerminatedAt = &value
	}
	if terminationReason.Valid {
		value := terminationReason.String
		session.TerminationReason = &value
	}
}

func validTerminationReason(value string) bool {
	switch value {
	case "user_requested", "idle_timeout", "maximum_duration":
		return true
	default:
		return false
	}
}

func duplicateKey(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}

func requireSingleRow(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected lab rows: %w", err)
	}
	if rowsAffected != 1 {
		return ErrStateConflict
	}
	return nil
}
