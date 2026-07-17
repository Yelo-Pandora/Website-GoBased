package traffic

import (
	"context"
	"errors"
	"testing"

	"website-gobased/internal/protocol"
	"website-gobased/services/platform-api/internal/lab"
)

type labReaderStub struct {
	snapshot lab.Snapshot
	err      error
}

func (s labReaderStub) Snapshot(context.Context, string, uint64) (lab.Snapshot, error) {
	return s.snapshot, s.err
}

type batchClientStub struct {
	result protocol.TrafficBatchResult
	err    error
}

func (s batchClientStub) Submit(
	context.Context,
	string,
	protocol.TrafficBatchRequest,
) (protocol.TrafficBatchResult, error) {
	return s.result, s.err
}

type limiterStub bool

func (s limiterStub) Allow(uint64) bool { return bool(s) }

type observerStub struct {
	labID  string
	result protocol.TrafficBatchResult
}

func (s *observerStub) Observe(labID string, result protocol.TrafficBatchResult) {
	s.labID = labID
	s.result = result
}

func TestServiceValidatesAndBuildsPath(t *testing.T) {
	result := validTrafficResult()
	service := NewService(
		labReaderStub{snapshot: runningSnapshot()},
		batchClientStub{result: result},
		limiterStub(true),
	)
	got, err := service.Submit(context.Background(), 7, "lab-test", protocol.TrafficBatchRequest{
		BatchID: "batch-test", ProductID: 1, RequestUnits: 10,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if len(got.Path) != 3 || got.Path[2] != "app-1" {
		t.Fatalf("Path = %#v", got.Path)
	}
}

func TestServiceRejectsIdentityMismatch(t *testing.T) {
	result := validTrafficResult()
	result.TargetInstanceID = "app-other"
	service := NewService(
		labReaderStub{snapshot: runningSnapshot()},
		batchClientStub{result: result},
		limiterStub(true),
	)
	_, err := service.Submit(context.Background(), 7, "lab-test", protocol.TrafficBatchRequest{
		BatchID: "batch-test", ProductID: 1, RequestUnits: 10,
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Submit() error = %v; want ErrUnavailable", err)
	}
}

func TestServiceObservesOnlyValidatedResults(t *testing.T) {
	result := validTrafficResult()
	observer := &observerStub{}
	service := NewService(
		labReaderStub{snapshot: runningSnapshot()},
		batchClientStub{result: result},
		limiterStub(true),
		observer,
	)
	if _, err := service.Submit(
		context.Background(),
		7,
		"lab-test",
		protocol.TrafficBatchRequest{
			BatchID: "batch-test", ProductID: 1, RequestUnits: 10,
		},
	); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if observer.labID != "lab-test" || observer.result.TargetInstanceID != "app-1" {
		t.Fatalf("observer capture = %#v", observer)
	}
}

func runningSnapshot() lab.Snapshot {
	return lab.Snapshot{
		Lab:      lab.Session{ID: "lab-test", Status: lab.StatusRunning},
		Topology: lab.Topology{Instances: []lab.Instance{{ID: "app-1", Name: "app-1"}}},
	}
}
