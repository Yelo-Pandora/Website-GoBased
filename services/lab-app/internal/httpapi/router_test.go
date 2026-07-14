package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
