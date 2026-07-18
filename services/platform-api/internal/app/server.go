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

	platformauth "website-gobased/services/platform-api/internal/auth"
	"website-gobased/services/platform-api/internal/balancer"
	"website-gobased/services/platform-api/internal/course"
	"website-gobased/services/platform-api/internal/httpapi"
	"website-gobased/services/platform-api/internal/lab"
	"website-gobased/services/platform-api/internal/lifecycle"
	"website-gobased/services/platform-api/internal/operation"
	"website-gobased/services/platform-api/internal/orchestrator"
	"website-gobased/services/platform-api/internal/traffic"
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
	authentication := platformauth.NewService(
		platformauth.NewRepository(database),
		platformauth.NewLimiter(
			cfg.AuthLoginWindow,
			cfg.AuthLoginMaxFailures,
			cfg.AuthLoginBlockDuration,
			cfg.AuthLoginMaxEntries,
		),
		cfg.AuthSessionTTL,
	)
	orchestratorClient := orchestrator.NewClient(
		cfg.OrchestratorSocketPath,
		cfg.LabOrchestratorTimeout,
	)
	defer orchestratorClient.CloseIdleConnections()
	cacheRuntimeClient, err := traffic.NewClient(cfg.LabGatewayAddr, cfg.TrafficRequestTimeout)
	if err != nil {
		return fmt.Errorf("configure cache runtime client: %w", err)
	}
	defer cacheRuntimeClient.CloseIdleConnections()
	operationWorker, err := operation.NewWorker(
		logger,
		operation.NewRepository(database),
		orchestratorClient,
		operation.WorkerConfig{
			Owner:          cfg.LabOperationWorkerID,
			PollInterval:   cfg.LabOperationPoll,
			LeaseDuration:  cfg.LabOperationLease,
			CommandTimeout: cfg.LabOrchestratorTimeout,
		},
		cacheRuntimeClient,
	)
	if err != nil {
		return fmt.Errorf("configure lab operation worker: %w", err)
	}
	go operationWorker.Run(ctx)
	lifecycleScheduler, err := lifecycle.NewScheduler(
		logger,
		lifecycle.NewRepository(database),
		orchestratorClient,
		lifecycle.SchedulerConfig{
			LifecyclePoll:     cfg.LabLifecyclePoll,
			ReconcileInterval: cfg.LabReconcileInterval,
			ReconcileCleanup:  cfg.LabReconcileCleanup,
			Deadlines: lifecycle.Config{
				IdleTimeout:  cfg.LabIdleTimeout,
				MaxDuration:  cfg.LabMaxDuration,
				ExpiringLead: cfg.LabExpiringLead,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("configure lab lifecycle scheduler: %w", err)
	}
	go lifecycleScheduler.Run(ctx)
	labs := lab.NewService(lab.NewRepository(database), lab.Quota{
		MaxActiveLabs:          cfg.LabMaxActive,
		MaxTemporaryContainers: cfg.LabMaxTempContainers,
		MaxInstancesPerLab:     cfg.LabMaxInstances,
	}, lab.LifetimeConfig{
		IdleTimeout: cfg.LabIdleTimeout,
		MaxDuration: cfg.LabMaxDuration,
	})
	adaptiveController, err := balancer.NewController(
		logger,
		balancer.NewRepository(database),
		balancer.DefaultConfig(),
	)
	if err != nil {
		return fmt.Errorf("configure adaptive balancer: %w", err)
	}
	labs.SetSnapshotDecorator(adaptiveController)
	go adaptiveController.Run(ctx)
	trafficClient, err := traffic.NewClient(cfg.LabGatewayAddr, cfg.TrafficRequestTimeout)
	if err != nil {
		return fmt.Errorf("configure traffic client: %w", err)
	}
	defer trafficClient.CloseIdleConnections()
	trafficService := traffic.NewService(
		labs,
		trafficClient,
		traffic.NewLimiter(
			cfg.TrafficRatePerSecond,
			cfg.TrafficBurst,
			cfg.TrafficLimiterEntries,
		),
		adaptiveController,
	)

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
			labs,
			authentication,
			httpapi.AuthConfig{
				CookieName:   cfg.AuthCookieName,
				CookieSecure: cfg.AuthCookieSecure,
			},
			trafficService,
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
