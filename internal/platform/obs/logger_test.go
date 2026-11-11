package obs

import (
	"context"
	"log/slog"
	"testing"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
	}{
		{
			name:  "info level",
			level: slog.LevelInfo,
		},
		{
			name:  "debug level",
			level: slog.LevelDebug,
		},
		{
			name:  "warn level",
			level: slog.LevelWarn,
		},
		{
			name:  "error level",
			level: slog.LevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLogger(tt.level)
			if logger == nil {
				t.Error("NewLogger() returned nil")
			}
		})
	}
}

func TestWithRequestID(t *testing.T) {
	logger := NewLogger(slog.LevelInfo)
	ctx := context.Background()

	loggerWithID := WithRequestID(ctx, logger, "test-request-123")
	if loggerWithID == nil {
		t.Error("WithRequestID() returned nil")
	}
}
