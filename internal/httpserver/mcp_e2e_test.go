package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pflege-de-labs/teamster/internal/audit"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// text is what the agent reads: the tool result as JSON, or its error text.
func text(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestMCPEndToEnd(t *testing.T) {
	// Subtests share one session and one store, and run in order.
	st := scopedStore(t, "editor")
	st.destinations["d1"] = models.Destination{ID: "d1", Name: "Platform alerts", TeamID: "platform", ChannelID: "alerts"}
	st.destinations["d2"] = models.Destination{ID: "d2", Name: "Payments alerts", TeamID: "payments", ChannelID: "alerts"}
	delete(st.destinations, "inside")
	delete(st.destinations, "outside")
	st.templates["tpl-1"] = models.Template{ID: "tpl-1", Name: "one", Body: "{}"}
	st.routes["r1"] = models.Route{ID: "r1", Name: "db", LabelSelector: map[string]string{"team": "db"}, DestinationID: "d1", TemplateID: "tpl-1", Priority: 5}
	st.users["tester"] = models.User{Subject: "tester", Name: "Tester", Source: "oidc", Roles: []string{"editor"}}
	const secret = "tmcp_e2e"
	tok, err := st.CreateMCPToken(t.Context(), models.MCPToken{Name: "e2e", TokenHash: hashToken(secret), CreatedBy: "tester", Access: models.MCPAccessWrite})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Server: config.ServerConfig{Addr: ":0"},
		Admin:  config.AdminConfig{Username: "admin", Password: "pass"},
		MCP:    config.MCPConfig{Enabled: true},

		Samples: config.SamplesConfig{Enabled: true},
	}
	graphClient := &fakeMessenger{
		teams:    []graph.Team{{ID: "platform", Name: "Platform"}},
		channels: map[string][]graph.Channel{"platform": {{ID: "alerts", Name: "Alerts"}}},
	}
	srv, err := NewServer(quietLog, cfg, audit.Wrap(st, audit.NewRecorder(quietLog, nil, 1, audit.NewDBSink(st))), graphClient, nil, &fakeMessenger{}, metrics.Disabled(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "e2e"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{secret}},
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res
	}

	t.Run("list_destinations shows the granted one only", func(t *testing.T) {
		var got []models.Destination
		if err := json.Unmarshal([]byte(text(t, call("list_destinations", nil))), &got); err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, d := range got {
			ids = append(ids, d.ID)
		}
		if !slices.Equal(ids, []string{"d1"}) {
			t.Errorf("destination ids = %v, want [d1]", ids)
		}
	})

	t.Run("get_destination of an ungranted id is a tool error", func(t *testing.T) {
		res := call("get_destination", map[string]any{"id": "d2"})
		if !res.IsError || !strings.Contains(strings.ToLower(text(t, res)), "not found") {
			t.Errorf("get_destination d2 = error %v, %s", res.IsError, text(t, res))
		}
	})

	t.Run("template lifecycle", func(t *testing.T) {
		res := call("create_template", map[string]any{"name": "e2e", "body": `{"type":"AdaptiveCard"}`})
		if res.IsError {
			t.Fatalf("create_template: %s", text(t, res))
		}
		var created models.Template
		if err := json.Unmarshal([]byte(text(t, res)), &created); err != nil || created.ID == "" {
			t.Fatalf("create_template result %q: %v", text(t, res), err)
		}
		if res := call("update_template", map[string]any{"id": created.ID, "name": "renamed", "body": `{"type":"AdaptiveCard"}`}); res.IsError {
			t.Fatalf("update_template: %s", text(t, res))
		}
		if out := text(t, call("get_template", map[string]any{"id": created.ID})); !strings.Contains(out, "renamed") {
			t.Errorf("get_template = %s, want the new name", out)
		}
		if res := call("delete_template", map[string]any{"id": created.ID}); res.IsError {
			t.Fatalf("delete_template: %s", text(t, res))
		}
		if res := call("get_template", map[string]any{"id": created.ID}); !res.IsError {
			t.Errorf("get_template after delete = %s, want an error", text(t, res))
		}
	})

	t.Run("list_channels returns the team's channels", func(t *testing.T) {
		if out := text(t, call("list_channels", map[string]any{"id": "platform"})); !strings.Contains(out, "alerts") {
			t.Errorf("list_channels = %s, want the alerts channel", out)
		}
	})

	// A path mismatch between a tool and its endpoint hid behind the stub admin API once.
	t.Run("every read tool is answered by the real admin API", func(t *testing.T) {
		tests := []struct {
			tool string
			args map[string]any
		}{
			{"list_templates", nil},
			{"get_template", map[string]any{"id": "tpl-1"}},
			{"list_destinations", nil},
			{"get_destination", map[string]any{"id": "d1"}},
			{"list_routes", nil},
			{"get_route", map[string]any{"id": "r1"}},
			{"preview_template", map[string]any{"body": `{"type":"AdaptiveCard"}`, "sample": "{}"}},
			{"match_route", map[string]any{"labels": map[string]string{"team": "db"}}},
			{"list_teams", nil},
			{"list_channels", map[string]any{"id": "platform"}},
			{"list_sampled_labels", nil},
		}
		for _, tt := range tests {
			if res := call(tt.tool, tt.args); res.IsError {
				t.Errorf("%s: %s", tt.tool, text(t, res))
			}
		}
	})

	t.Run("match_route returns the matching route", func(t *testing.T) {
		out := text(t, call("match_route", map[string]any{"labels": map[string]string{"team": "db"}}))
		if !strings.Contains(out, "r1") {
			t.Errorf("match_route = %s", out)
		}
	})

	t.Run("writes are audited as mcp", func(t *testing.T) {
		st.mu.Lock()
		defer st.mu.Unlock()
		var templates []string
		for _, e := range st.auditEvents {
			if e.Actor.Via != models.ViaMCP || e.Actor.Subject != "tester" {
				t.Errorf("audit event %s actor = %+v, want tester via mcp", e.Action, e.Actor)
			}
			if strings.HasPrefix(e.Action, "template.") {
				templates = append(templates, e.Action)
			}
		}
		if want := []string{"template.create", "template.update", "template.delete"}; !slices.Equal(templates, want) {
			t.Errorf("template audit actions = %v, want %v", templates, want)
		}
	})

	t.Run("malformed arguments leave the session usable", func(t *testing.T) {
		for _, args := range []map[string]any{{}, {"id": 5}} {
			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_template", Arguments: args})
			if err == nil && !res.IsError {
				t.Errorf("get_template %v succeeded", args)
			}
		}
		if res := call("list_templates", nil); res.IsError {
			t.Errorf("list_templates after malformed calls: %s", text(t, res))
		}
	})

	t.Run("a revoked token is refused", func(t *testing.T) {
		if err := st.DeleteMCPToken(t.Context(), tok.ID); err != nil {
			t.Fatal(err)
		}
		_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_templates"})
		if err == nil || !strings.Contains(err.Error(), "Unauthorized") {
			t.Errorf("call after revocation = %v, want the SDK's Unauthorized (401) error", err)
		}
	})
}

func TestMCPAcceptsAForeignHostOnLoopback(t *testing.T) {
	t.Parallel()

	// httptest listens on 127.0.0.1, so a foreign Host is the sidecar-proxy case the SDK guard would 403.
	st := scopedStore(t, "editor")
	st.users["tester"] = models.User{Subject: "tester", Name: "Tester", Source: "oidc", Roles: []string{"editor"}}
	const secret = "tmcp_host"
	if _, err := st.CreateMCPToken(t.Context(), models.MCPToken{Name: "h", TokenHash: hashToken(secret), CreatedBy: "tester", Access: models.MCPAccessRead}); err != nil {
		t.Fatal(err)
	}
	_, srv := mcpTestServer(t, st, config.MCPConfig{Enabled: true})
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	tests := []struct {
		name string
		host string
	}{
		{"loopback host", ""},
		{"foreign host", "teamster.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Host = tt.host
			req.Header.Set("Authorization", "Bearer "+secret)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
		})
	}
}
