package balancer

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"website-gobased/internal/protocol"
	"website-gobased/services/platform-api/internal/lab"
)

type storeStub struct {
	labs       []Lab
	operations map[string]Operation
	enqueued   []Adjustment
	enqueueErr error
}

func (s *storeStub) ListAdaptiveLabs(context.Context) ([]Lab, error) {
	return s.labs, nil
}

func (s *storeStub) EnqueueAdjustment(
	_ context.Context,
	adjustment Adjustment,
	_ time.Time,
) (Operation, error) {
	if s.enqueueErr != nil {
		return Operation{}, s.enqueueErr
	}
	s.enqueued = append(s.enqueued, adjustment)
	operation := Operation{OperationID: adjustment.OperationID, Status: "pending"}
	if s.operations == nil {
		s.operations = make(map[string]Operation)
	}
	s.operations[operation.OperationID] = operation
	return operation, nil
}

func (s *storeStub) FindAdjustment(_ context.Context, operationID string) (Operation, error) {
	return s.operations[operationID], nil
}

func TestTargetWeightsReducesCapacityRatio(t *testing.T) {
	t.Parallel()

	weights, err := TargetWeights([]Instance{
		{ID: "app-3", EffectiveCapacity: 30},
		{ID: "app-1", EffectiveCapacity: 100},
		{ID: "app-2", EffectiveCapacity: 100},
	})
	if err != nil {
		t.Fatalf("TargetWeights() error = %v", err)
	}
	want := []Weight{
		{InstanceID: "app-1", Weight: 10},
		{InstanceID: "app-2", Weight: 10},
		{InstanceID: "app-3", Weight: 3},
	}
	for i := range want {
		if weights[i] != want[i] {
			t.Fatalf("weights[%d] = %#v; want %#v", i, weights[i], want[i])
		}
	}
}

func TestControllerEnqueuesAfterThreeStableSamplesWithoutTraffic(t *testing.T) {
	store := &storeStub{labs: []Lab{adaptiveLab(100, 30, 100, 100)}}
	controller := newTestController(t, store)
	controller.newOperationID = func() (string, error) { return "balancer-test", nil }
	for range 3 {
		controller.sample(context.Background())
	}
	if len(store.enqueued) != 1 {
		t.Fatalf("enqueued adjustments = %d; want 1", len(store.enqueued))
	}
	if got := store.enqueued[0].Weights; len(got) != 2 ||
		got[0].Weight != 10 || got[1].Weight != 3 {
		t.Fatalf("target weights = %#v", got)
	}
	if view := controller.View("lab-test", store.labs[0].Instances); view.Status != StatusConverging {
		t.Fatalf("view status = %q; want %q", view.Status, StatusConverging)
	}
}

func TestControllerDoesNotReloadMatchingWeights(t *testing.T) {
	store := &storeStub{labs: []Lab{adaptiveLab(100, 30, 10, 3)}}
	controller := newTestController(t, store)
	for range 3 {
		controller.sample(context.Background())
	}
	if len(store.enqueued) != 0 {
		t.Fatalf("enqueued adjustments = %d; want 0", len(store.enqueued))
	}
	if view := controller.View("lab-test", store.labs[0].Instances); view.Status != StatusStable {
		t.Fatalf("view status = %q; want %q", view.Status, StatusStable)
	}
}

func TestControllerRetriesFailuresAfterBackoff(t *testing.T) {
	store := &storeStub{labs: []Lab{adaptiveLab(100, 30, 100, 100)}}
	controller := newTestController(t, store)
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }
	operationIDs := []string{"balancer-1", "balancer-2"}
	controller.newOperationID = func() (string, error) {
		operationID := operationIDs[0]
		operationIDs = operationIDs[1:]
		return operationID, nil
	}
	for range 3 {
		controller.sample(context.Background())
	}
	firstID := store.enqueued[0].OperationID
	store.operations[firstID] = Operation{
		OperationID:  firstID,
		Status:       "failed",
		ErrorCode:    "NGINX_CONFIG_INVALID",
		ErrorMessage: "configuration rejected",
	}
	controller.sample(context.Background())
	if view := controller.View("lab-test", store.labs[0].Instances); view.Status != StatusDegraded {
		t.Fatalf("view status = %q; want %q", view.Status, StatusDegraded)
	}
	now = now.Add(3 * time.Second)
	controller.sample(context.Background())
	if len(store.enqueued) != 1 {
		t.Fatalf("enqueued adjustments before retry = %d; want 1", len(store.enqueued))
	}
	now = now.Add(2 * time.Second)
	controller.sample(context.Background())
	if len(store.enqueued) != 2 {
		t.Fatalf("enqueued adjustments after retry = %d; want 2", len(store.enqueued))
	}
}

func TestControllerResetsStabilityWhenCapacityChanges(t *testing.T) {
	store := &storeStub{labs: []Lab{adaptiveLab(100, 100, 100, 100)}}
	controller := newTestController(t, store)
	controller.sample(context.Background())
	controller.sample(context.Background())
	store.labs[0].Instances[1].EffectiveCapacity = 30
	controller.sample(context.Background())
	controller.sample(context.Background())
	if len(store.enqueued) != 0 {
		t.Fatalf("enqueued adjustments = %d; want 0", len(store.enqueued))
	}
}

func TestControllerSmoothsObservedLoadAndReportsCapacityBoundary(t *testing.T) {
	store := &storeStub{}
	controller := newTestController(t, store)
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }
	result := protocol.TrafficBatchResult{TargetInstanceID: "app-1"}
	result.InstanceState.LoadRatio = 0.2
	controller.Observe("lab-test", result)
	result.InstanceState.LoadRatio = 1.4
	result.DroppedUnits = 1
	controller.Observe("lab-test", result)
	view := controller.View("lab-test", []Instance{{
		ID: "app-1", Status: "running", EffectiveCapacity: 100, CurrentWeight: 1,
	}})
	if len(view.SmoothedLoads) != 1 ||
		math.Abs(view.SmoothedLoads[0].LoadRatio-0.8) > 0.000001 {
		t.Fatalf("smoothed loads = %#v", view.SmoothedLoads)
	}
	if view.CapacityNotice != "cluster_overloaded" {
		t.Fatalf("capacity notice = %q", view.CapacityNotice)
	}
}

func TestControllerDecoratesOnlyAdaptiveSnapshots(t *testing.T) {
	controller := newTestController(t, &storeStub{})
	snapshot := lab.Snapshot{
		Lab: lab.Session{ID: "lab-test", BalancingMode: "adaptive"},
		Topology: lab.Topology{Instances: []lab.Instance{{
			ID: "app-1", Status: "running", EffectiveCapacity: 100, CurrentWeight: 100,
		}}},
	}
	controller.DecorateSnapshot(&snapshot)
	if snapshot.Balancer == nil || snapshot.Balancer.Status != StatusConverging ||
		len(snapshot.Balancer.TargetWeights) != 1 {
		t.Fatalf("adaptive snapshot = %#v", snapshot.Balancer)
	}
	snapshot.Lab.BalancingMode = "fixed"
	snapshot.Balancer = nil
	controller.DecorateSnapshot(&snapshot)
	if snapshot.Balancer != nil {
		t.Fatalf("fixed snapshot balancer = %#v; want nil", snapshot.Balancer)
	}
}

func adaptiveLab(
	firstCapacity int,
	secondCapacity int,
	firstWeight int,
	secondWeight int,
) Lab {
	return Lab{
		ID: "lab-test", UserID: 7,
		Instances: []Instance{
			{
				ID: "app-1", Status: "running",
				EffectiveCapacity: firstCapacity, CurrentWeight: firstWeight,
			},
			{
				ID: "app-2", Status: "running",
				EffectiveCapacity: secondCapacity, CurrentWeight: secondWeight,
			},
		},
	}
}

func newTestController(t *testing.T, store store) *Controller {
	t.Helper()
	controller, err := NewController(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		store,
		DefaultConfig(),
	)
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	return controller
}
