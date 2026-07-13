package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"website-gobased/internal/logging"
	"website-gobased/services/orchestrator/internal/app"
	"website-gobased/services/orchestrator/internal/transport/uds"
)

func main() {
	logger := logging.New("orchestrator")
	cfg := app.LoadConfig()

	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		runHealthcheck(cfg.SocketPath, logger)
		return
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if err := uds.Run(ctx, cfg.SocketPath, logger); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "orchestrator stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func runHealthcheck(socketPath string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := uds.CheckHealth(ctx, socketPath); err != nil {
		logger.Error("orchestrator healthcheck failed", "error", err)
		os.Exit(1)
	}
}
