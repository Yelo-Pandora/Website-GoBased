// Package httpapi provides the platform API HTTP boundary.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"website-gobased/internal/health"
	"website-gobased/internal/version"
)

type databasePinger interface {
	PingContext(ctx context.Context) error
}

// NewRouter builds the platform API routes.
func NewRouter(logger *slog.Logger, database databasePinger, labGatewayAddr string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if err := health.Write(w, http.StatusOK, health.Response{
			Service: "platform-api",
			Status:  "ok",
		}); err != nil {
			logger.Error("write health response", "error", err)
		}
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := database.PingContext(ctx); err != nil {
			if writeErr := health.Write(w, http.StatusServiceUnavailable, health.Response{
				Service: "platform-api",
				Status:  "not_ready",
				Details: map[string]any{"database": "unavailable"},
			}); writeErr != nil {
				logger.Error("write readiness failure", "error", writeErr)
			}
			return
		}

		if err := health.Write(w, http.StatusOK, health.Response{
			Service: "platform-api",
			Status:  "ready",
			Details: map[string]any{"database": "available"},
		}); err != nil {
			logger.Error("write readiness response", "error", err)
		}
	})
	mux.HandleFunc("GET /api/v1/system/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		response := map[string]any{
			"service":        "platform-api",
			"status":         "scaffold",
			"version":        version.Version,
			"commit":         version.Commit,
			"labGatewayAddr": labGatewayAddr,
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			logger.Error("write system info response", "error", err)
		}
	})

	return mux
}
