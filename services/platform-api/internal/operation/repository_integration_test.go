package operation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestCompleteProvisionIntegration(t *testing.T) {
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
	username := fmt.Sprintf("operation-test-%d", stamp)
	result, err := database.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, status)
		VALUES (?, 'integration-test-only', 'active')`, username)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	userDatabaseID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}
	userID := uint64(userDatabaseID)
	labID := fmt.Sprintf("lab-operation-%d", stamp)
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
		t.Fatalf("load test course: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO lab_sessions (
			id, user_id, course_id, scenario_type, scenario_template_id,
			status, balancing_mode, redis_enabled, created_at, updated_at
		) VALUES (?, ?, ?, 'application_cluster',
			'application_cluster_scenario_v1', 'Preparing', 'fixed', FALSE, ?, ?)`,
		labID, userID, courseID, now, now,
	); err != nil {
		t.Fatalf("insert test lab: %v", err)
	}
	operationID := fmt.Sprintf("operation-%d", stamp)
	result, err = database.ExecContext(ctx, `
		INSERT INTO lab_operations (
			operation_id, lab_id, requested_by, action, status,
			lease_owner, lease_expires_at, created_at, updated_at
		) VALUES (?, ?, ?, 'CREATE_LAB', 'running', 'worker-test', ?, ?, ?)`,
		operationID, labID, userID, now.Add(time.Minute), now, now,
	)
	if err != nil {
		t.Fatalf("insert test operation: %v", err)
	}
	operationDatabaseID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read test operation id: %v", err)
	}

	repository := NewRepository(database)
	err = repository.CompleteProvision(
		ctx,
		Record{
			ID: uint64(operationDatabaseID), OperationID: operationID,
			LabID: labID, RequestedBy: userID, Action: ActionCreateLab,
			LeaseOwner: "worker-test",
		},
		ProvisionResult{
			LabID: labID, ScenarioTemplateID: "application_cluster_scenario_v1",
			DatabaseName: "lab_operation", DatabaseUser: "lab_operation_user",
			NetworkID: "network-1", NetworkName: "lab-operation-net",
			Instances: []ProvisionInstance{{
				InstanceName: "app-1", ContainerID: "container-1",
				ContainerName: "lab-operation-app-1", Status: "running",
				CPULimitCores: 0.1, MemoryLimitMB: 128,
				PerformancePercent: 100, ProcessingSpeed: 20, MaxLoad: 100,
				CurrentWeight: 100,
			}},
		},
		"",
		"",
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("CompleteProvision() error = %v", err)
	}

	var sessionStatus string
	var databaseName string
	if err := database.QueryRowContext(ctx, `
		SELECT status, mysql_db_name
		FROM lab_sessions
		WHERE id = ?`, labID).Scan(&sessionStatus, &databaseName); err != nil {
		t.Fatalf("load completed lab: %v", err)
	}
	if sessionStatus != "Running" || databaseName != "lab_operation" {
		t.Fatalf("completed lab status=%q database=%q", sessionStatus, databaseName)
	}
	var instanceCount int
	if err := database.QueryRowContext(
		ctx, "SELECT COUNT(*) FROM lab_instances WHERE lab_id = ?", labID,
	).Scan(&instanceCount); err != nil {
		t.Fatalf("count lab instances: %v", err)
	}
	var resourceCount int
	if err := database.QueryRowContext(
		ctx, "SELECT COUNT(*) FROM lab_resources WHERE lab_id = ?", labID,
	).Scan(&resourceCount); err != nil {
		t.Fatalf("count lab resources: %v", err)
	}
	if instanceCount != 1 || resourceCount != 4 {
		t.Fatalf("persisted instances=%d resources=%d", instanceCount, resourceCount)
	}
}
