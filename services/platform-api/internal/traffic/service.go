// Package traffic validates and proxies synchronous teaching traffic batches.
package traffic

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	Observe(labID string, instance lab.Instance, result protocol.TrafficBatchResult)
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
		instance, _ := findInstance(snapshot.Topology.Instances, result.TargetInstanceID)
		s.observer.Observe(labID, instance, result)
	}
	result.Path = []string{"user-pool", "lab-gateway", result.TargetInstanceID}
	return result, nil
}

func findInstance(instances []lab.Instance, instanceID string) (lab.Instance, bool) {
	for _, instance := range instances {
		if instance.ID == instanceID {
			return instance, true
		}
	}
	return lab.Instance{}, false
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
		result.AcceptedUnits < 0 || result.DroppedUnits < 0 ||
		result.AcceptedUnits+result.DroppedUnits != result.ReceivedUnits ||
		result.InstanceState.ProcessingSpeed <= 0 || result.InstanceState.MaxLoad <= 0 ||
		result.InstanceState.CurrentLoad < 0 ||
		result.InstanceState.CurrentLoad > float64(result.InstanceState.MaxLoad) ||
		result.InstanceState.ObservedAt.IsZero() ||
		!result.InstanceState.ObservedAt.Equal(result.OccurredAt) {
		return false
	}
	expectedRatio := math.Round(
		result.InstanceState.CurrentLoad/float64(result.InstanceState.MaxLoad)*1000,
	) / 1000
	if math.Abs(result.InstanceState.LoadRatio-expectedRatio) > 0.001 {
		return false
	}
	validStatus := result.Status == "accepted" && result.AcceptedUnits > 0 &&
		result.DroppedUnits == 0 ||
		result.Status == "partially_accepted" && result.AcceptedUnits > 0 &&
			result.DroppedUnits > 0 ||
		result.Status == "dropped" && result.AcceptedUnits == 0 && result.DroppedUnits > 0
	validLoadState := result.InstanceState.LoadState == "idle" ||
		result.InstanceState.LoadState == "normal" || result.InstanceState.LoadState == "high" ||
		result.InstanceState.LoadState == "overloaded" || result.InstanceState.LoadState == "unavailable"
	if !validStatus || !validLoadState {
		return false
	}
	isCacheScenario := snapshot.Lab.ScenarioType == "multi_level_cache"
	if isCacheScenario {
		if !validCacheResult(request, result) {
			return false
		}
	} else if result.Cache != nil {
		return false
	}
	for _, instance := range snapshot.Topology.Instances {
		if instance.ID == result.TargetInstanceID &&
			instance.ProcessingSpeed == result.InstanceState.ProcessingSpeed &&
			instance.MaxLoad == result.InstanceState.MaxLoad {
			return true
		}
	}
	return false
}

func validCacheResult(request protocol.TrafficBatchRequest, result protocol.TrafficBatchResult) bool {
	value := result.Cache
	if value == nil || value.ActualLookupCount != 1 || value.ObservedAt.IsZero() ||
		!value.ObservedAt.Equal(result.OccurredAt) || value.SimulatedLatencyMS <= 0 ||
		len(value.Trace) == 0 || len(value.Trace) > 3 {
		return false
	}
	validResolvedBy := value.ResolvedBy == "l1" || value.ResolvedBy == "redis" ||
		value.ResolvedBy == "mysql" || value.ResolvedBy == "degraded"
	if !validResolvedBy || (value.Outcome != "found" && value.Outcome != "not_found") {
		return false
	}
	if value.Outcome == "found" {
		if value.Product == nil || value.Product.ID != request.ProductID ||
			value.Product.Name == "" || value.Product.Category == "" || value.Product.Version <= 0 {
			return false
		}
	} else if value.Product != nil {
		return false
	}
	for _, step := range value.Trace {
		if step.Layer != "l1" && step.Layer != "redis" && step.Layer != "mysql" {
			return false
		}
		if step.Result != "hit" && step.Result != "miss" && step.Result != "disabled" && step.Result != "not_found" &&
			step.Result != "unavailable" {
			return false
		}
	}
	return true
}
