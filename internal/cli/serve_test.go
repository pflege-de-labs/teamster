package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
)

func validConfig(t *testing.T) *config.Config {
	t.Helper()

	return &config.Config{
		Server:   config.ServerConfig{Addr: "127.0.0.1:0", ShutdownTimeout: 5 * time.Second},
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "serve.db")},
		Webhook:  config.WebhookConfig{Token: "token"},
		Admin:    config.AdminConfig{Username: "admin", Password: "pass"},
		Graph: config.GraphConfig{
			TenantID:     "tenant",
			ClientID:     "client",
			ClientSecret: "secret",
			BaseURL:      "http://127.0.0.1:1",
			TimeoutSec:   1,
		},
	}
}

// freePort returns a port that was free a moment ago, so the test can reach the
// server it starts. The server binds it again immediately afterwards.
func freePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

func waitForServer(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server never accepted a connection on %s", addr)
}

func TestServeShutsDownWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()

	cfg := validConfig(t)
	cfg.Server.Addr = freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- (&ServeCmd{}).Run(ctx, cfg) }()

	waitForServer(t, cfg.Server.Addr)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(fmt.Sprintf("http://%s/admin", cfg.Server.Addr))
	if err != nil {
		t.Fatalf("GET /admin: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("GET /admin = %d, want a redirect to the login page", resp.StatusCode)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() = %v, want a clean shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after the context was cancelled")
	}

	if _, err := net.DialTimeout("tcp", cfg.Server.Addr, time.Second); err == nil {
		t.Error("the listener is still accepting connections after shutdown")
	}
}

func TestServeReturnsImmediatelyOnAnAlreadyCancelledContext(t *testing.T) {
	t.Parallel()

	cfg := validConfig(t)
	cfg.Server.Addr = freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := (&ServeCmd{}).Run(ctx, cfg); err != nil {
		t.Errorf("Run() = %v, want nil", err)
	}
}

func TestServeErrors(t *testing.T) {
	t.Parallel()

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	t.Cleanup(func() { _ = occupied.Close() })

	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr string
	}{
		{
			name:    "incomplete config",
			mutate:  func(cfg *config.Config) { cfg.Webhook.Token = "" },
			wantErr: "config validation",
		},
		{
			name:    "database cannot be opened",
			mutate:  func(cfg *config.Config) { cfg.Database.Path = filepath.Join(t.TempDir(), "missing", "serve.db") },
			wantErr: "open db",
		},
		{
			name:    "address is already in use",
			mutate:  func(cfg *config.Config) { cfg.Server.Addr = occupied.Addr().String() },
			wantErr: "listen on",
		},
		{
			name:    "address is not parseable",
			mutate:  func(cfg *config.Config) { cfg.Server.Addr = "not-an-address" },
			wantErr: "listen on",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig(t)
			tt.mutate(cfg)

			err := (&ServeCmd{}).Run(context.Background(), cfg)
			if err == nil {
				t.Fatalf("Run() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Run() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

// Metrics run beside the service: their own listener, scrapeable while it
// serves, gone when it stops. The OTLP interval is an hour, so the only export
// that can reach the collector is the one shutdown forces — which is what makes
// this a test of the shutdown order and not just of the wiring.
func TestServeExportsMetricsAndStopsThem(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		exports  int
		received = func() int { mu.Lock(); defer mu.Unlock(); return exports }
	)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		exports++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	cfg := validConfig(t)
	cfg.Server.Addr = freePort(t)
	cfg.Metrics = config.MetricsConfig{
		Enabled:         true,
		Addr:            freePort(t),
		Path:            "/metrics",
		Prometheus:      true,
		OTLPEndpoint:    strings.TrimPrefix(collector.URL, "http://"),
		OTLPProtocol:    "http",
		OTLPInsecure:    true,
		OTLPInterval:    time.Hour,
		ShutdownTimeout: 5 * time.Second,
		ServiceName:     "teamster-test",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- (&ServeCmd{}).Run(ctx, cfg) }()

	waitForServer(t, cfg.Server.Addr)
	waitForServer(t, cfg.Metrics.Addr)

	// Drive one request so there is something to export, then scrape it.
	if resp, err := http.Get(fmt.Sprintf("http://%s/healthz", cfg.Server.Addr)); err == nil {
		_ = resp.Body.Close()
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", cfg.Metrics.Addr))
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", resp.StatusCode)
	}
	// The gauge reads the store, so its presence proves the callback was
	// registered against a database that is still open.
	if !strings.Contains(string(body), "teamster_active_alerts") {
		t.Errorf("the exposition carries no active alerts gauge:\n%s", body)
	}

	if got := received(); got != 0 {
		t.Fatalf("the collector received %d exports before shutdown, want none", got)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() = %v, want a clean shutdown", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run() did not return after the context was cancelled")
	}

	if got := received(); got == 0 {
		t.Error("shutting down exported nothing, so the last window of metrics is lost")
	}
	if _, err := net.DialTimeout("tcp", cfg.Metrics.Addr, time.Second); err == nil {
		t.Error("the metrics listener is still accepting connections after shutdown")
	}
}
