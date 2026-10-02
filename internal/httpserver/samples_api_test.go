package httpserver

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func samplesConfig(enabled bool) config.Config {
	return config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Samples: config.SamplesConfig{Enabled: enabled, MaxValuesPerKey: 2},
	}
}

func seededSamples() *fakeStore {
	st := newFakeStore()
	now := time.Now().UTC()
	st.samples = []models.EventSample{
		{Kind: models.SampleAttribute, Key: "summary", SeenCount: 1, LastSeen: now},
		{Kind: models.SampleAttribute, Key: "description", SeenCount: 1, LastSeen: now},
		{Kind: models.SampleLabel, Key: "env", Value: "prod", SeenCount: 3, LastSeen: now},
		{Kind: models.SampleLabel, Key: "env", Value: "stage", SeenCount: 1, LastSeen: now},
		{Kind: models.SampleLabel, Key: "env", Value: "dev", SeenCount: 1, LastSeen: now},
	}
	return st
}

func TestSamplesEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		enabled    bool
		roles      []authz.Role
		method     string
		fail       bool
		wantStatus int
		want       *samplesResponse
	}{
		{
			name: "an editor gets labels capped per key and attribute keys by name", enabled: true,
			roles: []authz.Role{authz.RoleEditor}, method: http.MethodGet, wantStatus: http.StatusOK,
			want: &samplesResponse{
				Labels:     map[string][]string{"env": {"prod", "stage"}},
				Attributes: []string{"description", "summary"},
			},
		},
		{
			name: "an admin may ask too", enabled: true,
			roles: []authz.Role{authz.RoleAdmin}, method: http.MethodGet, wantStatus: http.StatusOK,
		},
		{
			name: "a viewer has nothing to complete", enabled: true,
			roles: []authz.Role{authz.RoleViewer}, method: http.MethodGet, wantStatus: http.StatusForbidden,
		},
		{
			name: "disabled answers empty rather than with what an earlier run kept", enabled: false,
			roles: []authz.Role{authz.RoleEditor}, method: http.MethodGet, wantStatus: http.StatusOK,
			want: &samplesResponse{Labels: map[string][]string{}, Attributes: []string{}},
		},
		{
			name: "a store failure is a 500", enabled: true, fail: true,
			roles: []authz.Role{authz.RoleEditor}, method: http.MethodGet, wantStatus: http.StatusInternalServerError,
		},
		{
			name: "only GET", enabled: true,
			roles: []authz.Role{authz.RoleEditor}, method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(seededSamples(), tt.roles...)
			if tt.fail {
				st.fail("ListEventSamples")
			}
			handler := mustServer(t, samplesConfig(tt.enabled), st, &fakeMessenger{}).Handler

			rec := asRole(t, handler, tt.method, "/api/samples", "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("%s /api/samples = %d, want %d: %s", tt.method, rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.want == nil {
				return
			}
			var got samplesResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !reflect.DeepEqual(got, *tt.want) {
				t.Errorf("GET /api/samples = %+v, want %+v", got, *tt.want)
			}
		})
	}
}

// Only editors fetch samples, so only their pages advertise the endpoint.
func TestTheLayoutAdvertisesSamplesOnlyToEditors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		roles   []authz.Role
		enabled bool
		want    bool
	}{
		{"editor", []authz.Role{authz.RoleEditor}, true, true},
		{"viewer", []authz.Role{authz.RoleViewer}, true, false},
		{"disabled", []authz.Role{authz.RoleEditor}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := mustServer(t, samplesConfig(tt.enabled), sessionAs(newFakeStore(), tt.roles...), &fakeMessenger{}).Handler
			for _, path := range []string{"/admin", "/admin/routing"} {
				rec := asRole(t, handler, http.MethodGet, path, "")
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s = %d", path, rec.Code)
				}
				if got := strings.Contains(rec.Body.String(), `name="teamster-samples"`); got != tt.want {
					t.Errorf("GET %s advertises samples = %v, want %v", path, got, tt.want)
				}
			}
		})
	}
}

type recordingSampler struct {
	mu         sync.Mutex
	observed   []map[string]string
	attributes []map[string]string
}

func (r *recordingSampler) Observe(labels, attributes map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observed = append(r.observed, labels)
	r.attributes = append(r.attributes, attributes)
}

// Sampling happens before routing, so an event nothing routes still counts.
func TestEveryIncomingEventIsSampled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		body      string
		want      map[string]string
		wantAttrs map[string]string
	}{
		{
			"universal", "/webhook/universal", `{"state":"open","labels":{"team":"db"},"attributes":{"summary":"s"}}`,
			map[string]string{"team": "db", models.SourceLabel: models.SourceUniversal}, map[string]string{"summary": "s"},
		},
		{
			"alertmanager", "/webhook/alertmanager",
			`{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"Disk"},"annotations":{"runbook":"r"}}]}`,
			map[string]string{"alertname": "Disk", models.SourceLabel: models.SourceAlertmanager}, map[string]string{"runbook": "r"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &recordingSampler{}
			srv, err := NewServer(quietLog, samplesConfig(true), newFakeStore(), &fakeMessenger{}, nil, &fakeMessenger{}, metrics.Disabled(), rec)
			if err != nil {
				t.Fatalf("NewServer: %v", err)
			}
			postWebhook(t, srv.Handler, tt.path, "token", tt.body)

			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.observed) != 1 || !reflect.DeepEqual(rec.observed[0], tt.want) {
				t.Errorf("observed %v, want one event labelled %v", rec.observed, tt.want)
			}
			if len(rec.attributes) != 1 || !reflect.DeepEqual(rec.attributes[0], tt.wantAttrs) {
				t.Errorf("attributes %v, want %v", rec.attributes, tt.wantAttrs)
			}
		})
	}
}

// A group's alerts are sampled one by one, because its common labels miss
// what they differ in; the group itself is sampled too.
func TestAlertmanagerGroupSamplesEachAlert(t *testing.T) {
	t.Parallel()

	rec := &recordingSampler{}
	srv, err := NewServer(quietLog, samplesConfig(true), newFakeStore(), &fakeMessenger{}, nil, &fakeMessenger{}, metrics.Disabled(), rec)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	postWebhook(t, srv.Handler, "/webhook/alertmanager", "token", `{"status":"firing","commonLabels":{"team":"db"},"alerts":[
		{"status":"firing","labels":{"team":"db","pod":"a"},"annotations":{"summary":"a"}},
		{"status":"firing","labels":{"team":"db","pod":"b"}}]}`)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	want := []map[string]string{
		{"team": "db", "pod": "a", models.SourceLabel: models.SourceAlertmanager},
		{"team": "db", "pod": "b", models.SourceLabel: models.SourceAlertmanager},
		{"team": "db", models.SourceLabel: models.SourceAlertmanager},
	}
	if !reflect.DeepEqual(rec.observed, want) {
		t.Errorf("observed %v, want %v", rec.observed, want)
	}
	if len(rec.attributes) != 3 || rec.attributes[0]["summary"] != "a" {
		t.Errorf("attributes %v, want the first alert's annotations first", rec.attributes)
	}
}
