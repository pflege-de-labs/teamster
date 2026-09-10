package httpserver

import (
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// Without these a slow client can hold a connection open indefinitely, and a
// handler that never returns holds one with it.
func TestServerAppliesTheConfiguredTimeouts(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{
			Addr:         ":0",
			ReadTimeout:  20 * time.Second,
			WriteTimeout: 90 * time.Second,
			IdleTimeout:  200 * time.Second,
		},
		Webhook: config.WebhookConfig{Token: "token"},
	}

	srv := NewServer(cfg, newFakeStore(), &fakeMessenger{})

	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{name: "read", got: srv.ReadTimeout, want: 20 * time.Second},
		{name: "write", got: srv.WriteTimeout, want: 90 * time.Second},
		{name: "idle", got: srv.IdleTimeout, want: 200 * time.Second},
		{name: "read header", got: srv.ReadHeaderTimeout, want: headerGrace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("%s timeout = %s, want %s", tt.name, tt.got, tt.want)
			}
		})
	}
}

// A read timeout shorter than the header grace has to win, or the header
// timeout would quietly extend the limit the operator asked for.
func TestReadHeaderTimeoutNeverExceedsTheReadTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		read time.Duration
		want time.Duration
	}{
		{name: "read timeout longer than the grace", read: time.Minute, want: headerGrace},
		{name: "read timeout shorter than the grace", read: 2 * time.Second, want: 2 * time.Second},
		{name: "no read timeout configured", read: 0, want: headerGrace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := readHeaderTimeout(tt.read); got != tt.want {
				t.Errorf("readHeaderTimeout(%s) = %s, want %s", tt.read, got, tt.want)
			}
		})
	}
}

// BaseContext is deliberately unset: it would have to be the context a signal
// cancels, and that would abort in-flight requests exactly when the draining
// shutdown is trying to let them finish.
func TestServerHasNoBaseContext(t *testing.T) {
	t.Parallel()

	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0"}}, newFakeStore(), &fakeMessenger{})
	if srv.BaseContext != nil {
		t.Error("BaseContext is set; a signal-cancelled context would cut off draining requests")
	}
}
