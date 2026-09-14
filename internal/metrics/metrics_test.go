package metrics

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"go.opentelemetry.io/otel/metric"

	"github.com/pflege-de-labs/teamster/internal/config"
)

func enabled() config.MetricsConfig {
	return config.MetricsConfig{
		Enabled:         true,
		Addr:            "127.0.0.1:0",
		Path:            "/metrics",
		Prometheus:      true,
		OTLPInterval:    time.Minute,
		ShutdownTimeout: time.Second,
		ServiceName:     "teamster-test",
	}
}

// recordDuration puts one observation into a histogram of its own, so the
// exposition assertions do not depend on which instruments the rest of the
// service happens to register.
func recordDuration(t *testing.T, m *Metrics, seconds float64) {
	t.Helper()

	histogram, err := m.MeterProvider().Meter("test").Float64Histogram(
		"teamster.test.duration", metric.WithUnit("s"))
	if err != nil {
		t.Fatalf("test histogram: %v", err)
	}
	histogram.Record(context.Background(), seconds)
}

// otlpSink stands in for a collector and counts what reaches it, which is how
// the final flush is asserted without waiting for an interval to elapse.
func otlpSink(t *testing.T) (endpoint string, received func() int) {
	t.Helper()

	var (
		mu    sync.Mutex
		count int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	return strings.TrimPrefix(server.URL, "http://"), func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

func newTestMetrics(t *testing.T, cfg config.MetricsConfig) *Metrics {
	t.Helper()

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return m
}

// scrape asks the exporter for its exposition in the format the header names.
func scrape(t *testing.T, m *Metrics, accept string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept", accept)

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	return rec
}

func families(t *testing.T, body io.Reader, format expfmt.Format) map[string]*dto.MetricFamily {
	t.Helper()

	decoder := expfmt.NewDecoder(body, format)
	out := map[string]*dto.MetricFamily{}
	for {
		var family dto.MetricFamily
		switch err := decoder.Decode(&family); {
		case errors.Is(err, io.EOF):
			return out
		case err != nil:
			t.Fatalf("decode exposition: %v", err)
		}
		out[family.GetName()] = &family
	}
}

// The point of the milestone: a scraper that negotiates protobuf gets a native
// histogram, not the bucket list a classic one would carry.
func TestPrometheusExposesNativeHistograms(t *testing.T) {
	t.Parallel()

	m := newTestMetrics(t, enabled())
	recordDuration(t, m, 0.25)

	format := expfmt.NewFormat(expfmt.TypeProtoDelim)
	rec := scrape(t, m, string(format))

	family := families(t, rec.Body, format)["teamster_test_duration_seconds"]
	if family == nil {
		t.Fatalf("no duration family in the exposition")
	}

	histogram := family.Metric[0].Histogram
	if len(histogram.GetPositiveSpan()) == 0 {
		t.Error("no native buckets: the SDK aggregated a classic histogram instead")
	}
	if got := len(histogram.GetBucket()); got != 0 {
		t.Errorf("classic buckets present: %d", got)
	}
	// Prometheus refuses a schema outside this range, so a native histogram
	// carrying one is worse than no histogram.
	if schema := histogram.GetSchema(); schema < -4 || schema > 8 {
		t.Errorf("schema = %d, outside the range Prometheus accepts", schema)
	}
	if histogram.GetSampleCount() != 1 {
		t.Errorf("sample count = %d, want the one observation", histogram.GetSampleCount())
	}
}

// A scraper that does not negotiate protobuf still gets something — and that
// something has no buckets to compute a quantile from. This test is the
// detection rule in the README, written down.
func TestATextScrapeLosesTheBuckets(t *testing.T) {
	t.Parallel()

	m := newTestMetrics(t, enabled())
	recordDuration(t, m, 0.25)

	rec := scrape(t, m, string(expfmt.NewFormat(expfmt.TypeTextPlain)))
	body := rec.Body.String()

	buckets := strings.Count(body, "teamster_test_duration_seconds_bucket")
	if buckets != 1 {
		t.Errorf("bucket lines = %d, want exactly the synthetic +Inf one", buckets)
	}
	if !strings.Contains(body, `le="+Inf"`) {
		t.Errorf("no +Inf bucket in the text exposition:\n%s", body)
	}
	for _, want := range []string{"teamster_test_duration_seconds_sum", "teamster_test_duration_seconds_count"} {
		if !strings.Contains(body, want) {
			t.Errorf("the text exposition is missing %q", want)
		}
	}
}

func TestDisabled(t *testing.T) {
	t.Parallel()

	m := Disabled()

	if m.Enabled() {
		t.Error("Disabled() reports itself enabled")
	}
	if m.Handler() != nil {
		t.Error("Disabled() serves an exposition")
	}

	// Every recorder is safe on a disabled pipeline, which is what lets the
	// call sites record without asking.
	m.DeliveryRecorded(context.Background(), "route", OutcomePosted)
	m.WebhookReceived(context.Background(), "alertmanager", "firing")
	m.RenderFailed(context.Background(), "tmpl", StageRender)
	if err := m.ObserveActiveAlerts(func(context.Context) (int64, error) { return 0, nil }); err != nil {
		t.Errorf("ObserveActiveAlerts: %v", err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}

	// And the wrappers hand back exactly what they were given.
	handler := http.NewServeMux()
	if got := m.ServerMiddleware("test")(handler); got != http.Handler(handler) {
		t.Error("the server middleware wrapped the handler while disabled")
	}
	if got := m.RouteTag(handler); got != http.Handler(handler) {
		t.Error("RouteTag wrapped the handler while disabled")
	}
	transport := http.DefaultTransport
	if got := m.ClientTransport(transport); got != transport {
		t.Error("the client transport was wrapped while disabled")
	}
}

func TestNewRefusesAnUnknownOTLPProtocol(t *testing.T) {
	t.Parallel()

	cfg := enabled()
	cfg.OTLPEndpoint, cfg.OTLPProtocol = "localhost:4318", "carrier-pigeon"

	if _, err := New(cfg); err == nil {
		t.Error("New() accepted a protocol that does not exist")
	}
}

// Which readers get built is the one branch in the package worth asserting
// directly: everything downstream depends on it.
func TestReadersFollowTheConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mutate      func(*config.MetricsConfig)
		wantHandler bool
	}{
		{name: "prometheus alone", mutate: func(*config.MetricsConfig) {}, wantHandler: true},
		{
			name: "otlp alone",
			mutate: func(c *config.MetricsConfig) {
				c.Prometheus = false
				endpoint, _ := otlpSink(t)
				c.OTLPEndpoint, c.OTLPProtocol, c.OTLPInsecure = endpoint, "http", true
			},
		},
		{
			name: "both",
			mutate: func(c *config.MetricsConfig) {
				endpoint, _ := otlpSink(t)
				c.OTLPEndpoint, c.OTLPProtocol, c.OTLPInsecure = endpoint, "http", true
			},
			wantHandler: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := enabled()
			tt.mutate(&cfg)
			m := newTestMetrics(t, cfg)

			if got := m.Handler() != nil; got != tt.wantHandler {
				t.Errorf("serves an exposition = %v, want %v", got, tt.wantHandler)
			}
		})
	}
}

// The listener binds where it was told, or says so. A port already in use is
// the caller's to report, not a goroutine's to log.
func TestStart(t *testing.T) {
	t.Parallel()

	m := newTestMetrics(t, enabled())

	addr, stop, err := m.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if addr == "" {
		t.Fatal("Start() bound nothing while the exporter is on")
	}

	res, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("scrape the listener: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Errorf("GET /metrics = %d, want 200", res.StatusCode)
	}

	if err := stop(context.Background()); err != nil {
		t.Errorf("stop: %v", err)
	}
	if _, err := http.Get("http://" + addr + "/metrics"); err == nil {
		t.Error("the listener still answers after it was stopped")
	}
}

func TestStartServesNothingWithoutAnExporter(t *testing.T) {
	t.Parallel()

	cfg := enabled()
	cfg.Prometheus = false
	endpoint, _ := otlpSink(t)
	cfg.OTLPEndpoint, cfg.OTLPProtocol, cfg.OTLPInsecure = endpoint, "http", true

	addr, stop, err := newTestMetrics(t, cfg).Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if addr != "" {
		t.Errorf("Start() bound %q with no exposition to serve", addr)
	}
	if err := stop(context.Background()); err != nil {
		t.Errorf("stop: %v", err)
	}
}

func TestStartReportsABindFailure(t *testing.T) {
	t.Parallel()

	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("take a port: %v", err)
	}
	defer func() { _ = taken.Close() }()

	cfg := enabled()
	cfg.Addr = taken.Addr().String()

	if _, _, err := newTestMetrics(t, cfg).Start(); err == nil {
		t.Error("Start() = nil error on an address already in use")
	}
}

func TestObserveActiveAlerts(t *testing.T) {
	t.Parallel()

	m := newTestMetrics(t, enabled())
	if err := m.ObserveActiveAlerts(func(context.Context) (int64, error) { return 7, nil }); err != nil {
		t.Fatalf("ObserveActiveAlerts: %v", err)
	}

	format := expfmt.NewFormat(expfmt.TypeProtoDelim)
	rec := scrape(t, m, string(format))

	family := families(t, rec.Body, format)["teamster_active_alerts"]
	if family == nil {
		t.Fatal("no active alerts gauge in the exposition")
	}
	if got := family.Metric[0].Gauge.GetValue(); got != 7 {
		t.Errorf("active alerts = %v, want 7", got)
	}
}

// The interval is an hour, so the only export that can happen is the one
// Shutdown forces. If the defer that calls it ever loses its deadline, this is
// what notices.
func TestShutdownExportsWhatIsLeft(t *testing.T) {
	t.Parallel()

	endpoint, received := otlpSink(t)

	cfg := enabled()
	cfg.Prometheus = false
	cfg.OTLPEndpoint, cfg.OTLPProtocol, cfg.OTLPInsecure = endpoint, "http", true
	cfg.OTLPInterval = time.Hour

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	recordDuration(t, m, 0.5)

	if got := received(); got != 0 {
		t.Fatalf("the collector received %d exports before shutdown, want none", got)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := received(); got == 0 {
		t.Error("shutting down exported nothing, so the last window is lost")
	}
}

func TestRouteOf(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"/admin/":                    "/admin/",
		"POST /webhook/alertmanager": "/webhook/alertmanager",
		"GET example.com/admin":      "/admin",
		"":                           "",
	}

	for pattern, want := range tests {
		if got := routeOf(pattern); got != want {
			t.Errorf("routeOf(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// Registering the gauge twice must replace the callback, not leave the first
// one running against whatever it closed over — for this gauge, a database
// somebody may be about to close.
func TestObservingActiveAlertsTwiceReplacesTheCallback(t *testing.T) {
	t.Parallel()

	m := newTestMetrics(t, enabled())

	var first, second atomic.Int64
	if err := m.ObserveActiveAlerts(func(context.Context) (int64, error) {
		first.Add(1)
		return 1, nil
	}); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := m.ObserveActiveAlerts(func(context.Context) (int64, error) {
		second.Add(1)
		return 2, nil
	}); err != nil {
		t.Fatalf("second registration: %v", err)
	}

	scrape(t, m, string(expfmt.NewFormat(expfmt.TypeTextPlain)))

	if first.Load() != 0 {
		t.Errorf("the first callback ran %d times after being replaced", first.Load())
	}
	if second.Load() == 0 {
		t.Error("the second callback never ran")
	}
}

// The listener takes no credentials, so how often it can be made to ask the
// database is whatever an unauthenticated caller chooses. The cache makes that
// a property of the clock.
func TestScrapingInALoopDoesNotQueryInALoop(t *testing.T) {
	t.Parallel()

	m := newTestMetrics(t, enabled())

	var queries atomic.Int64
	if err := m.ObserveActiveAlerts(func(context.Context) (int64, error) {
		queries.Add(1)
		return 3, nil
	}); err != nil {
		t.Fatalf("ObserveActiveAlerts: %v", err)
	}

	for range 20 {
		scrape(t, m, string(expfmt.NewFormat(expfmt.TypeTextPlain)))
	}

	if got := queries.Load(); got > 2 {
		t.Errorf("20 scrapes made %d queries, want them collapsed by the cache", got)
	}
	if queries.Load() == 0 {
		t.Error("no query at all, so the gauge reports nothing")
	}
}

// A second Start would leave the first listener serving with nobody holding a
// way to stop it.
func TestStartingTwiceIsRefused(t *testing.T) {
	t.Parallel()

	m := newTestMetrics(t, enabled())

	if _, stop, err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	} else {
		t.Cleanup(func() { _ = stop(context.Background()) })
	}

	if _, _, err := m.Start(); err == nil {
		t.Error("Start() = nil error the second time, leaking the first listener")
	}
}

// Shutting down twice is what a defer and an explicit call together look like.
func TestShutdownIsSafeTwice(t *testing.T) {
	t.Parallel()

	m, err := New(enabled())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Errorf("second shutdown: %v", err)
	}
}
