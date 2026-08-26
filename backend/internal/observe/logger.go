package observe

import (
	"io"
	"log/slog"
	"strings"
)

func NewLogger(output io.Writer, level string) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: logLevel}))
}
