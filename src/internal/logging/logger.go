package logging

import (
	"io"
	"log/slog"
	"strings"
)

func New(environment string, output io.Writer) *slog.Logger {
	options := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	if strings.EqualFold(
		strings.TrimSpace(environment),
		"production",
	) {
		return slog.New(
			slog.NewJSONHandler(output, options),
		)
	}

	return slog.New(
		slog.NewTextHandler(output, options),
	)
}
