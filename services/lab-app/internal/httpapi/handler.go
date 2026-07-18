package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/health"
	"website-gobased/internal/protocol"
	"website-gobased/services/lab-app/internal/order"
	"website-gobased/services/lab-app/internal/product"
)

type handler struct {
	identity      RuntimeIdentity
	batches       batchProcessor
	cache         cacheStateProvider
	instanceCache cacheInstanceStateProvider
	controls      cacheControlProvider
}

func newHandler(
	identity RuntimeIdentity,
	batches batchProcessor,
	cache cacheStateProvider,
	instanceCache cacheInstanceStateProvider,
	controls cacheControlProvider,
) *handler {
	return &handler{
		identity: identity, batches: batches, cache: cache,
		instanceCache: instanceCache, controls: controls,
	}
}

func (h *handler) cacheAction(ctx *gin.Context) {
	if h.controls == nil {
		writeBatchError(ctx, http.StatusNotFound, "CACHE_NOT_AVAILABLE", "cache runtime is not available")
		return
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 1024)
	var request struct {
		Action string `json:"action"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil ||
		(request.Action != "add_l1" && request.Action != "remove_l1") {
		writeBatchError(ctx, http.StatusBadRequest, "CACHE_ACTION_INVALID", "cache action is invalid")
		return
	}
	state := h.controls.SetL1Enabled(request.Action == "add_l1")
	ctx.JSON(http.StatusOK, gin.H{"data": gin.H{"instance": state}})
}

func (h *handler) submitBatch(ctx *gin.Context) {
	if h.batches == nil {
		writeBatchError(ctx, http.StatusServiceUnavailable, "LAB_UNAVAILABLE", "lab runtime is unavailable")
		return
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4096)
	var request protocol.TrafficBatchRequest
	if err := ctx.ShouldBindJSON(&request); err != nil || !validBatchID(request.BatchID) {
		writeBatchError(ctx, http.StatusBadRequest, "TRAFFIC_REQUEST_INVALID", "traffic batch is invalid")
		return
	}
	result, err := h.batches.Process(ctx.Request.Context(), request)
	if err != nil {
		if errors.Is(err, order.ErrInvalidBatch) || errors.Is(err, product.ErrNotFound) {
			writeBatchError(ctx, http.StatusBadRequest, "TRAFFIC_REQUEST_INVALID", "traffic batch is invalid")
			return
		}
		writeBatchError(ctx, http.StatusServiceUnavailable, "LAB_UNAVAILABLE", "lab runtime is unavailable")
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": gin.H{"result": result}})
}

func validBatchID(value string) bool {
	if len(value) < 4 || len(value) > 64 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func writeBatchError(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
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

func (h *handler) cacheState(ctx *gin.Context) {
	if h.cache == nil {
		writeBatchError(ctx, http.StatusNotFound, "CACHE_NOT_AVAILABLE", "cache runtime is not available")
		return
	}
	state, err := h.cache.State(ctx.Request.Context())
	if err != nil {
		writeBatchError(ctx, http.StatusServiceUnavailable, "LAB_UNAVAILABLE", "cache state is unavailable")
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": gin.H{"cache": state}})
}

func (h *handler) cacheInstanceState(ctx *gin.Context) {
	if h.instanceCache == nil {
		writeBatchError(ctx, http.StatusNotFound, "CACHE_NOT_AVAILABLE", "cache runtime is not available")
		return
	}
	state, err := h.instanceCache.InstanceState(ctx.Request.Context())
	if err != nil {
		writeBatchError(ctx, http.StatusServiceUnavailable, "LAB_UNAVAILABLE", "cache state is unavailable")
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": gin.H{"cache": state}})
}
