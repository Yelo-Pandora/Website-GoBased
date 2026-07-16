package lifecycle

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"website-gobased/internal/protocol"
)

type schedulerStoreStub struct {
	transitions []Transition
	expected    []string
	advances    int
}

func (s *schedulerStoreStub) Advance(context.Context, time.Time, Config) ([]Transition, error) {
	s.advances++
	return s.transitions, nil
}

func (s *schedulerStoreStub) ExpectedLabIDs(context.Context) ([]string, error) {
	return s.expected, nil
}

type schedulerExecutorStub struct {
	commands []protocol.Command
}

func (s *schedulerExecutorStub) Execute(_ context.Context, command protocol.Command) (protocol.CommandResponse, error) {
	s.commands = append(s.commands, command)
	return protocol.CommandResponse{
		CommandID: command.CommandID, OperationID: command.OperationID, Status: "succeeded",
	}, nil
}

func TestSchedulerReconcilesExpectedLabs(t *testing.T) {
	store := &schedulerStoreStub{expected: []string{"lab-abcdef12"}}
	executor := &schedulerExecutorStub{}
	scheduler, err := NewScheduler(
		slog.Default(), store, executor,
		SchedulerConfig{
			LifecyclePoll: 5 * time.Second, ReconcileInterval: time.Minute, ReconcileCleanup: true,
			Deadlines: Config{IdleTimeout: 10 * time.Minute, MaxDuration: 30 * time.Minute, ExpiringLead: time.Minute},
		},
	)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	scheduler.now = func() time.Time { return time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC) }
	scheduler.reconcile(context.Background())
	if len(executor.commands) != 1 {
		t.Fatalf("commands = %d; want 1", len(executor.commands))
	}
	var payload struct {
		Expected []string `json:"expectedLabIds"`
		Cleanup  bool     `json:"cleanup"`
	}
	if err := json.Unmarshal(executor.commands[0].Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Expected) != 1 || payload.Expected[0] != "lab-abcdef12" || !payload.Cleanup {
		t.Fatalf("payload = %#v", payload)
	}
}
