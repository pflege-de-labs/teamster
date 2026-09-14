// Package metrics is what this service says about itself: how long requests
// take, how many alerts it delivered, how many cards it is keeping up to date.
//
// Everything is recorded against the OpenTelemetry metric API once, and read by
// as many exporters as the deployment configures — the Prometheus exporter on
// its own listener, an OTLP collector, both, or neither. Neither is the
// default: the attributes name routes, templates and channels, so exporting
// them is a decision an operator makes rather than one they inherit.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// scope names the instruments as coming from this package, which is what a
// reader shows beside them.
const scope = "github.com/pflege-de-labs/teamster/internal/metrics"

// Metrics owns the pipeline. A disabled one is a usable value rather than a nil
// pointer: its meter provider is the no-op provider, so a call site records
// unconditionally and never asks whether metrics are on.
type Metrics struct {
	enabled  bool
	provider metric.MeterProvider
	sdk      *sdkmetric.MeterProvider
	handler  *prometheusHandler
	listener *listener
	cfg      config.MetricsConfig

	stopped sync.Once
	stopErr error

	deliveries   metric.Int64Counter
	receipts     metric.Int64Counter
	renderFails  metric.Int64Counter
	activeAlerts metric.Int64ObservableGauge
	registration metric.Registration
}

// New builds the pipeline the configuration asks for. It never returns a nil
// *Metrics alongside a nil error, so a caller can use what it gets back
// whatever the configuration said.
func New(cfg config.MetricsConfig) (*Metrics, error) {
	m := &Metrics{provider: noop.NewMeterProvider(), cfg: cfg}

	if cfg.Enabled {
		sdk, handler, err := build(cfg)
		if err != nil {
			return nil, err
		}
		m.enabled, m.sdk, m.handler, m.provider = true, sdk, handler, sdk
	}

	if err := m.instruments(); err != nil {
		return nil, err
	}
	return m, nil
}

// Disabled is the pipeline that records nothing, for a caller that wants none
// and for every test that is not about metrics.
func Disabled() *Metrics {
	m, err := New(config.MetricsConfig{})
	if err != nil {
		// Building a no-op pipeline cannot fail: there are no exporters to
		// construct and the no-op meter returns no errors.
		panic(fmt.Sprintf("metrics: building a disabled pipeline failed: %v", err))
	}
	return m
}

// NewForReader builds a pipeline that records into the reader it is given, with
// the real instruments and the real view. It is the seam other packages' tests
// use to see what they record, which an exporter would only obscure.
func NewForReader(reader sdkmetric.Reader) *Metrics {
	m := &Metrics{enabled: true}
	m.sdk = sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithView(histogramView),
		sdkmetric.WithCardinalityLimit(cardinalityLimit),
	)
	m.provider = m.sdk

	if err := m.instruments(); err != nil {
		panic(fmt.Sprintf("metrics: building test instruments failed: %v", err))
	}
	return m
}

func (m *Metrics) Enabled() bool { return m.enabled }

// MeterProvider is what the HTTP instrumentation records against. It is the
// no-op provider when metrics are off, never the OpenTelemetry global — this
// service injects its dependencies rather than reaching for process state.
func (m *Metrics) MeterProvider() metric.MeterProvider { return m.provider }

// Shutdown flushes what has not been exported yet and stops collecting. The
// context wants a deadline of its own: by the time this runs, the one that
// cancelled the server is already cancelled, and a cancelled context abandons
// the final export — the window that matters most after a crash.
func (m *Metrics) Shutdown(ctx context.Context) error {
	if !m.enabled {
		return nil
	}

	// A deferred shutdown and an explicit one are the same call twice; the
	// second would otherwise report the reader it already closed as an error.
	m.stopped.Do(func() {
		var errs []error
		if m.registration != nil {
			errs = append(errs, m.registration.Unregister())
			m.registration = nil
		}
		if m.sdk != nil {
			errs = append(errs, m.sdk.Shutdown(ctx))
		}
		m.stopErr = errors.Join(errs...)
	})
	return m.stopErr
}
