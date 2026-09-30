package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/people"
)

// fakePeople knows people by lower-cased address; anyone else is unknown.
type fakePeople struct {
	mu       sync.Mutex
	byAddr   map[string]models.DirectoryUser
	errs     map[string]error
	resolves int
	installs int
	// installable get a chat when Ensure may install.
	installable map[string]bool
}

func (p *fakePeople) Resolve(_ context.Context, address string) (models.DirectoryUser, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolves++
	key := strings.ToLower(address)
	if err := p.errs[key]; err != nil {
		return models.DirectoryUser{}, err
	}
	u, ok := p.byAddr[key]
	if !ok {
		return models.DirectoryUser{}, fmt.Errorf("%q: %w", address, people.ErrUnknown)
	}
	return u, nil
}

func (p *fakePeople) Ensure(_ context.Context, u models.DirectoryUser, _, install bool) (models.DirectoryUser, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if install {
		p.installs++
		if p.installable[u.AADObjectID] {
			u.ConversationID, u.InstallState = "a:"+u.AADObjectID, models.InstallInstalled
		}
	}
	if u.ConversationID == "" {
		return u, people.OutcomeFailed, people.ErrNotInstalled
	}
	return u, people.OutcomeAlready, nil
}

type addressedFixture struct {
	server  *Server
	handler http.Handler
	store   *fakeStore
	bot     *fakeBotClient
	people  *fakePeople
}

// newAddressedFixture has a route sending password reminders to the people a
// message names, rendered with their given name, and three people: Alice and
// Bob with a chat, Carol without one.
func newAddressedFixture(t *testing.T, configure func(*config.Config)) addressedFixture {
	t.Helper()

	st := newFakeStore()
	st.templates["reminder"] = models.Template{ID: "reminder", Name: "Reminder", Text: "Hello {{ .Recipient.GivenName }}, your password expires"}
	st.routes["pw"] = models.Route{ID: "pw", Name: "Passwords", Addressed: true, TemplateID: "reminder", LabelSelector: map[string]string{"kind": "password"}}

	pp := &fakePeople{byAddr: map[string]models.DirectoryUser{}, errs: map[string]error{}}
	for _, u := range []models.DirectoryUser{
		{AADObjectID: "oid-alice", UserPrincipalName: "alice@corp.example", GivenName: "Alice", ConversationID: "a:alice", ServiceURL: "https://smba.example/", TenantID: "t", InstallState: models.InstallInstalled},
		{AADObjectID: "oid-bob", UserPrincipalName: "bob@corp.example", Mail: "b.b@corp.example", GivenName: "Bob", ConversationID: "a:bob", ServiceURL: "https://smba.example/", TenantID: "t", InstallState: models.InstallInstalled},
		{AADObjectID: "oid-carol", UserPrincipalName: "carol@corp.example", GivenName: "Carol", TenantID: "t"},
	} {
		st.directory[u.AADObjectID] = u
		pp.byAddr[u.UserPrincipalName] = u
		if u.Mail != "" {
			pp.byAddr[u.Mail] = u
		}
	}

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token", MaxRecipients: 10, FanoutConcurrency: 2},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     notificationsBotConfig(),
	}
	cfg.Bot.GlobalInstall, cfg.Bot.InlineInstallBudget = true, 5
	if configure != nil {
		configure(&cfg)
	}
	botClient := &fakeBotClient{}
	var server *Server
	capture := func(s *Server) { server = s }
	srv, err := NewServer(quietLog, cfg, st, &fakeMessenger{}, botClient, &fakeMessenger{}, metrics.Disabled(), nil, WithPeople(pp), capture)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return addressedFixture{server: server, handler: srv.Handler, store: st, bot: botClient, people: pp}
}

// sentTo lists the conversations messages went to, with their text, sorted.
func (f addressedFixture) sentTo() []string {
	f.bot.mu.Lock()
	defer f.bot.mu.Unlock()
	var out []string
	for _, s := range f.bot.sent {
		out = append(out, s.ref.ConversationID+": "+s.msg.Text)
	}
	sort.Strings(out)
	return out
}

type webhookAnswer struct {
	Status      string        `json:"status"`
	Delivered   int           `json:"delivered"`
	Undelivered []undelivered `json:"undelivered"`
}

func answerOf(t *testing.T, body []byte) webhookAnswer {
	t.Helper()
	var a webhookAnswer
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("decode answer %s: %v", body, err)
	}
	return a
}

func TestAddressedMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		body            string
		wantStatus      int
		wantAnswer      string
		wantSent        []string
		wantUndelivered []undelivered
	}{
		{
			name:       "each person gets their own",
			body:       `{"state":"open","labels":{"kind":"password"},"recipients":["alice@corp.example","B.B@corp.example"]}`,
			wantStatus: http.StatusOK, wantAnswer: "ok",
			wantSent: []string{"a:alice: Hello Alice, your password expires", "a:bob: Hello Bob, your password expires"},
		},
		{
			name:       "one address twice is one message",
			body:       `{"labels":{"kind":"password"},"recipients":["alice@corp.example","ALICE@corp.example"]}`,
			wantStatus: http.StatusOK, wantAnswer: "ok",
			wantSent: []string{"a:alice: Hello Alice, your password expires"},
		},
		{
			name:       "the label when there is no list",
			body:       `{"labels":{"kind":"password","teamster_recipient":"bob@corp.example"}}`,
			wantStatus: http.StatusOK, wantAnswer: "ok",
			wantSent: []string{"a:bob: Hello Bob, your password expires"},
		},
		{
			name:       "some reached, some not",
			body:       `{"labels":{"kind":"password"},"recipients":["alice@corp.example","nobody@corp.example","carol@corp.example"]}`,
			wantStatus: http.StatusOK, wantAnswer: "partial",
			wantSent: []string{"a:alice: Hello Alice, your password expires"},
			wantUndelivered: []undelivered{
				{Recipient: "nobody@corp.example", Reason: "unknown-recipient"},
				{Recipient: "carol@corp.example", Reason: "not-installed"},
			},
		},
		{
			name:       "nobody reached",
			body:       `{"labels":{"kind":"password"},"recipients":["nobody@corp.example"]}`,
			wantStatus: http.StatusUnprocessableEntity, wantAnswer: "undelivered",
			wantUndelivered: []undelivered{{Recipient: "nobody@corp.example", Reason: "unknown-recipient"}},
		},
		{
			name:       "nobody named",
			body:       `{"labels":{"kind":"password"}}`,
			wantStatus: http.StatusUnprocessableEntity, wantAnswer: "undelivered",
			wantUndelivered: []undelivered{{Recipient: "", Reason: "no-recipient"}},
		},
		{
			name:       "not an address",
			body:       `{"labels":{"kind":"password"},"recipients":["alice"]}`,
			wantStatus: http.StatusUnprocessableEntity, wantAnswer: "undelivered",
			wantUndelivered: []undelivered{{Recipient: "alice", Reason: "unknown-recipient"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAddressedFixture(t, nil)
			rec := postWebhook(t, f.handler, "/webhook/universal", "token", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
			answer := answerOf(t, rec.Body.Bytes())
			if answer.Status != tt.wantAnswer {
				t.Errorf("answer = %+v, want status %q", answer, tt.wantAnswer)
			}
			if got := f.sentTo(); strings.Join(got, "|") != strings.Join(tt.wantSent, "|") {
				t.Errorf("sent = %q, want %q", got, tt.wantSent)
			}
			if fmt.Sprint(answer.Undelivered) != fmt.Sprint(tt.wantUndelivered) {
				t.Errorf("undelivered = %+v, want %+v", answer.Undelivered, tt.wantUndelivered)
			}
			if len(tt.wantUndelivered) > 0 && answer.Delivered != len(tt.wantSent) {
				t.Errorf("delivered = %d, want %d", answer.Delivered, len(tt.wantSent))
			}
		})
	}
}

func TestAddressedMessagesAreTrackedPerPerson(t *testing.T) {
	t.Parallel()

	f := newAddressedFixture(t, nil)
	open := `{"state":"open","key":"pw-1","labels":{"kind":"password"},"recipients":["alice@corp.example","bob@corp.example"]}`
	for range 2 {
		if rec := postWebhook(t, f.handler, "/webhook/universal", "token", open); rec.Code != http.StatusOK {
			t.Fatalf("open = %d %s", rec.Code, rec.Body.String())
		}
	}
	if len(f.bot.sent) != 2 || len(f.bot.updated) != 2 {
		t.Fatalf("sent %d, updated %d; want two sends and two edits", len(f.bot.sent), len(f.bot.updated))
	}
	for _, key := range []string{"aad:oid-alice", "aad:oid-bob"} {
		if _, ok := f.store.activeChats[activeChatKey("pw-1", key)]; !ok {
			var keys []string
			for k := range f.store.activeChats {
				keys = append(keys, k)
			}
			t.Errorf("no claim row for %s in %v", key, keys)
		}
	}

	// A close needs no recipients: it walks the rows the open left.
	rec := postWebhook(t, f.handler, "/webhook/universal", "token", `{"state":"closed","key":"pw-1","labels":{"kind":"password"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("close = %d %s", rec.Code, rec.Body.String())
	}
	if len(f.bot.sent) != 4 {
		t.Errorf("sent = %d after the close, want a new message for each person", len(f.bot.sent))
	}
	if len(f.store.activeChats) != 0 {
		t.Errorf("claim rows left after the close: %v", f.store.activeChats)
	}
}

func TestAddressedMessageRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configure  func(*config.Config)
		failBot    error
		resolveErr error
		body       string
		wantStatus int
		wantReason string
	}{
		{
			name: "too many named", configure: func(c *config.Config) { c.Webhook.MaxRecipients = 1 },
			body:       `{"labels":{"kind":"password"},"recipients":["alice@corp.example","bob@corp.example"]}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "the directory is down", resolveErr: errors.New("graph 503"),
			body:       `{"labels":{"kind":"password"},"recipients":["alice@corp.example"]}`,
			wantStatus: http.StatusBadGateway,
		},
		{
			name: "the person blocked the bot", failBot: &bot.APIError{Code: "MessageWritesBlocked"},
			body:       `{"state":"open","labels":{"kind":"password"},"recipients":["alice@corp.example"]}`,
			wantStatus: http.StatusUnprocessableEntity, wantReason: "blocked",
		},
		{
			name: "the bot connector is down", failBot: errors.New("503"),
			body:       `{"labels":{"kind":"password"},"recipients":["alice@corp.example"]}`,
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAddressedFixture(t, tt.configure)
			f.bot.err = tt.failBot
			if tt.resolveErr != nil {
				f.people.errs["alice@corp.example"] = tt.resolveErr
			}
			rec := postWebhook(t, f.handler, "/webhook/universal", "token", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
			if tt.wantReason != "" {
				if answer := answerOf(t, rec.Body.Bytes()); len(answer.Undelivered) != 1 || answer.Undelivered[0].Reason != tt.wantReason {
					t.Errorf("answer = %+v, want one %s", answer, tt.wantReason)
				}
				if f.store.directory["oid-alice"].BlockedReason != "MessageWritesBlocked" {
					t.Error("the blocked flag was not recorded on the directory user")
				}
			}
		})
	}
}

func TestAddressedInstallsStayWithinTheBudget(t *testing.T) {
	t.Parallel()

	f := newAddressedFixture(t, func(c *config.Config) { c.Bot.InlineInstallBudget = 1 })
	f.people.byAddr["dave@corp.example"] = models.DirectoryUser{AADObjectID: "oid-dave", UserPrincipalName: "dave@corp.example", TenantID: "t"}

	postWebhook(t, f.handler, "/webhook/universal", "token", `{"labels":{"kind":"password"},"recipients":["carol@corp.example","dave@corp.example","alice@corp.example"]}`)
	if f.people.installs != 1 {
		t.Errorf("installs asked for = %d, want the budget of 1", f.people.installs)
	}
}

func TestAlertmanagerAddressesByLabel(t *testing.T) {
	t.Parallel()

	f := newAddressedFixture(t, nil)
	body := `{"alerts":[
		{"status":"firing","labels":{"kind":"password","teamster_recipient":"alice@corp.example"},"fingerprint":"a"},
		{"status":"firing","labels":{"kind":"password","teamster_recipient":"nobody@corp.example"},"fingerprint":"b"}
	]}`
	rec := postWebhook(t, f.handler, "/webhook/alertmanager", "token", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	if answer := answerOf(t, rec.Body.Bytes()); answer.Status != "partial" || answer.Delivered != 1 || len(answer.Undelivered) != 1 {
		t.Errorf("answer = %+v, want one delivered and one unknown across the batch", answer)
	}
}

func TestAddressedWithoutTheBot(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.routes["pw"] = models.Route{ID: "pw", Name: "Passwords", Addressed: true, LabelSelector: map[string]string{"kind": "password"}}
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{"kind":"password"},"recipients":["alice@corp.example"]}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "no bot is configured") {
		t.Errorf("status = %d %s, want 502 naming the missing bot", rec.Code, rec.Body.String())
	}
}

func TestDeriveKeyIncludesRecipientsOnlyWhenNamed(t *testing.T) {
	t.Parallel()

	base := models.Event{Source: models.SourceUniversal, Labels: map[string]string{"kind": "password"}, Universal: &models.UniversalEvent{}}
	withNone := base
	withNone.Universal = &models.UniversalEvent{Recipients: nil}
	alice := base
	alice.Universal = &models.UniversalEvent{Recipients: []string{"alice@corp.example"}}
	aliceAgain := base
	aliceAgain.Universal = &models.UniversalEvent{Recipients: []string{" ALICE@corp.example"}}
	bob := base
	bob.Universal = &models.UniversalEvent{Recipients: []string{"bob@corp.example"}}

	if deriveKey(base) != deriveKey(withNone) {
		t.Error("an empty recipients list changed the key")
	}
	if deriveKey(alice) == deriveKey(base) || deriveKey(alice) == deriveKey(bob) {
		t.Error("recipients do not tell events apart")
	}
	if deriveKey(alice) != deriveKey(aliceAgain) {
		t.Error("case or spacing of an address changed the key")
	}
}
