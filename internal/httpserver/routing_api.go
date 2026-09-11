package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/graph"
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
	Greedy   bool   `json:"greedy,omitempty"`
	Missing  bool   `json:"missing,omitempty"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
}

// Alerts flow left to right: what arrives, what decides, where it lands. The
// layout is computed here so it is deterministic and the tests can see it,
// leaving the browser to draw and to handle dragging.
const (
	columnGap = 300
	rowGap    = 104
	groupGap  = 64
)

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

	nodes, links := buildGraph(routes, destinations, templates, s.channelNamer(destinations))
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "links": links})
}

// buildGraph resolves the identifiers a route stores into nodes. A route
// pointing at something deleted becomes a node marked missing rather than a
// dropped link: that broken state is exactly what the view exists to show.
func buildGraph(routes []models.Route, destinations []models.Destination, templates []models.Template, channelName func(teamID, channelID string) string) ([]graphNode, []graphLink) {
	nodes := []graphNode{}
	links := []graphLink{}
	index := map[string]int{}

	add := func(node graphNode) {
		if _, seen := index[node.ID]; !seen {
			index[node.ID] = len(nodes)
			nodes = append(nodes, node)
		}
	}

	// Every alert enters the same router, so the flow starts from one node
	// rather than from each webhook endpoint.
	const sourceID = "source:webhook"
	add(graphNode{
		ID:     sourceID,
		Kind:   "source",
		Label:  "Incoming alerts",
		Detail: "POST /webhook/alertmanager · /webhook/universal",
	})

	// Routes in the order they are evaluated, so the picture reads the way the
	// router works: highest priority first, the default last.
	ordered := append([]models.Route(nil), routes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].IsDefault != ordered[j].IsDefault {
			return !ordered[i].IsDefault
		}
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority > ordered[j].Priority
		}
		return ordered[i].Name < ordered[j].Name
	})

	known := map[string]bool{}
	for _, route := range ordered {
		known[route.ID] = true
	}

	// A child sits one column right of its parent, so depth in the tree reads as
	// distance from the webhook.
	depths := routeDepths(ordered)
	rows := map[int]int{}
	maxDepth := 0

	for _, route := range ordered {
		depth := depths[route.ID]
		if depth > maxDepth {
			maxDepth = depth
		}

		routeID := "route:" + route.ID
		add(graphNode{
			ID:       routeID,
			Kind:     "route",
			Label:    route.Name,
			Selector: selectorSummary(route.LabelSelector),
			Priority: route.Priority,
			Default:  route.IsDefault,
			Greedy:   route.Greedy,
			X:        (1 + depth) * columnGap,
			Y:        rows[depth] * rowGap,
		})
		rows[depth]++

		// A child hangs off its parent, because that is the order it is
		// evaluated in; only a root is reached straight from the webhook.
		if route.ParentID != "" && known[route.ParentID] {
			links = append(links, graphLink{Source: "route:" + route.ParentID, Target: routeID})
			continue
		}
		links = append(links, graphLink{Source: sourceID, Target: routeID})
	}

	sinkColumn := (2 + maxDepth) * columnGap
	for row, destination := range destinations {
		add(graphNode{
			ID:     "destination:" + destination.ID,
			Kind:   "destination",
			Label:  destination.Name,
			Detail: channelName(destination.TeamID, destination.ChannelID),
			X:      sinkColumn,
			Y:      row * rowGap,
		})
	}

	templateTop := len(destinations)*rowGap + groupGap
	missingTop := templateTop + len(templates)*rowGap + groupGap
	missing := 0
	for row, template := range templates {
		add(graphNode{
			ID:    "template:" + template.ID,
			Kind:  "template",
			Label: template.Name,
			X:     sinkColumn,
			Y:     templateTop + row*rowGap,
		})
	}

	// An unset destination or template is the ancestor's, so the picture draws
	// the edge the alert will actually take rather than none at all.
	effective := inheritedTargets(ordered)

	for _, route := range ordered {
		for _, ref := range []struct {
			kind string
			id   string
		}{
			{"destination", effective[route.ID].destinationID},
			{"template", effective[route.ID].templateID},
		} {
			if ref.id == "" {
				continue
			}

			target := ref.kind + ":" + ref.id
			if _, known := index[target]; !known {
				add(graphNode{
					ID:      target,
					Kind:    ref.kind,
					Label:   "missing " + ref.kind,
					Detail:  ref.id,
					Missing: true,
					X:       sinkColumn,
					Y:       missingTop + missing*rowGap,
				})
				missing++
			}
			links = append(links, graphLink{Source: "route:" + route.ID, Target: target})
		}
	}

	return nodes, links
}

// channelNamer resolves the ids a destination stores into the names an
// operator recognises. Graph is best effort here: the picture is still worth
// drawing when the directory is unreachable, so a failed lookup falls back to
// the raw ids rather than failing the request.
func (s *Server) channelNamer(destinations []models.Destination) func(teamID, channelID string) string {
	teamNames := map[string]string{}
	if teams, err := s.directory.Teams(s.graph.ListTeams); err == nil {
		for _, team := range teams {
			teamNames[team.ID] = team.Name
		}
	}

	channelNames := map[string]string{}
	fetched := map[string]bool{}
	for _, destination := range destinations {
		if destination.TeamID == "" || fetched[destination.TeamID] {
			continue
		}
		fetched[destination.TeamID] = true

		channels, err := s.directory.Channels(destination.TeamID, func() ([]graph.Channel, error) {
			return s.graph.ListChannels(destination.TeamID)
		})
		if err != nil {
			continue
		}
		for _, channel := range channels {
			channelNames[destination.TeamID+"/"+channel.ID] = channel.Name
		}
	}

	return func(teamID, channelID string) string {
		team := teamNames[teamID]
		if team == "" {
			team = teamID
		}
		channel := channelNames[teamID+"/"+channelID]
		if channel == "" {
			channel = channelID
		}
		return team + " › " + channel
	}
}

// routeDepths counts how far each route sits below a root, stopping at MaxDepth
// so a tree broken by a cycle still produces a drawing.
func routeDepths(routes []models.Route) map[string]int {
	byID := map[string]models.Route{}
	for _, route := range routes {
		byID[route.ID] = route
	}

	depths := map[string]int{}
	for _, route := range routes {
		depth := 0
		for parent, ok := byID[route.ParentID]; ok && depth < routing.MaxDepth; parent, ok = byID[parent.ParentID] {
			depth++
		}
		depths[route.ID] = depth
	}
	return depths
}

type routeTargets struct {
	destinationID string
	templateID    string
}

// inheritedTargets resolves every route's destination and template through its
// ancestors, stopping at a parent that is missing or at MaxDepth so a broken
// tree cannot loop here.
func inheritedTargets(routes []models.Route) map[string]routeTargets {
	byID := map[string]models.Route{}
	for _, route := range routes {
		byID[route.ID] = route
	}

	effective := map[string]routeTargets{}
	for _, route := range routes {
		targets := routeTargets{destinationID: route.DestinationID, templateID: route.TemplateID}
		parent, ok := byID[route.ParentID]
		for depth := 0; ok && depth < routing.MaxDepth; depth++ {
			if targets.destinationID == "" {
				targets.destinationID = parent.DestinationID
			}
			if targets.templateID == "" {
				targets.templateID = parent.TemplateID
			}
			if targets.destinationID != "" && targets.templateID != "" {
				break
			}
			parent, ok = byID[parent.ParentID]
		}
		effective[route.ID] = targets
	}
	return effective
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

	result, err := s.router.Plan(req.Labels)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	routes, err := s.store.ListRoutes()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byID := map[string]models.Route{}
	for _, route := range routes {
		byID[route.ID] = route
	}

	answer := map[string]any{
		"reason":      result.Reason,
		"explanation": explainResult(result, byID),
		"deliveries":  deliveryAnswers(result, byID),
		"nodes":       matchedNodes(result),
	}
	// The route that matched first, kept for a caller that wants one answer —
	// even when a greedy child took the delivery away from it.
	if root, ok := byID[result.RootID]; ok {
		answer["route"] = routeAnswer(root)
	}
	writeJSON(w, http.StatusOK, answer)
}

func routeAnswer(route models.Route) map[string]any {
	return map[string]any{
		"id":       route.ID,
		"name":     route.Name,
		"node":     "route:" + route.ID,
		"selector": selectorSummary(route.LabelSelector),
		"priority": route.Priority,
	}
}

func deliveryAnswers(result routing.Result, byID map[string]models.Route) []map[string]any {
	answers := make([]map[string]any, 0, len(result.Deliveries))
	for _, delivery := range result.Deliveries {
		answer := map[string]any{
			"route":          routeAnswer(byID[delivery.RouteID]),
			"destination_id": delivery.DestinationID,
			"template_id":    delivery.TemplateID,
			"reason":         delivery.Reason,
		}
		answers = append(answers, answer)
	}
	return answers
}

// Every node on the path an alert takes, so the picture can highlight the whole
// fan-out rather than one route of it.
func matchedNodes(result routing.Result) []string {
	nodes := make([]string, 0, len(result.Deliveries)*2)
	for _, delivery := range result.Deliveries {
		nodes = append(nodes, "route:"+delivery.RouteID)
		if delivery.DestinationID != "" {
			nodes = append(nodes, "destination:"+delivery.DestinationID)
		}
	}
	return nodes
}

func explainResult(result routing.Result, byID map[string]models.Route) string {
	root := byID[result.RootID]

	var opening string
	switch result.Reason {
	case routing.ReasonSelector:
		opening = fmt.Sprintf("%q matched on %s, at priority %d", root.Name, selectorSummary(root.LabelSelector), root.Priority)
	case routing.ReasonDefault:
		opening = fmt.Sprintf("no selector matched, so the default route %q takes it", root.Name)
	case routing.ReasonNoRoutes:
		return "no routes are configured, so this alert would be rejected"
	default:
		return "no selector matched and no default route exists, so this alert would be rejected"
	}

	names := make([]string, 0, len(result.Deliveries))
	rootDelivers := false
	for _, delivery := range result.Deliveries {
		names = append(names, fmt.Sprintf("%q", delivery.RouteName))
		if delivery.RouteID == result.RootID {
			rootDelivers = true
		}
	}

	switch {
	case len(result.Deliveries) == 1 && rootDelivers:
		return opening
	case rootDelivers:
		return fmt.Sprintf("%s, and %d messages go out — from %s", opening, len(result.Deliveries), strings.Join(names, ", "))
	default:
		return fmt.Sprintf("%s, but a child route delivers instead of it — from %s", opening, strings.Join(names, ", "))
	}
}
