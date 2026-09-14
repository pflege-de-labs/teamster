package httpserver

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// recordingTelemetry remembers what it was told, so a test can assert the call
// rather than the exposition.
type recordingTelemetry struct {
	*metrics.Metrics

	mu        sync.Mutex
	delivered [][2]string
	receipts  [][2]string
	renders   [][2]string
}

func newRecordingTelemetry() *recordingTelemetry {
	return &recordingTelemetry{Metrics: metrics.Disabled()}
}

func (r *recordingTelemetry) DeliveryRecorded(_ context.Context, route, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delivered = append(r.delivered, [2]string{route, outcome})
}

func (r *recordingTelemetry) WebhookReceived(_ context.Context, source, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.receipts = append(r.receipts, [2]string{source, status})
}

func (r *recordingTelemetry) RenderFailed(_ context.Context, templateID, stage string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renders = append(r.renders, [2]string{templateID, stage})
}

func (r *recordingTelemetry) calls() (delivered, receipts, renders [][2]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][2]string(nil), r.delivered...),
		append([][2]string(nil), r.receipts...),
		append([][2]string(nil), r.renders...)
}

// measuringServer is the real pipeline pointed at a reader a test can collect
// from, which is the only way to see what the instrumentation actually records.
func measuringServer(t *testing.T, st *fakeStore, msg *fakeMessenger) (http.Handler, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	tel := metrics.NewForReader(reader)
	t.Cleanup(func() {
		if err := tel.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
	srv, err := NewServer(cfg, st, msg, tel)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv.Handler, reader
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return collected
}

func metricNamed(collected metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

// The histogram the milestone asked for, under the name the semantic
// conventions give it — and aggregated exponentially, which is what lets the
// Prometheus exporter render it natively.
func TestServerRequestsAreMeasured(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleAdmin)
	handler, reader := measuringServer(t, st, &fakeMessenger{})

	asRole(t, handler, http.MethodGet, "/api/templates", "")

	measured, ok := metricNamed(collect(t, reader), "http.server.request.duration")
	if !ok {
		t.Fatal("no http.server.request.duration was recorded")
	}
	if _, ok := measured.Data.(metricdata.ExponentialHistogram[float64]); !ok {
		t.Errorf("aggregated as %T, want an exponential histogram", measured.Data)
	}
}

// The route attribute is what makes the histogram answer "which endpoint is
// slow". It is also the thing that silently disappears if the instrumentation
// is moved outside the middleware that copies the request.
func TestServerRequestsCarryTheirRoute(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleAdmin)
	handler, reader := measuringServer(t, st, &fakeMessenger{})

	asRole(t, handler, http.MethodGet, "/api/templates", "")
	postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{},"fingerprint":"fp"}`)

	measured, ok := metricNamed(collect(t, reader), "http.server.request.duration")
	if !ok {
		t.Fatal("no http.server.request.duration was recorded")
	}
	histogram, ok := measured.Data.(metricdata.ExponentialHistogram[float64])
	if !ok {
		t.Fatalf("aggregated as %T", measured.Data)
	}

	routes := map[string]bool{}
	for _, point := range histogram.DataPoints {
		route, found := point.Attributes.Value(attribute.Key("http.route"))
		if !found {
			t.Error("a data point carries no http.route; the pattern never reached the instrumentation")
			continue
		}
		routes[route.AsString()] = true
	}

	for _, want := range []string{"/api/", "/webhook/universal"} {
		if !routes[want] {
			t.Errorf("no data point for route %q; got %v", want, routes)
		}
	}
}

// A kubelet asks every few seconds and learns nothing from the answer's
// latency, so the probes stay out of the histogram.
func TestProbesAreNotMeasured(t *testing.T) {
	t.Parallel()

	handler, reader := measuringServer(t, newFakeStore(), &fakeMessenger{})

	probe(t, handler, http.MethodGet, "/healthz")
	probe(t, handler, http.MethodGet, "/readyz")

	if _, ok := metricNamed(collect(t, reader), "http.server.request.duration"); ok {
		t.Error("the probes were measured")
	}
}

// Deliveries say which route and what happened, because "delivery is broken"
// and "one channel is broken" are different mornings.
func TestDeliveriesAreCounted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func(*fakeStore, *fakeMessenger)
		body   string
		want   [][2]string
		wantRe [][2]string
	}{
		{
			name: "a new card",
			body: `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			want: [][2]string{{"route", metrics.OutcomePosted}},
		},
		{
			name: "an update to one that exists",
			setup: func(st *fakeStore, _ *fakeMessenger) {
				st.activeAlerts[activeAlertKey("fp", "team", "channel")] = models.ActiveAlert{
					Fingerprint: "fp", TeamID: "team", ChannelID: "channel", MessageID: "graph-1", PostedAt: testPostedAt,
				}
			},
			body: `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			want: [][2]string{{"route", metrics.OutcomeUpdated}},
		},
		{
			name:  "a channel that refuses it",
			setup: func(_ *fakeStore, msg *fakeMessenger) { msg.postErr = errStore },
			body:  `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			want:  [][2]string{{"route", metrics.OutcomeFailed}},
		},
		{
			name:   "a template that is gone",
			setup:  func(st *fakeStore, _ *fakeMessenger) { delete(st.templates, "tmpl") },
			body:   `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			wantRe: [][2]string{{"tmpl", metrics.StageTemplate}},
		},
		{
			name:   "a template that does not render",
			setup:  func(st *fakeStore, _ *fakeMessenger) { st.templates["tmpl"] = models.Template{ID: "tmpl", Body: "{{"} },
			body:   `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			wantRe: [][2]string{{"tmpl", metrics.StageRender}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{}
			st, _ := seededServer(t, msg)
			if tt.setup != nil {
				tt.setup(st, msg)
			}

			tel := newRecordingTelemetry()
			srv, err := NewServer(config.Config{
				Server:  config.ServerConfig{Addr: ":0"},
				Webhook: config.WebhookConfig{Token: "token"},
				Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
			}, st, msg, tel)
			if err != nil {
				t.Fatalf("NewServer: %v", err)
			}

			postWebhook(t, srv.Handler, "/webhook/universal", "token", tt.body)

			delivered, receipts, renders := tel.calls()
			if !equalPairs(delivered, tt.want) {
				t.Errorf("deliveries = %v, want %v", delivered, tt.want)
			}
			if !equalPairs(renders, tt.wantRe) {
				t.Errorf("render failures = %v, want %v", renders, tt.wantRe)
			}
			if len(receipts) != 1 || receipts[0] != [2]string{"universal", "firing"} {
				t.Errorf("receipts = %v, want the alert counted once as it arrived", receipts)
			}
		})
	}
}

// A refused token is otherwise a 401 nobody watches, which looks exactly like a
// sender that stopped sending.
func TestARefusedTokenIsCounted(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, _ := seededServer(t, msg)

	tel := newRecordingTelemetry()
	srv, err := NewServer(config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}, st, msg, tel)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	postWebhook(t, srv.Handler, "/webhook/alertmanager", "wrong-token", `{"alerts":[]}`)

	_, receipts, _ := tel.calls()
	if len(receipts) != 1 || receipts[0] != [2]string{"alertmanager", "refused"} {
		t.Errorf("receipts = %v, want the refusal counted", receipts)
	}
}

func equalPairs(got, want [][2]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
