package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"website-gobased/internal/logging"
	"website-gobased/services/platform-api/internal/app"
)

func main() {
	logger := logging.New("platform-api")
	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "platform API stopped", slog.Any("error", err))
		os.Exit(1)
	}
}
