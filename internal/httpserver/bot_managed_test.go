package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func managed(welcome string) func(*config.BotConfig) {
	return func(b *config.BotConfig) {
		b.GlobalInstall, b.AppID, b.WelcomeMessage = true, "app", welcome
	}
}

// installActivity is the bot being added to, or removed from, Alice's chat.
func installActivity(kind, action string, added bool) map[string]any {
	activity := botActivityFields()
	activity["type"] = kind
	activity["action"] = action
	bot := []map[string]any{{"id": activity["recipient"].(map[string]any)["id"], "name": "Teamster"}}
	if kind == "conversationUpdate" {
		if added {
			activity["membersAdded"] = bot
		} else {
			activity["membersRemoved"] = bot
		}
	}
	return activity
}

func TestPersonalInstallIsRecorded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*config.BotConfig)
		activity  map[string]any
		wantSent  []string
	}{
		{name: "installation update records silently", configure: managed("Hello from IT"), activity: installActivity("installationUpdate", "add", true)},
		{name: "bot added greets when managed", configure: managed("Hello from IT"), activity: installActivity("conversationUpdate", "", true), wantSent: []string{"Hello from IT"}},
		{name: "no welcome configured", configure: managed(""), activity: installActivity("conversationUpdate", "", true)},
		{name: "not managed sends the link instructions", activity: installActivity("conversationUpdate", "", true), wantSent: []string{installInstructions}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newBotFixtureWith(t, tt.configure)
			if rec := f.post(t, tt.activity); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			u, ok := f.store.directory["aad-1"]
			if !ok || u.InstallState != models.InstallInstalled || u.ConversationID != "conv-1" || u.DisplayName != "Alice" {
				t.Errorf("directory user = %+v, %v; want Alice installed in conv-1", u, ok)
			}
			if sent := f.botClient.sentTexts(); strings.Join(sent, "|") != strings.Join(tt.wantSent, "|") {
				t.Errorf("sent = %q, want %q", sent, tt.wantSent)
			}
			if len(f.store.recipients) != 0 {
				t.Error("an install created a recipient: an install is not consent (ADR 0026)")
			}
		})
	}
}

func TestPersonalRemoval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configure     func(*config.BotConfig)
		activity      map[string]any
		wantRecipient bool
	}{
		{name: "managed keeps the link", configure: managed(""), activity: installActivity("conversationUpdate", "", false), wantRecipient: true},
		{name: "managed installation update", configure: managed(""), activity: installActivity("installationUpdate", "remove", false), wantRecipient: true},
		{name: "not managed retires the link", activity: installActivity("conversationUpdate", "", false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newBotFixtureWith(t, tt.configure)
			seedRecipient(t, f.store, "alice", "conv-1")
			f.store.directory["aad-1"] = models.DirectoryUser{AADObjectID: "aad-1", InstallState: models.InstallInstalled, ConversationID: "conv-1"}

			if rec := f.post(t, tt.activity); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := f.store.directory["aad-1"].InstallState; got != models.InstallRemoved {
				t.Errorf("directory state = %s, want removed so the next run reinstalls", got)
			}
			if got := len(f.store.recipients) == 1; got != tt.wantRecipient {
				t.Errorf("recipient kept = %v, want %v", got, tt.wantRecipient)
			}
		})
	}
}

func TestManagedChatCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		text   string
		linked bool
		want   string
	}{
		{name: "help leaves out unlink", text: "/help", want: managedHelpReply},
		{name: "unlink refused", text: "/unlink", linked: true, want: managedUnlinkReply},
		{name: "stop refused", text: "stop", linked: true, want: managedUnlinkReply},
		{name: "status without a link", text: "/status", want: managedStatus},
		{name: "test without a link", text: "/test", want: testText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newBotFixtureWith(t, managed(""))
			if tt.linked {
				seedRecipient(t, f.store, "alice", "conv-1")
			}
			activity := botActivityFields()
			activity["text"] = tt.text
			if rec := f.post(t, activity); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			sent := f.botClient.sentTexts()
			if len(sent) != 1 || !strings.Contains(sent[0], tt.want) {
				t.Errorf("sent = %q, want one reply containing %q", sent, tt.want)
			}
			if strings.Contains(sent[0], "/unlink") {
				t.Errorf("reply %q offers /unlink in a managed chat", sent[0])
			}
			if tt.linked && len(f.store.recipients) != 1 {
				t.Error("a managed chat was unlinked")
			}
		})
	}
}

func TestMessagesKeepTheDirectoryChatCurrent(t *testing.T) {
	t.Parallel()

	f := newBotFixtureWith(t, managed(""))
	f.store.directory["aad-1"] = models.DirectoryUser{AADObjectID: "aad-1", InstallState: models.InstallRemoved, ConversationID: "old", ServiceURL: "https://old.example/"}

	activity := botActivityFields()
	activity["text"] = "/help"
	f.post(t, activity)

	u := f.store.directory["aad-1"]
	if u.InstallState != models.InstallInstalled || u.ConversationID != "conv-1" || u.ServiceURL != activity["serviceUrl"] {
		t.Errorf("directory user = %+v, want the chat the message came from", u)
	}
}

func TestManagedNotificationsPageRefusesUnlink(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	bot := notificationsBotConfig()
	managed("")(&bot)
	handler := mustServer(t, config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     bot,
	}, st, &fakeMessenger{}).Handler
	seedRecipient(t, st, "tester", "conv-1")

	req := httptest.NewRequest(http.MethodGet, "/admin/notifications", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, req)
	if page.Code != http.StatusOK || strings.Contains(page.Body.String(), `action="/admin/notifications/unlink"`) {
		t.Errorf("page = %d, want it served without an unlink button", page.Code)
	}

	rec := postForm(t, handler, "/admin/notifications/unlink", url.Values{}, nil)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "error=managed") {
		t.Errorf("unlink = %d %s, want a redirect with error=managed", rec.Code, rec.Header().Get("Location"))
	}
	if len(st.recipients) != 1 {
		t.Error("a managed chat was unlinked from the page")
	}
}
