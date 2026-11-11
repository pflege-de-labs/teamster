package httpserver

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// mcpTool is one tool and the admin API call it dispatches to; "{id}" in path is the input's id.
type mcpTool struct {
	name, description string
	method, path      string
	writes            bool
}

type noInput struct{}

type idInput struct {
	ID string `json:"id" jsonschema:"the id, as returned by the list tool"`
}

type updateTemplateInput struct {
	models.Template
	ID string `json:"id" jsonschema:"id of the template to replace"`
}

type updateDestinationInput struct {
	models.Destination
	ID string `json:"id" jsonschema:"id of the destination to replace"`
}

type updateRouteInput struct {
	models.Route
	ID string `json:"id" jsonschema:"id of the route to replace"`
}

const (
	selectorHelp = "label_selector matches when every label is on the alert with the same value; an empty selector matches every alert. "
	routeHelp    = selectorHelp + "Every matching root route delivers, highest priority first. " +
		"A route with parent_id refines its parent and delivers as well as it, or instead of it when greedy; " +
		"a child that names no destination or template inherits the nearest ancestor's. " +
		"A default route is used only when no other root route matches; with none, the global default destination receives the alert."
	irreversible = " This cannot be undone."
)

// addMCPTools offers the tools this request's token may use; write tools only when it may write.
func (s *Server) addMCPTools(srv *mcp.Server, r *http.Request) {
	writes := mcpWrites(r.Context(), s.cfg.MCP)
	add := func(t mcpTool) bool { return !t.writes || writes }

	if t := (mcpTool{"list_templates", "List the Adaptive Card templates.", http.MethodGet, "/api/templates", false}); add(t) {
		register(s, srv, t, func(noInput) (string, any) { return "", nil })
	}
	if t := (mcpTool{"get_template", "Get one template by id.", http.MethodGet, "/api/templates/{id}", false}); add(t) {
		register(s, srv, t, func(in idInput) (string, any) { return in.ID, nil }, "id")
	}
	if t := (mcpTool{"create_template", "Create a template. title and text are Go templates over the alert; body is Adaptive Card JSON. " +
		"sources limits it to webhooks (alertmanager, universal, teamsv2); none means any.", http.MethodPost, "/api/templates", true}); add(t) {
		register(s, srv, t, func(in models.Template) (string, any) { return "", in })
	}
	if t := (mcpTool{"update_template", "Replace a template; send every field.", http.MethodPut, "/api/templates/{id}", true}); add(t) {
		register(s, srv, t, func(in updateTemplateInput) (string, any) { in.Template.ID = in.ID; return in.ID, in.Template }, "id")
	}
	if t := (mcpTool{"delete_template", "Delete a template." + irreversible, http.MethodDelete, "/api/templates/{id}", true}); add(t) {
		register(s, srv, t, func(in idInput) (string, any) { return in.ID, nil }, "id")
	}

	if t := (mcpTool{"list_destinations", "List the Team channel destinations.", http.MethodGet, "/api/destinations", false}); add(t) {
		register(s, srv, t, func(noInput) (string, any) { return "", nil })
	}
	if t := (mcpTool{"get_destination", "Get one destination by id.", http.MethodGet, "/api/destinations/{id}", false}); add(t) {
		register(s, srv, t, func(in idInput) (string, any) { return in.ID, nil }, "id")
	}
	if t := (mcpTool{"create_destination", "Create a destination: a Team id and channel id from list_teams and list_channels.",
		http.MethodPost, "/api/destinations", true}); add(t) {
		register(s, srv, t, func(in models.Destination) (string, any) { return "", in }, "name", "team_id", "channel_id")
	}
	if t := (mcpTool{"update_destination", "Replace a destination; send every field.", http.MethodPut, "/api/destinations/{id}", true}); add(t) {
		register(s, srv, t, func(in updateDestinationInput) (string, any) { in.Destination.ID = in.ID; return in.ID, in.Destination }, "id", "name", "team_id", "channel_id")
	}
	if t := (mcpTool{"delete_destination", "Delete a destination." + irreversible, http.MethodDelete, "/api/destinations/{id}", true}); add(t) {
		register(s, srv, t, func(in idInput) (string, any) { return in.ID, nil }, "id")
	}

	if t := (mcpTool{"list_routes", "List the routes. " + routeHelp, http.MethodGet, "/api/routes", false}); add(t) {
		register(s, srv, t, func(noInput) (string, any) { return "", nil })
	}
	if t := (mcpTool{"get_route", "Get one route by id.", http.MethodGet, "/api/routes/{id}", false}); add(t) {
		register(s, srv, t, func(in idInput) (string, any) { return in.ID, nil }, "id")
	}
	if t := (mcpTool{"create_route", "Create a route from label_selector to destination_id and template_id. " + routeHelp,
		http.MethodPost, "/api/routes", true}); add(t) {
		register(s, srv, t, func(in models.Route) (string, any) { return "", in }, "name")
	}
	if t := (mcpTool{"update_route", "Replace a route; send every field. " + routeHelp, http.MethodPut, "/api/routes/{id}", true}); add(t) {
		register(s, srv, t, func(in updateRouteInput) (string, any) { in.Route.ID = in.ID; return in.ID, in.Route }, "id", "name")
	}
	if t := (mcpTool{"delete_route", "Delete a route; one with children is refused." + irreversible, http.MethodDelete, "/api/routes/{id}", true}); add(t) {
		register(s, srv, t, func(in idInput) (string, any) { return in.ID, nil }, "id")
	}

	if t := (mcpTool{"preview_template", "Render a template body with a sample alert JSON, without saving it.",
		http.MethodPost, "/api/templates/preview", false}); add(t) {
		register(s, srv, t, func(in previewRequest) (string, any) { return "", in })
	}
	if t := (mcpTool{"match_route", "Dry-run routing: which routes and destinations the given alert labels reach. " + routeHelp,
		http.MethodPost, "/api/routing/match", false}); add(t) {
		register(s, srv, t, func(in matchRequest) (string, any) { return "", in }, "labels")
	}
	if t := (mcpTool{"list_teams", "List the Teams the application can post to.", http.MethodGet, "/api/graph/teams", false}); add(t) {
		register(s, srv, t, func(noInput) (string, any) { return "", nil })
	}
	if t := (mcpTool{"list_channels", "List the channels of one Team, by the id list_teams returned.", http.MethodGet, "/api/graph/teams/{id}/channels", false}); add(t) {
		register(s, srv, t, func(in idInput) (string, any) { return in.ID, nil }, "id")
	}
	if s.cfg.Samples.Enabled && s.mayComplete(r) {
		t := mcpTool{"list_sampled_labels", "List label keys and values seen on recent alerts, for match_route.", http.MethodGet, "/api/samples", false}
		register(s, srv, t, func(noInput) (string, any) { return "", nil })
	}
}

// register adds one tool; in extracts the path id and request body from the typed input.
func register[In any](s *Server, srv *mcp.Server, t mcpTool, in func(In) (id string, body any), required ...string) {
	mcp.AddTool(srv, &mcp.Tool{Name: t.name, Description: t.description, InputSchema: inputSchema[In](required)}, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		id, body := in(input)
		path := t.path
		if strings.Contains(path, "{id}") {
			var err error
			if path, err = apiPath(strings.Replace(path, "{id}", "%s", 1), id); err != nil {
				s.metrics.MCPToolCalled(ctx, t.name, "invalid")
				return toolError("id must be a single plain id"), nil, nil
			}
		}
		if !s.reachesOwnEndpoint(t, path) {
			s.metrics.MCPToolCalled(ctx, t.name, "invalid")
			return toolError("id is reserved for another endpoint"), nil, nil
		}
		status, raw, err := s.callAPI(ctx, t.method, path, body)
		if err != nil {
			logging.FromContext(ctx).Error("mcp: dispatch failed", "tool", t.name, "err", err)
			s.metrics.MCPToolCalled(ctx, t.name, "failed")
			return toolError(mcpServerError), nil, nil
		}
		s.metrics.MCPToolCalled(ctx, t.name, toolOutcome(status))
		res, err := apiResult(ctx, status, raw)
		if err != nil {
			logging.FromContext(ctx).Error("mcp: decode answer", "tool", t.name, "err", err)
			return toolError(mcpServerError), nil, nil
		}
		return res, nil, nil
	})
}

func toolOutcome(status int) string {
	switch {
	case status >= 500:
		return "failed"
	case status == http.StatusUnauthorized, status == http.StatusForbidden, status == http.StatusNotFound:
		return "refused"
	case status >= 400:
		return "invalid"
	}
	return "ok"
}

// inputSchema is the schema the SDK infers, so tools cannot drift from the models. Only the
// listed fields are required: inference demands every field without omitempty, server-set ones included.
// created_at and updated_at stay optional so a get_* result can be sent back as it is.
func inputSchema[In any](required []string) *jsonschema.Schema {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic("mcp: input schema: " + err.Error())
	}
	schema.Required = required
	return schema
}

// reachesOwnEndpoint guards against an id naming a sibling route such as /api/routes/global-default,
// which the mux serves with a different handler than the tool's own.
func (s *Server) reachesOwnEndpoint(t mcpTool, path string) bool {
	want := t.path
	if i := strings.Index(want, "{id}"); i >= 0 {
		want = want[:i]
	}
	_, got := s.adminRoutes.Handler(&http.Request{Method: t.method, URL: &url.URL{Path: path}})
	return got == want
}
