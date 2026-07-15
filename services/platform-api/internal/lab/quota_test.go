package lab

import (
	"errors"
	"testing"
)

func TestQuotaAdmitCreate(t *testing.T) {
	t.Parallel()

	quota := Quota{
		MaxActiveLabs:          10,
		MaxTemporaryContainers: 40,
		MaxInstancesPerLab:     4,
	}
	if err := quota.AdmitCreate(
		Usage{ActiveLabs: 9, TemporaryContainers: 38},
		Reservation{TemporaryContainers: 2, Instances: 1},
	); err != nil {
		t.Fatalf("AdmitCreate() error = %v", err)
	}
	if err := quota.AdmitCreate(
		Usage{ActiveLabs: 10, TemporaryContainers: 20},
		Reservation{TemporaryContainers: 1, Instances: 1},
	); !errors.Is(err, ErrCapacityExceeded) {
		t.Fatalf("AdmitCreate() error = %v; want ErrCapacityExceeded", err)
	}
	if err := quota.AdmitCreate(
		Usage{ActiveLabs: 1, TemporaryContainers: 39},
		Reservation{TemporaryContainers: 2, Instances: 1},
	); !errors.Is(err, ErrCapacityExceeded) {
		t.Fatalf("AdmitCreate() error = %v; want ErrCapacityExceeded", err)
	}
}

func TestQuotaAdmitInstanceCount(t *testing.T) {
	t.Parallel()

	quota := Quota{
		MaxActiveLabs:          10,
		MaxTemporaryContainers: 40,
		MaxInstancesPerLab:     4,
	}
	if err := quota.AdmitInstanceCount(3, 1); err != nil {
		t.Fatalf("AdmitInstanceCount() error = %v", err)
	}
	if err := quota.AdmitInstanceCount(4, 1); !errors.Is(err, ErrInstanceLimit) {
		t.Fatalf("AdmitInstanceCount() error = %v; want ErrInstanceLimit", err)
	}
	if err := quota.AdmitInstanceCount(1, -1); !errors.Is(err, ErrInstanceLimit) {
		t.Fatalf("AdmitInstanceCount() error = %v; want ErrInstanceLimit", err)
	}
}
