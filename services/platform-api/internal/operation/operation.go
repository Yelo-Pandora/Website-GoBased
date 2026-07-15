// Package operation provides the persistent lab operation queue.
package operation

import (
	"encoding/json"
	"errors"
	"time"
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

	ActionCreateLab = "CREATE_LAB"
)

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
