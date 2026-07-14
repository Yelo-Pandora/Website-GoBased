// Package httpapi provides the lab application HTTP boundary.
package httpapi

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/httpserver"
)

// RuntimeIdentity identifies a running lab application instance.
type RuntimeIdentity struct {
	LabID        string `json:"labId"`
	InstanceID   string `json:"instanceId"`
	ScenarioType string `json:"scenarioType"`
}

// NewRouter builds the lab application routes.
func NewRouter(logger *slog.Logger, identity RuntimeIdentity) *gin.Engine {
	handler := newHandler(identity)
	router := httpserver.NewRouter(logger)
	router.GET("/healthz", handler.health)
	router.GET("/internal/runtime-state", handler.runtimeState)
	return router
}
