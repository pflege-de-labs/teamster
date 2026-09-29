// Package logging builds the logger the process writes through and carries a
// request's logger in its context.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// New returns a logger writing cfg.Format lines at cfg.Level and above to w.
// Empty values, as in a Config built by hand, mean info and text.
func New(cfg config.LogConfig, w io.Writer) (*slog.Logger, error) {
	level := slog.LevelInfo
	if cfg.Level != "" {
		if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
			return nil, fmt.Errorf("log level %q: %w", cfg.Level, err)
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	switch cfg.Format {
	case "", "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("log format %q: want text or json", cfg.Format)
	}
}

type loggerKey struct{}

// WithLogger returns a context whose FromContext answers logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// FromContext returns the logger WithLogger stored, or slog.Default so that a
// context built outside a request still logs somewhere.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
