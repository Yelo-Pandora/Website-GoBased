package protocol

import "time"

// TrafficBatchRequest is one bounded teaching-equivalent request batch.
type TrafficBatchRequest struct {
	BatchID      string `json:"batchId"`
	ProductID    uint64 `json:"productId"`
	RequestUnits int    `json:"requestUnits"`
}

// InstanceState describes capacity after a batch has been evaluated.
type InstanceState struct {
	EffectiveCapacity       int       `json:"effectiveCapacity"`
	RemainingCapacity       int       `json:"remainingCapacity"`
	LoadRatio               float64   `json:"loadRatio"`
	LoadState               string    `json:"loadState"`
	CapacityWindowStartedAt time.Time `json:"capacityWindowStartedAt"`
	CapacityWindowEndsAt    time.Time `json:"capacityWindowEndsAt"`
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
	ProcessedUnits   int           `json:"processedUnits"`
	DroppedUnits     int           `json:"droppedUnits"`
	InstanceState    InstanceState `json:"instanceState"`
}
