// Package httpapi provides the lab application HTTP boundary.
package httpapi

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/httpserver"
	"website-gobased/internal/protocol"
)

type batchProcessor interface {
	Process(
		ctx context.Context,
		request protocol.TrafficBatchRequest,
	) (protocol.TrafficBatchResult, error)
}

// RuntimeIdentity identifies a running lab application instance.
type RuntimeIdentity struct {
	LabID        string `json:"labId"`
	InstanceID   string `json:"instanceId"`
	ScenarioType string `json:"scenarioType"`
}

// NewRouter builds the lab application routes.
func NewRouter(
	logger *slog.Logger,
	identity RuntimeIdentity,
	batches ...batchProcessor,
) *gin.Engine {
	var processor batchProcessor
	if len(batches) > 0 {
		processor = batches[0]
	}
	handler := newHandler(identity, processor)
	router := httpserver.NewRouter(logger)
	router.GET("/healthz", handler.health)
	router.GET("/internal/runtime-state", handler.runtimeState)
	router.POST("/internal/order-batch", handler.submitBatch)
	return router
}
