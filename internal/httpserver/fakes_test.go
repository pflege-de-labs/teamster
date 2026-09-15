package httpserver

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"sync"
	"time"

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
	routes       map[string]models.Route
	activeAlerts map[string]models.ActiveAlert
	grants       map[string]models.Grant
	sessions     map[string]models.Session
	loginFlows   map[string]models.LoginFlow
	linkFlows    map[string]models.LinkFlow

	failOn map[string]bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		templates:    map[string]models.Template{},
		destinations: map[string]models.Destination{},
		recipients:   map[string]models.Recipient{},
		routes:       map[string]models.Route{},
		activeAlerts: map[string]models.ActiveAlert{},
		sessions: map[string]models.Session{
			// Seeded so the request helpers can act as a signed-in operator; a
			// test that cares about being signed out builds its own request.
			testSessionID: {
				ID: testSessionID, Subject: "tester", Name: "tester", Source: "local",
				CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
			},
		},
		grants:     map[string]models.Grant{},
		loginFlows: map[string]models.LoginFlow{},
		linkFlows:  map[string]models.LinkFlow{},
		failOn:     map[string]bool{},
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
	f.destinations[d.ID] = d
	return d, nil
}

func (f *fakeStore) UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("UpdateDestination"); err != nil {
		return models.Destination{}, err
	}
	f.destinations[d.ID] = d
	return d, nil
}

func (f *fakeStore) DeleteDestination(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteDestination"); err != nil {
		return err
	}
	delete(f.destinations, id)
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
		r.ID = "generated"
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

func (f *fakeStore) DeleteRecipient(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteRecipient"); err != nil {
		return err
	}
	delete(f.recipients, id)
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

func (f *fakeStore) GetRecipientBySubject(ctx context.Context, subject string) (models.Recipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetRecipientBySubject"); err != nil {
		return models.Recipient{}, err
	}
	for _, r := range f.recipients {
		if r.Subject == subject {
			return r, nil
		}
	}
	return models.Recipient{}, store.ErrNotFound
}

func (f *fakeStore) CreateLinkFlow(ctx context.Context, flow models.LinkFlow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CreateLinkFlow"); err != nil {
		return err
	}
	f.linkFlows[flow.Code] = flow
	return nil
}

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
	return flow, nil
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

func (f *fakeStore) DeleteSession(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteSession"); err != nil {
		return err
	}
	delete(f.sessions, id)
	return nil
}

func (f *fakeStore) DeleteExpiredSessions(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failing("DeleteExpiredSessions")
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

// Keyed like the real store: one card per alert per channel.
func activeAlertKey(fingerprint, teamID, channelID string) string {
	return fingerprint + "\x00" + teamID + "\x00" + channelID
}

func (f *fakeStore) Ping(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failing("Ping")
}

// WithTx runs fn against a copy and keeps the copy only when fn succeeds, which
// is the behaviour the import depends on: a bundle rejected half way through
// leaves the configuration as it was.
// The fake serialises everything through one mutex, so the two transaction
// flavours are the same thing here; the distinction is a promise to a backend
// that has to work for it.
func (f *fakeStore) WithSerializableTx(ctx context.Context, fn func(context.Context, store.Store) error) error {
	return f.WithTx(ctx, fn)
}

func (f *fakeStore) WithTx(ctx context.Context, fn func(context.Context, store.Store) error) error {
	// The lock is dropped before fn runs: fn reaches back into the store
	// through the snapshot, and a mutex that is not reentrant would deadlock.
	f.mu.Lock()
	if err := f.failing("WithTx"); err != nil {
		f.mu.Unlock()
		return err
	}
	snapshot := &fakeStore{
		templates:    maps.Clone(f.templates),
		destinations: maps.Clone(f.destinations),
		routes:       maps.Clone(f.routes),
		grants:       maps.Clone(f.grants),
		activeAlerts: maps.Clone(f.activeAlerts),
		sessions:     maps.Clone(f.sessions),
		loginFlows:   maps.Clone(f.loginFlows),
		failOn:       maps.Clone(f.failOn),
	}
	f.mu.Unlock()

	if err := fn(ctx, snapshot); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.templates, f.destinations, f.routes = snapshot.templates, snapshot.destinations, snapshot.routes
	f.grants, f.activeAlerts = snapshot.grants, snapshot.activeAlerts
	f.sessions, f.loginFlows = snapshot.sessions, snapshot.loginFlows
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
func (f *fakeStore) ClaimActiveAlert(ctx context.Context, claim models.AlertClaim) (models.ActiveAlert, store.ClaimOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ClaimActiveAlert"); err != nil {
		return models.ActiveAlert{}, store.ClaimHeld, err
	}

	key := activeAlertKey(claim.Fingerprint, claim.TeamID, claim.ChannelID)
	existing, found := f.activeAlerts[key]
	switch {
	case found && existing.Posted():
		return existing, store.ClaimPosted, nil
	case found && existing.ClaimedAt.After(claim.StaleBefore):
		return existing, store.ClaimHeld, nil
	}

	card := models.ActiveAlert{
		Fingerprint: claim.Fingerprint,
		Status:      claim.Status,
		TeamID:      claim.TeamID,
		ChannelID:   claim.ChannelID,
		ClaimOwner:  claim.Owner,
		ClaimedAt:   claim.At,
		LastUpdate:  claim.At,
	}
	f.activeAlerts[key] = card
	if found {
		return card, store.ClaimRecovered, nil
	}
	return card, store.ClaimAcquired, nil
}

func (f *fakeStore) CompleteActiveAlertClaim(ctx context.Context, claim models.AlertClaim, messageID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CompleteActiveAlertClaim"); err != nil {
		return err
	}

	key := activeAlertKey(claim.Fingerprint, claim.TeamID, claim.ChannelID)
	existing, found := f.activeAlerts[key]
	if !found || existing.Posted() || existing.ClaimOwner != claim.Owner {
		return store.ErrClaimLost
	}
	existing.MessageID = messageID
	existing.Status = claim.Status
	existing.PostedAt = at
	existing.LastUpdate = at
	f.activeAlerts[key] = existing
	return nil
}

func (f *fakeStore) ReleaseActiveAlertClaim(ctx context.Context, claim models.AlertClaim) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ReleaseActiveAlertClaim"); err != nil {
		return err
	}

	key := activeAlertKey(claim.Fingerprint, claim.TeamID, claim.ChannelID)
	if existing, found := f.activeAlerts[key]; found && !existing.Posted() && existing.ClaimOwner == claim.Owner {
		delete(f.activeAlerts, key)
	}
	return nil
}

func (f *fakeStore) TouchActiveAlert(ctx context.Context, card models.ActiveAlert, status string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("TouchActiveAlert"); err != nil {
		return err
	}

	key := activeAlertKey(card.Fingerprint, card.TeamID, card.ChannelID)
	if existing, found := f.activeAlerts[key]; found && existing.MessageID == card.MessageID {
		existing.Status = status
		existing.LastUpdate = at
		f.activeAlerts[key] = existing
	}
	return nil
}

func (f *fakeStore) ListActiveAlerts(ctx context.Context, fingerprint string) ([]models.ActiveAlert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("ListActiveAlerts"); err != nil {
		return nil, err
	}
	var out []models.ActiveAlert
	for _, a := range f.activeAlerts {
		if a.Fingerprint == fingerprint {
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

func (f *fakeStore) CountActiveAlerts(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("CountActiveAlerts"); err != nil {
		return 0, err
	}
	var cards int64
	for _, a := range f.activeAlerts {
		if a.Posted() {
			cards++
		}
	}
	return cards, nil
}

func (f *fakeStore) GetActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) (models.ActiveAlert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("GetActiveAlert"); err != nil {
		return models.ActiveAlert{}, err
	}
	a, ok := f.activeAlerts[activeAlertKey(fingerprint, teamID, channelID)]
	if !ok {
		return models.ActiveAlert{}, store.ErrNotFound
	}
	return a, nil
}

func (f *fakeStore) DeleteActiveAlertCard(ctx context.Context, fingerprint, teamID, channelID, messageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failing("DeleteActiveAlertCard"); err != nil {
		return err
	}
	key := activeAlertKey(fingerprint, teamID, channelID)
	if existing, found := f.activeAlerts[key]; found && existing.MessageID == messageID {
		delete(f.activeAlerts, key)
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
