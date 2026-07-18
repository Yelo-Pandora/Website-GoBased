package order

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"website-gobased/internal/protocol"
)

type productStub struct{ err error }

func (s productStub) Require(context.Context, uint64) error { return s.err }

type aggregateStub struct {
	mu     sync.Mutex
	values []Aggregate
	err    error
}

func (s *aggregateStub) Add(_ context.Context, value Aggregate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.values = append(s.values, value)
	return nil
}

func TestProcessorSettlesAggregateLoad(t *testing.T) {
	stats := &aggregateStub{}
	processor, err := NewProcessor(productStub{}, stats, Config{
		LabID: "lab-test", InstanceID: "app-1", ProcessingSpeed: 10, MaxLoad: 10,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	processor.now = func() time.Time { return now }

	first := processBatch(t, processor, "batch-1", 6)
	second := processBatch(t, processor, "batch-2", 8)
	if first.Status != "accepted" || first.AcceptedUnits != 6 || first.InstanceState.CurrentLoad != 6 {
		t.Fatalf("first result = %#v", first)
	}
	if second.Status != "partially_accepted" || second.AcceptedUnits != 4 ||
		second.DroppedUnits != 4 || second.InstanceState.LoadState != "overloaded" {
		t.Fatalf("second result = %#v", second)
	}

	now = now.Add(500 * time.Millisecond)
	third := processBatch(t, processor, "batch-3", 2)
	if third.AcceptedUnits != 2 || third.DroppedUnits != 0 ||
		third.InstanceState.CurrentLoad != 7 || third.InstanceState.LoadRatio != 0.7 {
		t.Fatalf("third result = %#v", third)
	}

	now = now.Add(time.Second)
	fourth := processBatch(t, processor, "batch-4", 1)
	if fourth.InstanceState.CurrentLoad != 1 || fourth.InstanceState.LoadState != "idle" {
		t.Fatalf("fourth result = %#v", fourth)
	}
}

func TestProcessorFloorsFractionalAvailableLoad(t *testing.T) {
	processor, err := NewProcessor(productStub{}, &aggregateStub{}, Config{
		LabID: "lab-test", InstanceID: "app-1", ProcessingSpeed: 3, MaxLoad: 10,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	processor.now = func() time.Time { return now }
	processBatch(t, processor, "batch-1", 10)
	now = now.Add(500 * time.Millisecond)

	result := processBatch(t, processor, "batch-2", 2)
	if result.AcceptedUnits != 1 || result.DroppedUnits != 1 ||
		result.InstanceState.CurrentLoad != 9.5 || result.InstanceState.LoadRatio != 0.95 {
		t.Fatalf("result = %#v", result)
	}
}

func TestProcessorSerializesConcurrentLoad(t *testing.T) {
	processor, err := NewProcessor(productStub{}, &aggregateStub{}, Config{
		LabID: "lab-test", InstanceID: "app-1", ProcessingSpeed: 20, MaxLoad: 100,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	fixed := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	processor.now = func() time.Time { return fixed }

	var wait sync.WaitGroup
	results := make(chan protocol.TrafficBatchResult, 20)
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, processErr := processor.Process(context.Background(), protocol.TrafficBatchRequest{
				BatchID: "batch", ProductID: 1, RequestUnits: 10,
			})
			if processErr != nil {
				t.Errorf("Process() error = %v", processErr)
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	accepted := 0
	dropped := 0
	for result := range results {
		accepted += result.AcceptedUnits
		dropped += result.DroppedUnits
	}
	if accepted != 100 || dropped != 100 {
		t.Fatalf("accepted=%d dropped=%d; want 100/100", accepted, dropped)
	}
}

func TestProcessorDoesNotCommitLoadWhenStatisticsFail(t *testing.T) {
	stats := &aggregateStub{err: errors.New("database unavailable")}
	processor, err := NewProcessor(productStub{}, stats, Config{
		LabID: "lab-test", InstanceID: "app-1", ProcessingSpeed: 20, MaxLoad: 100,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	processor.now = func() time.Time {
		return time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	}
	if _, err := processor.Process(context.Background(), protocol.TrafficBatchRequest{
		BatchID: "batch-1", ProductID: 1, RequestUnits: 100,
	}); err == nil {
		t.Fatal("Process() error = nil")
	}
	stats.err = nil

	result := processBatch(t, processor, "batch-2", 100)
	if result.AcceptedUnits != 100 || result.DroppedUnits != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func processBatch(
	t *testing.T,
	processor *Processor,
	batchID string,
	requestUnits int,
) protocol.TrafficBatchResult {
	t.Helper()
	result, err := processor.Process(context.Background(), protocol.TrafficBatchRequest{
		BatchID: batchID, ProductID: 1, RequestUnits: requestUnits,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	return result
}
