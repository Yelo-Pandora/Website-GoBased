package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/health"
	"website-gobased/internal/httpserver"
	"website-gobased/internal/version"
	"website-gobased/services/platform-api/internal/course"
)

type handler struct {
	logger         *slog.Logger
	database       databasePinger
	labGatewayAddr string
	courses        courseService
}

func newHandler(
	logger *slog.Logger,
	database databasePinger,
	labGatewayAddr string,
	courses courseService,
) *handler {
	return &handler{
		logger:         logger,
		database:       database,
		labGatewayAddr: labGatewayAddr,
		courses:        courses,
	}
}

func (h *handler) listCourses(ctx *gin.Context) {
	courses, err := h.courses.List(ctx.Request.Context())
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "list courses", "error", err)
		httpserver.WriteError(
			ctx,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"unable to load courses",
		)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"data": gin.H{"courses": courses},
		"meta": gin.H{
			"authenticated": false,
			"total":         len(courses),
		},
	})
}

func (h *handler) getCourse(ctx *gin.Context) {
	detail, err := h.courses.Get(ctx.Request.Context(), ctx.Param("slug"))
	if errors.Is(err, course.ErrNotFound) {
		httpserver.WriteError(
			ctx,
			http.StatusNotFound,
			"COURSE_NOT_FOUND",
			"course not found",
		)
		return
	}
	if err != nil {
		h.logger.ErrorContext(ctx.Request.Context(), "get course", "error", err)
		httpserver.WriteError(
			ctx,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"unable to load course",
		)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": gin.H{"course": detail}})
}

func (h *handler) health(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, health.NewResponse("platform-api", "ok", nil))
}

func (h *handler) readiness(ctx *gin.Context) {
	pingCtx, cancel := context.WithTimeout(ctx.Request.Context(), 2*time.Second)
	defer cancel()

	if err := h.database.PingContext(pingCtx); err != nil {
		h.logger.WarnContext(
			ctx.Request.Context(),
			"platform database readiness check failed",
			"error", err,
		)
		ctx.JSON(
			http.StatusServiceUnavailable,
			health.NewResponse(
				"platform-api",
				"not_ready",
				map[string]any{"database": "unavailable"},
			),
		)
		return
	}

	ctx.JSON(
		http.StatusOK,
		health.NewResponse(
			"platform-api",
			"ready",
			map[string]any{"database": "available"},
		),
	)
}

func (h *handler) systemInfo(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"service":        "platform-api",
		"status":         "scaffold",
		"version":        version.Version,
		"commit":         version.Commit,
		"labGatewayAddr": h.labGatewayAddr,
	})
}
