package protocol

import "time"

// TrafficBatchRequest is one bounded teaching-equivalent request batch.
type TrafficBatchRequest struct {
	BatchID      string `json:"batchId"`
	ProductID    uint64 `json:"productId"`
	RequestUnits int    `json:"requestUnits"`
}

// InstanceState describes aggregate load after a batch has been evaluated.
type InstanceState struct {
	ProcessingSpeed int       `json:"processingSpeed"`
	MaxLoad         int       `json:"maxLoad"`
	CurrentLoad     float64   `json:"currentLoad"`
	LoadRatio       float64   `json:"loadRatio"`
	LoadState       string    `json:"loadState"`
	ObservedAt      time.Time `json:"observedAt"`
}

// TrafficBatchResult is the synchronous result returned by a real lab instance.
type TrafficBatchResult struct {
	BatchID          string        `json:"batchId"`
	LabID            string        `json:"labId"`
	Status           string        `json:"status"`
	OccurredAt       time.Time     `json:"occurredAt"`
	Path             []string      `json:"path"`
	TargetInstanceID string        `json:"targetInstanceId"`
	ReceivedUnits    int           `json:"receivedUnits"`
	AcceptedUnits    int           `json:"acceptedUnits"`
	DroppedUnits     int           `json:"droppedUnits"`
	InstanceState    InstanceState `json:"instanceState"`
	Cache            *CacheResult  `json:"cache,omitempty"`
}

// CacheProduct is the bounded product representation shared by cache APIs.
type CacheProduct struct {
	ID       uint64 `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Version  int    `json:"version"`
}

// CacheTraceStep records one real lookup decision.
type CacheTraceStep struct {
	Layer  string `json:"layer"`
	Result string `json:"result"`
}

// CacheDelta describes one immediately observable cache mutation.
type CacheDelta struct {
	Scope      string        `json:"scope"`
	InstanceID string        `json:"instanceId,omitempty"`
	Operation  string        `json:"operation"`
	ProductID  uint64        `json:"productId"`
	Reason     string        `json:"reason"`
	Product    *CacheProduct `json:"product,omitempty"`
	ExpiresAt  *time.Time    `json:"expiresAt,omitempty"`
}

// CacheResult accompanies one cache-scenario traffic batch.
type CacheResult struct {
	ActualLookupCount  int              `json:"actualLookupCount"`
	Outcome            string           `json:"outcome"`
	ResolvedBy         string           `json:"resolvedBy"`
	Trace              []CacheTraceStep `json:"trace"`
	Product            *CacheProduct    `json:"product,omitempty"`
	SimulatedLatencyMS int              `json:"simulatedLatencyMs"`
	ObservedAt         time.Time        `json:"observedAt"`
	Deltas             []CacheDelta     `json:"deltas"`
}

// CacheEntry is a positive product entry exposed in a bounded inventory.
type CacheEntry struct {
	Product        CacheProduct `json:"product"`
	ExpiresAt      time.Time    `json:"expiresAt"`
	LastAccessedAt *time.Time   `json:"lastAccessedAt,omitempty"`
	TTLMS          int64        `json:"ttlMs,omitempty"`
}

// CacheInstanceState is one application's latest reported L1 inventory.
type CacheInstanceState struct {
	InstanceID string       `json:"instanceId"`
	Status     string       `json:"status"`
	ObservedAt time.Time    `json:"observedAt"`
	Entries    []CacheEntry `json:"entries"`
}

// CacheRedisState is the shared Redis inventory visible to the experiment.
type CacheRedisState struct {
	Status     string       `json:"status"`
	ObservedAt time.Time    `json:"observedAt"`
	Entries    []CacheEntry `json:"entries"`
}

// CacheState is the authoritative bounded snapshot used by the frontend.
type CacheState struct {
	Catalog   []CacheProduct       `json:"catalog"`
	Instances []CacheInstanceState `json:"instances"`
	Redis     CacheRedisState      `json:"redis"`
}
