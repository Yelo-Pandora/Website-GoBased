package operation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"website-gobased/internal/protocol"
)

type queue interface {
	Claim(
		ctx context.Context,
		owner string,
		now time.Time,
		leaseExpires time.Time,
	) (Record, error)
	MarkRunning(
		ctx context.Context,
		record Record,
		now time.Time,
		leaseExpires time.Time,
	) error
	CompleteProvision(
		ctx context.Context,
		record Record,
		resultValue any,
		errorCode string,
		errorMessage string,
		now time.Time,
	) error
}

type commandExecutor interface {
	Execute(
		ctx context.Context,
		command protocol.Command,
	) (protocol.CommandResponse, error)
}

// WorkerConfig controls queue polling and operation leases.
type WorkerConfig struct {
	Owner          string
	PollInterval   time.Duration
	LeaseDuration  time.Duration
	CommandTimeout time.Duration
}

// Worker leases persistent operations and executes trusted orchestrator commands.
type Worker struct {
	logger       *slog.Logger
	queue        queue
	executor     commandExecutor
	config       WorkerConfig
	now          func() time.Time
	newCommandID func() (string, error)
}

// NewWorker returns a persistent operation queue worker.
func NewWorker(
	logger *slog.Logger,
	queue *Repository,
	executor commandExecutor,
	config WorkerConfig,
) (*Worker, error) {
	return newWorker(logger, queue, executor, config)
}

func newWorker(
	logger *slog.Logger,
	queue queue,
	executor commandExecutor,
	config WorkerConfig,
) (*Worker, error) {
	if logger == nil || queue == nil || executor == nil {
		return nil, errors.New("operation worker dependencies are required")
	}
	if config.Owner == "" || config.PollInterval <= 0 ||
		config.LeaseDuration <= 0 || config.CommandTimeout <= 0 {
		return nil, errors.New("operation worker config is invalid")
	}
	if config.LeaseDuration <= config.CommandTimeout {
		return nil, errors.New("operation lease duration must exceed command timeout")
	}
	return &Worker{
		logger:       logger,
		queue:        queue,
		executor:     executor,
		config:       config,
		now:          time.Now,
		newCommandID: newCommandID,
	}, nil
}

// Run drains available operations and waits until the context is canceled.
func (w *Worker) Run(ctx context.Context) {
	for {
		processed, err := w.processOne(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			w.logger.ErrorContext(ctx, "process lab operation", "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		if processed {
			continue
		}
		timer := time.NewTimer(w.config.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) processOne(ctx context.Context) (bool, error) {
	now := w.now().UTC().Truncate(time.Microsecond)
	record, err := w.queue.Claim(
		ctx,
		w.config.Owner,
		now,
		now.Add(w.config.LeaseDuration),
	)
	if errors.Is(err, ErrNoPending) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := w.queue.MarkRunning(
		ctx,
		record,
		now,
		now.Add(w.config.LeaseDuration),
	); err != nil {
		return true, err
	}
	if record.Action != ActionCreateLab {
		return true, w.queue.CompleteProvision(
			ctx,
			record,
			nil,
			"ACTION_NOT_SUPPORTED",
			"operation action is not supported by this worker",
			w.now().UTC().Truncate(time.Microsecond),
		)
	}
	commandID, err := w.newCommandID()
	if err != nil {
		return true, w.completeFailure(ctx, record, "INTERNAL_ERROR", err.Error())
	}
	command := protocol.Command{
		CommandType: "PROVISION_LAB",
		CommandID:   commandID,
		OperationID: record.OperationID,
		LabID:       record.LabID,
		RequestedBy: strconv.FormatUint(record.RequestedBy, 10),
		Payload:     record.Payload,
	}
	commandCtx, cancel := context.WithTimeout(ctx, w.config.CommandTimeout)
	response, err := w.executor.Execute(commandCtx, command)
	cancel()
	if err != nil {
		return true, w.completeFailure(
			ctx,
			record,
			"ORCHESTRATOR_UNAVAILABLE",
			"orchestrator command could not be completed",
		)
	}
	switch response.Status {
	case "succeeded":
		return true, w.queue.CompleteProvision(
			ctx,
			record,
			response.Result,
			"",
			"",
			w.now().UTC().Truncate(time.Microsecond),
		)
	case "failed", "rejected":
		code, message := commandError(response.Error)
		return true, w.completeFailure(ctx, record, code, message)
	default:
		return true, w.completeFailure(
			ctx,
			record,
			"ORCHESTRATOR_RESPONSE_INCOMPLETE",
			"orchestrator did not return a final command result",
		)
	}
}

func (w *Worker) completeFailure(
	ctx context.Context,
	record Record,
	code string,
	message string,
) error {
	return w.queue.CompleteProvision(
		ctx,
		record,
		nil,
		code,
		message,
		w.now().UTC().Truncate(time.Microsecond),
	)
}

func newCommandID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate command id: %w", err)
	}
	return "cmd-" + hex.EncodeToString(data), nil
}

func commandError(value any) (string, string) {
	code := "ORCHESTRATOR_COMMAND_FAILED"
	message := "orchestrator command failed"
	fields, ok := value.(map[string]any)
	if !ok {
		return code, message
	}
	if candidate, ok := fields["code"].(string); ok && candidate != "" {
		code = candidate
	}
	if candidate, ok := fields["message"].(string); ok && candidate != "" {
		message = candidate
	}
	return code, message
}
