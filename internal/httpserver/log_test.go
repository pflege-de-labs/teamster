package httpserver

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/metrics"
)

// quietLog keeps test output to failures; tests that assert on log lines use newLoggedTestServer.
var quietLog = slog.New(slog.DiscardHandler)

// logBuffer collects what a server logs, for the tests that assert on it.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newLoggedTestServer is newTestServer with its log, at debug, kept for inspection.
func newLoggedTestServer(t *testing.T, st *fakeStore, msg *fakeMessenger) (*http.Server, *logBuffer) {
	t.Helper()

	return newLoggedServer(t, testConfig(), st, msg, slog.LevelDebug)
}

func newLoggedServer(t *testing.T, cfg config.Config, st *fakeStore, msg *fakeMessenger, level slog.Level) (*http.Server, *logBuffer) {
	t.Helper()

	logs := &logBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: level}))
	srv, err := NewServer(logger, cfg, st, msg, nil, msg, metrics.Disabled(), nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv, logs
}

func testConfig() config.Config {
	return config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
}

// withTestLogger is what the logging middleware puts in a request's context.
func withTestLogger(ctx context.Context, logger *slog.Logger, id string) context.Context {
	return context.WithValue(logging.WithLogger(ctx, logger), requestIDKey{}, id)
}
