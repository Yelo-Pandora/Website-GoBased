// Package uds provides the orchestrator Unix Domain Socket transport.
package uds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/httpserver"
)

const maxCommandBodyBytes = 1 << 20

// Run listens on a Unix Domain Socket until the context is canceled.
func Run(
	ctx context.Context,
	socketPath string,
	logger *slog.Logger,
	executor commandExecutor,
) error {
	listener, err := listen(socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	server := &http.Server{
		Handler:           newRouter(logger, executor),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("orchestrator listening", "socketPath", socketPath)
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown orchestrator: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve orchestrator: %w", err)
	}
}

// CheckHealth checks the orchestrator health endpoint over UDS.
func CheckHealth(ctx context.Context, socketPath string) error {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 2 * time.Second}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://orchestrator/healthz", nil)
	if err != nil {
		return fmt.Errorf("create orchestrator health request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request orchestrator health: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("orchestrator health status: %s", response.Status)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return fmt.Errorf("read orchestrator health response: %w", err)
	}
	return nil
}

func listen(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o750); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refuse to replace non-socket path %s", socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect socket path: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on Unix socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		listener.Close()
		return nil, fmt.Errorf("set socket permissions: %w", err)
	}
	return listener, nil
}

func newRouter(logger *slog.Logger, executor commandExecutor) *gin.Engine {
	handler := newHandler(logger, executor)
	router := httpserver.NewRouter(logger)
	router.GET("/healthz", handler.health)
	router.POST("/v1/commands", handler.command)
	return router
}
