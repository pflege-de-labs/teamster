package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// uninstrumented is what a client gets when nobody is measuring: the transport
// it was handed, unchanged.
type uninstrumented struct{}

func (uninstrumented) ClientTransport(base http.RoundTripper) http.RoundTripper { return base }

// newTestClient points a client at a stub Graph API, bypassing the OAuth2
// exchange that NewClient would otherwise perform against Entra.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &Client{baseURL: srv.URL, httpClient: srv.Client()}
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	client, err := NewClient(config.GraphConfig{
		TenantID:     "tenant",
		ClientID:     "client",
		ClientSecret: "secret",
		BaseURL:      "https://graph.example/v1.0",
		TimeoutSec:   7,
	}, uninstrumented{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.baseURL != "https://graph.example/v1.0" {
		t.Errorf("baseURL = %q, want the configured URL", client.baseURL)
	}
	if client.httpClient.Timeout != 7*time.Second {
		t.Errorf("timeout = %v, want 7s", client.httpClient.Timeout)
	}
}

func TestPostMessage(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
		gotBody   MessageRequest
	)

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"message-1"}`))
	})

	id, err := client.PostMessage("team-1", "channel-1", Message{
		Title: "CPU <spiking>",
		Text:  "<p>worker is hot</p>",
		Cards: []json.RawMessage{json.RawMessage(`{"type":"AdaptiveCard"}`)},
	})
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if id != "message-1" {
		t.Errorf("PostMessage() = %q, want %q", id, "message-1")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/teams/team-1/channels/channel-1/messages"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	want := `<p><b>CPU &lt;spiking&gt;</b></p><p>worker is hot</p><attachment id="1"></attachment>`
	if gotBody.Body.Content != want {
		t.Errorf("body = %q, want %q", gotBody.Body.Content, want)
	}
	if len(gotBody.Attachments) != 1 || gotBody.Attachments[0].ContentType != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("attachments = %+v, want a single adaptive card", gotBody.Attachments)
	}
}

// A template that sends text alone has no attachment, and the body must not
// reference one that is not there.
func TestPostMessageWithoutACard(t *testing.T) {
	t.Parallel()

	var gotBody MessageRequest
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"message-1"}`))
	})

	if _, err := client.PostMessage("team-1", "channel-1", Message{Title: "Disk filling", Text: "<p>92% used</p>"}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if want := "<p><b>Disk filling</b></p><p>92% used</p>"; gotBody.Body.Content != want {
		t.Errorf("body = %q, want %q", gotBody.Body.Content, want)
	}
	if len(gotBody.Attachments) != 0 {
		t.Errorf("attachments = %+v, want none", gotBody.Attachments)
	}
}

func TestPostMessageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name: "server rejects the request",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden"}`))
			},
			wantErr: "failed",
		},
		{
			name: "response is not JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not json"))
			},
			wantErr: "decode post response",
		},
		{
			name: "response has no id",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{}`))
			},
			wantErr: "graph response missing id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, tt.handler)

			_, err := client.PostMessage("team", "channel", Message{Title: "summary", Cards: []json.RawMessage{json.RawMessage(`{}`)}})
			if err == nil {
				t.Fatalf("PostMessage() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("PostMessage() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestUpdateMessage(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.UpdateMessage("team-1", "channel-1", "message-1", Message{Title: "summary", Cards: []json.RawMessage{json.RawMessage(`{}`)}}); err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if want := "/teams/team-1/channels/channel-1/messages/message-1"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestUpdateMessageError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("message gone"))
	})

	err := client.UpdateMessage("team", "channel", "missing", Message{Title: "summary", Cards: []json.RawMessage{json.RawMessage(`{}`)}})
	if err == nil || !strings.Contains(err.Error(), "message gone") {
		t.Errorf("UpdateMessage() = %v, want the Graph error body to be surfaced", err)
	}
}

func TestDoRequestErrors(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name    string
		method  string
		url     string
		body    any
		wantErr string
	}{
		{
			name:    "unencodable body",
			method:  http.MethodPost,
			url:     client.baseURL,
			body:    make(chan int),
			wantErr: "encode request",
		},
		{
			name:    "invalid method",
			method:  "bad method",
			url:     client.baseURL,
			wantErr: "new request",
		},
		{
			name:    "unreachable host",
			method:  http.MethodPost,
			url:     "http://127.0.0.1:1/teams",
			wantErr: "graph request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := client.doRequest(tt.method, tt.url, tt.body)
			if err == nil {
				t.Fatalf("doRequest() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("doRequest() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestListTeams(t *testing.T) {
	t.Parallel()

	var gotPath, gotQuery string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"value":[{"id":"t1","displayName":"Operations"},{"id":"t2","displayName":"Platform"}]}`))
	})

	teams, err := client.ListTeams()
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(teams) != 2 || teams[0].ID != "t1" || teams[0].Name != "Operations" {
		t.Errorf("teams = %+v, want displayName mapped to Name", teams)
	}
	if gotPath != "/teams" {
		t.Errorf("path = %q, want /teams", gotPath)
	}
	if !strings.Contains(gotQuery, "select") {
		t.Errorf("query = %q, want it to select only the fields we use", gotQuery)
	}
}

func TestListChannels(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath, because r.URL.Path is already decoded and would hide a
		// missing PathEscape.
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"value":[{"id":"c1","displayName":"General"}]}`))
	})

	channels, err := client.ListChannels("team one")
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(channels) != 1 || channels[0].Name != "General" {
		t.Errorf("channels = %+v", channels)
	}
	if !strings.Contains(gotPath, "team%20one") {
		t.Errorf("path = %q, want the team id percent-escaped into it", gotPath)
	}
}

func TestDirectoryReadsReportFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name: "graph refuses",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"Authorization_RequestDenied"}}`))
			},
			wantErr: "Authorization_RequestDenied",
		},
		{
			name: "graph answers nonsense",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not json"))
			},
			wantErr: "decode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, tt.handler)

			if _, err := client.ListTeams(); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ListTeams error = %v, want it to mention %q", err, tt.wantErr)
			}
			if _, err := client.ListChannels("t"); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ListChannels error = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestListTeamsReturnsEmptyNotNil(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"value":[]}`))
	})

	teams, err := client.ListTeams()
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if teams == nil {
		t.Error("teams is nil, want an empty slice so it encodes as [] rather than null")
	}
}

// Graph returns these in no useful order, and they end up in a dropdown someone
// has to find a name in.
func TestDirectoryListsAreSortedByName(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "channels") {
			_, _ = w.Write([]byte(`{"value":[
				{"id":"c1","displayName":"Zulu"},
				{"id":"c2","displayName":"alpha"},
				{"id":"c3","displayName":"General"},
				{"id":"c4","displayName":"Änderungen"}
			]}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":[
			{"id":"t1","displayName":"Zentrale"},
			{"id":"t2","displayName":"operations"},
			{"id":"t3","displayName":"Ops"},
			{"id":"t4","displayName":"Überwachung"},
			{"id":"t5","displayName":"Ärzte"},
			{"id":"t6","displayName":"alpha"}
		]}`))
	})

	teams, err := client.ListTeams()
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}

	// Collation, not byte order: a diacritic sorts beside its base letter, so
	// Ärzte belongs after alpha rather than behind Zentrale, and case only
	// breaks ties, keeping "operations" and "Ops" together.
	wantTeams := []string{"alpha", "Ärzte", "operations", "Ops", "Überwachung", "Zentrale"}
	for i, want := range wantTeams {
		if teams[i].Name != want {
			t.Errorf("teams[%d] = %q, want %q (got %v)", i, teams[i].Name, want, names(teams))
		}
	}

	channels, err := client.ListChannels("t1")
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	for i, want := range []string{"alpha", "Änderungen", "General", "Zulu"} {
		if channels[i].Name != want {
			t.Errorf("channels[%d] = %q, want %q", i, channels[i].Name, want)
		}
	}
}

func TestSortByNameIsStableForEqualNames(t *testing.T) {
	t.Parallel()

	teams := []Team{{ID: "b", Name: "Ops"}, {ID: "a", Name: "ops"}, {ID: "c", Name: "Ops"}}
	sortByName(teams, func(t Team) string { return t.Name })

	// Which case sorts first is the collator's business; what matters is that
	// spellings of one name stay together and that identical names keep their
	// input order, so a redraw does not shuffle the dropdown.
	var identical []string
	for _, team := range teams {
		if team.Name == "Ops" {
			identical = append(identical, team.ID)
		}
	}
	if len(identical) != 2 || identical[0] != "b" || identical[1] != "c" {
		t.Errorf("identical names ordered %v, want b then c as they arrived", identical)
	}
}

func names(teams []Team) []string {
	out := make([]string, 0, len(teams))
	for _, team := range teams {
		out = append(out, team.Name)
	}
	return out
}

// Graph is the dependency most likely to be the problem, so the client's calls
// are measured — including the ones that fail, which the error string alone
// cannot tell apart from a timeout.
func TestCallsAreMeasured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		wantStatus int64
	}{
		{name: "a call that works", status: http.StatusOK, wantStatus: 200},
		{name: "a call Graph refuses", status: http.StatusInternalServerError, wantStatus: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"id":"message-1"}`))
			}))
			t.Cleanup(srv.Close)

			client := &Client{
				baseURL: srv.URL,
				httpClient: &http.Client{
					Transport: measuring{provider: provider}.ClientTransport(http.DefaultTransport),
				},
			}
			_, _ = client.PostMessage("team", "channel", Message{Title: "hello"})

			var collected metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &collected); err != nil {
				t.Fatalf("collect: %v", err)
			}

			point := durationPoint(t, collected, "http.client.request.duration")
			if got, ok := point.Attributes.Value(attribute.Key("http.response.status_code")); !ok || got.AsInt64() != tt.wantStatus {
				t.Errorf("status attribute = %v, want %d", got.AsInt64(), tt.wantStatus)
			}
			if got, ok := point.Attributes.Value(attribute.Key("server.address")); !ok || got.AsString() == "" {
				t.Error("no server.address attribute, so Graph cannot be told from the token endpoint")
			}
		})
	}
}

// measuring is the real wrapper with a provider a test can read back.
type measuring struct{ provider *sdkmetric.MeterProvider }

func (m measuring) ClientTransport(base http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(base,
		otelhttp.WithMeterProvider(m.provider),
		otelhttp.WithTracerProvider(tracenoop.NewTracerProvider()),
	)
}

func durationPoint(t *testing.T, collected metricdata.ResourceMetrics, name string) metricdata.HistogramDataPoint[float64] {
	t.Helper()

	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s is %T, want a float histogram", name, m.Data)
			}
			if len(histogram.DataPoints) != 1 {
				t.Fatalf("%s has %d data points, want 1", name, len(histogram.DataPoints))
			}
			return histogram.DataPoints[0]
		}
	}

	t.Fatalf("no metric named %s was recorded", name)
	return metricdata.HistogramDataPoint[float64]{}
}

func TestTokenURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.GraphConfig
		want string
	}{
		{
			name: "the public cloud endpoint is derived from the tenant",
			cfg:  config.GraphConfig{TenantID: "tenant-1"},
			want: "https://login.microsoftonline.com/tenant-1/oauth2/v2.0/token",
		},
		{
			name: "a configured endpoint is used as it stands",
			cfg:  config.GraphConfig{TenantID: "tenant-1", TokenURL: "https://login.example/token"},
			want: "https://login.example/token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := TokenURL(tt.cfg); got != tt.want {
				t.Errorf("TokenURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
