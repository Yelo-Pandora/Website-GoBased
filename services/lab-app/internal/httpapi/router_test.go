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
	result    protocol.TrafficBatchResult
	request   protocol.TrafficBatchRequest
	l1State   protocol.CacheInstanceState
	l1Enabled bool
}

func (s *batchProcessorStub) Process(
	_ context.Context,
	request protocol.TrafficBatchRequest,
) (protocol.TrafficBatchResult, error) {
	s.request = request
	return s.result, nil
}

func (s *batchProcessorStub) SetL1Enabled(enabled bool) protocol.CacheInstanceState {
	s.l1Enabled = enabled
	return s.l1State
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
		BatchID: "batch-test", LabID: "lab-test", Status: "accepted",
		OccurredAt:       time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC),
		TargetInstanceID: "app-1", ReceivedUnits: 10, AcceptedUnits: 10,
		InstanceState: protocol.InstanceState{
			ProcessingSpeed: 20, MaxLoad: 100, CurrentLoad: 10,
		},
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

func TestCacheActionTargetsLocalL1(t *testing.T) {
	processor := &batchProcessorStub{l1State: protocol.CacheInstanceState{
		InstanceID: "app-2", Status: "disabled",
	}}
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		RuntimeIdentity{LabID: "lab-test", InstanceID: "app-2"},
		processor,
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/cache-actions",
		strings.NewReader(`{"action":"remove_l1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || processor.l1Enabled ||
		!strings.Contains(response.Body.String(), `"instanceId":"app-2"`) {
		t.Fatalf("status=%d enabled=%t body=%q", response.Code, processor.l1Enabled, response.Body.String())
	}
}
