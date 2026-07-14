// Package httpserver provides shared Gin server middleware.
package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	requestIDHeader = "X-Request-ID"
	requestIDKey    = "requestId"
	maxRequestIDLen = 128
)

var fallbackRequestIDCounter atomic.Uint64

// NewRouter returns a Gin engine with the shared platform middleware.
func NewRouter(logger *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(
		requestIDMiddleware(),
		securityHeadersMiddleware(),
		accessLogMiddleware(logger),
		recoveryMiddleware(logger),
	)
	router.NoRoute(func(ctx *gin.Context) {
		WriteError(
			ctx,
			http.StatusNotFound,
			"ROUTE_NOT_FOUND",
			"route not found",
		)
	})
	router.NoMethod(func(ctx *gin.Context) {
		WriteError(
			ctx,
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"method not allowed",
		)
	})
	return router
}

// RequestID returns the request ID stored by the shared middleware.
func RequestID(ctx *gin.Context) string {
	requestID, ok := ctx.Get(requestIDKey)
	if !ok {
		return ""
	}
	value, ok := requestID.(string)
	if !ok {
		return ""
	}
	return value
}

// WriteError writes the shared JSON error envelope and stops the handler chain.
func WriteError(ctx *gin.Context, status int, code, message string) {
	ctx.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
		"requestId": RequestID(ctx),
	})
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		requestID := strings.TrimSpace(ctx.GetHeader(requestIDHeader))
		if !validRequestID(requestID) {
			requestID = newRequestID()
		}
		ctx.Set(requestIDKey, requestID)
		ctx.Header(requestIDHeader, requestID)
		ctx.Next()
	}
}

func validRequestID(requestID string) bool {
	if requestID == "" || len(requestID) > maxRequestIDLen {
		return false
	}
	for _, r := range requestID {
		if r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '-', '_', '.':
			continue
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return fmt.Sprintf(
		"fallback-%d-%d",
		time.Now().UTC().UnixNano(),
		fallbackRequestIDCounter.Add(1),
	)
}

func securityHeadersMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("Referrer-Policy", "no-referrer")
		ctx.Header("X-Content-Type-Options", "nosniff")
		ctx.Header("X-Frame-Options", "DENY")
		ctx.Next()
	}
}

func accessLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		startedAt := time.Now()
		ctx.Next()

		path := ctx.FullPath()
		if path == "" {
			path = ctx.Request.URL.Path
		}
		logger.InfoContext(
			ctx.Request.Context(),
			"HTTP request",
			"requestId", RequestID(ctx),
			"method", ctx.Request.Method,
			"path", path,
			"status", ctx.Writer.Status(),
			"durationMs", time.Since(startedAt).Milliseconds(),
		)
	}
}

func recoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(
					ctx.Request.Context(),
					"panic recovered from HTTP handler",
					"requestId", RequestID(ctx),
					"panic", recovered,
				)
				WriteError(
					ctx,
					http.StatusInternalServerError,
					"INTERNAL_ERROR",
					"internal server error",
				)
			}
		}()
		ctx.Next()
	}
}
