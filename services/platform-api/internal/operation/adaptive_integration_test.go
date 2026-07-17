package operation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"website-gobased/services/platform-api/internal/lab"
)

func TestCompleteAdaptiveWeightsDoesNotRefreshActivityIntegration(t *testing.T) {
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
	username := fmt.Sprintf("adaptive-operation-test-%d", stamp)
	result, err := database.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, status)
		VALUES (?, 'integration-test-only', 'active')`, username)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	userDatabaseID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read test user id: %v", err)
	}
	userID := uint64(userDatabaseID)
	labID := fmt.Sprintf("lab-adaptive-operation-%d", stamp)
	defer func() {
		if _, err := database.ExecContext(
			context.Background(), "DELETE FROM lab_sessions WHERE id = ?", labID,
		); err != nil {
			t.Errorf("delete test lab: %v", err)
		}
		if _, err := database.ExecContext(
			context.Background(), "DELETE FROM users WHERE id = ?", userID,
		); err != nil {
			t.Errorf("delete test user: %v", err)
		}
	}()

	var courseID uint64
	if err := database.QueryRowContext(
		ctx,
		"SELECT id FROM courses WHERE slug = 'application-cluster'",
	).Scan(&courseID); err != nil {
		t.Fatalf("load application cluster course: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO lab_sessions (
			id, user_id, course_id, scenario_type, scenario_template_id,
			status, balancing_mode, redis_enabled, started_at,
			last_effective_action_at, created_at, updated_at
		) VALUES (?, ?, ?, 'application_cluster',
			'application_cluster_scenario_v1', 'Running', 'adaptive',
			FALSE, ?, ?, ?, ?)`,
		labID, userID, courseID, now, now, now, now,
	); err != nil {
		t.Fatalf("insert test lab: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO lab_instances (
			lab_id, instance_name, container_id, status, cpu_limit_cores,
			memory_limit_mb, performance_percent, effective_capacity,
			current_weight, created_at, updated_at
		) VALUES (?, 'app-1', ?, 'running', 0.1, 128, 100, 100, 100, ?, ?)`,
		labID, fmt.Sprintf("container-%d", stamp), now, now,
	); err != nil {
		t.Fatalf("insert test instance: %v", err)
	}
	operationID := fmt.Sprintf("adaptive-operation-%d", stamp)
	result, err = database.ExecContext(ctx, `
		INSERT INTO lab_operations (
			operation_id, lab_id, requested_by, action, status,
			lease_owner, lease_expires_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'running', 'worker-test', ?, ?, ?)`,
		operationID,
		labID,
		userID,
		ActionApplyAdaptiveWeights,
		now.Add(time.Minute),
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert test operation: %v", err)
	}
	operationDatabaseID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read test operation id: %v", err)
	}

	repository := NewRepository(database)
	if err := repository.CompleteTopology(
		ctx,
		Record{
			ID: uint64(operationDatabaseID), OperationID: operationID,
			LabID: labID, RequestedBy: userID, Action: ActionApplyAdaptiveWeights,
			LeaseOwner: "worker-test",
		},
		TopologyResult{Weights: []lab.InstanceWeight{{
			InstanceID: "app-1", Weight: 1,
		}}},
		"",
		"",
		now.Add(2*time.Second),
	); err != nil {
		t.Fatalf("CompleteTopology() error = %v", err)
	}
	var currentWeight int
	var lastEffectiveActionAt time.Time
	if err := database.QueryRowContext(ctx, `
		SELECT i.current_weight, s.last_effective_action_at
		FROM lab_sessions AS s
		JOIN lab_instances AS i ON i.lab_id = s.id
		WHERE s.id = ? AND i.instance_name = 'app-1'`, labID).Scan(
		&currentWeight,
		&lastEffectiveActionAt,
	); err != nil {
		t.Fatalf("load completed adaptive state: %v", err)
	}
	if currentWeight != 1 {
		t.Fatalf("current weight = %d; want 1", currentWeight)
	}
	if !lastEffectiveActionAt.Equal(now) {
		t.Fatalf("last effective action = %v; want %v", lastEffectiveActionAt, now)
	}
	labs := lab.NewService(lab.NewRepository(database), lab.Quota{
		MaxActiveLabs: 10, MaxTemporaryContainers: 40, MaxInstancesPerLab: 4,
	})
	snapshot, err := labs.Snapshot(ctx, labID, userID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.LatestOperation != nil {
		t.Fatalf("latest user operation = %#v; want nil", snapshot.LatestOperation)
	}
}
