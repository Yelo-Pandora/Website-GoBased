package lifecycle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const (
	statusRunning     = "Running"
	statusExpiring    = "Expiring"
	statusTerminating = "Terminating"
	destroyAction     = "DESTROY_LAB"
)

// Config controls lifecycle deadline evaluation.
type Config struct {
	IdleTimeout  time.Duration
	MaxDuration  time.Duration
	ExpiringLead time.Duration
}

// Transition describes one persisted lifecycle state change.
type Transition struct {
	LabID       string
	Kind        string
	Reason      string
	OperationID string
}

// Repository persists lifecycle state transitions and system cleanup actions.
type Repository struct {
	database *sql.DB
}

// NewRepository returns a MySQL-backed lifecycle repository.
func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

// Advance evaluates active labs and applies due lifecycle transitions.
func (r *Repository) Advance(
	ctx context.Context,
	now time.Time,
	config Config,
) ([]Transition, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	rows, err := r.database.QueryContext(ctx, `
		SELECT id
		FROM lab_sessions
		WHERE status IN (?, ?)
		ORDER BY updated_at, id`, statusRunning, statusExpiring)
	if err != nil {
		return nil, fmt.Errorf("list lifecycle candidates: %w", err)
	}
	var labIDs []string
	for rows.Next() {
		var labID string
		if err := rows.Scan(&labID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan lifecycle candidate: %w", err)
		}
		labIDs = append(labIDs, labID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate lifecycle candidates: %w", err)
	}
	rows.Close()

	transitions := make([]Transition, 0, len(labIDs))
	for _, labID := range labIDs {
		transition, changed, err := r.advanceOne(ctx, labID, now, config)
		if err != nil {
			return transitions, err
		}
		if changed {
			transitions = append(transitions, transition)
		}
	}
	return transitions, nil
}

func (r *Repository) advanceOne(
	ctx context.Context,
	labID string,
	now time.Time,
	config Config,
) (Transition, bool, error) {
	tx, err := r.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Transition{}, false, fmt.Errorf("begin lifecycle transaction: %w", err)
	}
	defer tx.Rollback()

	var userID uint64
	var status string
	var startedAt sql.NullTime
	var lastActionAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT user_id, status, started_at, last_effective_action_at
		FROM lab_sessions
		WHERE id = ?
		FOR UPDATE`, labID).Scan(&userID, &status, &startedAt, &lastActionAt)
	if err == sql.ErrNoRows {
		return Transition{}, false, nil
	}
	if err != nil {
		return Transition{}, false, fmt.Errorf("load lifecycle lab %s: %w", labID, err)
	}
	if !startedAt.Valid || !lastActionAt.Valid {
		return Transition{}, false, nil
	}
	started := startedAt.Time.UTC()
	lastAction := lastActionAt.Time.UTC()
	decision := evaluate(now.UTC(), status, &started, &lastAction, config)
	switch decision.kind {
	case decisionExpiring:
		result, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, updated_at = ?
			WHERE id = ? AND status = ?`, statusExpiring, now, labID, statusRunning)
		if err != nil {
			return Transition{}, false, fmt.Errorf("mark lab expiring: %w", err)
		}
		if err := requireOneRow(result); err != nil {
			return Transition{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Transition{}, false, fmt.Errorf("commit expiring lab: %w", err)
		}
		return Transition{LabID: labID, Kind: string(decision.kind), Reason: decision.reason}, true, nil
	case decisionRunning:
		result, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, updated_at = ?
			WHERE id = ? AND status = ?`, statusRunning, now, labID, statusExpiring)
		if err != nil {
			return Transition{}, false, fmt.Errorf("restore lab running: %w", err)
		}
		if err := requireOneRow(result); err != nil {
			return Transition{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Transition{}, false, fmt.Errorf("commit running lab: %w", err)
		}
		return Transition{LabID: labID, Kind: string(decision.kind), Reason: decision.reason}, true, nil
	case decisionTerminating:
		var activeAction string
		err := tx.QueryRowContext(ctx, `
			SELECT action
			FROM lab_operations
			WHERE lab_id = ?
			  AND status IN ('pending', 'claimed', 'running', 'compensating')
			ORDER BY id DESC
			LIMIT 1`, labID).Scan(&activeAction)
		if err == nil {
			return Transition{}, false, nil
		}
		if err != sql.ErrNoRows {
			return Transition{}, false, fmt.Errorf("check active lifecycle operation: %w", err)
		}
		opID := expirationOperationID(labID)
		payload, err := json.Marshal(map[string]string{"reason": decision.reason})
		if err != nil {
			return Transition{}, false, fmt.Errorf("encode lifecycle operation: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO lab_operations (
				operation_id, lab_id, requested_by, action, status,
				payload_json, created_at, updated_at
			) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?)`,
			opID, labID, userID, destroyAction, payload, now, now,
		); err != nil {
			return Transition{}, false, fmt.Errorf("enqueue lifecycle destroy: %w", err)
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE lab_sessions
			SET status = ?, termination_reason = ?, updated_at = ?
			WHERE id = ? AND status IN (?, ?)`,
			statusTerminating,
			decision.reason,
			now,
			labID,
			statusRunning,
			statusExpiring,
		)
		if err != nil {
			return Transition{}, false, fmt.Errorf("mark lab terminating: %w", err)
		}
		if err := requireOneRow(result); err != nil {
			return Transition{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Transition{}, false, fmt.Errorf("commit terminating lab: %w", err)
		}
		return Transition{LabID: labID, Kind: string(decision.kind), Reason: decision.reason, OperationID: opID}, true, nil
	default:
		return Transition{}, false, nil
	}
}

// ExpectedLabIDs returns sessions whose managed resources are still expected.
func (r *Repository) ExpectedLabIDs(ctx context.Context) ([]string, error) {
	rows, err := r.database.QueryContext(ctx, `
		SELECT id
		FROM lab_sessions
		WHERE status IN ('Preparing', 'Running', 'Expiring', 'Terminating')
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list expected lab ids: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var labID string
		if err := rows.Scan(&labID); err != nil {
			return nil, fmt.Errorf("scan expected lab id: %w", err)
		}
		result = append(result, labID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expected lab ids: %w", err)
	}
	return result, nil
}

func validateConfig(config Config) error {
	if config.IdleTimeout <= 0 || config.MaxDuration <= 0 || config.ExpiringLead <= 0 {
		return fmt.Errorf("lifecycle durations must be positive")
	}
	if config.ExpiringLead >= config.IdleTimeout || config.ExpiringLead >= config.MaxDuration {
		return fmt.Errorf("lifecycle expiring lead must be shorter than both deadlines")
	}
	return nil
}

func expirationOperationID(labID string) string {
	digest := sha256.Sum256([]byte(labID))
	return "system-expire-" + hex.EncodeToString(digest[:16])
}

func requireOneRow(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read lifecycle affected rows: %w", err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("lifecycle state changed concurrently")
	}
	return nil
}
