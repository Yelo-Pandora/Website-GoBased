// Package operation provides the persistent lab operation queue.
package operation

import (
	"encoding/json"
	"errors"
	"time"

	"website-gobased/services/platform-api/internal/lab"
)

var (
	// ErrNoPending indicates that no operation is ready to be claimed.
	ErrNoPending = errors.New("no pending lab operation")
	// ErrLeaseLost indicates that another worker owns or completed an operation.
	ErrLeaseLost = errors.New("lab operation lease was lost")
)

const (
	StatusPending      = "pending"
	StatusClaimed      = "claimed"
	StatusRunning      = "running"
	StatusSucceeded    = "succeeded"
	StatusFailed       = "failed"
	StatusCompensating = "compensating"

	ActionCreateLab              = "CREATE_LAB"
	ActionResetLab               = "RESET_LAB"
	ActionDestroyLab             = "DESTROY_LAB"
	ActionAddInstance            = "ADD_INSTANCE"
	ActionRemoveInstance         = "REMOVE_INSTANCE"
	ActionSetInstancePerformance = "SET_INSTANCE_PERFORMANCE"
	ActionSetInstanceWeights     = "SET_INSTANCE_WEIGHTS"
)

// TopologyResult is the normalized result of one stage-seven topology action.
type TopologyResult struct {
	Instance          *ProvisionInstance   `json:"instance,omitempty"`
	RemovedInstanceID string               `json:"removedInstanceId,omitempty"`
	Weights           []lab.InstanceWeight `json:"weights,omitempty"`
}

// ProvisionResult is the validated resource result returned by the orchestrator.
type ProvisionResult struct {
	LabID              string              `json:"labId"`
	ScenarioTemplateID string              `json:"scenarioTemplateId"`
	DatabaseName       string              `json:"databaseName"`
	DatabaseUser       string              `json:"databaseUser"`
	NetworkID          string              `json:"networkId"`
	NetworkName        string              `json:"networkName"`
	Instances          []ProvisionInstance `json:"instances"`
	Redis              *ProvisionRedis     `json:"redis"`
}

// ProvisionInstance is one application server created for a lab.
type ProvisionInstance struct {
	InstanceName       string  `json:"instanceName"`
	ContainerID        string  `json:"containerId"`
	ContainerName      string  `json:"containerName"`
	Status             string  `json:"status"`
	CPULimitCores      float64 `json:"cpuLimitCores"`
	MemoryLimitMB      int     `json:"memoryLimitMb"`
	PerformancePercent int     `json:"performancePercent"`
	EffectiveCapacity  int     `json:"effectiveCapacity"`
	CurrentWeight      int     `json:"currentWeight"`
}

// ProvisionRedis is the optional per-lab Redis container.
type ProvisionRedis struct {
	ContainerID   string `json:"containerId"`
	ContainerName string `json:"containerName"`
	Status        string `json:"status"`
}

// Record is one claimed persistent operation.
type Record struct {
	ID           uint64
	OperationID  string
	LabID        string
	RequestedBy  uint64
	Action       string
	Payload      json.RawMessage
	Status       string
	LeaseOwner   string
	LeaseExpires time.Time
	AttemptCount int
	CreatedAt    time.Time
}
