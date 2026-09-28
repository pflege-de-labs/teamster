package httpserver

import (
	"net/http"
	"net/url"
	"sort"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// handleTeamsPage shows where the bot's app is installed and what depends on
// each team, so a missing install is found before an alert is (ADR 0045).
func (s *Server) handleTeamsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	page := views.Teams{
		Viewer:        s.viewerFor(r),
		BotConfigured: botConfigured(s.cfg.Bot),
		GraphLookups:  s.cfg.Bot.AppID != "",
	}
	if s.cfg.Bot.AppID != "" {
		page.InstallURL = "https://teams.microsoft.com/l/app/" + url.PathEscape(s.cfg.Bot.AppID)
	}

	destinations, err := s.store.ListDestinations(ctx)
	if err == nil {
		destinations, err = s.visibleDestinations(r, destinations)
	}
	if err != nil {
		page.Error = err.Error()
	}
	routes, err := s.store.ListRoutes(ctx)
	if err != nil && page.Error == "" {
		page.Error = err.Error()
	}
	botTeams, err := s.store.ListBotTeams(ctx)
	if err != nil && page.Error == "" {
		page.Error = err.Error()
	}

	// The directory only adds names and teams nothing uses yet, so the page
	// still answers without it.
	var directory []graph.Team
	if teams, err := s.directory.Teams(s.graph.ListTeams); err == nil {
		directory, _ = s.visibleTeams(r, teams)
	} else {
		page.DirectoryError = err.Error()
	}

	rows := teamRows(destinations, routes, botTeams, directory)
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	states := s.installStates(ctx, ids)
	for _, row := range rows {
		row.State = states[row.ID]
		switch {
		case row.State == installInstalled:
			page.Installed = append(page.Installed, row)
		case len(row.Destinations) > 0:
			page.NeedsApp = append(page.NeedsApp, row)
		default:
			page.Other = append(page.Other, row)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.TeamsPage(page).Render(ctx, w); err != nil {
		logError("render teams page", err)
	}
}

// teamRows is every team a destination names or the directory offers, with
// the destinations and routes that depend on it.
func teamRows(destinations []models.Destination, routes []models.Route, botTeams []models.BotTeam, directory []graph.Team) []views.TeamRow {
	byID := map[string]*views.TeamRow{}
	row := func(id string) *views.TeamRow {
		if r, ok := byID[id]; ok {
			return r
		}
		r := &views.TeamRow{ID: id, Name: id}
		byID[id] = r
		return r
	}

	for _, team := range directory {
		row(team.ID).Name = team.Name
	}
	teamOf := map[string]string{}
	for _, d := range destinations {
		r := row(d.TeamID)
		r.Destinations = append(r.Destinations, d.Name)
		teamOf[d.ID] = d.TeamID
	}
	for _, route := range routes {
		if teamID, ok := teamOf[route.DestinationID]; ok {
			label := route.Name
			if label == "" {
				label = route.ID
			}
			r := row(teamID)
			r.Routes = append(r.Routes, label)
		}
	}
	for _, team := range botTeams {
		if r, ok := byID[team.TeamID]; ok && team.ServiceURL != "" {
			r.LastHeard = team.UpdatedAt
		}
	}

	out := make([]views.TeamRow, 0, len(byID))
	for _, r := range byID {
		sort.Strings(r.Destinations)
		sort.Strings(r.Routes)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
