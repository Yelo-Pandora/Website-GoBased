package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRouterPreservesValidRequestID(t *testing.T) {
	router := NewRouter(testLogger())
	router.GET("/test", func(ctx *gin.Context) {
		ctx.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	request.Header.Set(requestIDHeader, "request-123")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get(requestIDHeader); got != "request-123" {
		t.Fatalf("request ID = %q; want request-123", got)
	}
}

func TestRouterReplacesInvalidRequestID(t *testing.T) {
	router := NewRouter(testLogger())
	router.GET("/test", func(ctx *gin.Context) {
		ctx.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	request.Header.Set(requestIDHeader, "invalid request ID")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	got := response.Header().Get(requestIDHeader)
	if got == "" || got == "invalid request ID" {
		t.Fatalf("request ID = %q; want generated ID", got)
	}
}

func TestRouterRecoversFromPanic(t *testing.T) {
	router := NewRouter(testLogger())
	router.GET("/panic", func(_ *gin.Context) {
		panic("test panic")
	})

	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusInternalServerError)
	}
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("request ID header is empty")
	}
}

func TestRouterReturnsJSONForUnknownRoute(t *testing.T) {
	router := NewRouter(testLogger())
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusNotFound)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q; want JSON", got)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
