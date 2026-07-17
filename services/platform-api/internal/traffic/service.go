// Package traffic validates and proxies synchronous teaching traffic batches.
package traffic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"website-gobased/internal/protocol"
	"website-gobased/services/platform-api/internal/lab"
)

var (
	// ErrInvalidRequest indicates invalid batch fields or a mismatched result.
	ErrInvalidRequest = errors.New("traffic request is invalid")
	// ErrNotFound hides missing and non-owned labs behind one error.
	ErrNotFound = errors.New("traffic lab was not found")
	// ErrNotRunning indicates that the lab cannot accept data-plane traffic.
	ErrNotRunning = errors.New("traffic lab is not running")
	// ErrRateLimited indicates that the authenticated user exceeded the HTTP limit.
	ErrRateLimited = errors.New("traffic request was rate limited")
	// ErrUnavailable hides internal gateway and response validation failures.
	ErrUnavailable = errors.New("traffic lab is unavailable")
)

type labReader interface {
	Snapshot(ctx context.Context, labID string, userID uint64) (lab.Snapshot, error)
}

type batchClient interface {
	Submit(
		ctx context.Context,
		labID string,
		request protocol.TrafficBatchRequest,
	) (protocol.TrafficBatchResult, error)
}

type userLimiter interface {
	Allow(userID uint64) bool
}

type resultObserver interface {
	Observe(labID string, result protocol.TrafficBatchResult)
}

// Service enforces platform ownership and validates an instance result.
type Service struct {
	labs     labReader
	client   batchClient
	limiter  userLimiter
	observer resultObserver
}

// NewService returns a synchronous traffic service.
func NewService(
	labs labReader,
	client batchClient,
	limiter userLimiter,
	observers ...resultObserver,
) *Service {
	service := &Service{labs: labs, client: client, limiter: limiter}
	if len(observers) > 0 {
		service.observer = observers[0]
	}
	return service
}

// Submit validates and proxies one traffic batch without updating lab activity.
func (s *Service) Submit(
	ctx context.Context,
	userID uint64,
	labID string,
	request protocol.TrafficBatchRequest,
) (protocol.TrafficBatchResult, error) {
	if userID == 0 || !validLabID(labID) || !validBatch(request) {
		return protocol.TrafficBatchResult{}, ErrInvalidRequest
	}
	if !s.limiter.Allow(userID) {
		return protocol.TrafficBatchResult{}, ErrRateLimited
	}
	snapshot, err := s.labs.Snapshot(ctx, labID, userID)
	if errors.Is(err, lab.ErrNotFound) || errors.Is(err, lab.ErrNotOwned) {
		return protocol.TrafficBatchResult{}, ErrNotFound
	}
	if err != nil {
		return protocol.TrafficBatchResult{}, fmt.Errorf("load traffic lab: %w", err)
	}
	if snapshot.Lab.Status != lab.StatusRunning {
		return protocol.TrafficBatchResult{}, ErrNotRunning
	}
	result, err := s.client.Submit(ctx, labID, request)
	if err != nil {
		return protocol.TrafficBatchResult{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if !validResult(snapshot, request, result) {
		return protocol.TrafficBatchResult{}, ErrUnavailable
	}
	if s.observer != nil {
		s.observer.Observe(labID, result)
	}
	result.Path = []string{"user-pool", "lab-gateway", result.TargetInstanceID}
	return result, nil
}

func validBatch(request protocol.TrafficBatchRequest) bool {
	if len(request.BatchID) < 4 || len(request.BatchID) > 64 ||
		strings.TrimSpace(request.BatchID) != request.BatchID ||
		request.ProductID == 0 || request.RequestUnits < 1 || request.RequestUnits > 100 {
		return false
	}
	for _, character := range request.BatchID {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func validLabID(value string) bool {
	if len(value) < 4 || len(value) > 64 {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && character == '-' {
			continue
		}
		return false
	}
	return true
}

func validResult(
	snapshot lab.Snapshot,
	request protocol.TrafficBatchRequest,
	result protocol.TrafficBatchResult,
) bool {
	if result.BatchID != request.BatchID || result.LabID != snapshot.Lab.ID ||
		result.OccurredAt.IsZero() || result.ReceivedUnits != request.RequestUnits ||
		result.ProcessedUnits < 0 || result.DroppedUnits < 0 ||
		result.ProcessedUnits+result.DroppedUnits != result.ReceivedUnits ||
		result.InstanceState.EffectiveCapacity <= 0 || result.InstanceState.RemainingCapacity < 0 ||
		result.InstanceState.CapacityWindowStartedAt.IsZero() ||
		!result.InstanceState.CapacityWindowEndsAt.After(result.InstanceState.CapacityWindowStartedAt) {
		return false
	}
	validStatus := result.Status == "processed" ||
		result.Status == "partially_processed" || result.Status == "dropped"
	validLoadState := result.InstanceState.LoadState == "idle" ||
		result.InstanceState.LoadState == "normal" || result.InstanceState.LoadState == "high" ||
		result.InstanceState.LoadState == "overloaded" || result.InstanceState.LoadState == "unavailable"
	if !validStatus || !validLoadState {
		return false
	}
	for _, instance := range snapshot.Topology.Instances {
		if instance.ID == result.TargetInstanceID {
			return true
		}
	}
	return false
}
