package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"website-gobased/services/lab-app/internal/httpapi"
)

// Run starts the lab application and blocks until shutdown.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	server := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.NewRouter(logger, httpapi.RuntimeIdentity{
			LabID:        cfg.LabID,
			InstanceID:   cfg.InstanceID,
			ScenarioType: cfg.ScenarioType,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("lab application listening", "address", cfg.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown lab application: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve lab application: %w", err)
	}
}
