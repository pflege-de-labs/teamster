package httpserver

import (
	"context"
	"sync"
	"time"

	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// A team's install state for the bot's Teams app (ADR 0045).
const (
	installInstalled = "installed"
	installMissing   = "missing"
	installUnknown   = "unknown"
)

// installLookups bounds the Graph calls one page makes: a tenant with hundreds
// of teams must not become hundreds of calls at once.
const (
	installLookups       = 8
	installLookupTimeout = 5 * time.Second
)

// installCache remembers the teams Graph said lack the app. An install found
// through Graph is stored as a bot_teams row instead, so replicas share it.
type installCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	missing map[string]time.Time
}

func newInstallCache(ttl time.Duration) *installCache {
	return &installCache{ttl: ttl, missing: map[string]time.Time{}}
}

func (c *installCache) isMissing(teamID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok := c.missing[teamID]
	return ok && time.Since(at) < c.ttl
}

func (c *installCache) markMissing(teamID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.missing) >= maxCachedTeams {
		c.missing = map[string]time.Time{}
	}
	c.missing[teamID] = time.Now()
}

// installStates answers for each team: a bot_teams row is installed; without
// one Graph looks for the bot by its client id; anything unanswered is unknown.
func (s *Server) installStates(ctx context.Context, teamIDs []string) map[string]string {
	states := make(map[string]string, len(teamIDs))
	for _, id := range teamIDs {
		states[id] = installUnknown
	}

	rows, err := s.store.ListBotTeams(ctx)
	if err != nil {
		logError(ctx, "list bot teams", err)
		return states
	}
	recorded := make(map[string]bool, len(rows))
	for _, row := range rows {
		recorded[row.TeamID] = true
	}

	var pending []string
	for _, id := range teamIDs {
		switch {
		case recorded[id]:
			states[id] = installInstalled
		case s.installs.isMissing(id):
			states[id] = installMissing
		case s.cfg.Bot.ClientID != "":
			pending = append(pending, id)
		}
	}
	if len(pending) == 0 {
		return states
	}

	for id, state := range s.lookUpInstalls(ctx, pending) {
		states[id] = state
	}
	return states
}

// lookUpInstalls asks Graph about each team, a few at a time, and stops
// waiting at the deadline. A late answer still lands in the cache or table.
func (s *Server) lookUpInstalls(ctx context.Context, teamIDs []string) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, installLookupTimeout)
	defer cancel()

	type answer struct{ teamID, state string }
	answers := make(chan answer, len(teamIDs))
	slots := make(chan struct{}, installLookups)

	for _, id := range teamIDs {
		go func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			answers <- answer{id, s.lookUpInstall(context.WithoutCancel(ctx), id)}
		}()
	}

	out := make(map[string]string, len(teamIDs))
	for range teamIDs {
		select {
		case a := <-answers:
			out[a.teamID] = a.state
		case <-ctx.Done():
			return out
		}
	}
	return out
}

func (s *Server) lookUpInstall(ctx context.Context, teamID string) string {
	installed, err := s.graph.HasInstalledApp(teamID, s.cfg.Bot.ClientID)
	if err != nil {
		// Usually TeamsAppInstallation.ReadForTeam.All is not granted.
		logging.FromContext(ctx).Warn("look up the Teams app", "team", teamID, "err", err)
		return installUnknown
	}
	if !installed {
		// Also what a bot.client-id other than the manifest's botId looks like.
		logging.FromContext(ctx).Info("Teams app not installed in team", "team", teamID, "bot_client_id", s.cfg.Bot.ClientID)
		s.installs.markMissing(teamID)
		return installMissing
	}
	// No service URL: only an activity from the team may name one, and until
	// then delivery uses bot.service-url.
	row := models.BotTeam{TeamID: teamID, TenantID: s.botTenant(), UpdatedAt: s.now()}
	if err := s.store.UpsertBotTeam(ctx, row); err != nil {
		logError(ctx, "record bot team found through Graph", err)
	}
	return installInstalled
}

func (s *Server) botTenant() string {
	if s.cfg.Bot.TenantID != "" {
		return s.cfg.Bot.TenantID
	}
	return s.cfg.Graph.TenantID
}
