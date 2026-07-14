package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"website-gobased/services/platform-api/internal/course"
)

type pingerStub struct {
	err error
}

type courseServiceStub struct{}

func (courseServiceStub) List(_ context.Context) ([]course.Course, error) {
	return []course.Course{{ID: 1, Slug: "standalone-architecture"}}, nil
}

func (courseServiceStub) Get(_ context.Context, slug string) (course.Detail, error) {
	if slug == "missing" {
		return course.Detail{}, course.ErrNotFound
	}
	return course.Detail{Course: course.Course{ID: 1, Slug: slug}}, nil
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
				courseServiceStub{},
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
		courseServiceStub{},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
	}
}

func TestCourseRoutes(t *testing.T) {
	router := NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pingerStub{},
		"http://lab-gateway:8080",
		courseServiceStub{},
	)

	tests := []struct {
		path       string
		wantStatus int
	}{
		{path: "/api/v1/courses", wantStatus: http.StatusOK},
		{path: "/api/v1/courses/standalone-architecture", wantStatus: http.StatusOK},
		{path: "/api/v1/courses/missing", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.wantStatus {
			t.Errorf("GET %s status = %d; want %d", test.path, response.Code, test.wantStatus)
		}
	}
}
