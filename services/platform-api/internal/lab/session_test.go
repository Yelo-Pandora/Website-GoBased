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

func TestScenarioForCourse(t *testing.T) {
	t.Parallel()

	scenario, ok := scenarioForCourse("multi-level-cache", "active")
	if !ok || !scenario.RedisRequired || scenario.TemporaryContainers != 2 {
		t.Fatalf("scenarioForCourse() = %#v, %t", scenario, ok)
	}
	if _, ok := scenarioForCourse("standalone-architecture", "theory"); ok {
		t.Fatal("scenarioForCourse() returned a theory-only scenario")
	}
}
