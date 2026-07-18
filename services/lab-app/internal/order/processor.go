// Package order applies teaching-equivalent aggregate load without saturating real CPU.
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

// Config fixes one instance identity and aggregate load model.
type Config struct {
	LabID           string
	InstanceID      string
	ProcessingSpeed int
	MaxLoad         int
}

// Processor serializes aggregate load updates within one application instance.
type Processor struct {
	products productReader
	stats    aggregateWriter
	config   Config
	now      func() time.Time
	mu       sync.Mutex

	currentLoad   float64
	lastUpdatedAt time.Time
}

// NewProcessor returns an aggregate-load processor.
func NewProcessor(
	products productReader,
	stats aggregateWriter,
	config Config,
) (*Processor, error) {
	if products == nil || stats == nil || config.LabID == "" || config.InstanceID == "" ||
		config.ProcessingSpeed <= 0 || config.MaxLoad <= 0 {
		return nil, errors.New("order processor config is incomplete")
	}
	return &Processor{products: products, stats: stats, config: config, now: time.Now}, nil
}

// Process validates a product, settles load, and stores aggregate counts.
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
	p.mu.Lock()
	defer p.mu.Unlock()
	result, nextLoad := p.allocate(request, now)
	if err := p.stats.Add(ctx, Aggregate{
		ProductID:     request.ProductID,
		InstanceID:    p.config.InstanceID,
		TimeBucket:    now.Truncate(time.Second),
		ReceivedUnits: result.ReceivedUnits,
		AcceptedUnits: result.AcceptedUnits,
		DroppedUnits:  result.DroppedUnits,
	}); err != nil {
		return protocol.TrafficBatchResult{}, err
	}
	p.currentLoad = nextLoad
	p.lastUpdatedAt = now
	return result, nil
}

func (p *Processor) allocate(
	request protocol.TrafficBatchRequest,
	now time.Time,
) (protocol.TrafficBatchResult, float64) {
	elapsedSeconds := 0.0
	if !p.lastUpdatedAt.IsZero() && now.After(p.lastUpdatedAt) {
		elapsedSeconds = now.Sub(p.lastUpdatedAt).Seconds()
	}
	settledLoad := max(
		0,
		p.currentLoad-float64(p.config.ProcessingSpeed)*elapsedSeconds,
	)
	available := max(0, float64(p.config.MaxLoad)-settledLoad)
	availableUnits := int(math.Floor(available + 1e-9))
	accepted := min(request.RequestUnits, availableUnits)
	dropped := request.RequestUnits - accepted
	nextLoad := min(float64(p.config.MaxLoad), settledLoad+float64(accepted))
	loadRatio := roundThree(nextLoad / float64(p.config.MaxLoad))
	status := "accepted"
	if accepted == 0 {
		status = "dropped"
	} else if dropped > 0 {
		status = "partially_accepted"
	}
	return protocol.TrafficBatchResult{
		BatchID:          request.BatchID,
		LabID:            p.config.LabID,
		Status:           status,
		OccurredAt:       now,
		TargetInstanceID: p.config.InstanceID,
		ReceivedUnits:    request.RequestUnits,
		AcceptedUnits:    accepted,
		DroppedUnits:     dropped,
		InstanceState: protocol.InstanceState{
			ProcessingSpeed: p.config.ProcessingSpeed,
			MaxLoad:         p.config.MaxLoad,
			CurrentLoad:     roundThree(nextLoad),
			LoadRatio:       loadRatio,
			LoadState:       loadState(loadRatio),
			ObservedAt:      now,
		},
	}, nextLoad
}

func roundThree(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func loadState(loadRatio float64) string {
	switch {
	case loadRatio <= 0.3:
		return "idle"
	case loadRatio <= 0.7:
		return "normal"
	case loadRatio < 1:
		return "high"
	default:
		return "overloaded"
	}
}
