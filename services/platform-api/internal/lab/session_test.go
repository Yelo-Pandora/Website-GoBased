package lab

import (
	"context"
	"errors"
	"testing"
	"time"
)

type createRepositoryStub struct {
	result       CreateResult
	err          error
	labID        string
	operationID  string
	ownedSession Session
	ownedErr     error
}

type actionRepositoryStub struct {
	createRepositoryStub
	actionResult ActionResult
	actionErr    error
	snapshot     Snapshot
	snapshotErr  error
	action       string
	labID        string
	operationID  string
	cacheInput   ActionInput
}

func (s *actionRepositoryStub) EnqueueCacheAction(
	_ context.Context,
	_ uint64,
	labID string,
	input ActionInput,
	_ time.Time,
) (ActionResult, error) {
	s.labID = labID
	s.operationID = input.OperationID
	s.cacheInput = input
	return s.actionResult, s.actionErr
}

func (s *actionRepositoryStub) EnqueueAction(
	_ context.Context,
	_ uint64,
	labID string,
	operationID string,
	action string,
	_ any,
	_ time.Time,
) (ActionResult, error) {
	s.action = action
	s.labID = labID
	s.operationID = operationID
	return s.actionResult, s.actionErr
}

func (s *actionRepositoryStub) FindSnapshot(
	_ context.Context,
	_ string,
	_ uint64,
) (Snapshot, error) {
	return s.snapshot, s.snapshotErr
}

func (s *createRepositoryStub) Create(
	_ context.Context,
	_ uint64,
	_ uint64,
	operationID string,
	labID string,
	_ Quota,
	_ time.Time,
) (CreateResult, error) {
	s.labID = labID
	s.operationID = operationID
	return s.result, s.err
}

func (s *createRepositoryStub) FindOwned(
	_ context.Context,
	_ string,
	_ uint64,
) (Session, error) {
	return s.ownedSession, s.ownedErr
}

func TestServiceCreateValidatesAndGeneratesLabID(t *testing.T) {
	t.Parallel()

	repository := &createRepositoryStub{}
	service := newService(repository, Quota{
		MaxActiveLabs:          10,
		MaxTemporaryContainers: 40,
		MaxInstancesPerLab:     4,
	})
	service.newID = func() (string, error) { return "lab-test1234", nil }
	if _, err := service.Create(
		context.Background(),
		7,
		3,
		"operation-1",
	); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repository.labID != "lab-test1234" || repository.operationID != "operation-1" {
		t.Fatalf("repository received labID=%q operationID=%q", repository.labID, repository.operationID)
	}
}

func TestServiceCreateRejectsInvalidOperationID(t *testing.T) {
	t.Parallel()

	service := newService(&createRepositoryStub{}, Quota{
		MaxActiveLabs:          10,
		MaxTemporaryContainers: 40,
		MaxInstancesPerLab:     4,
	})
	if _, err := service.Create(context.Background(), 7, 3, "bad operation"); err == nil {
		t.Fatal("Create() error = nil")
	}
}

func TestServiceGetOwnedPreservesOwnershipError(t *testing.T) {
	t.Parallel()

	service := newService(&createRepositoryStub{ownedErr: ErrNotOwned}, Quota{
		MaxActiveLabs:          10,
		MaxTemporaryContainers: 40,
		MaxInstancesPerLab:     4,
	})
	_, err := service.GetOwned(context.Background(), "lab-test1234", 7)
	if !errors.Is(err, ErrNotOwned) {
		t.Fatalf("GetOwned() error = %v; want ErrNotOwned", err)
	}
}

func TestServiceEnqueuesResetAndTerminate(t *testing.T) {
	t.Parallel()

	repository := &actionRepositoryStub{}
	service := newService(repository, Quota{
		MaxActiveLabs:          10,
		MaxTemporaryContainers: 40,
		MaxInstancesPerLab:     4,
	})
	if _, err := service.Reset(context.Background(), 7, "lab-test1234", "operation-reset"); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if repository.action != "RESET_LAB" || repository.labID != "lab-test1234" {
		t.Fatalf("Reset() repository capture = %#v", repository)
	}
	if _, err := service.Terminate(
		context.Background(), 7, "lab-test1234", "operation-destroy",
	); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if repository.action != "DESTROY_LAB" || repository.operationID != "operation-destroy" {
		t.Fatalf("Terminate() repository capture = %#v", repository)
	}
}

func TestServiceSnapshotHidesInvalidIdentityAsNotFound(t *testing.T) {
	t.Parallel()

	service := newService(&actionRepositoryStub{}, Quota{
		MaxActiveLabs:          10,
		MaxTemporaryContainers: 40,
		MaxInstancesPerLab:     4,
	})
	if _, err := service.Snapshot(context.Background(), "bad lab", 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Snapshot() error = %v; want ErrNotFound", err)
	}
}

func TestServiceSnapshotDerivesLifecycleDeadlines(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	lastActionAt := startedAt.Add(2 * time.Minute)
	repository := &actionRepositoryStub{snapshot: Snapshot{Lab: Session{
		ID: "lab-test1234", StartedAt: &startedAt, LastEffectiveActionAt: &lastActionAt,
	}}}
	service := newService(repository, Quota{
		MaxActiveLabs: 10, MaxTemporaryContainers: 40, MaxInstancesPerLab: 4,
	}, LifetimeConfig{IdleTimeout: 10 * time.Minute, MaxDuration: 30 * time.Minute})
	snapshot, err := service.Snapshot(context.Background(), "lab-test1234", 7)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Lab.IdleExpiresAt == nil ||
		!snapshot.Lab.IdleExpiresAt.Equal(lastActionAt.Add(10*time.Minute)) {
		t.Fatalf("IdleExpiresAt = %v", snapshot.Lab.IdleExpiresAt)
	}
	if snapshot.Lab.MaximumExpiresAt == nil ||
		!snapshot.Lab.MaximumExpiresAt.Equal(startedAt.Add(30*time.Minute)) {
		t.Fatalf("MaximumExpiresAt = %v", snapshot.Lab.MaximumExpiresAt)
	}
}

func TestValidActionInputAcceptsOnlyKnownBalancingModes(t *testing.T) {
	t.Parallel()

	adaptive := "adaptive"
	invalid := "automatic"
	if !validActionInput(ActionInput{
		ActionType:    ActionSetBalancingMode,
		BalancingMode: &adaptive,
	}) {
		t.Fatal("validActionInput() rejected adaptive mode")
	}
	if validActionInput(ActionInput{
		ActionType:    ActionSetBalancingMode,
		BalancingMode: &invalid,
	}) {
		t.Fatal("validActionInput() accepted an unknown mode")
	}
	if validActionInput(ActionInput{
		ActionType:    ActionSetBalancingMode,
		BalancingMode: &adaptive,
		Weights:       []InstanceWeight{{InstanceID: "app-1", Weight: 1}},
	}) {
		t.Fatal("validActionInput() accepted mixed mode and weight parameters")
	}
}

func TestServiceDispatchesCacheLayerActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		action   string
		targetID *string
	}{
		{name: "remove L1", action: ActionRemoveInstanceL1, targetID: pointerTo("app-2")},
		{name: "add L1", action: ActionAddInstanceL1, targetID: pointerTo("app-2")},
		{name: "remove Redis", action: ActionRemoveSessionRedis},
		{name: "add Redis", action: ActionAddSessionRedis},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &actionRepositoryStub{}
			service := newService(repository, Quota{
				MaxActiveLabs: 10, MaxTemporaryContainers: 40, MaxInstancesPerLab: 4,
			})
			input := ActionInput{
				OperationID: "operation-cache", ActionType: test.action,
				TargetInstanceID: test.targetID,
			}
			if _, err := service.Action(context.Background(), 7, "lab-test1234", input); err != nil {
				t.Fatalf("Action() error = %v", err)
			}
			if repository.cacheInput.ActionType != test.action || repository.labID != "lab-test1234" {
				t.Fatalf("cache action capture = %#v", repository)
			}
		})
	}
}

func TestValidActionInputRejectsCacheActionParameterMixes(t *testing.T) {
	t.Parallel()
	performance := 100
	if validActionInput(ActionInput{
		ActionType: ActionRemoveSessionRedis, TargetInstanceID: pointerTo("app-1"),
	}) {
		t.Fatal("Redis action accepted a target instance")
	}
	if validActionInput(ActionInput{
		ActionType: ActionRemoveInstanceL1, TargetInstanceID: pointerTo("app-1"),
		PerformancePercent: &performance,
	}) {
		t.Fatal("L1 action accepted a performance parameter")
	}
}

func pointerTo(value string) *string {
	return &value
}

func TestScenarioForCourse(t *testing.T) {
	t.Parallel()

	scenario, ok := scenarioForCourse("multi-level-cache", "active")
	if !ok || !scenario.RedisRequired || scenario.TemporaryContainers != 4 || scenario.InitialInstances != 3 {
		t.Fatalf("scenarioForCourse() = %#v, %t", scenario, ok)
	}
	if _, ok := scenarioForCourse("standalone-architecture", "theory"); ok {
		t.Fatal("scenarioForCourse() returned a theory-only scenario")
	}
}
