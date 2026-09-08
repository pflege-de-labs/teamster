package httpserver

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

var errStore = errors.New("store exploded")

// fakeStore is an in-memory store.Store. Setting failOn to a method name makes
// that method return errStore, which is how the handler error paths are driven.
type fakeStore struct {
	mu sync.Mutex

	templates    map[string]models.Template
	destinations map[string]models.Destination
	routes       map[string]models.Route
	activeAlerts map[string]models.ActiveAlert

	failOn map[string]bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		templates:    map[string]models.Template{},
		destinations: map[string]models.Destination{},
		routes:       map[string]models.Route{},
		activeAlerts: map[string]models.ActiveAlert{},
		failOn:       map[string]bool{},
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

func (f *fakeStore) UpsertActiveAlert(a models.ActiveAlert) error {
	if err := f.failing("UpsertActiveAlert"); err != nil {
		return err
	}
	f.activeAlerts[a.Fingerprint] = a
	return nil
}

func (f *fakeStore) GetActiveAlert(fingerprint string) (models.ActiveAlert, error) {
	if err := f.failing("GetActiveAlert"); err != nil {
		return models.ActiveAlert{}, err
	}
	a, ok := f.activeAlerts[fingerprint]
	if !ok {
		return models.ActiveAlert{}, store.ErrNotFound
	}
	return a, nil
}

func (f *fakeStore) DeleteActiveAlert(fingerprint string) error {
	if err := f.failing("DeleteActiveAlert"); err != nil {
		return err
	}
	delete(f.activeAlerts, fingerprint)
	return nil
}

type postCall struct {
	teamID    string
	channelID string
	card      json.RawMessage
	summary   string
}

type updateCall struct {
	postCall
	messageID string
}

type fakeMessenger struct {
	messageID string
	postErr   error
	updateErr error

	posts   []postCall
	updates []updateCall
}

func (f *fakeMessenger) PostMessage(teamID, channelID string, card json.RawMessage, summary string) (string, error) {
	f.posts = append(f.posts, postCall{teamID: teamID, channelID: channelID, card: card, summary: summary})
	if f.postErr != nil {
		return "", f.postErr
	}
	if f.messageID == "" {
		return "message-1", nil
	}
	return f.messageID, nil
}

func (f *fakeMessenger) UpdateMessage(teamID, channelID, messageID string, card json.RawMessage, summary string) error {
	f.updates = append(f.updates, updateCall{
		postCall:  postCall{teamID: teamID, channelID: channelID, card: card, summary: summary},
		messageID: messageID,
	})
	return f.updateErr
}
