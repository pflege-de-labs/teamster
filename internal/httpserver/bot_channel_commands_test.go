package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// channelMessage is a mention of the bot in the general channel of graph-team-1.
func channelMessage(text string) map[string]any {
	activity := teamActivity("message", nil)
	activity["text"] = "<at>Teamster</at> " + text
	activity["entities"] = []map[string]any{{"type": "mention", "text": "<at>Teamster</at>"}}
	activity["conversation"] = map[string]any{"id": "19:general@thread.tacv2;messageid=42", "conversationType": "channel"}
	activity["channelData"].(map[string]any)["channel"] = map[string]any{"id": "19:general@thread.tacv2"}
	return activity
}

func TestBotChannelCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		text        string
		destination bool
		isDefault   bool
		fail        string
		postErr     error
		// want is matched against every reply the bot sent, in order.
		want      []string
		wantPosts int
	}{
		{name: "help", text: "help", want: []string{channelHelpReply}},
		{name: "help with a slash", text: "/help", want: []string{channelHelpReply}},
		{name: "not a command", text: "what is this?", want: []string{channelNotACommandReply}},
		{name: "unknown", text: "/nope", want: []string{unknownCommandReply}},
		{name: "unlink", text: "unlink", want: []string{channelUnlinkReply}},
		{name: "status without a destination", text: "status", want: []string{channelNoDestinationStatus}},
		{name: "status lists routes", text: "status", destination: true, want: []string{"**Ops alerts**.\n\n**Routes posting here**\n\n- Ops: `team=ops`"}},
		{name: "status of the default", text: "status", destination: true, isDefault: true, want: []string{"global default"}},
		{name: "status store failure", text: "status", fail: "ListDestinations", want: []string{"Reference"}},
		{name: "status routes failure", text: "status", destination: true, fail: "ListRoutes", want: []string{"Reference"}},
		{name: "test without a destination", text: "test", want: []string{channelNoDestinationTest}},
		{name: "test posts to the channel", text: "test", destination: true, wantPosts: 1},
		{name: "test store failure", text: "test", fail: "ListDestinations", want: []string{"Reference"}},
		{name: "test post failure", text: "test", destination: true, postErr: errors.New("403"), want: []string{"Reference"}, wantPosts: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newBotFixture(t)
			f.channels.postErr = tt.postErr
			if tt.destination {
				f.store.destinations["ops"] = models.Destination{ID: "ops", Name: "Ops alerts", TeamID: "graph-team-1", ChannelID: "19:general@thread.tacv2", IsDefault: tt.isDefault}
				f.store.destinations["other"] = models.Destination{ID: "other", Name: "Other", TeamID: "graph-team-1", ChannelID: "19:other@thread.tacv2"}
				f.store.routes["ops"] = models.Route{ID: "ops", Name: "Ops", DestinationID: "ops", LabelSelector: map[string]string{"team": "ops"}}
				f.store.routes["elsewhere"] = models.Route{ID: "elsewhere", Name: "Elsewhere", DestinationID: "other"}
			}
			if tt.fail != "" {
				f.store.fail(tt.fail)
			}

			if rec := f.post(t, channelMessage(tt.text)); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			sent := f.botClient.sentTexts()
			if len(sent) != len(tt.want) {
				t.Fatalf("sent = %q, want %d replies", sent, len(tt.want))
			}
			for i, want := range tt.want {
				if !strings.Contains(sent[i], want) {
					t.Errorf("reply %d = %q, want it to contain %q", i, sent[i], want)
				}
			}
			if strings.Contains(strings.Join(sent, "\n"), "Elsewhere") {
				t.Errorf("replies = %q, list a route posting to another channel", sent)
			}

			f.channels.mu.Lock()
			posts := f.channels.posts
			f.channels.mu.Unlock()
			if len(posts) != tt.wantPosts {
				t.Fatalf("posts = %d, want %d", len(posts), tt.wantPosts)
			}
			if tt.wantPosts > 0 && (posts[0].teamID != "graph-team-1" || posts[0].channelID != "19:general@thread.tacv2" || !strings.Contains(posts[0].msg.Text, testText)) {
				t.Errorf("post = %+v, want the test alert in this channel", posts[0])
			}
		})
	}
}

// A channel message refreshes the team's service URL like any team activity.
func TestBotChannelCommandRecordsTheTeam(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	if rec := f.post(t, channelMessage("help")); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if _, ok := f.store.botTeams["graph-team-1"]; !ok {
		t.Error("bot team not recorded from a channel message")
	}
}

func TestActivityChannelID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		activity botActivity
		want     string
	}{
		{name: "from channelData", activity: func() botActivity {
			var a botActivity
			a.ChannelData.Channel.ID = "19:a@thread.tacv2"
			a.Conversation.ID = "19:b@thread.tacv2;messageid=1"
			return a
		}(), want: "19:a@thread.tacv2"},
		{name: "from the conversation", activity: botActivity{Conversation: botConversation{ID: "19:b@thread.tacv2;messageid=1"}}, want: "19:b@thread.tacv2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := activityChannelID(tt.activity); got != tt.want {
				t.Errorf("activityChannelID() = %q, want %q", got, tt.want)
			}
		})
	}
}
