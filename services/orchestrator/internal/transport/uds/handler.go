package uds

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"website-gobased/internal/health"
	"website-gobased/internal/protocol"
)

type handler struct {
	logger   *slog.Logger
	executor commandExecutor
}

type commandExecutor interface {
	Execute(ctx context.Context, command protocol.Command) protocol.CommandResponse
}

func newHandler(logger *slog.Logger, executor commandExecutor) *handler {
	return &handler{logger: logger, executor: executor}
}

func (h *handler) health(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, health.NewResponse("orchestrator", "ok", nil))
}

func (h *handler) command(ctx *gin.Context) {
	ctx.Request.Body = http.MaxBytesReader(
		ctx.Writer,
		ctx.Request.Body,
		maxCommandBodyBytes,
	)
	defer func() {
		if err := ctx.Request.Body.Close(); err != nil {
			h.logger.WarnContext(
				ctx.Request.Context(),
				"close command request body",
				"error", err,
			)
		}
	}()

	var command protocol.Command
	decoder := json.NewDecoder(ctx.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil {
		writeCommandError(
			ctx,
			http.StatusBadRequest,
			command,
			"INVALID_COMMAND",
			"invalid command body",
		)
		return
	}
	if err := ensureJSONBodyConsumed(decoder); err != nil {
		writeCommandError(
			ctx,
			http.StatusBadRequest,
			command,
			"INVALID_COMMAND",
			"invalid command body",
		)
		return
	}

	response := h.executor.Execute(ctx.Request.Context(), command)
	statusCode := http.StatusOK
	if response.Status == "rejected" {
		statusCode = http.StatusUnprocessableEntity
	}
	ctx.JSON(statusCode, response)
}

func ensureJSONBodyConsumed(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("multiple JSON values in command body")
}

func writeCommandError(
	ctx *gin.Context,
	statusCode int,
	command protocol.Command,
	code string,
	message string,
) {
	ctx.JSON(statusCode, protocol.CommandResponse{
		CommandID:   command.CommandID,
		OperationID: command.OperationID,
		Status:      "rejected",
		Error: map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
