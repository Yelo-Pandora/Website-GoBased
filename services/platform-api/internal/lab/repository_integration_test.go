package lab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestRepositoryCreateIntegration(t *testing.T) {
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
	username := fmt.Sprintf("lab-test-%d", stamp)
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
	defer func() {
		if _, err := database.ExecContext(
			context.Background(),
			"DELETE FROM lab_sessions WHERE user_id = ?",
			userID,
		); err != nil {
			t.Errorf("delete test lab sessions: %v", err)
		}
		if _, err := database.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id = ?",
			userID,
		); err != nil {
			t.Errorf("delete test user: %v", err)
		}
	}()

	var courseID uint64
	if err := database.QueryRowContext(
		ctx,
		"SELECT id FROM courses WHERE slug = 'application-data-separation'",
	).Scan(&courseID); err != nil {
		t.Fatalf("load test course: %v", err)
	}
	service := NewService(NewRepository(database), Quota{
		MaxActiveLabs:          10,
		MaxTemporaryContainers: 40,
		MaxInstancesPerLab:     4,
	})
	var labSequence atomic.Int64
	service.newID = func() (string, error) {
		return fmt.Sprintf("lab-test-%d-%d", stamp, labSequence.Add(1)), nil
	}
	operationID := fmt.Sprintf("operation-%d", stamp)
	start := make(chan struct{})
	results := make([]CreateResult, 2)
	errorsByCall := make([]error, 2)
	var waitGroup sync.WaitGroup
	for i := range results {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			results[index], errorsByCall[index] = service.Create(
				ctx,
				userID,
				courseID,
				operationID,
			)
		}(i)
	}
	close(start)
	waitGroup.Wait()
	for i, err := range errorsByCall {
		if err != nil {
			t.Fatalf("concurrent Create()[%d] error = %v", i, err)
		}
	}
	created := results[0]
	if results[1].Session.ID != created.Session.ID {
		t.Fatalf("concurrent Create() sessions = %q, %q", created.Session.ID, results[1].Session.ID)
	}
	if results[0].Existing == results[1].Existing {
		t.Fatalf("concurrent Create() existing flags = %t, %t", results[0].Existing, results[1].Existing)
	}
	if created.Existing || created.Session.Status != StatusPreparing ||
		created.Operation.Status != "pending" {
		created = results[1]
	}
	if created.Existing || created.Session.Status != StatusPreparing ||
		created.Operation.Status != "pending" {
		t.Fatalf("new Create() result = %#v", created)
	}
	repeated, err := service.Create(ctx, userID, courseID, operationID)
	if err != nil {
		t.Fatalf("repeated Create() error = %v", err)
	}
	if !repeated.Existing || repeated.Session.ID != created.Session.ID {
		t.Fatalf("repeated Create() result = %#v", repeated)
	}
	_, err = service.Create(
		ctx,
		userID,
		courseID,
		fmt.Sprintf("operation-second-%d", stamp),
	)
	if !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("second Create() error = %v; want ErrAlreadyActive", err)
	}
	if _, err := service.GetOwned(ctx, created.Session.ID, userID); err != nil {
		t.Fatalf("GetOwned() error = %v", err)
	}
	if _, err := service.GetOwned(ctx, created.Session.ID, userID+1); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("foreign GetOwned() error = %v; want ErrNotOwned", err)
	}
}
