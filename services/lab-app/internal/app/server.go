// server.go 作用是定义实验室应用程序的服务器启动和运行逻辑。它创建一个 HTTP 服务器，配置路由和超时设置，并处理应用程序的启动和优雅关闭。
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"website-gobased/internal/protocol"
	cacheprocessor "website-gobased/services/lab-app/internal/cache"
	"website-gobased/services/lab-app/internal/httpapi"
	"website-gobased/services/lab-app/internal/order"
	"website-gobased/services/lab-app/internal/product"
)

// Run 启动实验室应用程序的 HTTP 服务器，参数包括上下文、配置和日志记录器。它会监听指定的地址，并在接收到终止信号时优雅地关闭服务器。
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	database, err := sql.Open("mysql", cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("open lab database: %w", err)
	}
	defer database.Close()
	database.SetConnMaxLifetime(3 * time.Minute)
	database.SetMaxIdleConns(2)
	database.SetMaxOpenConns(4)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = database.PingContext(pingCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("connect lab database: %w", err)
	}
	productRepository := product.NewRepository(database)
	var processor interface {
		Process(context.Context, protocol.TrafficBatchRequest) (protocol.TrafficBatchResult, error)
	}
	var closeProcessor func() error
	if cfg.ScenarioType == "multi_level_cache" || cfg.ScenarioType == "cache_failures" {
		cacheProcessor, cacheErr := cacheprocessor.NewProcessor(
			productRepository,
			cfg.RedisAddr,
			cacheprocessor.Config{
				LabID: cfg.LabID, InstanceID: cfg.InstanceID,
				InstanceCount:   cfg.CacheInstanceCount,
				ProcessingSpeed: cfg.ProcessingSpeed, MaxLoad: cfg.MaxLoad,
				L1MaxEntries: cfg.CacheL1MaxEntries, L1TTL: cfg.CacheL1TTL,
				L2TTL: cfg.CacheL2TTL, L2JitterPercent: cfg.CacheL2JitterPercent,
				LatencyL1MS: cfg.CacheLatencyL1MS, LatencyRedisMS: cfg.CacheLatencyRedisMS,
				LatencyMySQLMS:    cfg.CacheLatencyMySQLMS,
				LatencyDegradedMS: cfg.CacheLatencyDegradedMS,
			},
		)
		if cacheErr != nil {
			return fmt.Errorf("configure cache processor: %w", cacheErr)
		}
		processor = cacheProcessor
		closeProcessor = cacheProcessor.Close
	} else {
		orderProcessor, orderErr := order.NewProcessor(
			productRepository,
			order.NewRepository(database),
			order.Config{
				LabID: cfg.LabID, InstanceID: cfg.InstanceID,
				ProcessingSpeed: cfg.ProcessingSpeed, MaxLoad: cfg.MaxLoad,
			},
		)
		if orderErr != nil {
			return fmt.Errorf("configure order processor: %w", orderErr)
		}
		processor = orderProcessor
	}
	if closeProcessor != nil {
		defer closeProcessor()
	}
	server := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.NewRouter(logger, httpapi.RuntimeIdentity{
			LabID:        cfg.LabID,
			InstanceID:   cfg.InstanceID,
			ScenarioType: cfg.ScenarioType,
		}, processor),
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
