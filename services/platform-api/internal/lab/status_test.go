package lab

import "testing"

func TestStatusActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status Status
		want   bool
	}{
		{StatusPreparing, true},
		{StatusRunning, true},
		{StatusExpiring, true},
		{StatusFailed, false},
		{StatusTerminating, true},
		{StatusTerminated, false},
	}
	for _, test := range tests {
		if got := test.status.Active(); got != test.want {
			t.Errorf("Status(%q).Active() = %t; want %t", test.status, got, test.want)
		}
	}
}

func TestValidateTransition(t *testing.T) {
	t.Parallel()

	allowed := [][2]Status{
		{StatusPreparing, StatusRunning},
		{StatusPreparing, StatusFailed},
		{StatusRunning, StatusExpiring},
		{StatusExpiring, StatusRunning},
		{StatusRunning, StatusTerminating},
		{StatusFailed, StatusTerminating},
		{StatusTerminating, StatusTerminated},
	}
	for _, transition := range allowed {
		if err := ValidateTransition(transition[0], transition[1]); err != nil {
			t.Errorf("ValidateTransition(%q, %q) error = %v", transition[0], transition[1], err)
		}
	}

	rejected := [][2]Status{
		{StatusPreparing, StatusTerminated},
		{StatusRunning, StatusPreparing},
		{StatusFailed, StatusRunning},
		{StatusTerminated, StatusRunning},
		{"unknown", StatusRunning},
	}
	for _, transition := range rejected {
		if err := ValidateTransition(transition[0], transition[1]); err == nil {
			t.Errorf("ValidateTransition(%q, %q) error = nil", transition[0], transition[1])
		}
	}
}
