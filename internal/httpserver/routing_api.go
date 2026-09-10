package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
)

const maxMatchBytes = 1 << 16

type graphNode struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Detail   string `json:"detail,omitempty"`
	Selector string `json:"selector,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Missing  bool   `json:"missing,omitempty"`
}

type graphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

func (s *Server) handleRoutingGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	routes, err := s.store.ListRoutes()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	destinations, err := s.store.ListDestinations()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	templates, err := s.store.ListTemplates()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	nodes, links := buildGraph(routes, destinations, templates)
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "links": links})
}

// buildGraph resolves the identifiers a route stores into nodes. A route
// pointing at something deleted becomes a node marked missing rather than a
// dropped link: that broken state is exactly what the view exists to show.
func buildGraph(routes []models.Route, destinations []models.Destination, templates []models.Template) ([]graphNode, []graphLink) {
	nodes := []graphNode{}
	links := []graphLink{}
	seen := map[string]bool{}

	add := func(node graphNode) {
		if !seen[node.ID] {
			seen[node.ID] = true
			nodes = append(nodes, node)
		}
	}

	for _, destination := range destinations {
		add(graphNode{
			ID:     "destination:" + destination.ID,
			Kind:   "destination",
			Label:  destination.Name,
			Detail: "team " + destination.TeamID + " · channel " + destination.ChannelID,
		})
	}
	for _, template := range templates {
		add(graphNode{ID: "template:" + template.ID, Kind: "template", Label: template.Name})
	}

	for _, route := range routes {
		routeID := "route:" + route.ID
		add(graphNode{
			ID:       routeID,
			Kind:     "route",
			Label:    route.Name,
			Selector: selectorSummary(route.LabelSelector),
			Priority: route.Priority,
			Default:  route.IsDefault,
		})

		for _, ref := range []struct {
			kind string
			id   string
		}{
			{"destination", route.DestinationID},
			{"template", route.TemplateID},
		} {
			if ref.id == "" {
				continue
			}

			target := ref.kind + ":" + ref.id
			if !seen[target] {
				add(graphNode{
					ID:      target,
					Kind:    ref.kind,
					Label:   "missing " + ref.kind,
					Detail:  ref.id,
					Missing: true,
				})
			}
			links = append(links, graphLink{Source: routeID, Target: target})
		}
	}

	return nodes, links
}

// selectorSummary renders a selector the way an operator writes it. Unlike the
// views version it leaves an empty selector empty, because a graph node shows
// nothing rather than prose about matching nothing.
func selectorSummary(selector map[string]string) string {
	if len(selector) == 0 {
		return ""
	}

	keys := make([]string, 0, len(selector))
	for key := range selector {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+selector[key])
	}
	return strings.Join(pairs, ", ")
}

func (s *Server) handleRoutingMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMatchBytes)

	var req struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	route, reason, err := s.router.Match(req.Labels)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	answer := map[string]any{"reason": reason, "explanation": explainReason(reason, route)}
	if reason == routing.ReasonSelector || reason == routing.ReasonDefault {
		answer["route"] = map[string]any{
			"id":       route.ID,
			"name":     route.Name,
			"node":     "route:" + route.ID,
			"selector": selectorSummary(route.LabelSelector),
			"priority": route.Priority,
		}
	}
	writeJSON(w, http.StatusOK, answer)
}

func explainReason(reason routing.Reason, route models.Route) string {
	switch reason {
	case routing.ReasonSelector:
		return fmt.Sprintf("%q matched on %s, at priority %d", route.Name, selectorSummary(route.LabelSelector), route.Priority)
	case routing.ReasonDefault:
		return fmt.Sprintf("no selector matched, so the default route %q takes it", route.Name)
	case routing.ReasonNoRoutes:
		return "no routes are configured, so this alert would be rejected"
	default:
		return "no selector matched and no default route exists, so this alert would be rejected"
	}
}
