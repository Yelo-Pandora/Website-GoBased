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

type cacheStateProvider interface {
	State(ctx context.Context) (protocol.CacheState, error)
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
	var cacheState cacheStateProvider
	if len(batches) > 0 {
		processor = batches[0]
		cacheState, _ = batches[0].(cacheStateProvider)
	}
	handler := newHandler(identity, processor, cacheState)
	router := httpserver.NewRouter(logger)
	router.GET("/healthz", handler.health)
	router.GET("/internal/runtime-state", handler.runtimeState)
	router.POST("/internal/order-batch", handler.submitBatch)
	router.GET("/internal/cache-state", handler.cacheState)
	return router
}
