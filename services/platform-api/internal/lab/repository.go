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

func duplicateKey(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}
