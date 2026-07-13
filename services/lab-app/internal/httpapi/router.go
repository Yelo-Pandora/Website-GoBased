// Package httpapi provides the lab application HTTP boundary.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"website-gobased/internal/health"
)

// RuntimeIdentity identifies a dynamic experiment application instance.
type RuntimeIdentity struct {
	LabID        string `json:"labId"`
	InstanceID   string `json:"instanceId"`
	ScenarioType string `json:"scenarioType"`
}

// NewRouter builds the lab application routes.
func NewRouter(logger *slog.Logger, identity RuntimeIdentity) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if err := health.Write(w, http.StatusOK, health.Response{
			Service: "lab-app",
			Status:  "ok",
		}); err != nil {
			logger.Error("write health response", "error", err)
		}
	})
	mux.HandleFunc("GET /internal/runtime-state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		response := map[string]any{
			"service":  "lab-app",
			"status":   "scaffold",
			"identity": identity,
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			logger.Error("write runtime state response", "error", err)
		}
	})
	return mux
}
