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
	processor, err := order.NewProcessor(
		product.NewRepository(database),
		order.NewRepository(database),
		order.Config{
			LabID:             cfg.LabID,
			InstanceID:        cfg.InstanceID,
			EffectiveCapacity: cfg.EffectiveCapacity,
			CapacityWindow:    cfg.CapacityWindow,
		},
	)
	if err != nil {
		return fmt.Errorf("configure order processor: %w", err)
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
