// logger.go 提供了用于配置结构化服务日志的实用函数。
package logging

import (
	"log/slog"
	"os"
)

// New 创建一个新的结构化日志记录器，带有指定的服务名称。
func New(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler).With("service", service)
}
