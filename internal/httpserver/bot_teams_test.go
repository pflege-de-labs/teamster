package httpserver

import (
	"net/http"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// teamActivity is botActivityFields moved into a team's channel.
func teamActivity(activityType string, extra map[string]any) map[string]any {
	activity := botActivityFields()
	activity["type"] = activityType
	activity["conversation"] = map[string]any{"id": "19:general@thread.tacv2", "conversationType": "channel"}
	channelData := map[string]any{
		"tenant": map[string]any{"id": "tenant-1"},
		"team":   map[string]any{"id": "19:team@thread.tacv2", "aadGroupId": "graph-team-1"},
	}
	for key, value := range extra {
		if key == "eventType" {
			channelData[key] = value
			continue
		}
		activity[key] = value
	}
	activity["channelData"] = channelData
	return activity
}

func TestBotTeamEvents(t *testing.T) {
	t.Parallel()

	botID := botActivityFields()["recipient"].(map[string]any)["id"]
	tests := []struct {
		name      string
		seeded    bool
		activity  map[string]any
		wantTeam  bool
		wantReply bool
	}{
		{name: "installed", activity: teamActivity("installationUpdate", map[string]any{"action": "add"}), wantTeam: true},
		{name: "installed by upgrade", activity: teamActivity("installationUpdate", map[string]any{"action": "add-upgrade"}), wantTeam: true},
		{name: "added as a member", activity: teamActivity("conversationUpdate", map[string]any{"membersAdded": []map[string]any{{"id": botID}}}), wantTeam: true},
		{name: "any other team activity refreshes", activity: teamActivity("conversationUpdate", map[string]any{"eventType": "channelCreated"}), wantTeam: true},
		{name: "uninstalled", seeded: true, activity: teamActivity("installationUpdate", map[string]any{"action": "remove"})},
		{name: "removed as a member", seeded: true, activity: teamActivity("conversationUpdate", map[string]any{"membersRemoved": []map[string]any{{"id": botID}}})},
		{name: "team deleted", seeded: true, activity: teamActivity("conversationUpdate", map[string]any{"eventType": "teamDeleted"})},
		{name: "another member leaving keeps the install", seeded: true, activity: teamActivity("conversationUpdate", map[string]any{"membersRemoved": []map[string]any{{"id": "29:bob"}}}), wantTeam: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newBotFixture(t)
			if tt.seeded {
				f.store.botTeams["graph-team-1"] = models.BotTeam{TeamID: "graph-team-1", TenantID: "tenant-1", ServiceURL: "https://old.example/"}
			}

			if rec := f.post(t, tt.activity); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			team, ok := f.store.botTeams["graph-team-1"]
			if ok != tt.wantTeam {
				t.Fatalf("bot team recorded = %v, want %v", ok, tt.wantTeam)
			}
			if ok && (team.ServiceURL != "https://smba.trafficmanager.net/teams/" || team.TenantID != "tenant-1") {
				t.Errorf("bot team = %+v, want the activity's service URL and tenant", team)
			}
			if got := len(f.botClient.sentTexts()); got != 0 {
				t.Errorf("sent %d messages into a team, want none", got)
			}
		})
	}
}

func TestBotTeamEventWithoutAGraphTeamIDIsIgnored(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	activity := teamActivity("installationUpdate", map[string]any{"action": "add"})
	activity["channelData"] = map[string]any{"tenant": map[string]any{"id": "tenant-1"}}

	if rec := f.post(t, activity); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(f.store.botTeams) != 0 {
		t.Errorf("bot teams = %v, want none without an aadGroupId", f.store.botTeams)
	}
}

func TestBotTeamEventStoreFailureStillAnswers200(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"add", "remove"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			f := newBotFixture(t)
			f.store.fail("UpsertBotTeam", "DeleteBotTeam")
			if rec := f.post(t, teamActivity("installationUpdate", map[string]any{"action": action})); rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200: the Connector would retry a failure no retry fixes", rec.Code)
			}
		})
	}
}
