package operation

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"website-gobased/internal/protocol"
)

type queueStub struct {
	record          Record
	claimErr        error
	markedRunning   bool
	completedCode   string
	completedResult any
}

func (s *queueStub) Claim(
	_ context.Context,
	owner string,
	_ time.Time,
	leaseExpires time.Time,
) (Record, error) {
	if s.claimErr != nil {
		return Record{}, s.claimErr
	}
	s.record.LeaseOwner = owner
	s.record.LeaseExpires = leaseExpires
	return s.record, nil
}

func (s *queueStub) MarkRunning(
	_ context.Context,
	_ Record,
	_ time.Time,
	_ time.Time,
) error {
	s.markedRunning = true
	return nil
}

func (s *queueStub) CompleteProvision(
	_ context.Context,
	_ Record,
	resultValue any,
	errorCode string,
	_ string,
	_ time.Time,
) error {
	s.completedCode = errorCode
	s.completedResult = resultValue
	return nil
}

type executorStub struct {
	command  protocol.Command
	response protocol.CommandResponse
	err      error
}

func (s *executorStub) Execute(
	_ context.Context,
	command protocol.Command,
) (protocol.CommandResponse, error) {
	s.command = command
	return s.response, s.err
}

func TestWorkerProcessesCreateLab(t *testing.T) {
	t.Parallel()

	queue := &queueStub{record: Record{
		ID:          3,
		OperationID: "operation-1",
		LabID:       "lab-test",
		RequestedBy: 7,
		Action:      ActionCreateLab,
		Payload:     json.RawMessage(`{"courseId":3}`),
	}}
	executor := &executorStub{response: protocol.CommandResponse{
		CommandID:   "command-1",
		OperationID: "operation-1",
		Status:      "succeeded",
		Result:      map[string]any{"created": true},
	}}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue,
		executor,
		WorkerConfig{
			Owner:          "worker-1",
			PollInterval:   time.Second,
			LeaseDuration:  time.Minute,
			CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	worker.newCommandID = func() (string, error) { return "command-1", nil }
	processed, err := worker.processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne() error = %v", err)
	}
	if !processed || !queue.markedRunning || queue.completedCode != "" {
		t.Fatalf("worker state: processed=%t queue=%#v", processed, queue)
	}
	if executor.command.CommandType != "PROVISION_LAB" ||
		executor.command.RequestedBy != "7" {
		t.Fatalf("command = %#v", executor.command)
	}
}

func TestWorkerPersistsRejectedCommand(t *testing.T) {
	t.Parallel()

	queue := &queueStub{record: Record{
		OperationID: "operation-1",
		LabID:       "lab-test",
		RequestedBy: 7,
		Action:      ActionCreateLab,
	}}
	executor := &executorStub{response: protocol.CommandResponse{
		Status: "rejected",
		Error: map[string]any{
			"code":    "COMMAND_NOT_IMPLEMENTED",
			"message": "not implemented",
		},
	}}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue,
		executor,
		WorkerConfig{
			Owner:          "worker-1",
			PollInterval:   time.Second,
			LeaseDuration:  time.Minute,
			CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	worker.newCommandID = func() (string, error) { return "command-1", nil }
	if _, err := worker.processOne(context.Background()); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}
	if queue.completedCode != "COMMAND_NOT_IMPLEMENTED" {
		t.Fatalf("completed code = %q", queue.completedCode)
	}
}

func TestWorkerNoPendingOperation(t *testing.T) {
	t.Parallel()

	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&queueStub{claimErr: ErrNoPending},
		&executorStub{},
		WorkerConfig{
			Owner:          "worker-1",
			PollInterval:   time.Second,
			LeaseDuration:  time.Minute,
			CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	processed, err := worker.processOne(context.Background())
	if err != nil || processed {
		t.Fatalf("processOne() = %t, %v", processed, err)
	}
}
