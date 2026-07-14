// Package httpapi provides the platform API HTTP boundary.
package httpapi

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/httpserver"
)

type databasePinger interface {
	PingContext(ctx context.Context) error
}

// NewRouter builds the platform API routes.
func NewRouter(
	logger *slog.Logger,
	database databasePinger,
	labGatewayAddr string,
) *gin.Engine {
	handler := newHandler(logger, database, labGatewayAddr)
	router := httpserver.NewRouter(logger)
	router.GET("/healthz", handler.health)
	router.GET("/readyz", handler.readiness)
	router.GET("/api/v1/system/info", handler.systemInfo)
	return router
}
