package httpserver

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// chatServer seeds a route that delivers to a person, and nothing else, so an
// assertion about the chat path is not reading through a channel delivery.
func chatServer(t *testing.T, botClient botSender, tel telemetry) (*fakeStore, http.Handler) {
	t.Helper()

	st := newFakeStore()
	st.templates["tmpl"] = models.Template{ID: "tmpl", Text: "**{{ .Alert.Status }}**"}
	st.recipients["person"] = models.Recipient{
		ID: "person", Subject: "oncall", Name: "On Call",
		ConversationID: "19:chat", ServiceURL: "https://smba.example/teams/",
		BotChannelID: "msteams", TenantID: "tenant", AADObjectID: "aad",
	}
	st.routes["route"] = models.Route{ID: "route", Name: "route", TemplateID: "tmpl", RecipientID: "person", IsDefault: true}

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
	if tel == nil {
		tel = metrics.Disabled()
	}
	srv, err := NewServer(cfg, st, &fakeMessenger{}, botClient, tel)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return st, srv.Handler
}

func chatRow(t *testing.T, st *fakeStore, fingerprint, recipientID string) (models.ActiveAlertRecipient, bool) {
	t.Helper()

	st.mu.Lock()
	defer st.mu.Unlock()
	row, ok := st.activeChats[activeChatKey(fingerprint, recipientID)]
	return row, ok
}

// A firing alert reaches the person's chat as markdown, not as the HTML the
// Graph path sends.
func TestChatDeliverySendsMarkdown(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{}
	st, handler := chatServer(t, botClient, nil)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(botClient.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(botClient.sent))
	}
	if got := botClient.sent[0].msg.Text; got != "**firing**" {
		t.Errorf("text = %q, want markdown rather than html", got)
	}
	if got := botClient.sent[0].ref.ConversationID; got != "19:chat" {
		t.Errorf("conversation = %q, want the recipient's", got)
	}

	row, ok := chatRow(t, st, "fp-1", "person")
	if !ok || !row.Posted() || row.MessageID != "activity-id" {
		t.Errorf("row = %+v/%v, want a posted row carrying the activity id", row, ok)
	}
}

// A re-fire edits the message in place rather than sending a second one: Teams
// marks an edit and does not re-notify, which is what a repeat should do.
func TestChatRefireEditsInPlace(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{}
	_, handler := chatServer(t, botClient, nil)

	for range 2 {
		rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
	}

	if len(botClient.sent) != 1 {
		t.Errorf("sent %d messages, want 1 -- a re-fire edits rather than sends", len(botClient.sent))
	}
	if len(botClient.updated) != 1 {
		t.Fatalf("updates = %d, want 1", len(botClient.updated))
	}
	if got := botClient.updated[0].activityID; got != "activity-id" {
		t.Errorf("edited activity %q, want the stored one", got)
	}
}

// bot.SendMessage returning ("", nil) is a documented success. Treating it as
// an unposted claim would send the person a second copy on the next firing.
func TestChatSendWithoutAnActivityIDDoesNotDuplicate(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{emptySendID: true}
	st, handler := chatServer(t, botClient, nil)

	for range 2 {
		rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
	}

	if len(botClient.sent) != 1 {
		t.Errorf("sent %d messages, want 1 -- an empty activity id is delivered, not unposted", len(botClient.sent))
	}
	// Nothing to edit, so the second firing restamps the row instead.
	if len(botClient.updated) != 0 {
		t.Errorf("updates = %d, want 0 -- there is no activity id to edit", len(botClient.updated))
	}
	row, ok := chatRow(t, st, "fp-1", "person")
	if !ok || !row.Posted() {
		t.Errorf("row = %+v/%v, want it still recorded as posted", row, ok)
	}
}

// A resolution is sent rather than edited: an edit does not re-notify, and
// being told the alert cleared is the whole point.
func TestChatResolveSendsANewMessage(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{}
	st, handler := chatServer(t, botClient, nil)

	if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("firing POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"resolved","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("resolved POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(botClient.sent) != 2 {
		t.Errorf("sent %d messages, want 2 -- a resolve sends rather than edits", len(botClient.sent))
	}
	if len(botClient.updated) != 0 {
		t.Errorf("updates = %d, want 0 -- an in-place resolve would be silent", len(botClient.updated))
	}
	if got := botClient.sent[1].msg.Text; got != "**resolved**" {
		t.Errorf("resolution text = %q, want the resolved render", got)
	}
	if _, ok := chatRow(t, st, "fp-1", "person"); ok {
		t.Error("row still stored, want it forgotten once resolved")
	}
}

// A permanent failure stops re-attempting within the alert and is counted as
// its own outcome, so it is visible as something other than transient noise.
// A transient one leaves the row exactly as a channel failure does.
func TestChatSendFailureIsPermanentOrTransient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantOutcome string
		wantRow     bool
	}{
		{
			name:        "the person blocked the bot",
			err:         &bot.APIError{StatusCode: 403, Code: "MessageWritesBlocked"},
			wantOutcome: metrics.OutcomeBlocked,
		},
		{
			// The actionable code lives one level in, which is the only way to
			// tell this apart from a generic 403.
			name:        "the conversation is blocked, reported one level in",
			err:         &bot.APIError{StatusCode: 403, InnerCode: "ConversationBlockedByUser"},
			wantOutcome: metrics.OutcomeBlocked,
		},
		{
			name:        "rate limited",
			err:         &bot.APIError{StatusCode: 429, RetryAfter: "30"},
			wantOutcome: metrics.OutcomeFailed,
		},
		{
			name:        "the connector is broken",
			err:         &bot.APIError{StatusCode: 502},
			wantOutcome: metrics.OutcomeFailed,
		},
		{
			name:        "the transport failed before any status",
			err:         errors.New("dial tcp: connection refused"),
			wantOutcome: metrics.OutcomeFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tel := newRecordingTelemetry()
			botClient := &fakeBotClient{err: tt.err}
			st, handler := chatServer(t, botClient, tel)

			rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("POST = %d, want 502", rec.Code)
			}

			delivered, _, _ := tel.calls()
			if len(delivered) != 1 || delivered[0] != [2]string{"route", tt.wantOutcome} {
				t.Errorf("delivered = %v, want one %q", delivered, tt.wantOutcome)
			}

			// Either way the unposted claim goes back, so the next attempt
			// does not wait out the staleness cutoff.
			if _, ok := chatRow(t, st, "fp-1", "person"); ok {
				t.Error("an unposted claim survived a failed send, want it released")
			}
		})
	}
}

// The permanent branch on the edit path has a row to drop, unlike the send
// path where releasing the claim already removes it.
func TestChatBlockedOnEditForgetsTheRow(t *testing.T) {
	t.Parallel()

	tel := newRecordingTelemetry()
	botClient := &fakeBotClient{}
	st, handler := chatServer(t, botClient, tel)

	if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("firing POST = %d, want 200", rec.Code)
	}

	// The person blocks the bot between the two firings.
	botClient.updateErr = &bot.APIError{StatusCode: 403, Code: "MessageWritesBlocked"}
	if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusBadGateway {
		t.Fatalf("second POST = %d, want 502", rec.Code)
	}

	delivered, _, _ := tel.calls()
	if len(delivered) != 2 || delivered[1] != [2]string{"route", metrics.OutcomeBlocked} {
		t.Errorf("delivered = %v, want the second counted as blocked", delivered)
	}
	if _, ok := chatRow(t, st, "fp-1", "person"); ok {
		t.Error("row survived a permanent failure, want it dropped")
	}
}

// A transient failure on the edit path leaves the row: it is the only record
// that this person was told the alert fires, and the sender's retry is what
// puts it right.
func TestChatTransientEditFailureKeepsTheRow(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{}
	st, handler := chatServer(t, botClient, nil)

	if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("firing POST = %d, want 200", rec.Code)
	}

	botClient.updateErr = &bot.APIError{StatusCode: 502}
	if rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`); rec.Code != http.StatusBadGateway {
		t.Fatalf("second POST = %d, want 502", rec.Code)
	}

	if _, ok := chatRow(t, st, "fp-1", "person"); !ok {
		t.Error("row dropped on a transient failure, want it kept for the retry")
	}
}

// A permanent send failure marks the recipient blocked -- the durable,
// visible trace ADR 0026 deferred to this PR -- in addition to the existing
// metric and dropped row.
func TestPermanentSendFailureMarksRecipientBlocked(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{err: &bot.APIError{StatusCode: 403, Code: "MessageWritesBlocked"}}
	st, handler := chatServer(t, botClient, nil)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST = %d, want 502", rec.Code)
	}

	recipient, err := st.GetRecipient(t.Context(), "person")
	if err != nil {
		t.Fatalf("GetRecipient: %v", err)
	}
	if !recipient.Blocked() {
		t.Error("Blocked() = false after a permanent send failure, want true")
	}
	if recipient.BlockedReason != "MessageWritesBlocked" {
		t.Errorf("BlockedReason = %q, want the API error's own code", recipient.BlockedReason)
	}
}

// The flag is self-healing: a successful send clears it, without anyone
// having to notice and clear it by hand.
func TestSuccessfulSendClearsRecipientBlocked(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{}
	st, handler := chatServer(t, botClient, nil)

	blocked := st.recipients["person"]
	blocked.BlockedAt = time.Now().Add(-time.Hour)
	blocked.BlockedReason = "MessageWritesBlocked"
	st.recipients["person"] = blocked

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	recipient, err := st.GetRecipient(t.Context(), "person")
	if err != nil {
		t.Fatalf("GetRecipient: %v", err)
	}
	if recipient.Blocked() {
		t.Error("Blocked() = true after a successful send, want the flag cleared")
	}
	if recipient.BlockedReason != "" {
		t.Errorf("BlockedReason = %q after a successful send, want empty", recipient.BlockedReason)
	}
}

// The decision most likely to be silently inverted later: the flag is
// informational, not a delivery gate (ADR 0026). A recipient already marked
// blocked is still attempted on the next alert, exactly like one that never
// was -- gating on it would mean a person who reinstalled the bot never
// receives another alert until an admin notices and clears the flag by hand.
func TestBlockedRecipientIsStillAttempted(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{}
	st, handler := chatServer(t, botClient, nil)

	blocked := st.recipients["person"]
	blocked.BlockedAt = time.Now().Add(-time.Hour)
	blocked.BlockedReason = "MessageWritesBlocked"
	st.recipients["person"] = blocked

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 -- a blocked recipient must still be attempted", rec.Code)
	}
	if len(botClient.sent) != 1 {
		t.Fatalf("sent %d messages, want 1: a blocked recipient must not be skipped", len(botClient.sent))
	}
}

// One route, two targets: both go out, and one failing does not cost the other
// its message.
func TestRouteFansOutToChannelAndChat(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{}
	st := newFakeStore()
	st.templates["tmpl"] = models.Template{ID: "tmpl", Text: "<p>{{ .Alert.Status }}</p>"}
	st.destinations["dest"] = models.Destination{ID: "dest", TeamID: "team", ChannelID: "channel"}
	st.recipients["person"] = models.Recipient{
		ID: "person", Subject: "oncall", ConversationID: "19:chat",
		ServiceURL: "https://smba.example/teams/", BotChannelID: "msteams",
	}
	st.routes["route"] = models.Route{
		ID: "route", Name: "route", TemplateID: "tmpl",
		DestinationID: "dest", RecipientID: "person", IsDefault: true,
	}

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
	msg := &fakeMessenger{}
	srv, err := NewServer(cfg, st, msg, botClient, metrics.Disabled())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := postWebhook(t, srv.Handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(msg.posts) != 1 {
		t.Errorf("channel posts = %d, want 1", len(msg.posts))
	}
	if len(botClient.sent) != 1 {
		t.Fatalf("chat sends = %d, want 1", len(botClient.sent))
	}
	// The same template, in each transport's own format.
	if got := msg.posts[0].msg.Text; got != "<p>firing</p>" {
		t.Errorf("channel text = %q, want html", got)
	}
	if got := botClient.sent[0].msg.Text; got != "firing" {
		t.Errorf("chat text = %q, want markdown", got)
	}
}

// A route pointing at a person in a deployment that never configured the bot is
// an alert that was meant to reach somebody and did not, so it is reported.
func TestChatDeliveryWithoutABotIsReported(t *testing.T) {
	t.Parallel()

	_, handler := chatServer(t, nil, nil)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST = %d, want 502", rec.Code)
	}
}

// A message with no status reaches a person's chat the same untracked way it
// reaches a channel: sent once, nothing claimed, no ActiveAlertRecipient row.
func TestChatDeliveryOfAMessageWithoutAStatusIsNotTracked(t *testing.T) {
	t.Parallel()

	botClient := &fakeBotClient{}
	st, handler := chatServer(t, botClient, nil)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(botClient.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(botClient.sent))
	}
	if got := botClient.sent[0].ref.ConversationID; got != "19:chat" {
		t.Errorf("conversation = %q, want the recipient's", got)
	}

	if len(st.activeChats) != 0 {
		t.Errorf("active chats = %d, want none: nothing is tracked for a status-less message", len(st.activeChats))
	}
}

// deliverToRecipientOnce guards on a missing bot exactly like deliverToRecipient
// does: an untracked send still has to go somewhere.
func TestChatDeliveryOfAMessageWithoutAStatusAndWithoutABotIsReported(t *testing.T) {
	t.Parallel()

	_, handler := chatServer(t, nil, nil)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{}}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST = %d, want 502", rec.Code)
	}
}
