// Package httpapi provides the platform API HTTP boundary.
package httpapi

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/httpserver"
	"website-gobased/services/platform-api/internal/course"
)

type databasePinger interface {
	PingContext(ctx context.Context) error
}

type courseService interface {
	List(ctx context.Context) ([]course.Course, error)
	Get(ctx context.Context, slug string) (course.Detail, error)
}

// NewRouter builds the platform API routes.
func NewRouter(
	logger *slog.Logger,
	database databasePinger,
	labGatewayAddr string,
	courses courseService,
) *gin.Engine {
	handler := newHandler(logger, database, labGatewayAddr, courses)
	router := httpserver.NewRouter(logger)
	router.GET("/healthz", handler.health)
	router.GET("/readyz", handler.readiness)
	router.GET("/api/v1/system/info", handler.systemInfo)
	api := router.Group("/api/v1")
	api.GET("/courses", handler.listCourses)
	api.GET("/courses/:slug", handler.getCourse)
	return router
}
