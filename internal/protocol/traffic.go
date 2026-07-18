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
}
