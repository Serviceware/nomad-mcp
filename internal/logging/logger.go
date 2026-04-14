package logging

import (
	"io"
	"log/slog"
	"strings"
)

const defaultLevel = slog.LevelInfo

func New(writer io.Writer, level string) *slog.Logger {
	return slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: parseLevel(level)}))
}

func parseLevel(level string) slog.Level {
	if strings.TrimSpace(level) == "" {
		return defaultLevel
	}

	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return defaultLevel
	}

	return parsed
}
