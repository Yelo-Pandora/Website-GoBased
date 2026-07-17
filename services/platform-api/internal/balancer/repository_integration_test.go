package balancer

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestRepositoryEnqueueAdjustmentIntegration(t *testing.T) {
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
	username := fmt.Sprintf("balancer-test-%d", stamp)
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
	labID := fmt.Sprintf("lab-balancer-%d", stamp)
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
	instances := []Instance{
		{ID: "app-1", Status: "running", EffectiveCapacity: 100, CurrentWeight: 100},
		{ID: "app-2", Status: "running", EffectiveCapacity: 30, CurrentWeight: 100},
	}
	for index, instance := range instances {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO lab_instances (
				lab_id, instance_name, container_id, status, cpu_limit_cores,
				memory_limit_mb, performance_percent, effective_capacity,
				current_weight, created_at, updated_at
			) VALUES (?, ?, ?, 'running', 0.1, 128, ?, ?, ?, ?, ?)`,
			labID,
			instance.ID,
			fmt.Sprintf("container-%d-%d", stamp, index),
			instance.EffectiveCapacity,
			instance.EffectiveCapacity,
			instance.CurrentWeight,
			now,
			now,
		); err != nil {
			t.Fatalf("insert test instance: %v", err)
		}
	}

	repository := NewRepository(database)
	labs, err := repository.ListAdaptiveLabs(ctx)
	if err != nil {
		t.Fatalf("ListAdaptiveLabs() error = %v", err)
	}
	var found bool
	for _, value := range labs {
		if value.ID == labID && len(value.Instances) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("adaptive lab %q not found in %#v", labID, labs)
	}
	operationID := fmt.Sprintf("balancer-operation-%d", stamp)
	operation, err := repository.EnqueueAdjustment(ctx, Adjustment{
		OperationID:         operationID,
		LabID:               labID,
		RequestedBy:         userID,
		TopologyFingerprint: TopologyFingerprint(instances),
		Weights: []Weight{
			{InstanceID: "app-1", Weight: 10},
			{InstanceID: "app-2", Weight: 3},
		},
	}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("EnqueueAdjustment() error = %v", err)
	}
	if operation.OperationID != operationID || operation.Status != "pending" {
		t.Fatalf("operation = %#v", operation)
	}
	stored, err := repository.FindAdjustment(ctx, operationID)
	if err != nil {
		t.Fatalf("FindAdjustment() error = %v", err)
	}
	if stored.Status != "pending" {
		t.Fatalf("stored operation = %#v", stored)
	}
}
