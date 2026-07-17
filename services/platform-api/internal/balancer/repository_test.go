package balancer

import (
	"encoding/json"
	"testing"
)

func TestEncodeAdjustmentPayloadKeepsStructuredServersAndFingerprint(t *testing.T) {
	adjustment := Adjustment{
		TopologyFingerprint: "fingerprint-test",
		Weights: []Weight{
			{InstanceID: "app-1", Weight: 10},
			{InstanceID: "app-2", Weight: 3},
		},
	}
	body, err := encodeAdjustmentPayload(adjustment)
	if err != nil {
		t.Fatalf("encodeAdjustmentPayload() error = %v", err)
	}
	var payload struct {
		ExpectedMode        string `json:"expectedMode"`
		TopologyFingerprint string `json:"topologyFingerprint"`
		Servers             []struct {
			InstanceName string `json:"instanceName"`
			Weight       int    `json:"weight"`
		} `json:"servers"`
		Request struct {
			Weights []Weight `json:"weights"`
		} `json:"request"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.ExpectedMode != "adaptive" ||
		payload.TopologyFingerprint != "fingerprint-test" ||
		len(payload.Servers) != 2 || payload.Servers[1].InstanceName != "app-2" ||
		payload.Request.Weights[1].Weight != 3 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestValidateAdjustmentWeightsRequiresCompleteRelativeSet(t *testing.T) {
	instances := []Instance{{ID: "app-1"}, {ID: "app-2"}}
	if err := validateAdjustmentWeights(instances, []Weight{
		{InstanceID: "app-1", Weight: 10},
		{InstanceID: "app-2", Weight: 3},
	}); err != nil {
		t.Fatalf("validateAdjustmentWeights() error = %v", err)
	}
	if err := validateAdjustmentWeights(instances, []Weight{
		{InstanceID: "app-1", Weight: 10},
	}); err != ErrStale {
		t.Fatalf("incomplete weight error = %v; want ErrStale", err)
	}
}
