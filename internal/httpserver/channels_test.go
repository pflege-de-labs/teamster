package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// fakeChannelBot records channel posts and edits the way fakeBotClient
// records chat messages.
type fakeChannelBot struct {
	serviceURL, tenantID, channelID string
	posted                          bot.Message
	updatedRef                      bot.ConversationReference
	updatedID                       string
	err                             error
}

func (f *fakeChannelBot) PostToChannel(_ context.Context, serviceURL, tenantID, channelID string, msg bot.Message) (bot.ChannelPost, error) {
	f.serviceURL, f.tenantID, f.channelID, f.posted = serviceURL, tenantID, channelID, msg
	return bot.ChannelPost{ConversationID: "conversation-1", ActivityID: "activity-1"}, f.err
}

func (f *fakeChannelBot) UpdateMessage(_ context.Context, ref bot.ConversationReference, activityID string, _ bot.Message) error {
	f.updatedRef, f.updatedID = ref, activityID
	return f.err
}

func TestBotChannelsPost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		team           *models.BotTeam
		serviceURL     string
		botTenant      string
		failOn         string
		botErr         error
		wantServiceURL string
		wantTenant     string
		wantErr        string
	}{
		{
			name:           "an installed team's own endpoint",
			team:           &models.BotTeam{TeamID: "team", TenantID: "tenant-a", ServiceURL: "https://smba.example/emea/"},
			serviceURL:     "https://fallback.example/",
			botTenant:      "tenant-b",
			wantServiceURL: "https://smba.example/emea/",
			wantTenant:     "tenant-a",
		},
		{
			name:           "a team found through Graph has no endpoint of its own yet",
			team:           &models.BotTeam{TeamID: "team", TenantID: "tenant-a"},
			serviceURL:     "https://fallback.example/",
			wantServiceURL: "https://fallback.example/",
			wantTenant:     "tenant-a",
		},
		{name: "a team found through Graph and no fallback", team: &models.BotTeam{TeamID: "team"}, wantErr: "no activity from it has named"},
		{name: "the fallback for an unknown team", serviceURL: "https://fallback.example/", botTenant: "tenant-b", wantServiceURL: "https://fallback.example/", wantTenant: "tenant-b"},
		{name: "a multi-tenant bot falls back to the Graph tenant", serviceURL: "https://fallback.example/", wantServiceURL: "https://fallback.example/", wantTenant: "graph-tenant"},
		{name: "no endpoint at all", wantErr: "bot.service-url is empty"},
		{name: "store failure", failOn: "GetBotTeam", serviceURL: "https://fallback.example/", wantErr: "bot team"},
		{
			name: "a refusal names the likely cause", serviceURL: "https://fallback.example/",
			botErr: &bot.APIError{StatusCode: http.StatusForbidden}, wantErr: "is the Teams app installed there",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := newFakeStore().fail(tt.failOn)
			if tt.team != nil {
				st.botTeams[tt.team.TeamID] = *tt.team
			}
			client := &fakeChannelBot{err: tt.botErr}
			channels := NewBotChannels(client, st, config.BotConfig{TenantID: tt.botTenant, ServiceURL: tt.serviceURL}, "graph-tenant")

			msg := graph.Message{Title: "Disk", Text: "<p><b>92%</b> used</p>", Cards: []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{}`)}}
			card, err := channels.PostToChannel(t.Context(), "team", "channel", msg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("PostToChannel() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PostToChannel: %v", err)
			}
			if card != (channelCard{ConversationID: "conversation-1", MessageID: "activity-1"}) {
				t.Errorf("card = %+v, want the conversation and activity the Connector answered", card)
			}
			if client.serviceURL != tt.wantServiceURL || client.tenantID != tt.wantTenant || client.channelID != "channel" {
				t.Errorf("posted to %s for %s in %s, want %s for %s", client.serviceURL, client.tenantID, client.channelID, tt.wantServiceURL, tt.wantTenant)
			}
			if client.posted.Text != "**92%** used" || len(client.posted.Cards) != 2 {
				t.Errorf("posted %+v, want the HTML as markdown and both cards", client.posted)
			}
		})
	}
}

func TestBotChannelsUpdate(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.botTeams["team"] = models.BotTeam{TeamID: "team", ServiceURL: "https://smba.example/emea/"}
	client := &fakeChannelBot{}
	channels := NewBotChannels(client, st, config.BotConfig{}, "")

	card := channelCard{ConversationID: "conversation-1", MessageID: "activity-1"}
	if err := channels.UpdateInChannel(t.Context(), "team", "channel", card, graph.Message{Text: "<p>resolved</p>"}); err != nil {
		t.Fatalf("UpdateInChannel: %v", err)
	}
	want := bot.ConversationReference{ServiceURL: "https://smba.example/emea/", ConversationID: "conversation-1"}
	if client.updatedRef != want || client.updatedID != "activity-1" {
		t.Errorf("updated %+v / %s, want %+v / activity-1", client.updatedRef, client.updatedID, want)
	}

	if err := NewBotChannels(client, newFakeStore(), config.BotConfig{}, "").UpdateInChannel(t.Context(), "team", "channel", card, graph.Message{}); err == nil {
		t.Error("UpdateInChannel() without any endpoint = nil error, want one")
	}
}

// A card the previous release posted through Graph has no conversation, so
// nothing can edit it: a re-fire replaces it and a resolve forgets it.
func TestUneditableCards(t *testing.T) {
	t.Parallel()

	legacy := models.ActiveAlert{Fingerprint: "fp-1", Status: "firing", TeamID: "team", ChannelID: "channel", MessageID: "graph-1", PostedAt: testPostedAt}

	t.Run("a re-fire posts a successor", func(t *testing.T) {
		t.Parallel()

		msg := &fakeMessenger{}
		st, handler := seededServer(t, msg)
		st.activeAlerts[activeAlertKey("fp-1", "team", "channel")] = legacy

		if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusOK {
			t.Fatalf("POST = %d (body %s)", rec.Code, rec.Body.String())
		}
		row := st.activeAlerts[activeAlertKey("fp-1", "team", "channel")]
		if len(msg.posts) != 1 || len(msg.updates) != 0 || row.ConversationID == "" || row.MessageID == "graph-1" {
			t.Errorf("posts %d, updates %d, row %+v; want one new card recorded in place of the old", len(msg.posts), len(msg.updates), row)
		}
	})

	t.Run("a resolve forgets it", func(t *testing.T) {
		t.Parallel()

		msg := &fakeMessenger{}
		st, handler := seededServer(t, msg)
		st.activeAlerts[activeAlertKey("fp-1", "team", "channel")] = legacy

		if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"resolved","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusOK {
			t.Fatalf("POST = %d (body %s)", rec.Code, rec.Body.String())
		}
		if _, ok := st.activeAlerts[activeAlertKey("fp-1", "team", "channel")]; ok || len(msg.updates) != 0 {
			t.Errorf("row kept = %v, updates = %d; want it forgotten without an edit", ok, len(msg.updates))
		}
	})

	t.Run("a store failure while forgetting it", func(t *testing.T) {
		t.Parallel()

		st, handler := seededServer(t, &fakeMessenger{})
		st.activeAlerts[activeAlertKey("fp-1", "team", "channel")] = legacy
		st.fail("DeleteActiveAlertCard")

		if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusBadGateway {
			t.Errorf("POST = %d, want 502", rec.Code)
		}
	})
}

func TestChannelDeliveryWithoutTheBot(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.templates["tmpl"] = models.Template{ID: "tmpl", Body: `{"text":"x"}`}
	st.destinations["dest"] = models.Destination{ID: "dest", TeamID: "team", ChannelID: "channel"}
	st.routes["route"] = models.Route{ID: "route", TemplateID: "tmpl", DestinationID: "dest", IsDefault: true}
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
	srv, err := NewServer(cfg, st, &fakeMessenger{}, nil, nil, newRecordingTelemetry(), nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := postWebhook(t, srv.Handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "needs the bot") {
		t.Errorf("POST = %d %s, want 502 naming the missing bot", rec.Code, rec.Body.String())
	}
}
