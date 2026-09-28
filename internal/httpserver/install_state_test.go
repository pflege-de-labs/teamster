package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func installServer(st *fakeStore, msg *fakeMessenger, appID string) *Server {
	return &Server{
		cfg:      config.Config{Bot: config.BotConfig{TenantID: "bot-tenant", AppID: appID}},
		store:    st,
		graph:    msg,
		installs: newInstallCache(time.Minute),
		now:      time.Now,
	}
}

func TestInstallStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		appID      string
		installed  map[string]bool
		installErr error
		failOn     string
		want       string
		wantRow    bool
	}{
		{name: "an install event recorded it", want: installInstalled, wantRow: true},
		{name: "without an app id nothing asks Graph", want: installUnknown},
		{name: "Graph finds it", appID: "app", installed: map[string]bool{"new": true}, want: installInstalled, wantRow: true},
		{name: "Graph does not find it", appID: "app", want: installMissing},
		{name: "Graph refuses to say", appID: "app", installErr: errors.New("403"), want: installUnknown},
		{name: "the store fails", failOn: "ListBotTeams", want: installUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := newFakeStore().fail(tt.failOn)
			team := "new"
			if tt.name == "an install event recorded it" {
				st.botTeams[team] = models.BotTeam{TeamID: team, ServiceURL: "https://smba.example/"}
			}
			msg := &fakeMessenger{installed: tt.installed, installErr: tt.installErr}
			s := installServer(st, msg, tt.appID)

			if got := s.installStates(t.Context(), []string{team})[team]; got != tt.want {
				t.Errorf("state = %q, want %q", got, tt.want)
			}
			row, ok := st.botTeams[team]
			if ok != tt.wantRow {
				t.Errorf("bot team recorded = %v, want %v", ok, tt.wantRow)
			}
			if ok && tt.appID != "" && (row.ServiceURL != "" || row.TenantID != "bot-tenant") {
				t.Errorf("row from Graph = %+v, want no service URL and the bot's tenant", row)
			}
		})
	}
}

func TestInstallStatesRememberAMissingApp(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	s := installServer(newFakeStore(), msg, "app")
	for range 3 {
		if got := s.installStates(t.Context(), []string{"team"})["team"]; got != installMissing {
			t.Fatalf("state = %q, want missing", got)
		}
	}
	if msg.installCalls != 1 {
		t.Errorf("Graph asked %d times, want once while the answer is fresh", msg.installCalls)
	}
}

func TestPickerCarriesInstallState(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.botTeams["t1"] = models.BotTeam{TeamID: "t1", ServiceURL: "https://smba.example/"}
	msg := &fakeMessenger{teams: []graph.Team{{ID: "t1", Name: "Ops"}, {ID: "t2", Name: "Dev"}}}
	handler := newTestServer(t, st, msg).Handler

	rec := do(t, handler, http.MethodGet, "/api/graph/teams", "")
	var body struct {
		Teams []pickerTeam `json:"teams"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	got := map[string]string{}
	for _, team := range body.Teams {
		got[team.ID] = team.InstallState
	}
	if got["t1"] != installInstalled || got["t2"] != installUnknown {
		t.Errorf("states = %v, want t1 installed and t2 unknown", got)
	}
}

func TestTeamsPage(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.destinations["d1"] = models.Destination{ID: "d1", Name: "Ops alerts", TeamID: "t1", ChannelID: "c"}
	st.destinations["d2"] = models.Destination{ID: "d2", Name: "Dev alerts", TeamID: "t2", ChannelID: "c"}
	st.routes["r1"] = models.Route{ID: "r1", Name: "critical", DestinationID: "d2"}
	st.botTeams["t1"] = models.BotTeam{TeamID: "t1", ServiceURL: "https://smba.example/", UpdatedAt: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)}
	msg := &fakeMessenger{teams: []graph.Team{{ID: "t1", Name: "Ops"}, {ID: "t2", Name: "Dev"}, {ID: "t3", Name: "Idle"}}}

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     config.BotConfig{AppID: "app-1"},
	}
	handler := mustServer(t, cfg, st, msg).Handler

	rec := do(t, handler, http.MethodGet, "/admin/teams", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/teams = %d", rec.Code)
	}
	needs := strings.Index(body, "Used by destinations")
	installed := strings.Index(body, "App installed")
	for _, want := range []string{"Dev alerts", "critical", "Ops alerts", "2026-09-01 08:00", "Other teams (1)", "https://teams.microsoft.com/l/app/app-1", "The bot is not configured"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if needs < 0 || installed < 0 || strings.Index(body, "Dev alerts") > installed {
		t.Error("the team a destination uses without the app is not listed first")
	}

	if rec := do(t, handler, http.MethodGet, "/admin", ""); !strings.Contains(rec.Body.String(), "Teams app missing") {
		t.Error("the destination in a team without the app carries no badge")
	}
	if rec := do(t, handler, http.MethodGet, "/api/routing/graph", ""); !strings.Contains(rec.Body.String(), `"app_missing":true`) {
		t.Error("the routing graph does not mark the destination whose team lacks the app")
	}
	if rec := do(t, handler, http.MethodPost, "/admin/teams", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin/teams = %d, want 405", rec.Code)
	}
}

func TestTeamsPageWithoutTheDirectory(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.destinations["d1"] = models.Destination{ID: "d1", Name: "Ops alerts", TeamID: "t1", ChannelID: "c"}
	handler := newTestServer(t, st.fail("ListBotTeams"), &fakeMessenger{directoryErr: errors.New("graph down")}).Handler

	body := do(t, handler, http.MethodGet, "/admin/teams", "").Body.String()
	for _, want := range []string{"graph down", "Ops alerts", "t1", errStore.Error()} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestChannelFailureOutcome(t *testing.T) {
	t.Parallel()

	if got := channelFailure(&appNotInstalledError{teamID: "t", err: errors.New("403")}); got != metrics.OutcomeAppMissing {
		t.Errorf("a refused team = %q, want app_missing", got)
	}
	if got := channelFailure(errors.New("timeout")); got != metrics.OutcomeFailed {
		t.Errorf("another failure = %q, want failed", got)
	}
}
