package httpserver

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
)

// directoryTTL is short enough that a renamed channel appears without a
// restart, long enough that browsing the admin UI does not hammer the Graph.
const directoryTTL = 5 * time.Minute

// maxCachedTeams bounds the channel cache; beyond it the map is dropped rather
// than grown, and the next reads refill it.
const maxCachedTeams = 256

// directoryCache keeps the team and channel lists a little while, because the
// Graph throttles aggressively and the lists barely change.
type directoryCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	teams    []graph.Team
	teamsAt  time.Time
	channels map[string]channelEntry
}

type channelEntry struct {
	channels []graph.Channel
	at       time.Time
}

func newDirectoryCache(ttl time.Duration) *directoryCache {
	return &directoryCache{ttl: ttl, channels: map[string]channelEntry{}}
}

// The fetch runs outside the lock: holding it across a call to a throttled
// Graph would queue every other admin request behind this one.
func (d *directoryCache) Teams(fetch func() ([]graph.Team, error)) ([]graph.Team, error) {
	d.mu.Lock()
	if d.teams != nil && time.Since(d.teamsAt) < d.ttl {
		teams := d.teams
		d.mu.Unlock()
		return teams, nil
	}
	d.mu.Unlock()

	teams, err := fetch()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	d.teams, d.teamsAt = teams, time.Now()
	d.mu.Unlock()
	return teams, nil
}

func (d *directoryCache) Channels(teamID string, fetch func() ([]graph.Channel, error)) ([]graph.Channel, error) {
	d.mu.Lock()
	if entry, ok := d.channels[teamID]; ok && time.Since(entry.at) < d.ttl {
		channels := entry.channels
		d.mu.Unlock()
		return channels, nil
	}
	d.mu.Unlock()

	channels, err := fetch()
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	// One entry per team, so a tenant with an implausible number of them cannot
	// grow this without bound.
	if len(d.channels) >= maxCachedTeams {
		d.channels = map[string]channelEntry{}
	}
	d.channels[teamID] = channelEntry{channels: channels, at: time.Now()}
	d.mu.Unlock()
	return channels, nil
}

func (s *Server) handleGraphTeams(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	teams, err := s.directory.Teams(s.graph.ListTeams)
	if err != nil {
		// The picker falls back to typing an id, so this must stay legible.
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
}

func (s *Server) handleGraphChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Without the suffix check, /api/graph/teams/channels would look up a team
	// whose id is literally "channels".
	path := strings.TrimPrefix(r.URL.Path, "/api/graph/teams/")
	if !strings.HasSuffix(path, "/channels") {
		writeJSONError(w, http.StatusNotFound, "expected /api/graph/teams/{id}/channels")
		return
	}

	teamID := strings.TrimSuffix(path, "/channels")
	if teamID == "" || strings.Contains(teamID, "/") {
		writeJSONError(w, http.StatusNotFound, "team id is required")
		return
	}

	channels, err := s.directory.Channels(teamID, func() ([]graph.Channel, error) {
		return s.graph.ListChannels(teamID)
	})
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}
