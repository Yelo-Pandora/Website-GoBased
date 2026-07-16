package order

import (
	"context"
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
}

func (s *aggregateStub) Add(_ context.Context, value Aggregate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = append(s.values, value)
	return nil
}

func TestProcessorCapacityOutcomes(t *testing.T) {
	stats := &aggregateStub{}
	processor, err := NewProcessor(productStub{}, stats, Config{
		LabID: "lab-test", InstanceID: "app-1",
		EffectiveCapacity: 10, CapacityWindow: time.Second,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	processor.now = func() time.Time {
		return time.Date(2026, 7, 16, 12, 0, 0, 100, time.UTC)
	}

	first, err := processor.Process(context.Background(), protocol.TrafficBatchRequest{
		BatchID: "batch-1", ProductID: 1, RequestUnits: 6,
	})
	if err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	second, err := processor.Process(context.Background(), protocol.TrafficBatchRequest{
		BatchID: "batch-2", ProductID: 1, RequestUnits: 8,
	})
	if err != nil {
		t.Fatalf("second Process() error = %v", err)
	}
	third, err := processor.Process(context.Background(), protocol.TrafficBatchRequest{
		BatchID: "batch-3", ProductID: 1, RequestUnits: 2,
	})
	if err != nil {
		t.Fatalf("third Process() error = %v", err)
	}
	if first.Status != "processed" || first.ProcessedUnits != 6 || first.DroppedUnits != 0 {
		t.Fatalf("first result = %#v", first)
	}
	if second.Status != "partially_processed" || second.ProcessedUnits != 4 || second.DroppedUnits != 4 {
		t.Fatalf("second result = %#v", second)
	}
	if third.Status != "dropped" || third.ProcessedUnits != 0 || third.DroppedUnits != 2 {
		t.Fatalf("third result = %#v", third)
	}
	if second.ReceivedUnits != second.ProcessedUnits+second.DroppedUnits {
		t.Fatalf("quantity conservation failed: %#v", second)
	}
}

func TestProcessorSerializesConcurrentCapacity(t *testing.T) {
	stats := &aggregateStub{}
	processor, err := NewProcessor(productStub{}, stats, Config{
		LabID: "lab-test", InstanceID: "app-1",
		EffectiveCapacity: 100, CapacityWindow: time.Second,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	fixed := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	processor.now = func() time.Time { return fixed }

	var wait sync.WaitGroup
	results := make(chan protocol.TrafficBatchResult, 20)
	for i := 0; i < 20; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			result, processErr := processor.Process(context.Background(), protocol.TrafficBatchRequest{
				BatchID: "batch", ProductID: 1, RequestUnits: 10,
			})
			if processErr != nil {
				t.Errorf("Process(%d) error = %v", index, processErr)
				return
			}
			results <- result
		}(i)
	}
	wait.Wait()
	close(results)
	processed := 0
	dropped := 0
	for result := range results {
		processed += result.ProcessedUnits
		dropped += result.DroppedUnits
	}
	if processed != 100 || dropped != 100 {
		t.Fatalf("processed=%d dropped=%d; want 100/100", processed, dropped)
	}
}
