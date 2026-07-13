package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"website-gobased/internal/logging"
	"website-gobased/services/lab-app/internal/app"
)

func main() {
	logger := logging.New("lab-app")
	cfg := app.LoadConfig()
	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "lab application stopped", slog.Any("error", err))
		os.Exit(1)
	}
}
