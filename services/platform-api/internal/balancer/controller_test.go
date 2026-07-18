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

func TestTargetWeightsUsesProcessingSpeedBaseline(t *testing.T) {
	t.Parallel()

	weights, err := TargetWeights([]Instance{
		{ID: "app-3", ProcessingSpeed: 6, MaxLoad: 100},
		{ID: "app-1", ProcessingSpeed: 20, MaxLoad: 100},
		{ID: "app-2", ProcessingSpeed: 20, MaxLoad: 100},
	}, nil)
	if err != nil {
		t.Fatalf("TargetWeights() error = %v", err)
	}
	want := []Weight{
		{InstanceID: "app-1", Weight: 10},
		{InstanceID: "app-2", Weight: 10},
		{InstanceID: "app-3", Weight: 3},
	}
	for index := range want {
		if weights[index] != want[index] {
			t.Fatalf("weights[%d] = %#v; want %#v", index, weights[index], want[index])
		}
	}
}

func TestTargetWeightsReducesHighLoadInstance(t *testing.T) {
	t.Parallel()

	weights, err := TargetWeights([]Instance{
		{ID: "app-1", ProcessingSpeed: 20, MaxLoad: 100},
		{ID: "app-2", ProcessingSpeed: 20, MaxLoad: 100},
	}, map[string]float64{"app-1": 0.9, "app-2": 0.1})
	if err != nil {
		t.Fatalf("TargetWeights() error = %v", err)
	}
	if weights[0].Weight >= weights[1].Weight {
		t.Fatalf("weights = %#v; high-load instance was not reduced", weights)
	}
}

func TestTargetWeightsIgnoresBalancedLoadSpread(t *testing.T) {
	t.Parallel()

	weights, err := TargetWeights([]Instance{
		{ID: "app-1", ProcessingSpeed: 20, MaxLoad: 100},
		{ID: "app-2", ProcessingSpeed: 20, MaxLoad: 100},
	}, map[string]float64{"app-1": 0.5, "app-2": 0.54})
	if err != nil {
		t.Fatalf("TargetWeights() error = %v", err)
	}
	if weights[0].Weight != 1 || weights[1].Weight != 1 {
		t.Fatalf("weights = %#v; want equal baseline", weights)
	}
}

func TestWeightsWithinShareThreshold(t *testing.T) {
	t.Parallel()

	target := []Weight{{InstanceID: "app-1", Weight: 1}, {InstanceID: "app-2", Weight: 1}}
	closeWeights := []Instance{
		{ID: "app-1", CurrentWeight: 51},
		{ID: "app-2", CurrentWeight: 49},
	}
	if !WeightsWithinShareThreshold(closeWeights, target) {
		t.Fatal("close weight shares were treated as material")
	}
	farWeights := []Instance{
		{ID: "app-1", CurrentWeight: 60},
		{ID: "app-2", CurrentWeight: 40},
	}
	if WeightsWithinShareThreshold(farWeights, target) {
		t.Fatal("far weight shares were treated as equivalent")
	}
}

func TestControllerEnqueuesAfterThreeConfigurationSamples(t *testing.T) {
	store := &storeStub{labs: []Lab{adaptiveLab(20, 6, 100, 100)}}
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
}

func TestControllerBecomesStableAfterThreeNoChangeSamples(t *testing.T) {
	store := &storeStub{labs: []Lab{adaptiveLab(20, 6, 10, 3)}}
	controller := newTestController(t, store)
	for range 5 {
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
	store := &storeStub{labs: []Lab{adaptiveLab(20, 6, 100, 100)}}
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

func TestControllerResetsConfigurationSamplesWhenSpeedChanges(t *testing.T) {
	store := &storeStub{labs: []Lab{adaptiveLab(20, 20, 1, 1)}}
	controller := newTestController(t, store)
	controller.sample(context.Background())
	controller.sample(context.Background())
	store.labs[0].Instances[1].ProcessingSpeed = 6
	controller.sample(context.Background())
	controller.sample(context.Background())
	if len(store.enqueued) != 0 {
		t.Fatalf("enqueued adjustments = %d; want 0", len(store.enqueued))
	}
}

func TestControllerEstimatesLoadAndUpdatesEWMAWithoutNewTraffic(t *testing.T) {
	controller := newTestController(t, &storeStub{})
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }
	instances := []Instance{{
		ID: "app-1", ContainerID: "container-app-1", Status: "running",
		ProcessingSpeed: 20, MaxLoad: 100, CurrentWeight: 1,
	}}
	controller.Observe("lab-test", lab.Instance{
		ID: "app-1", ContainerID: "container-app-1", ProcessingSpeed: 20, MaxLoad: 100,
	}, protocol.TrafficBatchResult{
		TargetInstanceID: "app-1",
		InstanceState: protocol.InstanceState{
			ProcessingSpeed: 20, MaxLoad: 100, CurrentLoad: 80,
			LoadRatio: 0.8, ObservedAt: now,
		},
	})
	first := controller.sampleLoadRatios("lab-test", instances, now)
	if first["app-1"] != 0.8 {
		t.Fatalf("first smoothed ratio = %v; want 0.8", first["app-1"])
	}
	now = now.Add(2 * time.Second)
	second := controller.sampleLoadRatios("lab-test", instances, now)
	if math.Abs(second["app-1"]-0.6) > 0.000001 {
		t.Fatalf("second smoothed ratio = %v; want 0.6", second["app-1"])
	}

	snapshot := lab.Snapshot{
		Lab: lab.Session{ID: "lab-test", BalancingMode: "fixed"},
		Topology: lab.Topology{Instances: []lab.Instance{{
			ID: "app-1", ContainerID: "container-app-1", Status: "running",
			ProcessingSpeed: 20, MaxLoad: 100,
		}}},
	}
	controller.DecorateSnapshot(&snapshot)
	if snapshot.Topology.Instances[0].CurrentLoad != 40 ||
		snapshot.Topology.Instances[0].LoadRatio != 0.4 || snapshot.Balancer != nil {
		t.Fatalf("fixed snapshot = %#v", snapshot)
	}
}

func TestControllerDecoratesAdaptiveSnapshot(t *testing.T) {
	controller := newTestController(t, &storeStub{})
	snapshot := lab.Snapshot{
		Lab: lab.Session{ID: "lab-test", BalancingMode: "adaptive"},
		Topology: lab.Topology{Instances: []lab.Instance{{
			ID: "app-1", ContainerID: "container-app-1", Status: "running",
			ProcessingSpeed: 20, MaxLoad: 100,
			CurrentWeight: 100,
		}}},
	}
	controller.DecorateSnapshot(&snapshot)
	if snapshot.Balancer == nil || snapshot.Balancer.Status != StatusConverging ||
		len(snapshot.Balancer.TargetWeights) != 1 {
		t.Fatalf("adaptive snapshot = %#v", snapshot.Balancer)
	}
}

func TestControllerDiscardsLoadFromReplacedContainer(t *testing.T) {
	controller := newTestController(t, &storeStub{})
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }
	controller.Observe("lab-test", lab.Instance{
		ID: "app-1", ContainerID: "container-old", ProcessingSpeed: 20, MaxLoad: 100,
	}, protocol.TrafficBatchResult{
		TargetInstanceID: "app-1",
		InstanceState: protocol.InstanceState{
			ProcessingSpeed: 20, MaxLoad: 100, CurrentLoad: 80,
			LoadRatio: 0.8, ObservedAt: now,
		},
	})
	snapshot := lab.Snapshot{
		Lab: lab.Session{ID: "lab-test", BalancingMode: "fixed"},
		Topology: lab.Topology{Instances: []lab.Instance{{
			ID: "app-1", ContainerID: "container-new", Status: "running",
			ProcessingSpeed: 20, MaxLoad: 100, LoadState: "idle",
		}}},
	}
	controller.DecorateSnapshot(&snapshot)
	instance := snapshot.Topology.Instances[0]
	if instance.CurrentLoad != 0 || instance.LoadRatio != 0 || instance.LoadState != "idle" {
		t.Fatalf("replaced instance = %#v", instance)
	}
}

func adaptiveLab(
	firstSpeed int,
	secondSpeed int,
	firstWeight int,
	secondWeight int,
) Lab {
	return Lab{
		ID: "lab-test", UserID: 7,
		Instances: []Instance{
			{
				ID: "app-1", ContainerID: "container-app-1", Status: "running",
				ProcessingSpeed: firstSpeed,
				MaxLoad:         100, CurrentWeight: firstWeight,
			},
			{
				ID: "app-2", ContainerID: "container-app-2", Status: "running",
				ProcessingSpeed: secondSpeed,
				MaxLoad:         100, CurrentWeight: secondWeight,
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
