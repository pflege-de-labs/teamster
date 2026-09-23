package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// fakeBotClient records what the handler asked it to send, standing in for
// the real Bot Connector client the way fakeMessenger stands in for Graph.
type fakeBotClient struct {
	mu   sync.Mutex
	sent []struct {
		ref bot.ConversationReference
		msg bot.Message
	}
	updated []struct {
		ref        bot.ConversationReference
		activityID string
		msg        bot.Message
	}
	err error

	// updateErr fails the edit rather than the send, which is what a re-fire
	// reaches; emptySendID makes SendMessage report the documented "delivered,
	// not updatable" success instead of an activity id.
	updateErr   error
	emptySendID bool
}

func (f *fakeBotClient) SendMessage(_ context.Context, ref bot.ConversationReference, msg bot.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, struct {
		ref bot.ConversationReference
		msg bot.Message
	}{ref, msg})
	if f.emptySendID {
		return "", f.err
	}
	return "activity-id", f.err
}

func (f *fakeBotClient) UpdateMessage(_ context.Context, ref bot.ConversationReference, activityID string, msg bot.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated = append(f.updated, struct {
		ref        bot.ConversationReference
		activityID string
		msg        bot.Message
	}{ref, activityID, msg})
	return f.updateErr
}

func (f *fakeBotClient) sentTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.sent))
	for i, s := range f.sent {
		out[i] = s.msg.Text
	}
	return out
}

func (f *fakeBotClient) count(text string) int {
	n := 0
	for _, t := range f.sentTexts() {
		if t == text {
			n++
		}
	}
	return n
}

// botFixture is a fully configured bot endpoint behind a real signing key,
// with a pre-signed token valid for the default activity's service URL. Tests
// drive it through the real handler and mux rather than calling
// dispatchBotActivity directly.
type botFixture struct {
	handler   http.Handler
	store     *fakeStore
	botClient *fakeBotClient
	token     string
}

func newBotFixture(t *testing.T) *botFixture {
	t.Helper()

	key := generateBotKey(t, "fixture-key", "msteams")
	idp := newBotIDP(t, key)
	const clientID = "bot-client-id"

	st := newFakeStore()
	botClient := &fakeBotClient{}

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     botTestConfig(idp, clientID),
	}
	srv, err := NewServer(cfg, st, &fakeMessenger{}, botClient, newRecordingTelemetry(), nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	serviceURL := botActivityFields()["serviceUrl"].(string)
	now := time.Now()
	token := signBotToken(t, key, botTokenClaims{
		Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
		IssuedAt: now.Add(-time.Minute), Expiry: now.Add(10 * time.Minute), NotBefore: now.Add(-time.Minute),
	})

	// Primes the shared rate-limited key set (item 1's floor on unrecognised
	// kids) with fixture-key before any test built on this fixture runs, so a
	// test that fires several requests concurrently -- see
	// TestBotMessageRaceHasExactlyOneWinner -- does not have all but one of
	// them lose the floor's one-attempt-per-window race on a kid nobody has
	// used yet. A membership change that is not the bot's own install is
	// side-effect free: no reply, no store write, see
	// TestBotConversationUpdateUserAddedDoesNotReply.
	priming := botActivityFields()
	priming["type"] = "conversationUpdate"
	priming["membersAdded"] = []map[string]any{{"id": "29:priming-only", "name": "Priming"}}
	postBotMessage(srv.Handler, marshalActivity(t, priming), "Bearer "+token)

	return &botFixture{handler: srv.Handler, store: st, botClient: botClient, token: token}
}

func (f *botFixture) post(t *testing.T, fields map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return postBotMessage(f.handler, marshalActivity(t, fields), "Bearer "+f.token)
}

// An install is not consent: only the reply is a side effect, nothing is
// written to the store.
func TestBotConversationUpdateBotAddedCreatesNoRecipient(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	activity := botActivityFields()
	activity["type"] = "conversationUpdate"
	activity["membersAdded"] = []map[string]any{
		{"id": activity["recipient"].(map[string]any)["id"], "name": "Teamster"},
	}

	rec := f.post(t, activity)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	recipients, err := f.store.ListRecipients(context.Background())
	if err != nil {
		t.Fatalf("ListRecipients: %v", err)
	}
	if len(recipients) != 0 {
		t.Errorf("recipients = %d, want 0: an install is not consent", len(recipients))
	}
	if got := f.botClient.count(installInstructions); got != 1 {
		t.Errorf("install instructions sent %d times, want 1", got)
	}
}

// conversationUpdate fires on every membership change, not just the bot's own
// install; a member other than the bot joining must not be read as one.
func TestBotConversationUpdateUserAddedDoesNotReply(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	activity := botActivityFields()
	activity["type"] = "conversationUpdate"
	activity["membersAdded"] = []map[string]any{{"id": "29:some-other-user", "name": "Bob"}}

	rec := f.post(t, activity)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := len(f.botClient.sentTexts()); got != 0 {
		t.Errorf("sent %d messages, want 0 for a membership change that is not the bot's own install", got)
	}
}

func TestNormalizeLinkCodeStripsMentionAndEntities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		entities []botEntity
		want     string
	}{
		{
			name:     "mention entity and nbsp",
			text:     "<at>Teamster</at>&nbsp;abcd-efgh-jkmn",
			entities: []botEntity{{Type: "mention", Text: "<at>Teamster</at>"}},
			want:     "ABCDEFGHJKMN",
		},
		{
			name: "bare code with dashes and mixed case",
			text: "  aBcD-eFgH-jKmN  ",
			want: "ABCDEFGHJKMN",
		},
		{
			name: "at tag with no matching entity",
			text: "<at>Teamster</at> ABCD-EFGH-JKMN",
			want: "ABCDEFGHJKMN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeLinkCode(tt.text, tt.entities); got != tt.want {
				t.Errorf("normalizeLinkCode(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func seedLinkFlow(t *testing.T, st *fakeStore, code, subject string, expiresAt time.Time) {
	t.Helper()
	if err := st.CreateLinkFlow(context.Background(), models.LinkFlow{
		Code: code, Subject: subject, ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("seed link flow: %v", err)
	}
}

func TestBotMessageRedeemsCode(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	seedLinkFlow(t, f.store, "ABCDEFGHJKMN", "alice", time.Now().Add(time.Minute))

	activity := botActivityFields()
	activity["text"] = "<at>Teamster</at>&nbsp;abcd-efgh-jkmn"
	activity["entities"] = []map[string]any{{"type": "mention", "text": "<at>Teamster</at>"}}
	activity["channelData"] = map[string]any{"tenant": map[string]any{"id": "tenant-1"}}

	rec := f.post(t, activity)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	recipients, err := f.store.ListRecipients(context.Background())
	if err != nil {
		t.Fatalf("ListRecipients: %v", err)
	}
	if len(recipients) != 1 {
		t.Fatalf("recipients = %d, want 1", len(recipients))
	}
	got := recipients[0]
	if got.Subject != "alice" {
		t.Errorf("Subject = %q, want %q (from the redeemed flow, never the activity)", got.Subject, "alice")
	}
	if got.ConversationID != "conv-1" || got.ServiceURL != "https://smba.trafficmanager.net/teams/" ||
		got.BotChannelID != "msteams" || got.AADObjectID != "aad-1" || got.Name != "Alice" || got.TenantID != "tenant-1" {
		t.Errorf("recipient = %+v, want the activity's conversation details", got)
	}

	if got := f.botClient.count(linkConfirmedReply); got != 1 {
		t.Errorf("confirmation sent %d times, want 1", got)
	}

	f.store.mu.Lock()
	_, stillLive := f.store.linkFlows["ABCDEFGHJKMN"]
	f.store.mu.Unlock()
	if stillLive {
		t.Error("the code is still present after being redeemed")
	}
}

func TestBotMessageReplayIsRefused(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	seedLinkFlow(t, f.store, "CDEFGHJKMNPQ", "alice", time.Now().Add(time.Minute))

	activity := botActivityFields()
	activity["text"] = "CDEFGHJKMNPQ"

	first := f.post(t, activity)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}
	second := f.post(t, activity)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d", second.Code)
	}

	recipients, err := f.store.ListRecipients(context.Background())
	if err != nil {
		t.Fatalf("ListRecipients: %v", err)
	}
	if len(recipients) != 1 {
		t.Errorf("recipients = %d, want exactly 1 despite the replay", len(recipients))
	}
	if got := f.botClient.count(linkConfirmedReply); got != 1 {
		t.Errorf("confirmations sent %d times, want 1", got)
	}
	if got := f.botClient.count(linkNeutralReply); got != 1 {
		t.Errorf("neutral replies sent %d times, want 1 (the replay)", got)
	}
}

func TestBotMessageExpiredCodeIsRefused(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	seedLinkFlow(t, f.store, "DEFGHJKMNPQR", "bob", time.Now().Add(-time.Minute))

	activity := botActivityFields()
	activity["text"] = "DEFGHJKMNPQR"

	rec := f.post(t, activity)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	recipients, err := f.store.ListRecipients(context.Background())
	if err != nil {
		t.Fatalf("ListRecipients: %v", err)
	}
	if len(recipients) != 0 {
		t.Errorf("recipients = %d, want 0: the code had expired", len(recipients))
	}
	if got := f.botClient.count(linkNeutralReply); got != 1 {
		t.Errorf("neutral replies sent %d times, want 1", got)
	}

	// The code must be spent regardless of having expired: TakeLinkFlow
	// deletes the row before judging it live, and that deletion must not be
	// rolled back by anything that happens afterwards -- see redeemLinkCode.
	f.store.mu.Lock()
	_, stillLive := f.store.linkFlows["DEFGHJKMNPQR"]
	f.store.mu.Unlock()
	if stillLive {
		t.Error("the expired code is still present: it was restored rather than spent")
	}
}

// A re-link replaces the conversation but keeps the subject and row id, so
// UpdateRecipient runs rather than a second CreateRecipient.
func TestBotMessageRelinkUpdatesRatherThanDuplicates(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	existing, err := f.store.CreateRecipient(context.Background(), models.Recipient{
		ID: "carol-row", Subject: "carol", Name: "Old Name",
		ConversationID: "old-conv", ServiceURL: "https://old.example/", BotChannelID: "msteams",
	})
	if err != nil {
		t.Fatalf("seed recipient: %v", err)
	}

	seedLinkFlow(t, f.store, "EFGHJKMNPQRS", "carol", time.Now().Add(time.Minute))

	activity := botActivityFields()
	activity["text"] = "EFGHJKMNPQRS"
	activity["from"] = map[string]any{"id": "29:carol", "name": "Carol", "aadObjectId": "aad-carol"}

	rec := f.post(t, activity)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	recipients, err := f.store.ListRecipients(context.Background())
	if err != nil {
		t.Fatalf("ListRecipients: %v", err)
	}
	if len(recipients) != 1 {
		t.Fatalf("recipients = %d, want 1 (updated, not duplicated)", len(recipients))
	}
	got := recipients[0]
	if got.ID != existing.ID {
		t.Errorf("ID = %q, want the existing row id %q to be kept", got.ID, existing.ID)
	}
	if got.Subject != "carol" {
		t.Errorf("Subject = %q, want it unchanged by the re-link", got.Subject)
	}
	if got.ConversationID != "conv-1" || got.Name != "Carol" {
		t.Errorf("recipient = %+v, want the new conversation's details", got)
	}
}

// raceLinkCode returns the nth of a sequence of distinct, syntactically valid
// link codes, all drawn from linkCodeAlphabet by rotating it n places.
func raceLinkCode(n int) string {
	const length = linkCodeGroups * linkCodeGroupLen
	code := make([]byte, length)
	for i := range code {
		code[i] = linkCodeAlphabet[(n+i)%len(linkCodeAlphabet)]
	}
	return string(code)
}

// TestBotMessageRaceHasExactlyOneWinner redeems several distinct, still-live
// codes for the SAME subject concurrently. Each on its own is a normal
// redemption -- TakeLinkFlow's atomic delete-on-read already makes one code
// redeemed twice safe with no transaction at all -- so the race that matters
// is between GetRecipientBySubject and the create/update that follows it: if
// every racer's lookup misses before any of them writes, an unprotected
// implementation creates one row per racer instead of one row overall.
// subjectLookupDelay (see fakes_test.go) widens that window deterministically
// instead of hoping the scheduler interleaves two fast map operations.
func TestBotMessageRaceHasExactlyOneWinner(t *testing.T) {
	f := newBotFixture(t)

	const racers = 8
	bodies := make([]string, racers)
	for i := range racers {
		code := raceLinkCode(i)
		seedLinkFlow(t, f.store, code, "dave", time.Now().Add(time.Minute))
		activity := botActivityFields()
		activity["text"] = code
		bodies[i] = marshalActivity(t, activity)
	}

	f.store.mu.Lock()
	f.store.subjectLookupDelay = 20 * time.Millisecond
	f.store.mu.Unlock()

	start := make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			rec := postBotMessage(f.handler, bodies[i], "Bearer "+f.token)
			codes[i] = rec.Code
		}(i)
	}
	close(start)
	wg.Wait()

	for _, code := range codes {
		if code != http.StatusOK {
			t.Errorf("status = %d, want 200", code)
		}
	}

	recipients, err := f.store.ListRecipients(context.Background())
	if err != nil {
		t.Fatalf("ListRecipients: %v", err)
	}
	if len(recipients) != 1 {
		t.Fatalf("recipients = %d, want exactly 1 despite %d concurrent redemptions for the same subject", len(recipients), racers)
	}
	// Every code was live and distinct, so every racer's redemption itself
	// succeeds -- the invariant under test is the row count above, not who
	// "wins": unlike one code shared by every racer, there is no loser here.
	if got := f.botClient.count(linkConfirmedReply); got != racers {
		t.Errorf("confirmations = %d, want %d", got, racers)
	}
}

// A re-link whose activity carries no channelData -- Teams omits it for some
// shapes -- must not blank out a tenant the recipient already had.
func TestBotMessageRelinkWithAbsentTenantKeepsPriorTenant(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	_, err := f.store.CreateRecipient(context.Background(), models.Recipient{
		ID: "erin-row", Subject: "erin", Name: "Old Name",
		ConversationID: "old-conv-erin", ServiceURL: "https://old.example/", BotChannelID: "msteams",
		TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("seed recipient: %v", err)
	}

	seedLinkFlow(t, f.store, "FGHJKMNPQRST", "erin", time.Now().Add(time.Minute))

	activity := botActivityFields()
	activity["text"] = "FGHJKMNPQRST"
	activity["from"] = map[string]any{"id": "29:erin", "name": "Erin", "aadObjectId": "aad-erin"}
	// No channelData set at all.

	rec := f.post(t, activity)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	recipients, err := f.store.ListRecipients(context.Background())
	if err != nil {
		t.Fatalf("ListRecipients: %v", err)
	}
	if len(recipients) != 1 {
		t.Fatalf("recipients = %d, want 1", len(recipients))
	}
	if got := recipients[0].TenantID; got != "tenant-1" {
		t.Errorf("TenantID = %q, want the prior tenant-1 preserved when the activity carries none", got)
	}
}

// A leaked code that redeems against an already-linked subject must not
// silently take over that person's alert stream: the conversation it
// displaces gets told.
func TestBotMessageRelinkNotifiesDisplacedConversation(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	_, err := f.store.CreateRecipient(context.Background(), models.Recipient{
		ID: "frank-row", Subject: "frank", Name: "Old Name",
		ConversationID: "old-conv-frank", ServiceURL: "https://old2.example/", BotChannelID: "msteams",
	})
	if err != nil {
		t.Fatalf("seed recipient: %v", err)
	}

	seedLinkFlow(t, f.store, "GHJKMNPQRSTU", "frank", time.Now().Add(time.Minute))

	activity := botActivityFields()
	activity["text"] = "GHJKMNPQRSTU"
	activity["from"] = map[string]any{"id": "29:frank", "name": "Frank", "aadObjectId": "aad-frank"}

	rec := f.post(t, activity)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if got := f.botClient.count(linkDisplacedReply); got != 1 {
		t.Fatalf("displaced notices sent %d times, want 1", got)
	}

	f.botClient.mu.Lock()
	var notifiedConversation string
	for _, s := range f.botClient.sent {
		if s.msg.Text == linkDisplacedReply {
			notifiedConversation = s.ref.ConversationID
		}
	}
	f.botClient.mu.Unlock()
	if notifiedConversation != "old-conv-frank" {
		t.Errorf("displaced notice went to conversation %q, want the previous conversation %q", notifiedConversation, "old-conv-frank")
	}

	// The winning conversation still gets the ordinary confirmation.
	if got := f.botClient.count(linkConfirmedReply); got != 1 {
		t.Errorf("confirmations = %d, want 1", got)
	}
}

// message is what actually arrives for a personal-scope bot, so it -- not
// only conversationUpdate -- must refresh a known recipient's ServiceURL when
// Teams has moved the conversation to a different regional endpoint.
func TestBotMessageRefreshesServiceURLOnPlainMessage(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	_, err := f.store.CreateRecipient(context.Background(), models.Recipient{
		ID: "gina-row", Subject: "gina", Name: "Gina",
		ConversationID: "conv-1", ServiceURL: "https://old-region.example/", BotChannelID: "msteams",
	})
	if err != nil {
		t.Fatalf("seed recipient: %v", err)
	}

	activity := botActivityFields() // "conv-1", the new region's serviceUrl, text "hello"

	rec := f.post(t, activity)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got, err := f.store.GetRecipient(context.Background(), "gina-row")
	if err != nil {
		t.Fatalf("GetRecipient: %v", err)
	}
	if got.ServiceURL != activity["serviceUrl"].(string) {
		t.Errorf("ServiceURL = %q, want it refreshed to %q by a plain message", got.ServiceURL, activity["serviceUrl"])
	}
}

// seedRecipient links a conversation, so the unlink paths have something to
// retire.
func seedRecipient(t *testing.T, st *fakeStore, subject, conversationID string) models.Recipient {
	t.Helper()

	recipient, err := st.CreateRecipient(context.Background(), models.Recipient{
		Subject: subject, Name: "Alice", ConversationID: conversationID,
		ServiceURL: "https://smba.trafficmanager.net/teams/", BotChannelID: "msteams",
	})
	if err != nil {
		t.Fatalf("seed recipient: %v", err)
	}
	return recipient
}

func TestBotUnlinkCommandRetiresTheLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
	}{
		{name: "bare", text: "unlink"},
		{name: "mentioned", text: "<at>Teamster</at>&nbsp;unlink"},
		{name: "cased and padded", text: "  UnLink  "},
		{name: "stop", text: "stop"},
		{name: "unsubscribe", text: "unsubscribe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newBotFixture(t)
			seedRecipient(t, f.store, "alice", "conv-1")

			activity := botActivityFields()
			activity["text"] = tt.text
			activity["entities"] = []map[string]any{{"type": "mention", "text": "<at>Teamster</at>"}}

			if rec := f.post(t, activity); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}

			recipients, err := f.store.ListRecipients(context.Background())
			if err != nil {
				t.Fatalf("ListRecipients: %v", err)
			}
			if len(recipients) != 0 {
				t.Errorf("recipients = %d, want the link retired", len(recipients))
			}
			if f.botClient.count(unlinkConfirmedReply) != 1 {
				t.Errorf("replies = %v, want one confirmation", f.botClient.sentTexts())
			}
		})
	}
}

// "unlink" is an ordinary word. Only a message that is the command retires a
// link -- one that merely contains it must not.
func TestBotUnlinkCommandIsTheWholeMessage(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	seedRecipient(t, f.store, "alice", "conv-1")

	activity := botActivityFields()
	activity["text"] = "how do I unlink this chat?"

	if rec := f.post(t, activity); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	recipients, _ := f.store.ListRecipients(context.Background())
	if len(recipients) != 1 {
		t.Errorf("recipients = %d, want the link left alone", len(recipients))
	}
	if f.botClient.count(unlinkConfirmedReply) != 0 {
		t.Errorf("replies = %v, want no unlink confirmation", f.botClient.sentTexts())
	}
}

func TestBotUnlinkCommandOnAnUnlinkedChat(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)

	activity := botActivityFields()
	activity["text"] = "unlink"

	if rec := f.post(t, activity); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if f.botClient.count(unlinkNotLinkedReply) != 1 {
		t.Errorf("replies = %v, want the nothing-to-unlink reply", f.botClient.sentTexts())
	}
}

// The command retires the chat it was sent from, never anyone else's.
func TestBotUnlinkCommandRetiresOnlyItsOwnConversation(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	seedRecipient(t, f.store, "alice", "conv-1")
	seedRecipient(t, f.store, "bob", "conv-2")

	activity := botActivityFields()
	activity["text"] = "unlink"

	if rec := f.post(t, activity); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	recipients, _ := f.store.ListRecipients(context.Background())
	if len(recipients) != 1 {
		t.Fatalf("recipients = %d, want only the sender's retired", len(recipients))
	}
	if recipients[0].Subject != "bob" {
		t.Errorf("remaining subject = %q, want bob's link untouched", recipients[0].Subject)
	}
}

// Uninstalling the bot is the person saying they are done; the link should not
// outlive it. There is nobody left to reply to.
func TestBotRemovedRetiresTheLink(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	seedRecipient(t, f.store, "alice", "conv-1")

	activity := botActivityFields()
	activity["type"] = "conversationUpdate"
	activity["text"] = ""
	activity["membersRemoved"] = []map[string]any{{"id": "28:bot-app-id", "name": "Teamster"}}

	if rec := f.post(t, activity); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	recipients, _ := f.store.ListRecipients(context.Background())
	if len(recipients) != 0 {
		t.Errorf("recipients = %d, want the link retired", len(recipients))
	}
	if texts := f.botClient.sentTexts(); len(texts) != 0 {
		t.Errorf("replies = %v, want none: the bot has just been removed", texts)
	}
}

// Somebody else leaving is not the bot being uninstalled.
func TestBotRemovedIgnoresAnotherMemberLeaving(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	seedRecipient(t, f.store, "alice", "conv-1")

	activity := botActivityFields()
	activity["type"] = "conversationUpdate"
	activity["text"] = ""
	activity["membersRemoved"] = []map[string]any{{"id": "29:user-1", "name": "Alice"}}

	if rec := f.post(t, activity); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	recipients, _ := f.store.ListRecipients(context.Background())
	if len(recipients) != 1 {
		t.Errorf("recipients = %d, want the link left alone", len(recipients))
	}
}

// A store that cannot answer must not look like a successful unlink.
func TestBotUnlinkCommandOnAStoreFailure(t *testing.T) {
	t.Parallel()

	f := newBotFixture(t)
	seedRecipient(t, f.store, "alice", "conv-1")
	f.store.fail("DeleteRecipient")

	activity := botActivityFields()
	activity["text"] = "unlink"

	if rec := f.post(t, activity); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	recipients, _ := f.store.ListRecipients(context.Background())
	if len(recipients) != 1 {
		t.Errorf("recipients = %d, want the link intact", len(recipients))
	}
	if f.botClient.count(unlinkConfirmedReply) != 0 {
		t.Errorf("replies = %v, want no confirmation of an unlink that failed", f.botClient.sentTexts())
	}
}
