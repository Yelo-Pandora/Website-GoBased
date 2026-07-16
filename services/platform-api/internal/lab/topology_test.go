package lab

import "testing"

func TestValidateWeightsUsesRelativeCompleteSet(t *testing.T) {
	instances := []Instance{
		{ID: "app-1", Name: "app-1"},
		{ID: "app-2", Name: "app-2"},
	}
	servers, err := validateWeights(instances, []InstanceWeight{
		{InstanceID: "app-1", Weight: 20},
		{InstanceID: "app-2", Weight: 80},
	})
	if err != nil {
		t.Fatalf("validateWeights() error = %v", err)
	}
	if len(servers) != 2 || servers[0].Weight != 20 || servers[1].Weight != 80 {
		t.Fatalf("servers = %#v", servers)
	}
	if _, err := validateWeights(instances, []InstanceWeight{
		{InstanceID: "app-1", Weight: 100},
	}); err != ErrWeightInvalid {
		t.Fatalf("incomplete weights error = %v", err)
	}
}

func TestNextInstanceNameReusesAvailableSlot(t *testing.T) {
	instances := []Instance{{Name: "app-1"}, {Name: "app-3"}}
	if got := nextInstanceName(instances, 4); got != "app-2" {
		t.Fatalf("nextInstanceName() = %q; want app-2", got)
	}
}
