package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/config"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      config.LogConfig
		wantErr  bool
		wantLine string
		wantNone bool
	}{
		{name: "text at info", cfg: config.LogConfig{Level: "info", Format: "text"}, wantLine: "level=INFO msg=hello"},
		{name: "json at info", cfg: config.LogConfig{Level: "info", Format: "json"}, wantLine: `"msg":"hello"`},
		{name: "empty format is text", cfg: config.LogConfig{Level: "info"}, wantLine: "msg=hello"},
		{name: "empty level is info", cfg: config.LogConfig{Format: "text"}, wantLine: "level=INFO msg=hello"},
		{name: "warn drops info", cfg: config.LogConfig{Level: "warn", Format: "text"}, wantNone: true},
		{name: "unknown level", cfg: config.LogConfig{Level: "loud", Format: "text"}, wantErr: true},
		{name: "unknown format", cfg: config.LogConfig{Level: "info", Format: "xml"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			logger, err := New(tt.cfg, &out)
			if tt.wantErr {
				if err == nil {
					t.Fatal("New() error = nil, want one")
				}
				return
			}
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			logger.Info("hello")
			if tt.wantNone {
				if out.Len() != 0 {
					t.Errorf("wrote %q, want nothing", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), tt.wantLine) {
				t.Errorf("wrote %q, want it to contain %q", out.String(), tt.wantLine)
			}
		})
	}
}

func TestFromContext(t *testing.T) {
	t.Parallel()

	if got := FromContext(context.Background()); got != slog.Default() {
		t.Errorf("FromContext(empty) = %v, want slog.Default()", got)
	}
	logger := slog.New(slog.DiscardHandler)
	if got := FromContext(WithLogger(context.Background(), logger)); got != logger {
		t.Errorf("FromContext() = %v, want the stored logger", got)
	}
}
