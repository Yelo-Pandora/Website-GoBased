// Package lab provides experiment session rules and persistence.
package lab

import "fmt"

// Status is the persisted lifecycle state of a lab session.
type Status string

const (
	StatusPreparing   Status = "Preparing"
	StatusRunning     Status = "Running"
	StatusExpiring    Status = "Expiring"
	StatusFailed      Status = "Failed"
	StatusTerminating Status = "Terminating"
	StatusTerminated  Status = "Terminated"
)

// Valid reports whether the status belongs to the stable lifecycle set.
func (s Status) Valid() bool {
	switch s {
	case StatusPreparing,
		StatusRunning,
		StatusExpiring,
		StatusFailed,
		StatusTerminating,
		StatusTerminated:
		return true
	default:
		return false
	}
}

// Active reports whether the status counts toward the single-active-lab rule.
func (s Status) Active() bool {
	switch s {
	case StatusPreparing, StatusRunning, StatusExpiring, StatusTerminating:
		return true
	default:
		return false
	}
}

// CanTransition reports whether one lifecycle transition is allowed.
func CanTransition(from, to Status) bool {
	switch from {
	case StatusPreparing:
		return to == StatusRunning ||
			to == StatusFailed ||
			to == StatusTerminating
	case StatusRunning:
		return to == StatusExpiring ||
			to == StatusFailed ||
			to == StatusTerminating
	case StatusExpiring:
		return to == StatusRunning ||
			to == StatusFailed ||
			to == StatusTerminating
	case StatusFailed:
		return to == StatusTerminating
	case StatusTerminating:
		return to == StatusFailed || to == StatusTerminated
	default:
		return false
	}
}

// ValidateTransition rejects unknown states and invalid lifecycle changes.
func ValidateTransition(from, to Status) error {
	if !from.Valid() {
		return fmt.Errorf("invalid source lab status %q", from)
	}
	if !to.Valid() {
		return fmt.Errorf("invalid target lab status %q", to)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("lab status cannot transition from %s to %s", from, to)
	}
	return nil
}
