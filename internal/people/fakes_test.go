package people

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func openStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), store.Options{
		Driver:  store.DriverSQLite,
		Path:    filepath.Join(t.TempDir(), "people.db"),
		Migrate: store.MigrateAuto,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// fakeGraph is a directory of users, keyed by object id.
type fakeGraph struct {
	mu        sync.Mutex
	users     map[string]graph.User
	listErr   error
	getErr    error
	installFn func(userID string) (bool, error)
	catalogFn func() (string, error)

	gets, mailFinds, installs, catalogs int
}

func newFakeGraph(users ...graph.User) *fakeGraph {
	g := &fakeGraph{users: map[string]graph.User{}}
	for _, u := range users {
		g.users[u.ID] = u
	}
	return g
}

func (g *fakeGraph) GetUser(_ context.Context, idOrUPN string) (graph.User, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gets++
	if g.getErr != nil {
		return graph.User{}, g.getErr
	}
	for _, u := range g.users {
		if u.ID == idOrUPN || strings.EqualFold(u.UserPrincipalName, idOrUPN) {
			return u, nil
		}
	}
	return graph.User{}, &graph.APIError{Status: 404}
}

func (g *fakeGraph) FindUserByMail(_ context.Context, mail string) (graph.User, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mailFinds++
	for _, u := range g.users {
		if strings.EqualFold(u.Mail, mail) {
			return u, nil
		}
	}
	return graph.User{}, graph.ErrNotFound
}

func (g *fakeGraph) ListMemberUsers(_ context.Context, fn func([]graph.User) error) error {
	g.mu.Lock()
	if g.listErr != nil {
		g.mu.Unlock()
		return g.listErr
	}
	var page []graph.User
	for _, u := range g.users {
		if u.AccountEnabled && u.UserType == "Member" {
			page = append(page, u)
		}
	}
	g.mu.Unlock()
	return fn(page)
}

func (g *fakeGraph) ResolveCatalogApp(context.Context, string, string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.catalogs++
	if g.catalogFn != nil {
		return g.catalogFn()
	}
	return "cat-1", nil
}

func (g *fakeGraph) InstallAppForUser(_ context.Context, userID, _ string) (bool, error) {
	g.mu.Lock()
	g.installs++
	fn := g.installFn
	g.mu.Unlock()
	if fn != nil {
		return fn(userID)
	}
	return false, nil
}

// fakeChats opens a chat for anyone in installed, after failing failFirst
// times for them.
type fakeChats struct {
	mu        sync.Mutex
	installed map[string]bool
	failFirst map[string]int
	calls     int
}

func newFakeChats(installed ...string) *fakeChats {
	c := &fakeChats{installed: map[string]bool{}, failFirst: map[string]int{}}
	for _, id := range installed {
		c.installed[id] = true
	}
	return c
}

func (c *fakeChats) install(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.installed[id] = true
}

func (c *fakeChats) CreatePersonalConversation(_ context.Context, serviceURL, tenantID, botID, oid string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if serviceURL == "" || tenantID == "" || botID == "" {
		return "", errors.New("incomplete conversation request")
	}
	if !c.installed[oid] {
		return "", errors.New("403 bot not installed")
	}
	if c.failFirst[oid] > 0 {
		c.failFirst[oid]--
		return "", errors.New("not yet")
	}
	return "a:" + oid, nil
}

type fakeRecorder struct {
	mu       sync.Mutex
	installs map[string]int
	lookups  map[string]int
	runs     map[string]int
}

func newFakeRecorder() *fakeRecorder {
	return &fakeRecorder{installs: map[string]int{}, lookups: map[string]int{}, runs: map[string]int{}}
}

func (r *fakeRecorder) AppInstall(_ context.Context, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.installs[outcome]++
}

func (r *fakeRecorder) DirectoryLookup(_ context.Context, result string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups[result]++
}

func (r *fakeRecorder) ReconcileRun(_ context.Context, kind, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[kind+"/"+outcome]++
}

// clock is a settable now.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func member(id, upn, mail string) graph.User {
	return graph.User{ID: id, UserPrincipalName: upn, Mail: mail, DisplayName: id, GivenName: id, AccountEnabled: true, UserType: "Member"}
}

func noSleep(context.Context, time.Duration) error { return nil }
