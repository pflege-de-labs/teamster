package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
)

func directoryMessenger() *fakeMessenger {
	return &fakeMessenger{
		teams: []graph.Team{{ID: "team-1", Name: "Operations"}, {ID: "team-2", Name: "Platform"}},
		channels: map[string][]graph.Channel{
			"team-1": {{ID: "chan-1", Name: "General"}, {ID: "chan-2", Name: "Alerts"}},
		},
	}
}

func TestGraphTeamsEndpoint(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), directoryMessenger()).Handler
	rec := do(t, handler, http.MethodGet, "/api/graph/teams", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET teams = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Teams []graph.Team `json:"teams"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Teams) != 2 || payload.Teams[0].Name != "Operations" {
		t.Errorf("teams = %+v, want the two fake teams with names", payload.Teams)
	}
}

func TestGraphChannelsEndpoint(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), directoryMessenger()).Handler

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCount  int
	}{
		{name: "known team", path: "/api/graph/teams/team-1/channels", wantStatus: http.StatusOK, wantCount: 2},
		{name: "team without channels", path: "/api/graph/teams/team-2/channels", wantStatus: http.StatusOK},
		{name: "a team literally named channels is not a team id", path: "/api/graph/teams/channels", wantStatus: http.StatusNotFound},
		{name: "nested path", path: "/api/graph/teams/a/b/channels", wantStatus: http.StatusNotFound},
		{name: "team id without the channels suffix", path: "/api/graph/teams/team-1", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, handler, http.MethodGet, tt.path, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("GET %s = %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			var payload struct {
				Channels []graph.Channel `json:"channels"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(payload.Channels) != tt.wantCount {
				t.Errorf("channels = %+v, want %d", payload.Channels, tt.wantCount)
			}
		})
	}
}

// The picker degrades to typing an id, so a Graph failure must be reported
// rather than hidden, and must not take the admin page down with it.
func TestGraphEndpointsReportFailures(t *testing.T) {
	t.Parallel()

	msg := directoryMessenger()
	msg.directoryErr = errors.New("graph GET failed: 403 Forbidden")
	handler := newTestServer(t, newFakeStore(), msg).Handler

	for _, path := range []string{"/api/graph/teams", "/api/graph/teams/team-1/channels"} {
		rec := do(t, handler, http.MethodGet, path, "")
		if rec.Code != http.StatusBadGateway {
			t.Errorf("GET %s = %d, want 502", path, rec.Code)
		}
		if !json.Valid(rec.Body.Bytes()) {
			t.Errorf("GET %s did not answer JSON: %s", path, rec.Body.String())
		}
	}

	if rec := do(t, handler, http.MethodGet, "/admin", ""); rec.Code != http.StatusOK {
		t.Error("a Graph failure took the admin page down with it")
	}
}

func TestGraphEndpointsRejectNonGet(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), directoryMessenger()).Handler
	for _, path := range []string{"/api/graph/teams", "/api/graph/teams/team-1/channels"} {
		rec := do(t, handler, http.MethodPost, path, "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want 405", path, rec.Code)
		}
	}
}

// Graph throttles, so a second look at the same list must not reach it again.
func TestDirectoryCacheServesRepeatedReads(t *testing.T) {
	t.Parallel()

	msg := directoryMessenger()
	handler := newTestServer(t, newFakeStore(), msg).Handler

	for range 3 {
		do(t, handler, http.MethodGet, "/api/graph/teams", "")
		do(t, handler, http.MethodGet, "/api/graph/teams/team-1/channels", "")
	}

	if msg.teamCalls != 1 {
		t.Errorf("ListTeams called %d times, want 1", msg.teamCalls)
	}
	if msg.channelCalls != 1 {
		t.Errorf("ListChannels called %d times, want 1", msg.channelCalls)
	}
}

func TestDirectoryCacheExpires(t *testing.T) {
	t.Parallel()

	calls := 0
	cache := newDirectoryCache(time.Millisecond)
	fetch := func() ([]graph.Team, error) {
		calls++
		return []graph.Team{{ID: "t"}}, nil
	}

	if _, err := cache.Teams(fetch); err != nil {
		t.Fatalf("first read: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := cache.Teams(fetch); err != nil {
		t.Fatalf("second read: %v", err)
	}

	if calls != 2 {
		t.Errorf("fetched %d times, want the entry to expire and refetch", calls)
	}
}

func TestDirectoryCacheDoesNotCacheFailures(t *testing.T) {
	t.Parallel()

	calls := 0
	cache := newDirectoryCache(time.Hour)
	failing := func() ([]graph.Team, error) {
		calls++
		return nil, errors.New("boom")
	}

	for range 2 {
		if _, err := cache.Teams(failing); err == nil {
			t.Fatal("want the failure surfaced")
		}
	}
	if calls != 2 {
		t.Errorf("fetched %d times, want a failure not to be remembered", calls)
	}
}

func TestDirectoryCacheKeepsTeamsApart(t *testing.T) {
	t.Parallel()

	cache := newDirectoryCache(time.Hour)
	for _, id := range []string{"a", "b"} {
		got, err := cache.Channels(id, func() ([]graph.Channel, error) {
			return []graph.Channel{{ID: id + "-chan"}}, nil
		})
		if err != nil {
			t.Fatalf("channels for %s: %v", id, err)
		}
		if got[0].ID != id+"-chan" {
			t.Errorf("team %s got %+v, want its own channels", id, got)
		}
	}
}

// The fetch runs outside the lock, so concurrent readers must neither deadlock
// nor see a torn cache. Run under -race to mean anything.
func TestDirectoryCacheUnderConcurrentReaders(t *testing.T) {
	t.Parallel()

	cache := newDirectoryCache(time.Hour)
	slowFetch := func() ([]graph.Team, error) {
		time.Sleep(2 * time.Millisecond)
		return []graph.Team{{ID: "t", Name: "Operations"}}, nil
	}

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			teams, err := cache.Teams(slowFetch)
			if err != nil || len(teams) != 1 || teams[0].Name != "Operations" {
				t.Errorf("concurrent read got %+v, %v", teams, err)
			}
		}()
	}
	wg.Wait()
}

func TestDirectoryCacheDropsEntriesBeyondTheCap(t *testing.T) {
	t.Parallel()

	cache := newDirectoryCache(time.Hour)
	fetch := func() ([]graph.Channel, error) { return []graph.Channel{{ID: "c"}}, nil }

	for i := range maxCachedTeams + 1 {
		if _, err := cache.Channels(strconv.Itoa(i), fetch); err != nil {
			t.Fatalf("channels: %v", err)
		}
	}

	cache.mu.Lock()
	size := len(cache.channels)
	cache.mu.Unlock()

	if size > maxCachedTeams {
		t.Errorf("cache holds %d teams, want it bounded at %d", size, maxCachedTeams)
	}
}
