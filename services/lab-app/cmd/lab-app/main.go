package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"website-gobased/internal/logging"
	"website-gobased/services/lab-app/internal/app"
)

func main() {
	logger := logging.New("lab-app")
	cfg := app.LoadConfig()
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(cfg.Addr); err != nil {
			logger.Error("lab application healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}
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

func runHealthcheck(addr string) error {
	if len(addr) > 0 && addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"http://"+addr+"/healthz",
		nil,
	)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("request health endpoint: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint status: %s", response.Status)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return fmt.Errorf("read health response: %w", err)
	}
	return nil
}
