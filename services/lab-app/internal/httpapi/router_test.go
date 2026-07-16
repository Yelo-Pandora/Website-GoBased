package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"website-gobased/internal/protocol"
)

type batchProcessorStub struct {
	result  protocol.TrafficBatchResult
	request protocol.TrafficBatchRequest
}

func (s *batchProcessorStub) Process(
	_ context.Context,
	request protocol.TrafficBatchRequest,
) (protocol.TrafficBatchResult, error) {
	s.request = request
	return s.result, nil
}

func TestRuntimeState(t *testing.T) {
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		RuntimeIdentity{
			LabID:        "lab-test",
			InstanceID:   "app-1",
			ScenarioType: "application_cluster",
		},
	)
	request := httptest.NewRequest(http.MethodGet, "/internal/runtime-state", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"instanceId":"app-1"`) {
		t.Fatalf("body = %q; want instance identity", response.Body.String())
	}
}

func TestSubmitBatchReturnsInstanceResult(t *testing.T) {
	processor := &batchProcessorStub{result: protocol.TrafficBatchResult{
		BatchID: "batch-test", LabID: "lab-test", Status: "processed",
		OccurredAt:       time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC),
		TargetInstanceID: "app-1", ReceivedUnits: 10, ProcessedUnits: 10,
		InstanceState: protocol.InstanceState{EffectiveCapacity: 100, RemainingCapacity: 90},
	}}
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		RuntimeIdentity{LabID: "lab-test", InstanceID: "app-1"},
		processor,
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/order-batch",
		strings.NewReader(`{"batchId":"batch-test","productId":1,"requestUnits":10}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if processor.request.RequestUnits != 10 ||
		!strings.Contains(response.Body.String(), `"targetInstanceId":"app-1"`) {
		t.Fatalf("request=%#v body=%q", processor.request, response.Body.String())
	}
}

func TestSubmitBatchRejectsInvalidInput(t *testing.T) {
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		RuntimeIdentity{LabID: "lab-test", InstanceID: "app-1"},
		&batchProcessorStub{},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/order-batch",
		strings.NewReader(`{"batchId":"bad id","productId":1,"requestUnits":10}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest ||
		!strings.Contains(response.Body.String(), "TRAFFIC_REQUEST_INVALID") {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}
