// Package httpapi provides the platform API HTTP boundary.
package httpapi

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/httpserver"
	"website-gobased/services/platform-api/internal/course"
	"website-gobased/services/platform-api/internal/lab"
)

type databasePinger interface {
	PingContext(ctx context.Context) error
}

type courseService interface {
	List(ctx context.Context, userID *uint64) ([]course.Course, error)
	Get(ctx context.Context, slug string, userID *uint64) (course.Detail, error)
}

type labService interface {
	Create(
		ctx context.Context,
		userID uint64,
		courseID uint64,
		operationID string,
	) (lab.CreateResult, error)
	Snapshot(ctx context.Context, labID string, userID uint64) (lab.Snapshot, error)
	Reset(
		ctx context.Context,
		userID uint64,
		labID string,
		operationID string,
	) (lab.ActionResult, error)
	Terminate(
		ctx context.Context,
		userID uint64,
		labID string,
		operationID string,
	) (lab.ActionResult, error)
}

// NewRouter builds the platform API routes.
func NewRouter(
	logger *slog.Logger,
	database databasePinger,
	labGatewayAddr string,
	courses courseService,
	labs labService,
	authentication authenticationService,
	authConfig AuthConfig,
) *gin.Engine {
	handler := newHandler(
		logger,
		database,
		labGatewayAddr,
		courses,
		labs,
		authentication,
		authConfig,
	)
	router := httpserver.NewRouter(logger)
	router.GET("/healthz", handler.health)
	router.GET("/readyz", handler.readiness)
	router.GET("/api/v1/system/info", handler.systemInfo)
	api := router.Group("/api/v1")
	api.POST("/auth/login", handler.login)
	api.GET(
		"/auth/me",
		handler.optionalAuthentication,
		handler.requireAuthentication,
		handler.currentUser,
	)
	api.POST(
		"/auth/logout",
		handler.optionalAuthentication,
		handler.requireAuthentication,
		handler.requireCSRF,
		handler.logout,
	)
	api.GET("/courses", handler.optionalAuthentication, handler.listCourses)
	api.GET("/courses/:slug", handler.optionalAuthentication, handler.getCourse)
	api.POST(
		"/labs",
		handler.optionalAuthentication,
		handler.requireAuthentication,
		handler.requireCSRF,
		handler.createLab,
	)
	api.GET(
		"/labs/:id",
		handler.optionalAuthentication,
		handler.requireAuthentication,
		handler.getLab,
	)
	api.POST(
		"/labs/:id/reset",
		handler.optionalAuthentication,
		handler.requireAuthentication,
		handler.requireCSRF,
		handler.resetLab,
	)
	api.DELETE(
		"/labs/:id",
		handler.optionalAuthentication,
		handler.requireAuthentication,
		handler.requireCSRF,
		handler.terminateLab,
	)
	return router
}
