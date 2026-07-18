// Package cache implements the real L1, Redis, and MySQL teaching path.
package cache

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	"website-gobased/internal/protocol"
	"website-gobased/services/lab-app/internal/order"
	"website-gobased/services/lab-app/internal/product"
)

type productRepository interface {
	Get(ctx context.Context, productID uint64) (protocol.CacheProduct, error)
	List(ctx context.Context) ([]protocol.CacheProduct, error)
}

// Config fixes one cache processor and its teaching model.
type Config struct {
	LabID, InstanceID                                              string
	InstanceCount                                                  int
	ProcessingSpeed, MaxLoad                                       int
	L1MaxEntries                                                   int
	L1TTL, L2TTL                                                   time.Duration
	L2JitterPercent                                                int
	LatencyL1MS, LatencyRedisMS, LatencyMySQLMS, LatencyDegradedMS int
}

// Processor performs exactly one real cache lookup per HTTP batch.
type Processor struct {
	products      productRepository
	l1            *L1Store
	redis         *redisStore
	config        Config
	now           func() time.Time
	mu            sync.Mutex
	currentLoad   float64
	lastUpdatedAt time.Time
}

func NewProcessor(products productRepository, redisAddress string, config Config) (*Processor, error) {
	if products == nil || redisAddress == "" || config.LabID == "" || config.InstanceID == "" ||
		config.InstanceCount <= 0 || config.ProcessingSpeed <= 0 || config.MaxLoad <= 0 ||
		config.L1MaxEntries <= 0 || config.L1TTL <= 0 || config.L2TTL <= 0 {
		return nil, errors.New("cache processor config is incomplete")
	}
	return &Processor{
		products: products,
		l1:       NewL1Store(config.L1MaxEntries, config.L1TTL),
		redis:    newRedisStore(redisAddress, config.LabID, config.L2TTL, config.L2JitterPercent),
		config:   config, now: time.Now,
	}, nil
}

func (p *Processor) Close() error { return p.redis.Close() }

func (p *Processor) Process(ctx context.Context, request protocol.TrafficBatchRequest) (protocol.TrafficBatchResult, error) {
	if request.BatchID == "" || request.ProductID == 0 || request.RequestUnits <= 0 {
		return protocol.TrafficBatchResult{}, order.ErrInvalidBatch
	}
	now := p.now().UTC()
	cacheResult := protocol.CacheResult{
		ActualLookupCount: 1, Outcome: "found", ObservedAt: now,
		Trace: []protocol.CacheTraceStep{}, Deltas: []protocol.CacheDelta{},
	}
	resolvedBy := ""
	productValue, hit := p.l1.Get(request.ProductID, now)
	if hit {
		resolvedBy = "l1"
		cacheResult.Trace = append(cacheResult.Trace, protocol.CacheTraceStep{Layer: "l1", Result: "hit"})
	} else {
		cacheResult.Trace = append(cacheResult.Trace, protocol.CacheTraceStep{Layer: "l1", Result: "miss"})
		redisProduct, redisHit, redisErr := p.redis.Get(ctx, request.ProductID)
		if redisErr == nil && redisHit {
			productValue = redisProduct
			resolvedBy = "redis"
			cacheResult.Trace = append(cacheResult.Trace, protocol.CacheTraceStep{Layer: "redis", Result: "hit"})
			cacheResult.Deltas = append(cacheResult.Deltas, p.putL1(productValue, now, "redis_fill")...)
		} else {
			result := "miss"
			if redisErr != nil {
				result = "unavailable"
			}
			cacheResult.Trace = append(cacheResult.Trace, protocol.CacheTraceStep{Layer: "redis", Result: result})
			mysqlProduct, err := p.products.Get(ctx, request.ProductID)
			if errors.Is(err, product.ErrNotFound) {
				cacheResult.Outcome = "not_found"
				resolvedBy = "mysql"
				cacheResult.Trace = append(cacheResult.Trace, protocol.CacheTraceStep{Layer: "mysql", Result: "not_found"})
			} else if err != nil {
				return protocol.TrafficBatchResult{}, err
			} else {
				productValue = mysqlProduct
				resolvedBy = "mysql"
				if redisErr != nil {
					resolvedBy = "degraded"
				}
				cacheResult.Trace = append(cacheResult.Trace, protocol.CacheTraceStep{Layer: "mysql", Result: "hit"})
				if expiresAt, err := p.redis.Set(ctx, productValue); err == nil {
					cacheResult.Deltas = append(cacheResult.Deltas, protocol.CacheDelta{
						Scope: "redis", Operation: "add", ProductID: productValue.ID,
						Reason: "mysql_fill", Product: &productValue, ExpiresAt: &expiresAt,
					})
				}
				cacheResult.Deltas = append(cacheResult.Deltas, p.putL1(productValue, now, "mysql_fill")...)
			}
		}
	}
	cacheResult.ResolvedBy = resolvedBy
	cacheResult.SimulatedLatencyMS = p.latency(resolvedBy)
	if cacheResult.Outcome == "found" {
		cacheResult.Product = &productValue
	}
	p.report(context.WithoutCancel(ctx), now)
	return p.allocate(request, now, cacheResult), nil
}

func (p *Processor) putL1(value protocol.CacheProduct, now time.Time, reason string) []protocol.CacheDelta {
	expiresAt, evicted := p.l1.Put(value, now)
	deltas := []protocol.CacheDelta{{
		Scope: "instance", InstanceID: p.config.InstanceID, Operation: "add",
		ProductID: value.ID, Reason: reason, Product: &value, ExpiresAt: &expiresAt,
	}}
	if evicted != nil {
		deltas = append(deltas, protocol.CacheDelta{
			Scope: "instance", InstanceID: p.config.InstanceID, Operation: "remove",
			ProductID: evicted.ID, Reason: "capacity_lru", Product: evicted,
		})
	}
	return deltas
}

func (p *Processor) report(ctx context.Context, now time.Time) {
	reportCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_ = p.redis.Report(reportCtx, protocol.CacheInstanceState{
		InstanceID: p.config.InstanceID, Status: "live", ObservedAt: now,
		Entries: p.l1.Snapshot(now),
	})
}

func (p *Processor) State(ctx context.Context) (protocol.CacheState, error) {
	now := p.now().UTC()
	catalog, err := p.products.List(ctx)
	if err != nil {
		return protocol.CacheState{}, err
	}
	p.report(context.WithoutCancel(ctx), now)
	ids := make([]string, 0, p.config.InstanceCount)
	for i := 1; i <= p.config.InstanceCount; i++ {
		ids = append(ids, "app-"+strconv.Itoa(i))
	}
	instances, redisState := p.redis.State(ctx, catalog, ids, now)
	for index := range instances {
		if instances[index].InstanceID == p.config.InstanceID {
			instances[index] = protocol.CacheInstanceState{
				InstanceID: p.config.InstanceID, Status: "live", ObservedAt: now,
				Entries: p.l1.Snapshot(now),
			}
		}
	}
	return protocol.CacheState{Catalog: catalog, Instances: instances, Redis: redisState}, nil
}

func (p *Processor) latency(layer string) int {
	switch layer {
	case "l1":
		return p.config.LatencyL1MS
	case "redis":
		return p.config.LatencyRedisMS
	case "mysql":
		return p.config.LatencyMySQLMS
	default:
		return p.config.LatencyDegradedMS
	}
}

func (p *Processor) allocate(request protocol.TrafficBatchRequest, now time.Time, cacheResult protocol.CacheResult) protocol.TrafficBatchResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	elapsed := 0.0
	if !p.lastUpdatedAt.IsZero() && now.After(p.lastUpdatedAt) {
		elapsed = now.Sub(p.lastUpdatedAt).Seconds()
	}
	settled := max(0, p.currentLoad-float64(p.config.ProcessingSpeed)*elapsed)
	available := int(math.Floor(max(0, float64(p.config.MaxLoad)-settled) + 1e-9))
	accepted := min(request.RequestUnits, available)
	dropped := request.RequestUnits - accepted
	next := min(float64(p.config.MaxLoad), settled+float64(accepted))
	ratio := math.Round(next/float64(p.config.MaxLoad)*1000) / 1000
	status := "accepted"
	if accepted == 0 {
		status = "dropped"
	} else if dropped > 0 {
		status = "partially_accepted"
	}
	p.currentLoad, p.lastUpdatedAt = next, now
	return protocol.TrafficBatchResult{
		BatchID: request.BatchID, LabID: p.config.LabID, Status: status, OccurredAt: now,
		TargetInstanceID: p.config.InstanceID, ReceivedUnits: request.RequestUnits,
		AcceptedUnits: accepted, DroppedUnits: dropped, Cache: &cacheResult,
		InstanceState: protocol.InstanceState{
			ProcessingSpeed: p.config.ProcessingSpeed, MaxLoad: p.config.MaxLoad,
			CurrentLoad: math.Round(next*1000) / 1000, LoadRatio: ratio,
			LoadState: cacheLoadState(ratio), ObservedAt: now,
		},
	}
}

func cacheLoadState(ratio float64) string {
	if ratio <= .3 {
		return "idle"
	}
	if ratio <= .7 {
		return "normal"
	}
	if ratio < 1 {
		return "high"
	}
	return "overloaded"
}
