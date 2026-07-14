// router.go 作用是定义实验室应用程序的 HTTP 路由。
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"website-gobased/internal/health"
)

// RuntimeIdentity 作用是表示实验室应用程序的运行时身份信息，包括实验 ID、容器实例 ID 和场景类型（例如application_cluster）。
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
