package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

var (
	readTools = []string{
		"get_destination", "get_route", "get_template", "list_channels", "list_destinations", "list_routes",
		"list_teams", "list_templates", "match_route", "preview_template",
	}
	writeTools = []string{
		"create_destination", "create_route", "create_template", "delete_destination", "delete_route",
		"delete_template", "update_destination", "update_route", "update_template",
	}
)

type seenCall struct{ method, path, body string }

// toolSession serves the tools on an in-memory transport over an admin API that records what it is asked.
func toolSession(t *testing.T, access string, readOnly, samples bool, status int, tel telemetry) (*mcp.ClientSession, func() []seenCall) {
	t.Helper()
	api, _ := mcpTestServer(t, newFakeStore(), config.MCPConfig{Enabled: true, ReadOnly: readOnly})
	api.cfg.Samples.Enabled = samples
	if tel != nil {
		api.metrics = tel
	}
	var mu sync.Mutex
	var seen []seenCall
	api.adminAPI = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw []byte
		if r.Body != nil {
			raw, _ = io.ReadAll(r.Body)
		}
		mu.Lock()
		seen = append(seen, seenCall{r.Method, r.URL.EscapedPath(), string(raw)})
		mu.Unlock()
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = w.Write([]byte(`{"error":"nope"}`))
		} else {
			_, _ = w.Write([]byte(`{}`))
		}
	})

	ctx := mcpCtx(access, authz.RoleEditor)
	srv := api.mcpServerFor(httptest.NewRequest(http.MethodPost, "/mcp", nil).WithContext(ctx))
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, func() []seenCall {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

func TestMCPToolList(t *testing.T) {
	t.Parallel()

	all := append(append([]string{}, readTools...), writeTools...)
	tests := []struct {
		name     string
		access   string
		readOnly bool
		samples  bool
		want     []string
	}{
		{"write token", models.MCPAccessWrite, false, true, append(slices.Clone(all), "list_sampled_labels")},
		{"read token", models.MCPAccessRead, false, true, append(slices.Clone(readTools), "list_sampled_labels")},
		{"read-only deployment", models.MCPAccessWrite, true, true, append(slices.Clone(readTools), "list_sampled_labels")},
		{"samples off", models.MCPAccessWrite, false, false, all},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cs, _ := toolSession(t, tt.access, tt.readOnly, tt.samples, 200, nil)
			var got []string
			for tool, err := range cs.Tools(context.Background(), nil) {
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, tool.Name)
			}
			sort.Strings(got)
			sort.Strings(tt.want)
			if !slices.Equal(got, tt.want) {
				t.Errorf("tools = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMCPToolDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tool     string
		args     map[string]any
		method   string
		path     string
		wantBody map[string]any // subset of the JSON body sent
	}{
		{"list_templates", nil, "GET", "/api/templates", nil},
		{"get_template", map[string]any{"id": "t 1"}, "GET", "/api/templates/t%201", nil},
		{"create_template", map[string]any{"name": "n", "body": "{}"}, "POST", "/api/templates", map[string]any{"name": "n"}},
		{"create_template", map[string]any{"name": "n", "text": "hi"}, "POST", "/api/templates", map[string]any{"text": "hi"}},
		{"update_template", map[string]any{"id": "t1", "name": "n", "body": "{}", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z"}, "PUT", "/api/templates/t1", map[string]any{"id": "t1", "name": "n"}},
		{"delete_template", map[string]any{"id": "t1"}, "DELETE", "/api/templates/t1", nil},
		{"list_destinations", nil, "GET", "/api/destinations", nil},
		{"get_destination", map[string]any{"id": "d1"}, "GET", "/api/destinations/d1", nil},
		{"create_destination", map[string]any{"name": "n", "team_id": "t", "channel_id": "c"}, "POST", "/api/destinations", map[string]any{"team_id": "t"}},
		{"update_destination", map[string]any{"id": "d1", "name": "n", "team_id": "t", "channel_id": "c"}, "PUT", "/api/destinations/d1", map[string]any{"id": "d1"}},
		{"delete_destination", map[string]any{"id": "d1"}, "DELETE", "/api/destinations/d1", nil},
		{"list_routes", nil, "GET", "/api/routes", nil},
		{"get_route", map[string]any{"id": "r1"}, "GET", "/api/routes/r1", nil},
		{"create_route", map[string]any{"name": "n", "label_selector": map[string]any{"a": "b"}}, "POST", "/api/routes", map[string]any{"label_selector": map[string]any{"a": "b"}}},
		{"update_route", map[string]any{"id": "r1", "name": "n"}, "PUT", "/api/routes/r1", map[string]any{"id": "r1"}},
		{"delete_route", map[string]any{"id": "r1"}, "DELETE", "/api/routes/r1", nil},
		{"preview_template", map[string]any{"body": "{}", "sample": "{}"}, "POST", "/api/templates/preview", map[string]any{"sample": "{}"}},
		{"match_route", map[string]any{"labels": map[string]any{"a": "b"}}, "POST", "/api/routing/match", map[string]any{"labels": map[string]any{"a": "b"}}},
		{"list_teams", nil, "GET", "/api/graph/teams", nil},
		{"list_channels", map[string]any{"id": "t1"}, "GET", "/api/graph/teams/t1/channels", nil},
		{"list_sampled_labels", nil, "GET", "/api/samples", nil},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()

			cs, seen := toolSession(t, models.MCPAccessWrite, false, true, 200, nil)
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatal(err)
			}
			calls := seen()
			if len(calls) != 1 || calls[0].method != tt.method || calls[0].path != tt.path {
				t.Fatalf("api saw %+v, want one %s %s (result %+v)", calls, tt.method, tt.path, res)
			}
			if tt.wantBody != nil {
				var got map[string]any
				if err := json.Unmarshal([]byte(calls[0].body), &got); err != nil {
					t.Fatalf("body %q: %v", calls[0].body, err)
				}
				for k, v := range tt.wantBody {
					gj, _ := json.Marshal(got[k])
					wj, _ := json.Marshal(v)
					if string(gj) != string(wj) {
						t.Errorf("body[%q] = %s, want %s", k, gj, wj)
					}
				}
			}
		})
	}
}

func TestMCPToolFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		tool        string
		args        map[string]any
		status      int
		wantOutcome string
		wantCalls   int
	}{
		{"ok", "list_routes", nil, 200, "ok", 1},
		{"forbidden is refused", "delete_route", map[string]any{"id": "r1"}, 403, "refused", 1},
		{"not found is refused", "get_route", map[string]any{"id": "r1"}, 404, "refused", 1},
		{"validation is invalid", "create_route", map[string]any{"name": "n"}, 422, "invalid", 1},
		{"server error fails", "list_routes", nil, 500, "failed", 1},
		{"bad id is invalid and never dispatched", "get_route", map[string]any{"id": "a/b"}, 200, "invalid", 0},
		{"reserved route id", "update_route", map[string]any{"id": "global-default", "name": "n"}, 200, "invalid", 0},
		{"reserved template id", "get_template", map[string]any{"id": "source-defaults"}, 200, "invalid", 0},
		{"preview as a template id", "get_template", map[string]any{"id": "preview"}, 200, "invalid", 0},
		{"normal id dispatches", "get_template", map[string]any{"id": "t1"}, 200, "ok", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tel := newRecordingTelemetry()
			cs, seen := toolSession(t, models.MCPAccessWrite, false, false, tt.status, tel)
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatalf("protocol error: %v", err)
			}
			if wantErr := tt.status >= 400 || tt.wantCalls == 0; res.IsError != wantErr {
				t.Errorf("IsError = %v, want %v", res.IsError, wantErr)
			}
			if got := len(seen()); got != tt.wantCalls {
				t.Errorf("api calls = %d, want %d", got, tt.wantCalls)
			}
			tel.mu.Lock()
			defer tel.mu.Unlock()
			if want := [][2]string{{tt.tool, tt.wantOutcome}}; !slices.Equal(tel.tools, want) {
				t.Errorf("metric = %v, want %v", tel.tools, want)
			}
		})
	}
}
