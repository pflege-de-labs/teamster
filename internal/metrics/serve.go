package metrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// metricsHeaderGrace bounds how long a scraper may take over its request
// headers. The listener is meant for one collector on a private interface, so
// it is short.
const metricsHeaderGrace = 5 * time.Second

type listener struct {
	server *http.Server
	net    net.Listener
}

// Start binds the metrics listener and serves the Prometheus exporter on it.
// Binding happens here rather than in the goroutine, so an address already in
// use is an error the caller can return instead of a log line nobody reads.
//
// It answers "" when there is nothing to serve — metrics off, or OTLP only.
func (m *Metrics) Start() (string, func(context.Context) error, error) {
	stop := func(context.Context) error { return nil }
	if !m.enabled || m.handler == nil {
		return "", stop, nil
	}

	if m.listener != nil {
		return "", stop, errors.New("the metrics listener is already running")
	}

	mux := http.NewServeMux()
	mux.Handle(m.cfg.Path, m.handler.handler)

	network, err := net.Listen("tcp", m.cfg.Addr)
	if err != nil {
		return "", stop, fmt.Errorf("listen on %s: %w", m.cfg.Addr, err)
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: metricsHeaderGrace,
		ReadTimeout:       metricsHeaderGrace,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
	}
	m.listener = &listener{server: server, net: network}

	go func() {
		// ErrServerClosed is what Shutdown looks like from in here.
		_ = server.Serve(network)
	}()

	return network.Addr().String(), m.stopListener, nil
}

func (m *Metrics) stopListener(ctx context.Context) error {
	if m.listener == nil {
		return nil
	}
	return m.listener.server.Shutdown(ctx)
}

// Handler is the Prometheus exposition, for a test that would rather not bind a
// port. It is nil when the exporter is not configured.
func (m *Metrics) Handler() http.Handler {
	if m.handler == nil {
		return nil
	}
	return m.handler.handler
}
