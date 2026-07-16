package lifecycle

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestRepositoryAdvanceIntegration(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DSN")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DSN is not set")
	}
	database, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	stamp := time.Now().UTC().UnixNano()
	username := fmt.Sprintf("lifecycle-test-%d", stamp)
	result, err := database.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, status)
		VALUES (?, 'integration-test-only', 'active')`, username)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read test user id: %v", err)
	}
	labID := fmt.Sprintf("lab-lifecycle-%d", stamp)
	defer func() {
		_, _ = database.ExecContext(context.Background(), "DELETE FROM lab_sessions WHERE id = ?", labID)
		_, _ = database.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", userID)
	}()

	var courseID uint64
	if err := database.QueryRowContext(
		ctx, "SELECT id FROM courses WHERE slug = 'application-cluster'",
	).Scan(&courseID); err != nil {
		t.Fatalf("load test course: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	startedAt := now.Add(-20 * time.Minute)
	lastActionAt := now.Add(-10 * time.Minute)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO lab_sessions (
			id, user_id, course_id, scenario_type, scenario_template_id,
			status, balancing_mode, redis_enabled, started_at,
			last_effective_action_at, created_at, updated_at
		) VALUES (?, ?, ?, 'application_cluster',
			'application_cluster_scenario_v1', 'Running', 'fixed', FALSE, ?, ?, ?, ?)`,
		labID, userID, courseID, startedAt, lastActionAt, startedAt, now,
	); err != nil {
		t.Fatalf("insert test lab: %v", err)
	}

	repository := NewRepository(database)
	transitions, err := repository.Advance(ctx, now, Config{
		IdleTimeout:  10 * time.Minute,
		MaxDuration:  30 * time.Minute,
		ExpiringLead: time.Minute,
	})
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	if len(transitions) != 1 || transitions[0].Kind != "terminating" ||
		transitions[0].Reason != "idle_timeout" {
		t.Fatalf("transitions = %#v", transitions)
	}
	var status string
	var reason string
	if err := database.QueryRowContext(ctx, `
		SELECT status, termination_reason FROM lab_sessions WHERE id = ?`, labID,
	).Scan(&status, &reason); err != nil {
		t.Fatalf("load transitioned lab: %v", err)
	}
	if status != "Terminating" || reason != "idle_timeout" {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
	var operationCount int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM lab_operations
		WHERE lab_id = ? AND action = 'DESTROY_LAB' AND status = 'pending'`, labID,
	).Scan(&operationCount); err != nil {
		t.Fatalf("count lifecycle operations: %v", err)
	}
	if operationCount != 1 {
		t.Fatalf("operationCount = %d; want 1", operationCount)
	}
}
