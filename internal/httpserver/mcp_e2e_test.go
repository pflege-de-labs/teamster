package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pflege-de-labs/teamster/internal/audit"
	"github.com/pflege-de-labs/teamster/internal/config"
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
	}
	srv, err := NewServer(quietLog, cfg, audit.Wrap(st, audit.NewRecorder(quietLog, nil, 1, audit.NewDBSink(st))), &fakeMessenger{}, nil, &fakeMessenger{}, metrics.Disabled(), nil)
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
		out := text(t, call("list_destinations", nil))
		if !strings.Contains(out, `"d1"`) || strings.Contains(out, `"d2"`) {
			t.Errorf("list_destinations = %s", out)
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

	t.Run("match_route returns the matching route", func(t *testing.T) {
		out := text(t, call("match_route", map[string]any{"labels": map[string]string{"team": "db"}}))
		if !strings.Contains(out, "r1") {
			t.Errorf("match_route = %s", out)
		}
	})

	t.Run("writes are audited as mcp", func(t *testing.T) {
		st.mu.Lock()
		defer st.mu.Unlock()
		var n int
		for _, e := range st.auditEvents {
			n++
			if e.Actor.Via != models.ViaMCP || e.Actor.Subject != "tester" {
				t.Errorf("audit event %s actor = %+v, want tester via mcp", e.Action, e.Actor)
			}
		}
		if n < 3 {
			t.Errorf("audit events = %d, want the template create, update and delete", n)
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
