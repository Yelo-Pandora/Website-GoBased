package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/health"
)

type handler struct {
	identity RuntimeIdentity
}

func newHandler(identity RuntimeIdentity) *handler {
	return &handler{identity: identity}
}

func (h *handler) health(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, health.NewResponse("lab-app", "ok", nil))
}

func (h *handler) runtimeState(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"service":  "lab-app",
		"status":   "scaffold",
		"identity": h.identity,
	})
}
