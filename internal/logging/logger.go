// Package logging configures structured service logging.
package logging

import (
	"log/slog"
	"os"
)

// New returns a JSON logger carrying the service name.
func New(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler).With("service", service)
}
