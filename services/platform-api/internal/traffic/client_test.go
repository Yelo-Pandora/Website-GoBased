package traffic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"website-gobased/internal/protocol"
)

func TestClientFixesGatewayHostAndDecodesResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != "lab-test.lab.internal" {
			t.Errorf("Host = %q", request.Host)
		}
		if request.URL.Path != "/internal/order-batch" {
			t.Errorf("Path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{
			"result": validTrafficResult(),
		}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	result, err := client.Submit(context.Background(), "lab-test", protocol.TrafficBatchRequest{
		BatchID: "batch-test", ProductID: 1, RequestUnits: 10,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if result.TargetInstanceID != "app-1" || result.AcceptedUnits != 10 {
		t.Fatalf("result = %#v", result)
	}
}

func validTrafficResult() protocol.TrafficBatchResult {
	observedAt := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	return protocol.TrafficBatchResult{
		BatchID: "batch-test", LabID: "lab-test", Status: "accepted",
		OccurredAt: observedAt, TargetInstanceID: "app-1",
		ReceivedUnits: 10, AcceptedUnits: 10,
		InstanceState: protocol.InstanceState{
			ProcessingSpeed: 20, MaxLoad: 100, CurrentLoad: 10,
			LoadRatio: 0.1, LoadState: "idle", ObservedAt: observedAt,
		},
	}
}
