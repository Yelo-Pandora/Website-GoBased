package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type pingerStub struct {
	err error
}

func (p pingerStub) PingContext(_ context.Context) error {
	return p.err
}

func TestReadiness(t *testing.T) {
	tests := []struct {
		name       string
		pingError  error
		wantStatus int
	}{
		{
			name:       "database available",
			wantStatus: http.StatusOK,
		},
		{
			name:       "database unavailable",
			pingError:  errors.New("database unavailable"),
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := NewRouter(
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				pingerStub{err: test.pingError},
				"http://lab-gateway:8080",
			)
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d; want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestSystemInfo(t *testing.T) {
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pingerStub{},
		"http://lab-gateway:8080",
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
	}
}
