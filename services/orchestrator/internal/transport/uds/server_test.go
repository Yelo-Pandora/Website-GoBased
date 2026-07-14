package uds

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandRoute(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "invalid JSON",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "multiple JSON values",
			body:       `{}` + `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "valid scaffold command",
			body: `{
				"commandType":"PROVISION_LAB",
				"commandId":"cmd-1",
				"operationId":"op-1",
				"labId":"lab-test",
				"requestedBy":"user-1"
			}`,
			wantStatus: http.StatusNotImplemented,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := newRouter(slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/commands",
				strings.NewReader(test.body),
			)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d; want %d", response.Code, test.wantStatus)
			}
		})
	}
}
