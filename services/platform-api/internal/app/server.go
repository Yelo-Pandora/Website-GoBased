package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"website-gobased/services/platform-api/internal/course"
	"website-gobased/services/platform-api/internal/httpapi"
)

// Run starts the platform API and blocks until shutdown.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	database, err := sql.Open("mysql", cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("open platform database: %w", err)
	}
	defer database.Close()

	database.SetConnMaxLifetime(3 * time.Minute)
	database.SetMaxIdleConns(5)
	database.SetMaxOpenConns(10)
	if err := waitForDatabase(ctx, database); err != nil {
		return err
	}

	server := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.NewRouter(
			logger,
			database,
			cfg.LabGatewayAddr,
			course.NewService(
				course.NewRepository(database),
				course.NewContentStore(),
			),
		),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("platform API listening", "address", cfg.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown platform API: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve platform API: %w", err)
	}
}

func waitForDatabase(ctx context.Context, database *sql.DB) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	for {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := database.PingContext(pingCtx)
		cancel()
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("wait for platform database: %w", err)
		case <-ticker.C:
		}
	}
}
