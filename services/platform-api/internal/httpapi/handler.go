package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/health"
	"website-gobased/internal/version"
)

type handler struct {
	logger         *slog.Logger
	database       databasePinger
	labGatewayAddr string
}

func newHandler(
	logger *slog.Logger,
	database databasePinger,
	labGatewayAddr string,
) *handler {
	return &handler{
		logger:         logger,
		database:       database,
		labGatewayAddr: labGatewayAddr,
	}
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
