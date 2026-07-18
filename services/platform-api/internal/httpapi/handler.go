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
	"website-gobased/internal/protocol"
	"website-gobased/internal/version"
	"website-gobased/services/platform-api/internal/course"
	"website-gobased/services/platform-api/internal/lab"
	"website-gobased/services/platform-api/internal/traffic"
)

type handler struct {
	logger         *slog.Logger
	database       databasePinger
	labGatewayAddr string
	courses        courseService
	labs           labService
	authentication authenticationService
	authConfig     AuthConfig
	traffic        trafficService
	cacheStates    *traffic.Client
}

func newHandler(
	logger *slog.Logger,
	database databasePinger,
	labGatewayAddr string,
	courses courseService,
	labs labService,
	authentication authenticationService,
	authConfig AuthConfig,
	trafficValue trafficService,
) *handler {
	cacheStates, _ := traffic.NewClient(labGatewayAddr, 2*time.Second)
	return &handler{
		logger:         logger,
		database:       database,
		labGatewayAddr: labGatewayAddr,
		courses:        courses,
		labs:           labs,
		authentication: authentication,
		authConfig:     authConfig,
		traffic:        trafficValue,
		cacheStates:    cacheStates,
	}
}

func (h *handler) submitTrafficBatch(ctx *gin.Context) {
	var request protocol.TrafficBatchRequest
	if err := decodeJSON(ctx, &request); err != nil || h.traffic == nil {
		h.writeTrafficError(ctx, traffic.ErrInvalidRequest)
		return
	}
	result, err := h.traffic.Submit(
		ctx.Request.Context(),
		currentLabUserID(ctx),
		ctx.Param("id"),
		request,
	)
	if err != nil {
		h.writeTrafficError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": gin.H{"result": result}})
}

func (h *handler) writeTrafficError(ctx *gin.Context, err error) {
	status := http.StatusServiceUnavailable
	code := "LAB_UNAVAILABLE"
	message := "lab traffic is unavailable"
	switch {
	case errors.Is(err, traffic.ErrInvalidRequest):
		status, code, message = http.StatusBadRequest,
			"TRAFFIC_REQUEST_INVALID", "traffic batch is invalid"
	case errors.Is(err, traffic.ErrNotFound):
		status, code, message = http.StatusNotFound, "LAB_NOT_FOUND", "lab not found"
	case errors.Is(err, traffic.ErrNotRunning):
		status, code, message = http.StatusConflict, "LAB_NOT_RUNNING", "lab is not running"
	case errors.Is(err, traffic.ErrRateLimited):
		status, code, message = http.StatusTooManyRequests, "RATE_LIMITED", "traffic rate limit exceeded"
	case !errors.Is(err, traffic.ErrUnavailable):
		h.logger.ErrorContext(ctx.Request.Context(), "submit traffic batch", "error", err)
	}
	httpserver.WriteError(ctx, status, code, message)
}

func (h *handler) listCourses(ctx *gin.Context) {
	userID := currentUserID(ctx)
	courses, err := h.courses.List(ctx.Request.Context(), userID)
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
			"authenticated": userID != nil,
			"total":         len(courses),
		},
	})
}

func (h *handler) getCourse(ctx *gin.Context) {
	detail, err := h.courses.Get(
		ctx.Request.Context(),
		ctx.Param("slug"),
		currentUserID(ctx),
	)
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

type createLabRequest struct {
	OperationID string `json:"operationId"`
	CourseID    uint64 `json:"courseId"`
}

type labOperationRequest struct {
	OperationID string `json:"operationId"`
}

type labActionRequest struct {
	OperationID      string  `json:"operationId"`
	ActionType       string  `json:"actionType"`
	TargetInstanceID *string `json:"targetInstanceId"`
	Parameters       struct {
		PerformancePercent *int                 `json:"performancePercent,omitempty"`
		Weights            []lab.InstanceWeight `json:"weights,omitempty"`
		BalancingMode      *string              `json:"balancingMode,omitempty"`
	} `json:"parameters"`
}

func (h *handler) createLab(ctx *gin.Context) {
	var request createLabRequest
	if err := decodeJSON(ctx, &request); err != nil {
		h.writeLabError(ctx, lab.ErrInvalidRequest)
		return
	}
	result, err := h.labs.Create(
		ctx.Request.Context(),
		currentLabUserID(ctx),
		request.CourseID,
		request.OperationID,
	)
	if err != nil {
		h.writeLabError(ctx, err)
		return
	}
	ctx.JSON(http.StatusAccepted, gin.H{"data": gin.H{
		"lab": result.Session, "operation": result.Operation,
	}})
}

func (h *handler) getLab(ctx *gin.Context) {
	snapshot, err := h.labs.Snapshot(
		ctx.Request.Context(),
		ctx.Param("id"),
		currentLabUserID(ctx),
	)
	if err != nil {
		h.writeLabError(ctx, err)
		return
	}
	if h.cacheStates != nil && (snapshot.Lab.ScenarioType == "multi_level_cache" || snapshot.Lab.ScenarioType == "cache_failures") && snapshot.Lab.Status == lab.StatusRunning {
		cacheState, cacheErr := h.cacheStates.CacheState(ctx.Request.Context(), snapshot.Lab.ID)
		if cacheErr == nil {
			snapshot.Cache = &cacheState
		}
	}
	ctx.JSON(http.StatusOK, gin.H{"data": snapshot})
}

func (h *handler) resetLab(ctx *gin.Context) {
	var request labOperationRequest
	if err := decodeJSON(ctx, &request); err != nil {
		h.writeLabError(ctx, lab.ErrInvalidRequest)
		return
	}
	result, err := h.labs.Reset(
		ctx.Request.Context(),
		currentLabUserID(ctx),
		ctx.Param("id"),
		request.OperationID,
	)
	if err != nil {
		h.writeLabError(ctx, err)
		return
	}
	ctx.JSON(http.StatusAccepted, gin.H{
		"data": gin.H{"operation": result.Operation},
	})
}

func (h *handler) terminateLab(ctx *gin.Context) {
	var request labOperationRequest
	if err := decodeJSON(ctx, &request); err != nil {
		h.writeLabError(ctx, lab.ErrInvalidRequest)
		return
	}
	result, err := h.labs.Terminate(
		ctx.Request.Context(),
		currentLabUserID(ctx),
		ctx.Param("id"),
		request.OperationID,
	)
	if err != nil {
		h.writeLabError(ctx, err)
		return
	}
	ctx.JSON(http.StatusAccepted, gin.H{
		"data": gin.H{"operation": result.Operation},
	})
}

func (h *handler) submitLabAction(ctx *gin.Context) {
	var request labActionRequest
	if err := decodeJSON(ctx, &request); err != nil {
		h.writeLabError(ctx, lab.ErrInvalidRequest)
		return
	}
	result, err := h.labs.Action(
		ctx.Request.Context(),
		currentLabUserID(ctx),
		ctx.Param("id"),
		lab.ActionInput{
			OperationID:        request.OperationID,
			ActionType:         request.ActionType,
			TargetInstanceID:   request.TargetInstanceID,
			PerformancePercent: request.Parameters.PerformancePercent,
			Weights:            request.Parameters.Weights,
			BalancingMode:      request.Parameters.BalancingMode,
		},
	)
	if err != nil {
		h.writeLabError(ctx, err)
		return
	}
	ctx.JSON(http.StatusAccepted, gin.H{"data": gin.H{"operation": result.Operation}})
}

func (h *handler) writeLabError(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := "INTERNAL_ERROR"
	message := "unable to complete lab request"
	switch {
	case errors.Is(err, lab.ErrInvalidRequest):
		status, code, message = http.StatusBadRequest, "VALIDATION_FAILED", "invalid lab request"
	case errors.Is(err, lab.ErrCourseNotFound):
		status, code, message = http.StatusNotFound, "COURSE_NOT_FOUND", "course not found"
	case errors.Is(err, lab.ErrNotFound), errors.Is(err, lab.ErrNotOwned):
		status, code, message = http.StatusNotFound, "LAB_NOT_FOUND", "lab not found"
	case errors.Is(err, lab.ErrAlreadyActive):
		status, code, message = http.StatusConflict, "LAB_ALREADY_ACTIVE", "an active lab already exists"
	case errors.Is(err, lab.ErrOperationConflict),
		errors.Is(err, lab.ErrBusy), errors.Is(err, lab.ErrStateConflict):
		status, code, message = http.StatusConflict, "LAB_BUSY", "lab has another operation in progress"
	case errors.Is(err, lab.ErrNotRunning):
		status, code, message = http.StatusConflict, "LAB_NOT_RUNNING", "lab is not running"
	case errors.Is(err, lab.ErrInstanceNotFound):
		status, code, message = http.StatusNotFound, "INSTANCE_NOT_FOUND", "lab instance not found"
	case errors.Is(err, lab.ErrMinInstanceLimit):
		status, code, message = http.StatusConflict,
			"MIN_INSTANCE_LIMIT", "at least one instance must remain"
	case errors.Is(err, lab.ErrMaxInstanceLimit):
		status, code, message = http.StatusConflict,
			"MAX_INSTANCE_LIMIT", "maximum instance count reached"
	case errors.Is(err, lab.ErrActionNotAllowed):
		status, code, message = http.StatusUnprocessableEntity,
			"ACTION_NOT_ALLOWED", "lab action is not allowed"
	case errors.Is(err, lab.ErrWeightInvalid):
		status, code, message = http.StatusBadRequest,
			"VALIDATION_FAILED", "instance weights are invalid"
	case errors.Is(err, lab.ErrCapacityExceeded), errors.Is(err, lab.ErrInstanceLimit):
		status, code, message = http.StatusServiceUnavailable,
			"RESOURCE_CAPACITY_EXCEEDED", "lab resource capacity is exhausted"
	case errors.Is(err, lab.ErrLabUnavailable):
		status, code, message = http.StatusServiceUnavailable,
			"DOCKER_UNAVAILABLE", "lab resources are unavailable"
	default:
		h.logger.ErrorContext(ctx.Request.Context(), "complete lab request", "error", err)
	}
	httpserver.WriteError(ctx, status, code, message)
}

func currentLabUserID(ctx *gin.Context) uint64 {
	session, ok := currentSession(ctx)
	if !ok {
		return 0
	}
	return session.User.ID
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
