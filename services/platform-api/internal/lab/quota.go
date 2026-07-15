package lab

import "errors"

var (
	// ErrCapacityExceeded indicates that a global resource limit is exhausted.
	ErrCapacityExceeded = errors.New("lab resource capacity exceeded")
	// ErrInstanceLimit indicates that a lab would exceed its instance limit.
	ErrInstanceLimit = errors.New("lab instance limit exceeded")
)

// Quota contains server-owned admission limits for temporary lab resources.
type Quota struct {
	MaxActiveLabs          int
	MaxTemporaryContainers int
	MaxInstancesPerLab     int
}

// Usage is the current global resource usage observed by the control plane.
type Usage struct {
	ActiveLabs          int
	TemporaryContainers int
}

// Reservation is the capacity required by one requested topology change.
type Reservation struct {
	TemporaryContainers int
	Instances           int
}

// Validate checks that all configured limits are usable.
func (q Quota) Validate() error {
	if q.MaxActiveLabs <= 0 ||
		q.MaxTemporaryContainers <= 0 ||
		q.MaxInstancesPerLab <= 0 {
		return errors.New("lab quota limits must be positive")
	}
	return nil
}

// AdmitCreate validates the initial reservation for a new lab session.
func (q Quota) AdmitCreate(usage Usage, reservation Reservation) error {
	if err := q.Validate(); err != nil {
		return err
	}
	if reservation.Instances <= 0 ||
		reservation.Instances > q.MaxInstancesPerLab {
		return ErrInstanceLimit
	}
	if reservation.TemporaryContainers < reservation.Instances {
		return ErrCapacityExceeded
	}
	if usage.ActiveLabs < 0 || usage.TemporaryContainers < 0 {
		return errors.New("lab resource usage cannot be negative")
	}
	if usage.ActiveLabs+1 > q.MaxActiveLabs ||
		usage.TemporaryContainers+reservation.TemporaryContainers >
			q.MaxTemporaryContainers {
		return ErrCapacityExceeded
	}
	return nil
}

// AdmitInstanceCount checks the resulting application instance count.
func (q Quota) AdmitInstanceCount(current, delta int) error {
	if err := q.Validate(); err != nil {
		return err
	}
	next := current + delta
	if next <= 0 || next > q.MaxInstancesPerLab {
		return ErrInstanceLimit
	}
	return nil
}
