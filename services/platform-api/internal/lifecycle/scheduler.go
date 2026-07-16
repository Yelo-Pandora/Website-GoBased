// Package lifecycle schedules lab expiration and resource reconciliation.
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"website-gobased/internal/protocol"
)

type store interface {
	Advance(context.Context, time.Time, Config) ([]Transition, error)
	ExpectedLabIDs(context.Context) ([]string, error)
}

type commandExecutor interface {
	Execute(context.Context, protocol.Command) (protocol.CommandResponse, error)
}

// SchedulerConfig controls lifecycle and reconciliation polling.
type SchedulerConfig struct {
	LifecyclePoll     time.Duration
	ReconcileInterval time.Duration
	ReconcileCleanup  bool
	Deadlines         Config
}

// Scheduler owns periodic lifecycle transitions and resource reconciliation.
type Scheduler struct {
	logger   *slog.Logger
	store    store
	executor commandExecutor
	config   SchedulerConfig
	now      func() time.Time
	sequence uint64
}

// NewScheduler returns a lifecycle scheduler.
func NewScheduler(
	logger *slog.Logger,
	store store,
	executor commandExecutor,
	config SchedulerConfig,
) (*Scheduler, error) {
	if logger == nil || store == nil || executor == nil {
		return nil, fmt.Errorf("lifecycle scheduler dependencies are required")
	}
	if config.LifecyclePoll <= 0 || config.ReconcileInterval <= 0 {
		return nil, fmt.Errorf("lifecycle scheduler intervals must be positive")
	}
	if err := validateConfig(config.Deadlines); err != nil {
		return nil, err
	}
	return &Scheduler{
		logger: logger, store: store, executor: executor, config: config, now: time.Now,
	}, nil
}

// Run starts lifecycle polling until the context is canceled.
func (s *Scheduler) Run(ctx context.Context) {
	s.advance(ctx)
	s.reconcile(ctx)
	lifecycleTicker := time.NewTicker(s.config.LifecyclePoll)
	defer lifecycleTicker.Stop()
	reconcileTicker := time.NewTicker(s.config.ReconcileInterval)
	defer reconcileTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-lifecycleTicker.C:
			s.advanceAt(ctx, now.UTC().Truncate(time.Microsecond))
		case <-reconcileTicker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *Scheduler) advance(ctx context.Context) {
	s.advanceAt(ctx, s.now().UTC().Truncate(time.Microsecond))
}

func (s *Scheduler) advanceAt(ctx context.Context, now time.Time) {
	transitions, err := s.store.Advance(ctx, now, s.config.Deadlines)
	if err != nil {
		s.logger.ErrorContext(ctx, "advance lab lifecycle", "error", err)
		return
	}
	for _, transition := range transitions {
		s.logger.InfoContext(ctx, "lab lifecycle transition",
			"labId", transition.LabID, "kind", transition.Kind,
			"reason", transition.Reason, "operationId", transition.OperationID)
	}
}

func (s *Scheduler) reconcile(ctx context.Context) {
	expected, err := s.store.ExpectedLabIDs(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "load expected lab ids", "error", err)
		return
	}
	sequence := atomic.AddUint64(&s.sequence, 1)
	operationID := fmt.Sprintf("system-reconcile-%d-%d", s.now().UnixNano(), sequence)
	commandID := fmt.Sprintf("cmd-reconcile-%d-%d", s.now().UnixNano(), sequence)
	payload, err := json.Marshal(struct {
		ExpectedLabIDs []string `json:"expectedLabIds"`
		Cleanup        bool     `json:"cleanup"`
	}{ExpectedLabIDs: expected, Cleanup: s.config.ReconcileCleanup})
	if err != nil {
		s.logger.ErrorContext(ctx, "encode resource reconciliation", "error", err)
		return
	}
	response, err := s.executor.Execute(ctx, protocol.Command{
		CommandType: "RECONCILE_RESOURCES",
		CommandID:   commandID,
		OperationID: operationID,
		LabID:       "lab-system",
		RequestedBy: "system",
		Payload:     payload,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "reconcile managed resources", "error", err)
		return
	}
	if response.Status != "succeeded" {
		s.logger.ErrorContext(ctx, "reconcile managed resources failed", "status", response.Status, "error", response.Error)
		return
	}
	s.logger.InfoContext(ctx, "reconciled managed resources", "result", response.Result)
}
