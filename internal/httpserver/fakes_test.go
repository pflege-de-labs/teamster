package httpserver

import (
	"errors"
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
	routes       map[string]models.Route
	activeAlerts map[string]models.ActiveAlert
	grants       map[string]models.Grant
	sessions     map[string]models.Session
	loginFlows   map[string]models.LoginFlow

	failOn map[string]bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		templates:    map[string]models.Template{},
		destinations: map[string]models.Destination{},
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
		failOn:     map[string]bool{},
	}
}

func (f *fakeStore) fail(methods ...string) *fakeStore {
	for _, method := range methods {
		f.failOn[method] = true
	}
	return f
}

func (f *fakeStore) failing(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn[method] {
		return errStore
	}
	return nil
}

func (f *fakeStore) Close() error { return f.failing("Close") }

func (f *fakeStore) ListTemplates() ([]models.Template, error) {
	if err := f.failing("ListTemplates"); err != nil {
		return nil, err
	}
	out := []models.Template{}
	for _, t := range f.templates {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeStore) CreateTemplate(t models.Template) (models.Template, error) {
	if err := f.failing("CreateTemplate"); err != nil {
		return models.Template{}, err
	}
	if t.ID == "" {
		t.ID = "generated"
	}
	f.templates[t.ID] = t
	return t, nil
}

func (f *fakeStore) UpdateTemplate(t models.Template) (models.Template, error) {
	if err := f.failing("UpdateTemplate"); err != nil {
		return models.Template{}, err
	}
	f.templates[t.ID] = t
	return t, nil
}

func (f *fakeStore) DeleteTemplate(id string) error {
	if err := f.failing("DeleteTemplate"); err != nil {
		return err
	}
	delete(f.templates, id)
	return nil
}

func (f *fakeStore) GetTemplate(id string) (models.Template, error) {
	if err := f.failing("GetTemplate"); err != nil {
		return models.Template{}, err
	}
	t, ok := f.templates[id]
	if !ok {
		return models.Template{}, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeStore) ListDestinations() ([]models.Destination, error) {
	if err := f.failing("ListDestinations"); err != nil {
		return nil, err
	}
	out := []models.Destination{}
	for _, d := range f.destinations {
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeStore) CreateDestination(d models.Destination) (models.Destination, error) {
	if err := f.failing("CreateDestination"); err != nil {
		return models.Destination{}, err
	}
	if d.ID == "" {
		d.ID = "generated"
	}
	f.destinations[d.ID] = d
	return d, nil
}

func (f *fakeStore) UpdateDestination(d models.Destination) (models.Destination, error) {
	if err := f.failing("UpdateDestination"); err != nil {
		return models.Destination{}, err
	}
	f.destinations[d.ID] = d
	return d, nil
}

func (f *fakeStore) DeleteDestination(id string) error {
	if err := f.failing("DeleteDestination"); err != nil {
		return err
	}
	delete(f.destinations, id)
	return nil
}

func (f *fakeStore) GetDestination(id string) (models.Destination, error) {
	if err := f.failing("GetDestination"); err != nil {
		return models.Destination{}, err
	}
	d, ok := f.destinations[id]
	if !ok {
		return models.Destination{}, store.ErrNotFound
	}
	return d, nil
}

func (f *fakeStore) ListRoutes() ([]models.Route, error) {
	if err := f.failing("ListRoutes"); err != nil {
		return nil, err
	}
	out := []models.Route{}
	for _, r := range f.routes {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeStore) CreateRoute(r models.Route) (models.Route, error) {
	if err := f.failing("CreateRoute"); err != nil {
		return models.Route{}, err
	}
	if r.ID == "" {
		r.ID = "generated"
	}
	f.routes[r.ID] = r
	return r, nil
}

func (f *fakeStore) UpdateRoute(r models.Route) (models.Route, error) {
	if err := f.failing("UpdateRoute"); err != nil {
		return models.Route{}, err
	}
	f.routes[r.ID] = r
	return r, nil
}

func (f *fakeStore) DeleteRoute(id string) error {
	if err := f.failing("DeleteRoute"); err != nil {
		return err
	}
	delete(f.routes, id)
	return nil
}

func (f *fakeStore) GetRoute(id string) (models.Route, error) {
	if err := f.failing("GetRoute"); err != nil {
		return models.Route{}, err
	}
	r, ok := f.routes[id]
	if !ok {
		return models.Route{}, store.ErrNotFound
	}
	return r, nil
}

func (f *fakeStore) CreateSession(session models.Session) error {
	if err := f.failing("CreateSession"); err != nil {
		return err
	}
	f.sessions[session.ID] = session
	return nil
}

func (f *fakeStore) GetSession(id string) (models.Session, error) {
	if err := f.failing("GetSession"); err != nil {
		return models.Session{}, err
	}
	session, ok := f.sessions[id]
	if !ok || !session.ExpiresAt.After(time.Now()) {
		return models.Session{}, store.ErrNotFound
	}
	return session, nil
}

func (f *fakeStore) DeleteSession(id string) error {
	if err := f.failing("DeleteSession"); err != nil {
		return err
	}
	delete(f.sessions, id)
	return nil
}

func (f *fakeStore) DeleteExpiredSessions() error { return f.failing("DeleteExpiredSessions") }

func (f *fakeStore) CreateLoginFlow(flow models.LoginFlow) error {
	if err := f.failing("CreateLoginFlow"); err != nil {
		return err
	}
	f.loginFlows[flow.State] = flow
	return nil
}

func (f *fakeStore) TakeLoginFlow(state string) (models.LoginFlow, error) {
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

func (f *fakeStore) ListGrants() ([]models.Grant, error) {
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

func (f *fakeStore) CreateGrant(g models.Grant) (models.Grant, error) {
	if err := f.failing("CreateGrant"); err != nil {
		return models.Grant{}, err
	}
	if g.ID == "" {
		g.ID = "generated"
	}
	f.grants[g.ID] = g
	return g, nil
}

func (f *fakeStore) DeleteGrant(id string) error {
	if err := f.failing("DeleteGrant"); err != nil {
		return err
	}
	delete(f.grants, id)
	return nil
}

func (f *fakeStore) UpsertActiveAlert(a models.ActiveAlert) error {
	if err := f.failing("UpsertActiveAlert"); err != nil {
		return err
	}
	f.activeAlerts[activeAlertKey(a.Fingerprint, a.TeamID, a.ChannelID)] = a
	return nil
}

func (f *fakeStore) ListActiveAlerts(fingerprint string) ([]models.ActiveAlert, error) {
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

func (f *fakeStore) GetActiveAlert(fingerprint, teamID, channelID string) (models.ActiveAlert, error) {
	if err := f.failing("GetActiveAlert"); err != nil {
		return models.ActiveAlert{}, err
	}
	a, ok := f.activeAlerts[activeAlertKey(fingerprint, teamID, channelID)]
	if !ok {
		return models.ActiveAlert{}, store.ErrNotFound
	}
	return a, nil
}

func (f *fakeStore) DeleteActiveAlert(fingerprint, teamID, channelID string) error {
	if err := f.failing("DeleteActiveAlert"); err != nil {
		return err
	}
	delete(f.activeAlerts, activeAlertKey(fingerprint, teamID, channelID))
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
	f.posts = append(f.posts, postCall{teamID: teamID, channelID: channelID, msg: msg})
	if f.postErr != nil && (f.postErrFor == "" || f.postErrFor == channelID) {
		return "", f.postErr
	}
	if f.messageID == "" {
		return "message-1", nil
	}
	return f.messageID, nil
}

func (f *fakeMessenger) ListTeams() ([]graph.Team, error) {
	f.teamCalls++
	if f.directoryErr != nil {
		return nil, f.directoryErr
	}
	return f.teams, nil
}

func (f *fakeMessenger) ListChannels(teamID string) ([]graph.Channel, error) {
	f.channelCalls++
	if f.directoryErr != nil {
		return nil, f.directoryErr
	}
	return f.channels[teamID], nil
}

func (f *fakeMessenger) UpdateMessage(teamID, channelID, messageID string, msg graph.Message) error {
	f.updates = append(f.updates, updateCall{
		postCall:  postCall{teamID: teamID, channelID: channelID, msg: msg},
		messageID: messageID,
	})
	return f.updateErr
}
