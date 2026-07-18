// Package balancer controls adaptive per-lab Nginx weights.
package balancer

import (
	"errors"
	"time"
)

const (
	// ActionApplyWeights is the internal operation used for adaptive changes.
	ActionApplyWeights = "APPLY_ADAPTIVE_WEIGHTS"

	// StatusConverging combines sampling, application, and confirmation.
	StatusConverging = "converging"
	// StatusStable indicates that persisted and target weights match.
	StatusStable = "stable"
	// StatusDegraded indicates a failed adjustment waiting for retry.
	StatusDegraded = "degraded"
)

var (
	// ErrBusy indicates that another lab operation is in progress.
	ErrBusy = errors.New("adaptive lab has another operation in progress")
	// ErrStale indicates that persisted topology no longer matches the sample.
	ErrStale = errors.New("adaptive topology sample is stale")
	// ErrNoChange indicates that persisted weights already match the target.
	ErrNoChange = errors.New("adaptive weights already match the target")
)

// Config controls sampling, smoothing, and failed-operation retries.
type Config struct {
	SampleInterval  time.Duration
	StableSamples   int
	MetricFreshness time.Duration
	EWMAAlpha       float64
	FailureBackoff  []time.Duration
}

// DefaultConfig returns the approved stage-eight control parameters.
func DefaultConfig() Config {
	return Config{
		SampleInterval:  2 * time.Second,
		StableSamples:   3,
		MetricFreshness: 10 * time.Second,
		EWMAAlpha:       0.5,
		FailureBackoff: []time.Duration{
			4 * time.Second,
			8 * time.Second,
			16 * time.Second,
			30 * time.Second,
		},
	}
}

func (c Config) validate() error {
	if c.SampleInterval <= 0 || c.StableSamples <= 0 || c.MetricFreshness <= 0 ||
		c.EWMAAlpha <= 0 || c.EWMAAlpha > 1 || len(c.FailureBackoff) == 0 {
		return errors.New("adaptive balancer config is invalid")
	}
	for _, delay := range c.FailureBackoff {
		if delay <= 0 {
			return errors.New("adaptive balancer backoff is invalid")
		}
	}
	return nil
}

// Instance is the persisted control-plane state used by the balancer.
type Instance struct {
	ID              string
	ContainerID     string
	Status          string
	ProcessingSpeed int
	MaxLoad         int
	CurrentWeight   int
}

// Lab is one adaptive experiment and its application instances.
type Lab struct {
	ID        string
	UserID    uint64
	Instances []Instance
}

// Weight is one calculated relative Nginx weight.
type Weight struct {
	InstanceID string `json:"instanceId"`
	Weight     int    `json:"weight"`
}

// Adjustment is one validated internal operation candidate.
type Adjustment struct {
	OperationID         string
	LabID               string
	RequestedBy         uint64
	TopologyFingerprint string
	Weights             []Weight
}

// Operation is the status of one internal adaptive adjustment.
type Operation struct {
	OperationID  string
	Status       string
	ErrorCode    string
	ErrorMessage string
	Skipped      bool
}

// SmoothedLoad is one fresh EWMA value exposed in a lab snapshot.
type SmoothedLoad struct {
	InstanceID string  `json:"instanceId"`
	LoadRatio  float64 `json:"loadRatio"`
}

// ViewError is the safe public failure description for a controller.
type ViewError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// View is the read-only adaptive status attached to a lab snapshot.
type View struct {
	Status         string         `json:"status"`
	TargetWeights  []Weight       `json:"targetWeights"`
	SmoothedLoads  []SmoothedLoad `json:"smoothedLoads"`
	CapacityNotice string         `json:"capacityNotice,omitempty"`
	LastError      *ViewError     `json:"lastError,omitempty"`
}
