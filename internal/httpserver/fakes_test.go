package httpserver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

var errStore = errors.New("store exploded")

const testSessionID = "test-session"

// fakeStore is an in-memory store.Store. Setting failOn to a method name makes
// that method return errStore, which is how the handler error paths are driven.
type fakeStore struct {
	mu sync.Mutex

	templates    map[string]models.Template
	destinations map[string]models.Destination
	recipients   map[string]models.Recipient
	webhooks     map[string]models.WebhookEndpoint
	accessTokens map[string]models.AccessToken
	botTeams     map[string]models.BotTeam
	directory    map[string]models.DirectoryUser
	runs         []models.DirectoryRun
	routes       map[string]models.Route
	activeEvents map[string]models.ActiveEvent
	// Keyed and cloned separately from activeEvents, because the real store
	// keeps chat messages in their own table for the reason ADR 0026 gives.
	activeChats  map[string]models.ActiveEventRecipient
	grants       map[string]models.Grant
	sessions     map[string]models.Session
	brokerTokens map[string]models.BrokerToken
	loginFlows   map[string]models.LoginFlow
	linkFlows    map[string]models.LinkFlow
	samples      []models.EventSample
	auditEvents  []models.AuditEvent
	users        map[string]models.User
	authzGen     int64
	groups       map[string]models.Group
	members      []models.GroupMember
	permissions  map[string]models.Permission
	broadcasts   map[string]models.Broadcast
	// globalDefaultTemplate is the catch-all's template; "" is the built-in one.
	globalDefaultTemplate string
	// sourceDefaults maps a source to its default template.
	sourceDefaults map[string]string
	seeded         bool

	failOn map[string]bool

	// subjectLookupDelay, when set, is slept in GetRecipientBySubject after
	// its map read and its lock have both been released -- a test hook that
	// widens the check-then-act window between the lookup and a later
	// CreateRecipient/UpdateRecipient, the way a real network round trip
	// would, so a check-then-act race is reproducible instead of hoped for.
	subjectLookupDelay time.Duration
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		permissions:    map[string]models.Permission{},
		broadcasts:     map[string]models.Broadcast{},
		groups:         map[string]models.Group{},
		users:          map[string]models.User{},
		templates:      map[string]models.Template{},
		sourceDefaults: map[string]string{},
		destinations:   map[string]models.Destination{},
		recipients:     map[string]models.Recipient{},
		webhooks:       map[string]models.WebhookEndpoint{},
		accessTokens:   map[string]models.AccessToken{},
		botTeams:       map[string]models.BotTeam{},
		directory:      map[string]models.DirectoryUser{},
		routes:         map[string]models.Route{},
		activeEvents:   map[string]models.ActiveEvent{},
		activeChats:    map[string]models.ActiveEventRecipient{},
		sessions: map[string]models.Session{
			// Seeded so the request helpers can act as a signed-in operator; a
			// test that cares about being signed out builds its own request.
			testSessionID: {
				ID: testSessionID, Subject: "tester", Name: "tester", Source: "local",
				CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
			},
		},
		grants:       map[string]models.Grant{},
		brokerTokens: map[string]models.BrokerToken{},
		loginFlows:   map[string]models.LoginFlow{},
		linkFlows:    map[string]models.LinkFlow{},
		failOn:       map[string]bool{},
	}
}

func (f *fakeStore) fail(methods ...string) *fakeStore {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, method := range methods {
		f.failOn[method] = true
	}
	return f
}

// failing is called with f.mu held: every method locks for its whole body, so
// that a test may drive two goroutines through the fake without racing on the
// maps behind it.
func (f *fakeStore) failing(method string) error {
	if f.failOn[method] {
		return errStore
	}
	return nil
}

func (f *fakeStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failing("Close")
}

func (f *fakeStore) ListTemplates(ctx context.Context) ([]models.Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListTemplates"); err != nil {
		return nil, err
	}
	out := []models.Template{}
	for _, t := range f.templates {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeStore) CreateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateTemplate"); err != nil {
		return models.Template{}, err
	}
	if t.ID == "" {
		t.ID = "generated"
	}
	f.templates[t.ID] = t
	return t, nil
}

func (f *fakeStore) UpdateTemplate(ctx context.Context, t models.Template) (models.Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpdateTemplate"); err != nil {
		return models.Template{}, err
	}
	f.templates[t.ID] = t
	return t, nil
}

func (f *fakeStore) DeleteTemplate(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteTemplate"); err != nil {
		return err
	}
	delete(f.templates, id)
	if f.globalDefaultTemplate == id {
		f.globalDefaultTemplate = ""
	}
	for source, templateID := range f.sourceDefaults {
		if templateID == id {
			delete(f.sourceDefaults, source)
		}
	}
	return nil
}

func (f *fakeStore) GetSourceDefaultTemplate(ctx context.Context, source string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetSourceDefaultTemplate"); err != nil {
		return "", err
	}
	// The real store refuses a source no webhook sets; so must the fake.
	if _, err := models.NormalizeSources([]string{source}); err != nil {
		return "", err
	}
	return f.sourceDefaults[source], nil
}

func (f *fakeStore) SourceDefaultTemplates(ctx context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("SourceDefaultTemplates"); err != nil {
		return nil, err
	}
	return maps.Clone(f.sourceDefaults), nil
}

func (f *fakeStore) SetSourceDefaultTemplate(ctx context.Context, source, templateID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("SetSourceDefaultTemplate"); err != nil {
		return err
	}
	if _, err := models.NormalizeSources([]string{source}); err != nil {
		return err
	}
	if templateID == "" {
		delete(f.sourceDefaults, source)
		return nil
	}
	t, ok := f.templates[templateID]
	if !ok {
		return store.ErrNotFound
	}
	if !t.Handles(source) {
		return store.ErrTemplateSource
	}
	f.sourceDefaults[source] = templateID
	return nil
}

func (f *fakeStore) SeedTemplates(ctx context.Context, templates []models.Template) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("SeedTemplates"); err != nil {
		return false, err
	}
	if f.seeded {
		return false, nil
	}
	f.seeded = true
	for i, t := range templates {
		if t.ID == "" {
			t.ID = fmt.Sprintf("seeded-%d", i)
		}
		f.templates[t.ID] = t
		if len(t.Sources) == 1 && f.sourceDefaults[t.Sources[0]] == "" {
			f.sourceDefaults[t.Sources[0]] = t.ID
		}
	}
	return true, nil
}

func (f *fakeStore) GetGlobalDefaultTemplate(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetGlobalDefaultTemplate"); err != nil {
		return "", err
	}
	return f.globalDefaultTemplate, nil
}

func (f *fakeStore) SetGlobalDefaultTemplate(ctx context.Context, templateID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("SetGlobalDefaultTemplate"); err != nil {
		return err
	}
	if _, ok := f.templates[templateID]; templateID != "" && !ok {
		return store.ErrNotFound
	}
	f.globalDefaultTemplate = templateID
	return nil
}

func (f *fakeStore) GetTemplate(ctx context.Context, id string) (models.Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetTemplate"); err != nil {
		return models.Template{}, err
	}
	t, ok := f.templates[id]
	if !ok {
		return models.Template{}, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeStore) ListDestinations(ctx context.Context) ([]models.Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListDestinations"); err != nil {
		return nil, err
	}
	out := []models.Destination{}
	for _, d := range f.destinations {
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeStore) CreateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateDestination"); err != nil {
		return models.Destination{}, err
	}
	if d.ID == "" {
		d.ID = "generated"
	}
	d.IsDefault = true
	for _, other := range f.destinations {
		if other.IsDefault {
			d.IsDefault = false
		}
	}
	f.destinations[d.ID] = d
	return d, nil
}

func (f *fakeStore) UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpdateDestination"); err != nil {
		return models.Destination{}, err
	}
	d.IsDefault = f.destinations[d.ID].IsDefault
	f.destinations[d.ID] = d
	return d, nil
}

func (f *fakeStore) DeleteDestination(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteDestination"); err != nil {
		return err
	}
	if f.destinations[id].IsDefault && len(f.destinations) > 1 {
		return store.ErrDefaultDestination
	}
	delete(f.destinations, id)
	return nil
}

func (f *fakeStore) GetDefaultDestination(ctx context.Context) (models.Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetDefaultDestination"); err != nil {
		return models.Destination{}, err
	}
	for _, d := range f.destinations {
		if d.IsDefault {
			return d, nil
		}
	}
	return models.Destination{}, store.ErrNotFound
}

func (f *fakeStore) SetDefaultDestination(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("SetDefaultDestination"); err != nil {
		return err
	}
	if _, ok := f.destinations[id]; !ok {
		return store.ErrNotFound
	}
	for key, d := range f.destinations {
		d.IsDefault = key == id
		f.destinations[key] = d
	}
	return nil
}

func (f *fakeStore) GetDestination(ctx context.Context, id string) (models.Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetDestination"); err != nil {
		return models.Destination{}, err
	}
	d, ok := f.destinations[id]
	if !ok {
		return models.Destination{}, store.ErrNotFound
	}
	return d, nil
}

func (f *fakeStore) ListRecipients(ctx context.Context) ([]models.Recipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListRecipients"); err != nil {
		return nil, err
	}
	out := []models.Recipient{}
	for _, r := range f.recipients {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeStore) CreateRecipient(ctx context.Context, r models.Recipient) (models.Recipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateRecipient"); err != nil {
		return models.Recipient{}, err
	}
	if r.ID == "" {
		// Unique per call, like the real store's newID(""): a constant here
		// would let two duplicate creates collapse onto one map entry and
		// mask exactly the race a caller-side transaction exists to prevent.
		r.ID = uuid.NewString()
	}
	f.recipients[r.ID] = r
	return r, nil
}

func (f *fakeStore) UpdateRecipient(ctx context.Context, r models.Recipient) (models.Recipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpdateRecipient"); err != nil {
		return models.Recipient{}, err
	}
	// The real store leaves the subject alone, so neither does this.
	if existing, ok := f.recipients[r.ID]; ok {
		r.Subject = existing.Subject
	}
	f.recipients[r.ID] = r
	return r, nil
}

// DeleteRecipient cascades to that recipient's active_alert_recipients rows,
// mirroring the real store's SQLiteStore/PostgresStore override: an event
// claimed or posted to this person must not survive them, or a close would
// fail against a row nothing will ever clear again.
func (f *fakeStore) DeleteRecipient(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteRecipient"); err != nil {
		return err
	}
	delete(f.recipients, id)
	f.deleteActiveEventRecipientsForLocked(id)
	return nil
}

func (f *fakeStore) GetRecipient(ctx context.Context, id string) (models.Recipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetRecipient"); err != nil {
		return models.Recipient{}, err
	}
	r, ok := f.recipients[id]
	if !ok {
		return models.Recipient{}, store.ErrNotFound
	}
	return r, nil
}

func (f *fakeStore) GetRecipientByConversation(ctx context.Context, conversationID string) (models.Recipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetRecipientByConversation"); err != nil {
		return models.Recipient{}, err
	}
	// Ordered, so a conversation two subjects share resolves the same way the
	// real query's ORDER BY does rather than however the map iterates.
	var found *models.Recipient
	for _, r := range f.recipients {
		if r.ConversationID != conversationID {
			continue
		}
		if found == nil || r.CreatedAt.Before(found.CreatedAt) || (r.CreatedAt.Equal(found.CreatedAt) && r.ID < found.ID) {
			match := r
			found = &match
		}
	}
	if found == nil {
		return models.Recipient{}, store.ErrNotFound
	}
	return *found, nil
}

func (f *fakeStore) GetRecipientBySubject(ctx context.Context, subject string) (models.Recipient, error) {
	f.mu.Lock()
	if err := f.failing("GetRecipientBySubject"); err != nil {
		f.mu.Unlock()
		return models.Recipient{}, err
	}
	var found models.Recipient
	var ok bool
	for _, r := range f.recipients {
		if r.Subject == subject {
			found, ok = r, true
			break
		}
	}
	delay := f.subjectLookupDelay
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}

	if !ok {
		return models.Recipient{}, store.ErrNotFound
	}
	return found, nil
}

func (f *fakeStore) UpsertBotTeam(ctx context.Context, t models.BotTeam) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpsertBotTeam"); err != nil {
		return err
	}
	f.botTeams[t.TeamID] = t
	return nil
}

func (f *fakeStore) GetBotTeam(ctx context.Context, teamID string) (models.BotTeam, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetBotTeam"); err != nil {
		return models.BotTeam{}, err
	}
	t, ok := f.botTeams[teamID]
	if !ok {
		return models.BotTeam{}, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeStore) ListBotTeams(ctx context.Context) ([]models.BotTeam, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListBotTeams"); err != nil {
		return nil, err
	}
	out := make([]models.BotTeam, 0, len(f.botTeams))
	for _, t := range f.botTeams {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TeamID < out[j].TeamID })
	return out, nil
}

func (f *fakeStore) CountDestinationsWithoutBotTeam(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CountDestinationsWithoutBotTeam"); err != nil {
		return 0, err
	}
	var n int64
	for _, d := range f.destinations {
		if _, ok := f.botTeams[d.TeamID]; !ok {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) DeleteBotTeam(ctx context.Context, teamID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteBotTeam"); err != nil {
		return err
	}
	delete(f.botTeams, teamID)
	return nil
}

func (f *fakeStore) ListAccessTokens(ctx context.Context) ([]models.AccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListAccessTokens"); err != nil {
		return nil, err
	}
	out := []models.AccessToken{}
	for _, t := range f.accessTokens {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeStore) CreateAccessToken(ctx context.Context, t models.AccessToken) (models.AccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateAccessToken"); err != nil {
		return models.AccessToken{}, err
	}
	for _, existing := range f.accessTokens {
		if existing.Name == t.Name || existing.TokenHash == t.TokenHash {
			return models.AccessToken{}, store.ErrConflict
		}
	}
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.CreatedAt = time.Now().UTC()
	f.accessTokens[t.ID] = t
	if t.Scoped() {
		f.authzGen++
	}
	return t, nil
}

func (f *fakeStore) GetAccessTokenByHash(ctx context.Context, tokenHash string) (models.AccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetAccessTokenByHash"); err != nil {
		return models.AccessToken{}, err
	}
	for _, t := range f.accessTokens {
		if t.TokenHash == tokenHash {
			return t, nil
		}
	}
	return models.AccessToken{}, store.ErrNotFound
}

func (f *fakeStore) TouchAccessToken(ctx context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("TouchAccessToken"); err != nil {
		return err
	}
	if t, ok := f.accessTokens[id]; ok {
		t.LastUsedAt = at
		f.accessTokens[id] = t
	}
	return nil
}

func (f *fakeStore) DeleteAccessToken(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteAccessToken"); err != nil {
		return err
	}
	delete(f.accessTokens, id)
	f.authzGen++
	return nil
}

func (f *fakeStore) ListWebhookEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListWebhookEndpoints"); err != nil {
		return nil, err
	}
	out := []models.WebhookEndpoint{}
	for _, e := range f.webhooks {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TeamSlug != out[j].TeamSlug {
			return out[i].TeamSlug < out[j].TeamSlug
		}
		return out[i].ChannelSlug < out[j].ChannelSlug
	})
	return out, nil
}

func (f *fakeStore) CreateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateWebhookEndpoint"); err != nil {
		return models.WebhookEndpoint{}, err
	}
	if e.ID == "" {
		e.ID = "generated"
	}
	f.webhooks[e.ID] = e
	return e, nil
}

func (f *fakeStore) UpdateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpdateWebhookEndpoint"); err != nil {
		return models.WebhookEndpoint{}, err
	}
	// The real store leaves the token alone, so neither does this.
	if existing, ok := f.webhooks[e.ID]; ok {
		e.TokenHash = existing.TokenHash
	}
	f.webhooks[e.ID] = e
	return e, nil
}

func (f *fakeStore) RotateWebhookEndpointToken(ctx context.Context, id, tokenHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("RotateWebhookEndpointToken"); err != nil {
		return err
	}
	e, ok := f.webhooks[id]
	if !ok {
		return store.ErrNotFound
	}
	e.TokenHash = tokenHash
	f.webhooks[id] = e
	return nil
}

func (f *fakeStore) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteWebhookEndpoint"); err != nil {
		return err
	}
	delete(f.webhooks, id)
	return nil
}

func (f *fakeStore) GetWebhookEndpoint(ctx context.Context, id string) (models.WebhookEndpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetWebhookEndpoint"); err != nil {
		return models.WebhookEndpoint{}, err
	}
	e, ok := f.webhooks[id]
	if !ok {
		return models.WebhookEndpoint{}, store.ErrNotFound
	}
	return e, nil
}

func (f *fakeStore) GetWebhookEndpointBySlug(ctx context.Context, teamSlug, channelSlug string) (models.WebhookEndpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetWebhookEndpointBySlug"); err != nil {
		return models.WebhookEndpoint{}, err
	}
	for _, e := range f.webhooks {
		if e.TeamSlug == teamSlug && e.ChannelSlug == channelSlug {
			return e, nil
		}
	}
	return models.WebhookEndpoint{}, store.ErrNotFound
}

// MarkRecipientBlocked and ClearRecipientBlocked mutate only the blocked
// fields on the map entry, mirroring the real store's narrow statements: a
// fake that routed these through a full update could pass a test the real
// store would fail if a caller ever handed it a stale recipient.
func (f *fakeStore) MarkRecipientBlocked(ctx context.Context, id string, at time.Time, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("MarkRecipientBlocked"); err != nil {
		return err
	}
	// An UPDATE matching no row is not an error in the real store either, so
	// a recipient deleted between the send and this call is silently a no-op
	// rather than a failure the caller has to handle.
	r, ok := f.recipients[id]
	if !ok {
		return nil
	}
	r.BlockedAt = at
	r.BlockedReason = reason
	f.recipients[id] = r
	return nil
}

func (f *fakeStore) ClearRecipientBlocked(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ClearRecipientBlocked"); err != nil {
		return err
	}
	r, ok := f.recipients[id]
	if !ok {
		// A no-op, like the real store's UPDATE matching zero rows: clearing a
		// recipient that does not exist, or was never blocked, is not an error.
		return nil
	}
	r.BlockedAt = time.Time{}
	r.BlockedReason = ""
	f.recipients[id] = r
	return nil
}

func (f *fakeStore) CreateLinkFlow(ctx context.Context, flow models.LinkFlow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateLinkFlow"); err != nil {
		return err
	}
	// The real store's primary key raises ErrConflict on a collision rather
	// than silently overwriting the row, which is what the code-minting retry
	// loop depends on.
	if _, exists := f.linkFlows[flow.Code]; exists {
		return store.ErrConflict
	}
	f.linkFlows[flow.Code] = flow
	return nil
}

// TakeLinkFlow matches the real store's liveLinkFlow check: the row is deleted
// either way, but an expired one is reported as ErrNotFound rather than handed
// back as live. Without this an "expired code is refused" test would pass
// against production and fail against the fake.
func (f *fakeStore) TakeLinkFlow(ctx context.Context, code string) (models.LinkFlow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("TakeLinkFlow"); err != nil {
		return models.LinkFlow{}, err
	}
	flow, ok := f.linkFlows[code]
	if !ok {
		return models.LinkFlow{}, store.ErrNotFound
	}
	delete(f.linkFlows, code)
	if !flow.ExpiresAt.After(time.Now()) {
		return models.LinkFlow{}, store.ErrNotFound
	}
	return flow, nil
}

func (f *fakeStore) DeleteLinkFlowsForSubject(ctx context.Context, subject string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteLinkFlowsForSubject"); err != nil {
		return err
	}
	for code, flow := range f.linkFlows {
		if flow.Subject == subject {
			delete(f.linkFlows, code)
		}
	}
	return nil
}

func (f *fakeStore) ListRoutes(ctx context.Context) ([]models.Route, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListRoutes"); err != nil {
		return nil, err
	}
	out := []models.Route{}
	for _, r := range f.routes {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeStore) CreateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateRoute"); err != nil {
		return models.Route{}, err
	}
	if r.ID == "" {
		r.ID = "generated"
	}
	f.routes[r.ID] = r
	return r, nil
}

func (f *fakeStore) UpdateRoute(ctx context.Context, r models.Route) (models.Route, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpdateRoute"); err != nil {
		return models.Route{}, err
	}
	f.routes[r.ID] = r
	return r, nil
}

func (f *fakeStore) DeleteRoute(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteRoute"); err != nil {
		return err
	}
	delete(f.routes, id)
	return nil
}

func (f *fakeStore) GetRoute(ctx context.Context, id string) (models.Route, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetRoute"); err != nil {
		return models.Route{}, err
	}
	r, ok := f.routes[id]
	if !ok {
		return models.Route{}, store.ErrNotFound
	}
	return r, nil
}

func (f *fakeStore) CreateSession(ctx context.Context, session models.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateSession"); err != nil {
		return err
	}
	f.sessions[session.ID] = session
	return nil
}

func (f *fakeStore) GetSession(ctx context.Context, id string) (models.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetSession"); err != nil {
		return models.Session{}, err
	}
	session, ok := f.sessions[id]
	if !ok || !session.ExpiresAt.After(time.Now()) {
		return models.Session{}, store.ErrNotFound
	}
	return session, nil
}

// DeleteSession also drops the session's broker token, mirroring the real
// stores' deleteSessionCascade.
func (f *fakeStore) DeleteSession(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteSession"); err != nil {
		return err
	}
	delete(f.sessions, id)
	delete(f.brokerTokens, id)
	return nil
}

func (f *fakeStore) DeleteExpiredSessions(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failing("DeleteExpiredSessions")
}

func (f *fakeStore) CreateBrokerToken(ctx context.Context, t models.BrokerToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateBrokerToken"); err != nil {
		return err
	}
	f.brokerTokens[t.SessionID] = t
	return nil
}

func (f *fakeStore) GetBrokerToken(ctx context.Context, sessionID string) (models.BrokerToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetBrokerToken"); err != nil {
		return models.BrokerToken{}, err
	}
	t, ok := f.brokerTokens[sessionID]
	if !ok {
		return models.BrokerToken{}, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeStore) UpdateBrokerToken(ctx context.Context, t models.BrokerToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpdateBrokerToken"); err != nil {
		return err
	}
	if _, ok := f.brokerTokens[t.SessionID]; !ok {
		return store.ErrNotFound
	}
	f.brokerTokens[t.SessionID] = t
	return nil
}

func (f *fakeStore) DeleteBrokerToken(ctx context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteBrokerToken"); err != nil {
		return err
	}
	delete(f.brokerTokens, sessionID)
	return nil
}

func (f *fakeStore) CreateLoginFlow(ctx context.Context, flow models.LoginFlow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateLoginFlow"); err != nil {
		return err
	}
	f.loginFlows[flow.State] = flow
	return nil
}

func (f *fakeStore) TakeLoginFlow(ctx context.Context, state string) (models.LoginFlow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("TakeLoginFlow"); err != nil {
		return models.LoginFlow{}, err
	}
	flow, ok := f.loginFlows[state]
	if !ok {
		return models.LoginFlow{}, store.ErrNotFound
	}
	delete(f.loginFlows, state)
	return flow, nil
}

// Keyed like the real store: one card per event per channel.
func activeEventKey(key, teamID, channelID string) string {
	return key + "\x00" + teamID + "\x00" + channelID
}

func (f *fakeStore) Ping(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failing("Ping")
}

// WithTx runs fn against a copy and keeps the copy only when fn succeeds, which
// is the behaviour the import depends on: a bundle rejected half way through
// leaves the configuration as it was.
func (f *fakeStore) WithTx(ctx context.Context, fn func(context.Context, store.Store) error) error {
	// The lock is dropped before fn runs: fn reaches back into the store
	// through the snapshot, and a mutex that is not reentrant would deadlock.
	f.mu.Lock()
	if err := f.failing("WithTx"); err != nil {
		f.mu.Unlock()
		return err
	}
	snapshot := f.snapshot()
	f.mu.Unlock()

	if err := fn(ctx, snapshot); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.adopt(snapshot)
	return nil
}

// WithSerializableTx holds the lock for fn's whole duration instead of
// dropping it the way WithTx does, which is what actually serialises two
// concurrent callers rather than merely cloning and hoping: a redemption that
// checks GetRecipientBySubject and then creates or updates depends on nothing
// else running between the two. It still clones and writes back only on
// success, so a failure inside fn rolls back everything fn did, the same
// atomicity WithSerializableTx buys against the real backends.
func (f *fakeStore) WithSerializableTx(ctx context.Context, fn func(context.Context, store.Store) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("WithSerializableTx"); err != nil {
		return err
	}

	snapshot := f.snapshot()

	if err := fn(ctx, snapshot); err != nil {
		return err
	}

	f.adopt(snapshot)
	return nil
}

func (f *fakeStore) ListGrants(ctx context.Context) ([]models.Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListGrants"); err != nil {
		return nil, err
	}
	out := make([]models.Grant, 0, len(f.grants))
	for _, grant := range f.grants {
		out = append(out, grant)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeStore) CreateGrant(ctx context.Context, g models.Grant) (models.Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateGrant"); err != nil {
		return models.Grant{}, err
	}
	if g.ID == "" {
		// Unique, like the real store's uuid: one id for every grant, or a
		// second channel would quietly replace the first.
		g.ID = fmt.Sprintf("generated-%d", len(f.grants)+1)
	}
	f.grants[g.ID] = g
	return g, nil
}

func (f *fakeStore) DeleteGrantsForRole(ctx context.Context, role string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteGrantsForRole"); err != nil {
		return err
	}
	for id, grant := range f.grants {
		if grant.Role == role {
			delete(f.grants, id)
		}
	}
	return nil
}

func (f *fakeStore) DeleteGrant(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteGrant"); err != nil {
		return err
	}
	delete(f.grants, id)
	return nil
}

// The claim methods run under the one mutex, which makes them atomic in the
// same way the real store's single statements are. A fake that claimed in two
// steps would pass tests the real thing would fail.
func (f *fakeStore) ClaimActiveEvent(ctx context.Context, claim models.EventClaim) (models.ActiveEvent, store.ClaimOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ClaimActiveEvent"); err != nil {
		return models.ActiveEvent{}, store.ClaimHeld, err
	}

	key := activeEventKey(claim.Key, claim.TeamID, claim.ChannelID)
	existing, found := f.activeEvents[key]
	switch {
	case found && existing.Posted():
		return existing, store.ClaimPosted, nil
	case found && existing.ClaimedAt.After(claim.StaleBefore):
		return existing, store.ClaimHeld, nil
	}

	card := models.ActiveEvent{
		Key:        claim.Key,
		State:      claim.State,
		TeamID:     claim.TeamID,
		ChannelID:  claim.ChannelID,
		ClaimOwner: claim.Owner,
		ClaimedAt:  claim.At,
		LastUpdate: claim.At,
	}
	f.activeEvents[key] = card
	if found {
		return card, store.ClaimRecovered, nil
	}
	return card, store.ClaimAcquired, nil
}

func (f *fakeStore) CompleteActiveEventClaim(ctx context.Context, claim models.EventClaim, messageID, conversationID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CompleteActiveEventClaim"); err != nil {
		return err
	}

	key := activeEventKey(claim.Key, claim.TeamID, claim.ChannelID)
	existing, found := f.activeEvents[key]
	if !found || existing.Posted() || existing.ClaimOwner != claim.Owner {
		return store.ErrClaimLost
	}
	existing.MessageID = messageID
	existing.ConversationID = conversationID
	existing.State = claim.State
	existing.PostedAt = at
	existing.LastUpdate = at
	f.activeEvents[key] = existing
	return nil
}

func (f *fakeStore) ReleaseActiveEventClaim(ctx context.Context, claim models.EventClaim) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ReleaseActiveEventClaim"); err != nil {
		return err
	}

	key := activeEventKey(claim.Key, claim.TeamID, claim.ChannelID)
	if existing, found := f.activeEvents[key]; found && !existing.Posted() && existing.ClaimOwner == claim.Owner {
		delete(f.activeEvents, key)
	}
	return nil
}

func (f *fakeStore) TouchActiveEvent(ctx context.Context, card models.ActiveEvent, state models.EventState, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("TouchActiveEvent"); err != nil {
		return err
	}

	key := activeEventKey(card.Key, card.TeamID, card.ChannelID)
	if existing, found := f.activeEvents[key]; found && existing.MessageID == card.MessageID {
		existing.State = state
		existing.LastUpdate = at
		f.activeEvents[key] = existing
	}
	return nil
}

func (f *fakeStore) ListActiveEvents(ctx context.Context, key string) ([]models.ActiveEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListActiveEvents"); err != nil {
		return nil, err
	}
	var out []models.ActiveEvent
	for _, a := range f.activeEvents {
		if a.Key == key {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TeamID == out[j].TeamID {
			return out[i].ChannelID < out[j].ChannelID
		}
		return out[i].TeamID < out[j].TeamID
	})
	return out, nil
}

func (f *fakeStore) CountActiveEvents(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CountActiveEvents"); err != nil {
		return 0, err
	}
	var cards int64
	for _, a := range f.activeEvents {
		if a.Posted() {
			cards++
		}
	}
	return cards, nil
}

func (f *fakeStore) GetActiveEvent(ctx context.Context, key, teamID, channelID string) (models.ActiveEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetActiveEvent"); err != nil {
		return models.ActiveEvent{}, err
	}
	a, ok := f.activeEvents[activeEventKey(key, teamID, channelID)]
	if !ok {
		return models.ActiveEvent{}, store.ErrNotFound
	}
	return a, nil
}

// Keyed like the real store: one message per event per person.
func activeChatKey(key, recipientID string) string {
	return key + "\x00" + recipientID
}

// The chat half of the claim protocol, mirroring the channel methods above --
// including running under the one mutex, so a fake cannot pass a race the real
// store would lose.
func (f *fakeStore) ClaimActiveEventRecipient(ctx context.Context, claim models.RecipientClaim) (models.ActiveEventRecipient, store.ClaimOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ClaimActiveEventRecipient"); err != nil {
		return models.ActiveEventRecipient{}, store.ClaimHeld, err
	}

	key := activeChatKey(claim.Key, claim.RecipientID)
	existing, found := f.activeChats[key]
	switch {
	case found && existing.Posted():
		return existing, store.ClaimPosted, nil
	case found && existing.ClaimedAt.After(claim.StaleBefore):
		return existing, store.ClaimHeld, nil
	}

	card := models.ActiveEventRecipient{
		Key:         claim.Key,
		State:       claim.State,
		RecipientID: claim.RecipientID,
		ClaimOwner:  claim.Owner,
		ClaimedAt:   claim.At,
		LastUpdate:  claim.At,
	}
	f.activeChats[key] = card
	if found {
		return card, store.ClaimRecovered, nil
	}
	return card, store.ClaimAcquired, nil
}

func (f *fakeStore) CompleteActiveEventRecipientClaim(ctx context.Context, claim models.RecipientClaim, messageID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CompleteActiveEventRecipientClaim"); err != nil {
		return err
	}

	key := activeChatKey(claim.Key, claim.RecipientID)
	existing, found := f.activeChats[key]
	if !found || existing.Posted() || existing.ClaimOwner != claim.Owner {
		return store.ErrClaimLost
	}
	existing.MessageID = messageID
	existing.State = claim.State
	existing.PostedAt = at
	existing.LastUpdate = at
	f.activeChats[key] = existing
	return nil
}

func (f *fakeStore) ReleaseActiveEventRecipientClaim(ctx context.Context, claim models.RecipientClaim) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ReleaseActiveEventRecipientClaim"); err != nil {
		return err
	}

	key := activeChatKey(claim.Key, claim.RecipientID)
	if existing, found := f.activeChats[key]; found && !existing.Posted() && existing.ClaimOwner == claim.Owner {
		delete(f.activeChats, key)
	}
	return nil
}

func (f *fakeStore) TouchActiveEventRecipient(ctx context.Context, card models.ActiveEventRecipient, state models.EventState, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("TouchActiveEventRecipient"); err != nil {
		return err
	}

	key := activeChatKey(card.Key, card.RecipientID)
	if existing, found := f.activeChats[key]; found && existing.MessageID == card.MessageID {
		existing.State = state
		existing.LastUpdate = at
		f.activeChats[key] = existing
	}
	return nil
}

func (f *fakeStore) ListActiveEventRecipients(ctx context.Context, key string) ([]models.ActiveEventRecipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListActiveEventRecipients"); err != nil {
		return nil, err
	}
	var out []models.ActiveEventRecipient
	for _, a := range f.activeChats {
		if a.Key == key {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecipientID < out[j].RecipientID })
	return out, nil
}

func (f *fakeStore) DeleteActiveEventRecipientCard(ctx context.Context, eventKey, recipientID, messageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteActiveEventRecipientCard"); err != nil {
		return err
	}
	key := activeChatKey(eventKey, recipientID)
	if existing, found := f.activeChats[key]; found && existing.MessageID == messageID {
		delete(f.activeChats, key)
	}
	return nil
}

func (f *fakeStore) DeleteActiveEventRecipientsFor(ctx context.Context, recipientID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteActiveEventRecipientsFor"); err != nil {
		return err
	}
	f.deleteActiveEventRecipientsForLocked(recipientID)
	return nil
}

// deleteActiveEventRecipientsForLocked is the unlocked body both
// DeleteActiveEventRecipientsFor and DeleteRecipient's cascade share -- f.mu
// is not reentrant, so DeleteRecipient cannot simply call the locking method.
func (f *fakeStore) deleteActiveEventRecipientsForLocked(recipientID string) {
	for key, card := range f.activeChats {
		if card.RecipientID == recipientID {
			delete(f.activeChats, key)
		}
	}
}

func (f *fakeStore) DeleteActiveEventCard(ctx context.Context, eventKey, teamID, channelID, messageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteActiveEventCard"); err != nil {
		return err
	}
	key := activeEventKey(eventKey, teamID, channelID)
	if existing, found := f.activeEvents[key]; found && existing.MessageID == messageID {
		delete(f.activeEvents, key)
	}
	return nil
}

type postCall struct {
	teamID    string
	channelID string
	msg       graph.Message
}

type updateCall struct {
	postCall
	messageID string
}

type fakeMessenger struct {
	mu sync.Mutex

	// postGate, when set, holds every PostMessage until it is closed, and
	// posting reports each arrival. Together they let a test schedule the
	// interleaving it wants instead of hoping the scheduler produces it.
	postGate chan struct{}
	posting  chan struct{}
	// messageIDs hands out a different id per post, which is how a test tells
	// two cards apart when it wanted one.
	messageIDs int

	messageID string
	postErr   error
	// postErrFor limits postErr to one channel, which is how a partial fan-out
	// failure is staged.
	postErrFor string
	updateErr  error

	teams        []graph.Team
	channels     map[string][]graph.Channel
	directoryErr error
	teamCalls    int
	channelCalls int
	// installed answers HasInstalledApp; installErr fails it, as a missing
	// Graph permission does.
	installed    map[string]bool
	installErr   error
	installCalls int

	posts   []postCall
	updates []updateCall
}

func (f *fakeMessenger) PostMessage(teamID, channelID string, msg graph.Message) (string, error) {
	f.mu.Lock()
	f.posts = append(f.posts, postCall{teamID: teamID, channelID: channelID, msg: msg})
	f.messageIDs++
	id, postErr, gate, posting := f.messageID, f.postErr, f.postGate, f.posting
	if f.postErr != nil && f.postErrFor != "" && f.postErrFor != channelID {
		postErr = nil
	}
	if id == "" {
		id = fmt.Sprintf("message-%d", f.messageIDs)
	}
	f.mu.Unlock()

	// Outside the lock: the point of the gate is to hold this caller inside
	// the Graph call while other callers reach the store.
	if posting != nil {
		posting <- struct{}{}
	}
	if gate != nil {
		<-gate
	}

	if postErr != nil {
		return "", postErr
	}
	return id, nil
}

func (f *fakeMessenger) ListTeams() ([]graph.Team, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.teamCalls++
	if f.directoryErr != nil {
		return nil, f.directoryErr
	}
	return f.teams, nil
}

func (f *fakeMessenger) HasInstalledApp(teamID, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installCalls++
	if f.installErr != nil {
		return false, f.installErr
	}
	return f.installed[teamID], nil
}

func (f *fakeMessenger) ListChannels(teamID string) ([]graph.Channel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channelCalls++
	if f.directoryErr != nil {
		return nil, f.directoryErr
	}
	return f.channels[teamID], nil
}

func (f *fakeMessenger) UpdateMessage(teamID, channelID, messageID string, msg graph.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, updateCall{
		postCall:  postCall{teamID: teamID, channelID: channelID, msg: msg},
		messageID: messageID,
	})
	return f.updateErr
}

// PostToChannel and UpdateInChannel make the fake the channel transport as
// well, so a test reads posts and updates from one place.
func (f *fakeMessenger) PostToChannel(_ context.Context, teamID, channelID string, msg graph.Message) (channelCard, error) {
	id, err := f.PostMessage(teamID, channelID, msg)
	if err != nil {
		return channelCard{}, err
	}
	return channelCard{ConversationID: "conversation-" + id, MessageID: id}, nil
}

func (f *fakeMessenger) UpdateInChannel(_ context.Context, teamID, channelID string, card channelCard, msg graph.Message) error {
	return f.UpdateMessage(teamID, channelID, card.MessageID, msg)
}

// cardOf is the one card a template renders, for a test that wants to compare
// it as a string. graph.Message carries a slice because a Teams V2 payload may
// bring several.
func cardOf(msg graph.Message) string {
	if len(msg.Cards) == 0 {
		return ""
	}
	return string(msg.Cards[0])
}

// RecordEventSamples merges by kind, key and value, as the upsert does.
func (f *fakeStore) RecordEventSamples(ctx context.Context, samples []models.EventSample) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("RecordEventSamples"); err != nil {
		return err
	}
next:
	for _, sample := range samples {
		for i, existing := range f.samples {
			if existing.Kind == sample.Kind && existing.Key == sample.Key && existing.Value == sample.Value {
				f.samples[i].SeenCount += sample.SeenCount
				f.samples[i].LastSeen = sample.LastSeen
				continue next
			}
		}
		f.samples = append(f.samples, sample)
	}
	return nil
}

func (f *fakeStore) ListEventSamples(ctx context.Context, limit int) ([]models.EventSample, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListEventSamples"); err != nil {
		return nil, err
	}
	out := append([]models.EventSample(nil), f.samples...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) PruneEventSamples(ctx context.Context, cutoff time.Time, keepPerKey int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return 0, f.failing("PruneEventSamples")
}

// The directory, kept only as far as the bot handlers use it.

func (f *fakeStore) UpsertDirectoryUser(_ context.Context, u models.DirectoryUser) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpsertDirectoryUser"); err != nil {
		return err
	}
	existing, ok := f.directory[u.AADObjectID]
	if ok {
		u.ConversationID, u.ServiceURL, u.InstallState = existing.ConversationID, existing.ServiceURL, existing.InstallState
		u.InstalledAt, u.NextAttemptAt, u.CreatedAt = existing.InstalledAt, existing.NextAttemptAt, existing.CreatedAt
	} else {
		u.InstallState, u.CreatedAt = models.InstallUnknown, time.Now()
	}
	f.directory[u.AADObjectID] = u
	return nil
}

func (f *fakeStore) GetDirectoryUser(_ context.Context, aadObjectID string) (models.DirectoryUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetDirectoryUser"); err != nil {
		return models.DirectoryUser{}, err
	}
	u, ok := f.directory[aadObjectID]
	if !ok {
		return models.DirectoryUser{}, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeStore) FindDirectoryUser(_ context.Context, address string) (models.DirectoryUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.directory {
		if strings.EqualFold(u.UserPrincipalName, address) || strings.EqualFold(u.Mail, address) {
			return u, nil
		}
	}
	return models.DirectoryUser{}, store.ErrNotFound
}

func (f *fakeStore) GetDirectoryUserByConversation(_ context.Context, conversationID string) (models.DirectoryUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.directory {
		if conversationID != "" && u.ConversationID == conversationID {
			return u, nil
		}
	}
	return models.DirectoryUser{}, store.ErrNotFound
}

func (f *fakeStore) SetDirectoryUserInstalled(_ context.Context, aadObjectID, conversationID, serviceURL string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("SetDirectoryUserInstalled"); err != nil {
		return err
	}
	u, ok := f.directory[aadObjectID]
	if !ok {
		return store.ErrNotFound
	}
	u.ConversationID, u.ServiceURL, u.InstallState, u.InstalledAt = conversationID, serviceURL, models.InstallInstalled, at
	f.directory[aadObjectID] = u
	return nil
}

func (f *fakeStore) MarkDirectoryUserRemoved(_ context.Context, aadObjectID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("MarkDirectoryUserRemoved"); err != nil {
		return err
	}
	u, ok := f.directory[aadObjectID]
	if !ok {
		return store.ErrNotFound
	}
	u.InstallState, u.NextAttemptAt = models.InstallRemoved, at
	f.directory[aadObjectID] = u
	return nil
}
func (f *fakeStore) RecordDirectoryInstallFailure(ctx context.Context, aadObjectID string, state models.InstallState, lastError string, next, at time.Time) error {
	return store.ErrNotFound
}

func (f *fakeStore) ListDirectoryUsersDue(ctx context.Context, now, reverifyBefore time.Time, ignoreBackoff bool, limit int) ([]models.DirectoryUser, error) {
	return nil, store.ErrNotFound
}

func (f *fakeStore) MarkDirectoryUsersDeparted(ctx context.Context, seenBefore, at time.Time) (int64, error) {
	return 0, store.ErrNotFound
}

func (f *fakeStore) PurgeDepartedDirectoryUsers(ctx context.Context, before time.Time) (int64, error) {
	return 0, store.ErrNotFound
}

func (f *fakeStore) GetDirectoryRun(ctx context.Context, id string) (models.DirectoryRun, error) {
	return models.DirectoryRun{}, store.ErrNotFound
}

func (f *fakeStore) ClaimDirectoryRun(ctx context.Context, id, owner string, now, staleBefore time.Time) (bool, error) {
	return false, store.ErrNotFound
}

func (f *fakeStore) HeartbeatDirectoryRun(ctx context.Context, id, owner string, counts models.RunCounts, now time.Time) (bool, error) {
	return false, store.ErrNotFound
}

func (f *fakeStore) FinishDirectoryRun(ctx context.Context, id, owner string, state models.RunState, counts models.RunCounts, lastError string, now time.Time) (bool, error) {
	return false, store.ErrNotFound
}

func (f *fakeStore) PruneDirectoryRuns(ctx context.Context, before time.Time) (int64, error) {
	return 0, store.ErrNotFound
}

func (f *fakeStore) CountDirectoryUsersByState(context.Context) (map[models.InstallState]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CountDirectoryUsersByState"); err != nil {
		return nil, err
	}
	out := map[models.InstallState]int64{}
	for _, u := range f.directory {
		out[u.InstallState]++
	}
	return out, nil
}

func (f *fakeStore) ListDirectoryUserProblems(_ context.Context, limit int) ([]models.DirectoryUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListDirectoryUserProblems"); err != nil {
		return nil, err
	}
	var out []models.DirectoryUser
	for _, u := range f.directory {
		if (u.InstallState == models.InstallFailed || u.InstallState == models.InstallIneligible) && len(out) < limit {
			out = append(out, u)
		}
	}
	return out, nil
}

func (f *fakeStore) RequestDirectoryRun(_ context.Context, r models.DirectoryRun) (models.DirectoryRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("RequestDirectoryRun"); err != nil {
		return models.DirectoryRun{}, err
	}
	for _, existing := range f.runs {
		if existing.State == models.RunRequested || existing.State == models.RunRunning {
			return models.DirectoryRun{}, store.ErrConflict
		}
	}
	r.ID = fmt.Sprintf("run-%d", len(f.runs)+1)
	r.State = models.RunRequested
	if r.RequestedAt.IsZero() {
		r.RequestedAt = time.Now()
	}
	f.runs = append(f.runs, r)
	return r, nil
}

func (f *fakeStore) LatestDirectoryRun(context.Context) (models.DirectoryRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("LatestDirectoryRun"); err != nil {
		return models.DirectoryRun{}, err
	}
	if len(f.runs) == 0 {
		return models.DirectoryRun{}, store.ErrNotFound
	}
	return f.runs[len(f.runs)-1], nil
}

func (f *fakeStore) MarkDirectoryUserBlocked(_ context.Context, aadObjectID string, at time.Time, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("MarkDirectoryUserBlocked"); err != nil {
		return err
	}
	if u, ok := f.directory[aadObjectID]; ok {
		u.BlockedAt, u.BlockedReason = at, reason
		f.directory[aadObjectID] = u
	}
	return nil
}

func (f *fakeStore) ClearDirectoryUserBlocked(_ context.Context, aadObjectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.directory[aadObjectID]; ok {
		u.BlockedAt, u.BlockedReason = time.Time{}, ""
		f.directory[aadObjectID] = u
	}
	return nil
}

func (f *fakeStore) UpdateRecipientChatsForObjectID(_ context.Context, aadObjectID, conversationID, serviceURL string, at time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpdateRecipientChatsForObjectID"); err != nil {
		return 0, err
	}
	var n int64
	for id, r := range f.recipients {
		if aadObjectID != "" && r.AADObjectID == aadObjectID && (r.ConversationID != conversationID || r.ServiceURL != serviceURL) {
			r.ConversationID, r.ServiceURL, r.UpdatedAt = conversationID, serviceURL, at
			f.recipients[id] = r
			n++
		}
	}
	return n, nil
}

// The audit trail, newest first like the real one.

func (f *fakeStore) InsertAuditEvent(_ context.Context, e models.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("InsertAuditEvent"); err != nil {
		return err
	}
	f.auditEvents = append(f.auditEvents, e)
	return nil
}

func (f *fakeStore) ListAuditEvents(_ context.Context, filter models.AuditFilter) ([]models.AuditEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListAuditEvents"); err != nil {
		return nil, err
	}
	var out []models.AuditEvent
	for i := len(f.auditEvents) - 1; i >= 0; i-- {
		e := f.auditEvents[i]
		switch {
		case filter.Actor != "" && e.Actor.Subject != filter.Actor,
			filter.ResourceType != "" && e.ResourceType != filter.ResourceType,
			filter.ResourceID != "" && e.ResourceID != filter.ResourceID,
			filter.Action != "" && e.Action != filter.Action:
			continue
		}
		out = append(out, e)
	}
	if filter.CursorID != "" {
		if i := slices.IndexFunc(out, func(e models.AuditEvent) bool { return e.ID == filter.CursorID }); i >= 0 {
			out = out[i+1:]
		}
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (f *fakeStore) PruneAuditEvents(context.Context, time.Time, int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return 0, f.failing("PruneAuditEvents")
}

// Users, as far as the sign-in and the users page need them.

func (f *fakeStore) RecordSignIn(_ context.Context, u models.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("RecordSignIn"); err != nil {
		return err
	}
	if existing, ok := f.users[u.Subject]; ok {
		u.FirstSeen, u.DisabledAt, u.DisabledBy = existing.FirstSeen, existing.DisabledAt, existing.DisabledBy
		if u.ObjectID == "" {
			u.ObjectID = existing.ObjectID
		}
	} else {
		u.FirstSeen = u.LastSeen
	}
	f.users[u.Subject] = u
	return nil
}

func (f *fakeStore) GetUser(_ context.Context, subject string) (models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetUser"); err != nil {
		return models.User{}, err
	}
	u, ok := f.users[subject]
	if !ok {
		return models.User{}, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeStore) ListUsers(_ context.Context, search string, limit int) ([]models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListUsers"); err != nil {
		return nil, err
	}
	search = strings.ToLower(search)
	var out []models.User
	for _, u := range f.users {
		if search == "" || strings.Contains(strings.ToLower(u.Subject+" "+u.Name+" "+u.Email), search) {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) DisableUser(_ context.Context, subject, by string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DisableUser"); err != nil {
		return err
	}
	u, ok := f.users[subject]
	if !ok {
		return store.ErrNotFound
	}
	u.DisabledAt, u.DisabledBy = time.Now().UTC(), by
	f.users[subject] = u
	for id, s := range f.sessions {
		if s.Subject == subject {
			delete(f.sessions, id)
			delete(f.brokerTokens, id)
		}
	}
	return nil
}

func (f *fakeStore) EnableUser(_ context.Context, subject string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("EnableUser"); err != nil {
		return err
	}
	u, ok := f.users[subject]
	if !ok {
		return store.ErrNotFound
	}
	u.DisabledAt, u.DisabledBy = time.Time{}, ""
	f.users[subject] = u
	return nil
}

func (f *fakeStore) AuthzGeneration(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authzGen, f.failing("AuthzGeneration")
}

func (f *fakeStore) BumpAuthzGeneration(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("BumpAuthzGeneration"); err != nil {
		return err
	}
	f.authzGen++
	return nil
}

// Groups, with the store's cycle rule and generation bumps.

func (f *fakeStore) ListGroups(context.Context) ([]models.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListGroups"); err != nil {
		return nil, err
	}
	out := make([]models.Group, 0, len(f.groups))
	for _, g := range f.groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeStore) GetGroup(_ context.Context, id string) (models.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetGroup"); err != nil {
		return models.Group{}, err
	}
	g, ok := f.groups[id]
	if !ok {
		return models.Group{}, store.ErrNotFound
	}
	return g, nil
}

func (f *fakeStore) CreateGroup(_ context.Context, g models.Group) (models.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateGroup"); err != nil {
		return models.Group{}, err
	}
	for _, existing := range f.groups {
		if existing.Name == g.Name {
			return models.Group{}, store.ErrConflict
		}
	}
	if g.ID == "" {
		g.ID = fmt.Sprintf("group-%d", len(f.groups)+1)
	}
	f.groups[g.ID] = g
	f.authzGen++
	return g, nil
}

func (f *fakeStore) UpdateGroup(_ context.Context, g models.Group) (models.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpdateGroup"); err != nil {
		return models.Group{}, err
	}
	existing, ok := f.groups[g.ID]
	if !ok {
		return models.Group{}, store.ErrNotFound
	}
	for id, other := range f.groups {
		if id != g.ID && other.Name == g.Name {
			return models.Group{}, store.ErrConflict
		}
	}
	existing.Name, existing.Description = g.Name, g.Description
	f.groups[g.ID] = existing
	f.authzGen++
	return existing, nil
}

func (f *fakeStore) DeleteGroup(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteGroup"); err != nil {
		return err
	}
	if _, ok := f.groups[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.groups, id)
	f.members = slices.DeleteFunc(f.members, func(m models.GroupMember) bool {
		return m.GroupID == id || (m.Type == models.MemberGroup && m.ID == id)
	})
	for key, p := range f.permissions {
		if (p.PrincipalType == models.PrincipalGroup && p.PrincipalID == id) || (p.ResourceType == "Group" && p.ResourceID == id) {
			delete(f.permissions, key)
		}
	}
	f.authzGen++
	return nil
}

func (f *fakeStore) ListGroupMembers(_ context.Context, groupID string) ([]models.GroupMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListGroupMembers"); err != nil {
		return nil, err
	}
	var out []models.GroupMember
	for _, m := range f.members {
		if m.GroupID == groupID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeStore) ListAllGroupMembers(context.Context) ([]models.GroupMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListAllGroupMembers"); err != nil {
		return nil, err
	}
	return slices.Clone(f.members), nil
}

func (f *fakeStore) AddGroupMember(_ context.Context, m models.GroupMember) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("AddGroupMember"); err != nil {
		return err
	}
	if _, ok := f.groups[m.GroupID]; !ok {
		return store.ErrNotFound
	}
	if m.Type == models.MemberGroup {
		if _, ok := f.groups[m.ID]; !ok {
			return store.ErrNotFound
		}
		// The member must not already contain the group, however deep.
		stack, seen := []string{m.ID}, map[string]bool{}
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if current == m.GroupID {
				return store.ErrGroupCycle
			}
			if seen[current] {
				continue
			}
			seen[current] = true
			for _, existing := range f.members {
				if existing.GroupID == current && existing.Type == models.MemberGroup {
					stack = append(stack, existing.ID)
				}
			}
		}
	}
	for _, existing := range f.members {
		if existing.GroupID == m.GroupID && existing.Type == m.Type && existing.ID == m.ID {
			return nil
		}
	}
	f.members = append(f.members, m)
	f.authzGen++
	return nil
}

func (f *fakeStore) RemoveGroupMember(_ context.Context, m models.GroupMember) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("RemoveGroupMember"); err != nil {
		return err
	}
	before := len(f.members)
	f.members = slices.DeleteFunc(f.members, func(existing models.GroupMember) bool {
		return existing.GroupID == m.GroupID && existing.Type == m.Type && existing.ID == m.ID
	})
	if len(f.members) == before {
		return store.ErrNotFound
	}
	f.authzGen++
	return nil
}

// snapshot is the store a transaction works on; adopt commits it. The caller holds f.mu.
func (f *fakeStore) snapshot() *fakeStore {
	return &fakeStore{
		templates:    maps.Clone(f.templates),
		destinations: maps.Clone(f.destinations),
		recipients:   maps.Clone(f.recipients),
		webhooks:     maps.Clone(f.webhooks),
		accessTokens: maps.Clone(f.accessTokens),
		routes:       maps.Clone(f.routes),
		grants:       maps.Clone(f.grants),
		activeEvents: maps.Clone(f.activeEvents),
		activeChats:  maps.Clone(f.activeChats),
		sessions:     maps.Clone(f.sessions),
		brokerTokens: maps.Clone(f.brokerTokens),
		loginFlows:   maps.Clone(f.loginFlows),
		linkFlows:    maps.Clone(f.linkFlows),
		failOn:       maps.Clone(f.failOn),
		auditEvents:  slices.Clone(f.auditEvents),
		users:        maps.Clone(f.users),
		groups:       maps.Clone(f.groups),
		members:      slices.Clone(f.members),
		permissions:  maps.Clone(f.permissions),
		authzGen:     f.authzGen,

		globalDefaultTemplate: f.globalDefaultTemplate,
		sourceDefaults:        maps.Clone(f.sourceDefaults),
		seeded:                f.seeded,
		subjectLookupDelay:    f.subjectLookupDelay,
	}
}

func (f *fakeStore) adopt(snapshot *fakeStore) {
	f.templates, f.destinations, f.routes = snapshot.templates, snapshot.destinations, snapshot.routes
	f.recipients, f.grants, f.activeEvents = snapshot.recipients, snapshot.grants, snapshot.activeEvents
	f.webhooks, f.accessTokens = snapshot.webhooks, snapshot.accessTokens
	f.activeChats, f.brokerTokens = snapshot.activeChats, snapshot.brokerTokens
	f.sessions, f.loginFlows, f.linkFlows = snapshot.sessions, snapshot.loginFlows, snapshot.linkFlows
	f.globalDefaultTemplate = snapshot.globalDefaultTemplate
	f.sourceDefaults, f.seeded = snapshot.sourceDefaults, snapshot.seeded
	f.auditEvents, f.users, f.groups, f.members = snapshot.auditEvents, snapshot.users, snapshot.groups, snapshot.members
	f.permissions, f.authzGen = snapshot.permissions, snapshot.authzGen
}

// Permissions, keyed by id.

func (f *fakeStore) ListPermissions(context.Context) ([]models.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListPermissions"); err != nil {
		return nil, err
	}
	return f.sortedPermissions(func(models.Permission) bool { return true }), nil
}

func (f *fakeStore) sortedPermissions(keep func(models.Permission) bool) []models.Permission {
	var out []models.Permission
	for _, p := range f.permissions {
		if keep(p) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (f *fakeStore) ListPermissionsFor(_ context.Context, typ, id string) ([]models.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListPermissionsFor"); err != nil {
		return nil, err
	}
	return f.sortedPermissions(func(p models.Permission) bool { return p.ResourceType == typ && p.ResourceID == id }), nil
}

func (f *fakeStore) GetPermission(_ context.Context, id string) (models.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.permissions[id]
	if !ok {
		return models.Permission{}, store.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) PutPermission(_ context.Context, p models.Permission) (models.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("PutPermission"); err != nil {
		return models.Permission{}, err
	}
	for id, existing := range f.permissions {
		if existing.PrincipalType == p.PrincipalType && existing.PrincipalID == p.PrincipalID &&
			existing.ResourceType == p.ResourceType && existing.ResourceID == p.ResourceID {
			if len(p.Actions) == 0 {
				delete(f.permissions, id)
				f.authzGen++
				return models.Permission{}, nil
			}
			existing.Actions = p.Actions
			f.permissions[id] = existing
			f.authzGen++
			return existing, nil
		}
	}
	if len(p.Actions) == 0 {
		return models.Permission{}, nil
	}
	if p.ID == "" {
		p.ID = fmt.Sprintf("perm-%d", len(f.permissions)+1)
		for f.permissions[p.ID].ID != "" {
			p.ID += "x"
		}
	}
	f.permissions[p.ID] = p
	f.authzGen++
	return p, nil
}

func (f *fakeStore) DeletePermission(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeletePermission"); err != nil {
		return err
	}
	if _, ok := f.permissions[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.permissions, id)
	f.authzGen++
	return nil
}

func (f *fakeStore) DeletePermissionsFor(_ context.Context, typ, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeletePermissionsFor"); err != nil {
		return err
	}
	for key, p := range f.permissions {
		if p.ResourceType == typ && p.ResourceID == id {
			delete(f.permissions, key)
		}
	}
	f.authzGen++
	return nil
}

func (f *fakeStore) ListAuditEventsAfter(context.Context, models.AuditCursor, time.Time, int) ([]models.AuditEvent, error) {
	return nil, nil
}

func (f *fakeStore) AuditCursor(context.Context, string) (models.AuditCursor, bool, error) {
	return models.AuditCursor{}, false, nil
}

func (f *fakeStore) AdvanceAuditCursor(context.Context, string, models.AuditCursor, models.AuditCursor) (bool, error) {
	return true, nil
}

// Broadcasts (ADR 0083), outside transactions as in the real store.

func (f *fakeStore) ListReachableDirectoryUsers(_ context.Context, after string, limit int) ([]models.DirectoryUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListReachableDirectoryUsers"); err != nil {
		return nil, err
	}
	var out []models.DirectoryUser
	for _, u := range f.directory {
		if u.Eligible && u.InstallState == models.InstallInstalled && u.ConversationID != "" && u.AADObjectID > after {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AADObjectID < out[j].AADObjectID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) CreateBroadcast(_ context.Context, b models.Broadcast) (models.Broadcast, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateBroadcast"); err != nil {
		return models.Broadcast{}, err
	}
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	if b.RequestedAt.IsZero() {
		b.RequestedAt = time.Now().UTC()
	}
	b.State = models.RunRequested
	f.broadcasts[b.ID] = b
	return b, nil
}

func (f *fakeStore) GetBroadcast(_ context.Context, id string) (models.Broadcast, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetBroadcast"); err != nil {
		return models.Broadcast{}, err
	}
	b, ok := f.broadcasts[id]
	if !ok {
		return models.Broadcast{}, store.ErrNotFound
	}
	return b, nil
}

func (f *fakeStore) ListBroadcasts(_ context.Context, requestedBy string, limit int) ([]models.Broadcast, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListBroadcasts"); err != nil {
		return nil, err
	}
	var out []models.Broadcast
	for _, b := range f.broadcasts {
		if requestedBy == "" || b.RequestedBy == requestedBy {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RequestedAt.After(out[j].RequestedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// broadcastDue is a broadcast that waits, or runs with a stale heartbeat.
func broadcastDue(b models.Broadcast, staleBefore time.Time) bool {
	return b.State == models.RunRequested || (b.State == models.RunRunning && b.HeartbeatAt.Before(staleBefore))
}

func (f *fakeStore) NextBroadcast(_ context.Context, staleBefore time.Time) (models.Broadcast, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("NextBroadcast"); err != nil {
		return models.Broadcast{}, err
	}
	var next *models.Broadcast
	for _, b := range f.broadcasts {
		if broadcastDue(b, staleBefore) && (next == nil || b.RequestedAt.Before(next.RequestedAt)) {
			next = &b
		}
	}
	if next == nil {
		return models.Broadcast{}, store.ErrNotFound
	}
	return *next, nil
}

func (f *fakeStore) ClaimBroadcast(_ context.Context, id, owner string, now, staleBefore time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ClaimBroadcast"); err != nil {
		return false, err
	}
	b, ok := f.broadcasts[id]
	if !ok || !broadcastDue(b, staleBefore) {
		return false, nil
	}
	b.State, b.Owner, b.HeartbeatAt = models.RunRunning, owner, now
	if b.StartedAt.IsZero() {
		b.StartedAt = now
	}
	f.broadcasts[id] = b
	return true, nil
}

func (f *fakeStore) HeartbeatBroadcast(_ context.Context, id, owner, cursor string, c models.BroadcastCounts, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("HeartbeatBroadcast"); err != nil {
		return false, err
	}
	b, ok := f.broadcasts[id]
	if !ok || b.Owner != owner || b.State != models.RunRunning {
		return false, nil
	}
	b.Cursor, b.BroadcastCounts, b.HeartbeatAt = cursor, c, now
	f.broadcasts[id] = b
	return true, nil
}

func (f *fakeStore) FinishBroadcast(_ context.Context, id, owner string, state models.RunState, cursor string, c models.BroadcastCounts, lastError string, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("FinishBroadcast"); err != nil {
		return false, err
	}
	b, ok := f.broadcasts[id]
	if !ok || b.Owner != owner || b.State != models.RunRunning {
		return false, nil
	}
	b.State, b.Cursor, b.BroadcastCounts, b.LastError, b.FinishedAt, b.HeartbeatAt = state, cursor, c, lastError, now, now
	f.broadcasts[id] = b
	return true, nil
}

func (f *fakeStore) PruneBroadcasts(_ context.Context, before time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for id, b := range f.broadcasts {
		if (b.State == models.RunDone || b.State == models.RunFailed) && b.FinishedAt.Before(before) {
			delete(f.broadcasts, id)
			n++
		}
	}
	return n, nil
}
