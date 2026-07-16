// Package order applies teaching-equivalent capacity without saturating real CPU.
package order

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"website-gobased/internal/protocol"
)

var (
	// ErrInvalidBatch indicates that a batch violates the internal contract.
	ErrInvalidBatch = errors.New("traffic batch is invalid")
)

type productReader interface {
	Require(ctx context.Context, productID uint64) error
}

type aggregateWriter interface {
	Add(ctx context.Context, value Aggregate) error
}

// Config fixes one instance identity and capacity model.
type Config struct {
	LabID             string
	InstanceID        string
	EffectiveCapacity int
	CapacityWindow    time.Duration
}

// Processor serializes capacity allocation within one application instance.
type Processor struct {
	products productReader
	stats    aggregateWriter
	config   Config
	now      func() time.Time
	mu       sync.Mutex
	window   capacityWindow
}

type capacityWindow struct {
	startedAt time.Time
	received  int
	processed int
}

// NewProcessor returns a capacity-window processor.
func NewProcessor(products productReader, stats aggregateWriter, config Config) (*Processor, error) {
	if products == nil || stats == nil || config.LabID == "" || config.InstanceID == "" ||
		config.EffectiveCapacity <= 0 || config.CapacityWindow <= 0 {
		return nil, errors.New("order processor config is incomplete")
	}
	return &Processor{products: products, stats: stats, config: config, now: time.Now}, nil
}

// Process validates a product, consumes the current window, and stores aggregate counts.
func (p *Processor) Process(
	ctx context.Context,
	request protocol.TrafficBatchRequest,
) (protocol.TrafficBatchResult, error) {
	if request.BatchID == "" || request.ProductID == 0 || request.RequestUnits <= 0 {
		return protocol.TrafficBatchResult{}, ErrInvalidBatch
	}
	if err := p.products.Require(ctx, request.ProductID); err != nil {
		return protocol.TrafficBatchResult{}, err
	}

	now := p.now().UTC()
	result, bucket := p.allocate(request, now)
	if err := p.stats.Add(ctx, Aggregate{
		ProductID:      request.ProductID,
		InstanceID:     p.config.InstanceID,
		TimeBucket:     bucket,
		ReceivedUnits:  result.ReceivedUnits,
		ProcessedUnits: result.ProcessedUnits,
		DroppedUnits:   result.DroppedUnits,
	}); err != nil {
		return protocol.TrafficBatchResult{}, err
	}
	return result, nil
}

func (p *Processor) allocate(
	request protocol.TrafficBatchRequest,
	now time.Time,
) (protocol.TrafficBatchResult, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	bucket := now.Truncate(p.config.CapacityWindow)
	if p.window.startedAt.IsZero() || !p.window.startedAt.Equal(bucket) {
		p.window = capacityWindow{startedAt: bucket}
	}
	remaining := max(0, p.config.EffectiveCapacity-p.window.processed)
	processed := min(request.RequestUnits, remaining)
	dropped := request.RequestUnits - processed
	p.window.received += request.RequestUnits
	p.window.processed += processed
	loadRatio := float64(p.window.received) / float64(p.config.EffectiveCapacity)
	status := "processed"
	if processed == 0 {
		status = "dropped"
	} else if dropped > 0 {
		status = "partially_processed"
	}
	return protocol.TrafficBatchResult{
		BatchID:          request.BatchID,
		LabID:            p.config.LabID,
		Status:           status,
		OccurredAt:       now,
		TargetInstanceID: p.config.InstanceID,
		ReceivedUnits:    request.RequestUnits,
		ProcessedUnits:   processed,
		DroppedUnits:     dropped,
		InstanceState: protocol.InstanceState{
			EffectiveCapacity:       p.config.EffectiveCapacity,
			RemainingCapacity:       max(0, p.config.EffectiveCapacity-p.window.processed),
			LoadRatio:               math.Round(loadRatio*1000) / 1000,
			LoadState:               loadState(loadRatio),
			CapacityWindowStartedAt: bucket,
			CapacityWindowEndsAt:    bucket.Add(p.config.CapacityWindow),
		},
	}, bucket
}

func loadState(loadRatio float64) string {
	switch {
	case loadRatio <= 0.3:
		return "idle"
	case loadRatio <= 0.7:
		return "normal"
	case loadRatio <= 1:
		return "high"
	default:
		return "overloaded"
	}
}
