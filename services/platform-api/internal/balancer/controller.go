package balancer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type store interface {
	ListAdaptiveLabs(context.Context) ([]Lab, error)
	EnqueueAdjustment(context.Context, Adjustment, time.Time) (Operation, error)
	FindAdjustment(context.Context, string) (Operation, error)
}

type controlState struct {
	capacityFingerprint string
	stableSamples       int
	status              string
	targetWeights       []Weight
	operationID         string
	failureCount        int
	nextRetry           time.Time
	lastError           *ViewError
}

type metricState struct {
	smoothed    float64
	updatedAt   time.Time
	lastDropped time.Time
}

// Controller samples adaptive labs and creates serialized weight operations.
type Controller struct {
	logger         *slog.Logger
	store          store
	config         Config
	now            func() time.Time
	newOperationID func() (string, error)

	mu      sync.RWMutex
	states  map[string]controlState
	metrics map[string]map[string]metricState
}

// NewController returns an adaptive controller with fixed safety parameters.
func NewController(logger *slog.Logger, store store, config Config) (*Controller, error) {
	if logger == nil || store == nil {
		return nil, errors.New("adaptive balancer dependencies are required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &Controller{
		logger:         logger,
		store:          store,
		config:         config,
		now:            time.Now,
		newOperationID: newOperationID,
		states:         make(map[string]controlState),
		metrics:        make(map[string]map[string]metricState),
	}, nil
}

// Run samples adaptive labs until the context is canceled.
func (c *Controller) Run(ctx context.Context) {
	c.sample(ctx)
	ticker := time.NewTicker(c.config.SampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sample(ctx)
		}
	}
}

func (c *Controller) sample(ctx context.Context) {
	labs, err := c.store.ListAdaptiveLabs(ctx)
	if err != nil {
		c.logger.ErrorContext(ctx, "list adaptive labs", "error", err)
		return
	}
	seen := make(map[string]bool, len(labs))
	for _, lab := range labs {
		seen[lab.ID] = true
		if err := c.sampleLab(ctx, lab, c.now().UTC().Truncate(time.Microsecond)); err != nil {
			c.logger.ErrorContext(ctx, "sample adaptive lab", "labId", lab.ID, "error", err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for labID := range c.states {
		if !seen[labID] {
			delete(c.states, labID)
		}
	}
	for labID := range c.metrics {
		if !seen[labID] {
			delete(c.metrics, labID)
		}
	}
}

func (c *Controller) sampleLab(ctx context.Context, lab Lab, now time.Time) error {
	c.mu.RLock()
	state := cloneControlState(c.states[lab.ID])
	c.mu.RUnlock()

	if state.operationID != "" {
		operation, err := c.store.FindAdjustment(ctx, state.operationID)
		if err != nil {
			return fmt.Errorf("load adaptive adjustment: %w", err)
		}
		switch operation.Status {
		case "pending", "claimed", "running", "compensating":
			state.status = StatusConverging
			c.saveState(lab.ID, state)
			return nil
		case "succeeded":
			state.operationID = ""
			state.failureCount = 0
			state.nextRetry = time.Time{}
			state.lastError = nil
			if operation.Skipped {
				state.stableSamples = 0
				state.status = StatusConverging
				c.saveState(lab.ID, state)
				return nil
			}
		case "failed":
			state.operationID = ""
			state.failureCount++
			state.nextRetry = now.Add(c.backoff(state.failureCount))
			state.status = StatusDegraded
			state.lastError = &ViewError{
				Code: operation.ErrorCode, Message: operation.ErrorMessage,
			}
		default:
			return fmt.Errorf("adaptive adjustment has invalid status %q", operation.Status)
		}
	}

	target, err := TargetWeights(lab.Instances)
	if err != nil || !eligibleInstances(lab.Instances) {
		state.capacityFingerprint = ""
		state.stableSamples = 0
		state.status = StatusConverging
		state.targetWeights = nil
		c.saveState(lab.ID, state)
		return nil
	}
	fingerprint := CapacityFingerprint(lab.Instances)
	if fingerprint != state.capacityFingerprint {
		state.capacityFingerprint = fingerprint
		state.stableSamples = 1
		state.failureCount = 0
		state.nextRetry = time.Time{}
		state.lastError = nil
	} else if state.stableSamples < c.config.StableSamples {
		state.stableSamples++
	}
	state.targetWeights = target
	if state.stableSamples < c.config.StableSamples {
		state.status = StatusConverging
		c.saveState(lab.ID, state)
		return nil
	}
	if WeightsEqual(lab.Instances, target) {
		state.status = StatusStable
		state.failureCount = 0
		state.nextRetry = time.Time{}
		state.lastError = nil
		c.saveState(lab.ID, state)
		return nil
	}
	if !state.nextRetry.IsZero() && now.Before(state.nextRetry) {
		state.status = StatusDegraded
		c.saveState(lab.ID, state)
		return nil
	}
	operationID, err := c.newOperationID()
	if err != nil {
		return err
	}
	operation, err := c.store.EnqueueAdjustment(ctx, Adjustment{
		OperationID:         operationID,
		LabID:               lab.ID,
		RequestedBy:         lab.UserID,
		TopologyFingerprint: TopologyFingerprint(lab.Instances),
		Weights:             target,
	}, now)
	switch {
	case err == nil:
		state.operationID = operation.OperationID
		state.status = StatusConverging
	case errors.Is(err, ErrBusy):
		state.status = StatusConverging
	case errors.Is(err, ErrNoChange):
		state.status = StatusStable
	case errors.Is(err, ErrStale):
		state.stableSamples = 0
		state.status = StatusConverging
	default:
		state.failureCount++
		state.nextRetry = now.Add(c.backoff(state.failureCount))
		state.status = StatusDegraded
		state.lastError = &ViewError{
			Code: "BALANCER_UNAVAILABLE", Message: "adaptive adjustment could not be queued",
		}
		c.saveState(lab.ID, state)
		return fmt.Errorf("enqueue adaptive adjustment: %w", err)
	}
	c.saveState(lab.ID, state)
	return nil
}

func (c *Controller) saveState(labID string, state controlState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.states[labID] = cloneControlState(state)
}

func (c *Controller) backoff(failureCount int) time.Duration {
	index := failureCount - 1
	if index < 0 {
		index = 0
	}
	if index >= len(c.config.FailureBackoff) {
		index = len(c.config.FailureBackoff) - 1
	}
	return c.config.FailureBackoff[index]
}

func eligibleInstances(instances []Instance) bool {
	if len(instances) == 0 {
		return false
	}
	for _, instance := range instances {
		if instance.ID == "" || instance.Status != "running" ||
			instance.EffectiveCapacity <= 0 || instance.CurrentWeight <= 0 {
			return false
		}
	}
	return true
}

func cloneControlState(state controlState) controlState {
	state.targetWeights = append([]Weight(nil), state.targetWeights...)
	if state.lastError != nil {
		value := *state.lastError
		state.lastError = &value
	}
	return state
}

func newOperationID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate adaptive operation id: %w", err)
	}
	return "balancer-" + hex.EncodeToString(data), nil
}
