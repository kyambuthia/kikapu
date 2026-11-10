package obs

import (
	"context"
	"log/slog"
	"os"
)

func NewLogger(level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	return slog.New(handler)
}

func WithRequestID(ctx context.Context, logger *slog.Logger, requestID string) *slog.Logger {
	return logger.With("request_id", requestID)
}
