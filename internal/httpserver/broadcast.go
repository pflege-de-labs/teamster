package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
	"github.com/pflege-de-labs/teamster/internal/store"
)

var (
	errBroadcastNames = errors.New("a broadcast goes to everyone: drop recipients and the teamster_recipient label")
	errBroadcastState = errors.New("a broadcast is delivered once: drop state")
	// errMayNotBroadcast is a broadcast from a token below the level everyone.
	errMayNotBroadcast = errors.New("this token may not broadcast: it needs the message level everyone")
)

const (
	// broadcastTick is how often a replica looks for a broadcast to deliver.
	broadcastTick = 5 * time.Second
	// broadcastStale is how long a heartbeat may be silent before another
	// replica takes the broadcast over; one comes after every chunk.
	broadcastStale = 2 * time.Minute
	// broadcastChunk is how many people are sent to between heartbeats, and
	// so at most how many get the message twice after a takeover.
	broadcastChunk = 25
	// broadcastChunkBudget bounds one chunk's sends, so throttling backoff
	// cannot outlast the heartbeat and hand the broadcast to a second replica (ADR 0085).
	broadcastChunkBudget = broadcastStale / 2
	// broadcastRetention is how long a finished broadcast is listed.
	broadcastRetention = 30 * 24 * time.Hour
	// directoryPage is how many directory users are read at once.
	directoryPage = 500
	// reasonNoAddressedRoute is a broadcast no route that addresses people matches.
	reasonNoAddressedRoute = "no-addressed-route"
)

// WithBroadcasts hands serve the loop that delivers broadcasts (ADR 0083).
func WithBroadcasts(run *func(context.Context)) Option {
	return func(s *Server) { *run = s.runBroadcasts }
}

// checkBroadcast refuses a broadcast that also names people or has a lifecycle.
func checkBroadcast(ev models.Event) error {
	switch {
	case len(models.AddressesOf(ev)) > 0:
		return errBroadcastNames
	case ev.State != models.StateNone:
		return errBroadcastState
	}
	return nil
}

// isBroadcast reports whether any of the events is a broadcast.
func isBroadcast(events []models.Event) bool {
	for _, ev := range events {
		if ev.Universal != nil && ev.Universal.Broadcast {
			return true
		}
	}
	return false
}

// broadcastAnswer is what the webhook says about a broadcast it accepted.
type broadcastAnswer struct {
	Status      string           `json:"status"`
	Broadcast   models.Broadcast `json:"broadcast"`
	StatusURL   string           `json:"status_url"`
	Delivered   int              `json:"delivered"`
	Undelivered []undelivered    `json:"undelivered,omitempty"`
}

// handleBroadcast routes a broadcast, delivers what goes to channels and
// linked chats now, and queues the people for the background run.
func (s *Server) handleBroadcast(w http.ResponseWriter, r *http.Request, from sender, ev models.Event) {
	ctx := r.Context()
	b, rep, err := s.startBroadcast(ctx, from, ev)
	if err != nil || b.ID == "" {
		writeReport(w, r, rep, err)
		return
	}
	logging.FromContext(ctx).Info("broadcast queued", "broadcast", b.ID, "token", from.token.Name, "creator", b.RequestedBy)
	writeJSON(w, http.StatusAccepted, broadcastAnswer{
		Status: "accepted", Broadcast: b, StatusURL: "/webhook/broadcasts/" + b.ID,
		Delivered: rep.delivered, Undelivered: rep.undelivered,
	})
}

// startBroadcast is processEvent for a broadcast: the addressed deliveries are
// stored for the run instead of expanded. Without one, nothing is queued and
// the report says why.
func (s *Server) startBroadcast(ctx context.Context, from sender, ev models.Event) (models.Broadcast, report, error) {
	if ev.Key == "" {
		ev.Key = deriveKey(ev)
	}
	ev.Labels = models.WithSourceLabel(ev.Labels, ev.Source)
	if s.samples != nil {
		s.samples.Observe(ev.Labels, models.AttributesOf(ev))
	}
	result, err := s.router.Plan(ctx, ev.Labels)
	if err != nil {
		return models.Broadcast{}, report{}, fmt.Errorf("route: %w", err)
	}
	switch result.Reason {
	case routing.ReasonNoRoutes:
		return models.Broadcast{}, report{}, errors.New("no routes configured")
	case routing.ReasonNone:
		return models.Broadcast{}, report{}, errors.New("no matching route and no default route")
	}

	var addressed, others []routing.Delivery
	for _, d := range result.Deliveries {
		if d.Kind == routing.DeliveryAddressed {
			addressed = append(addressed, d)
		} else {
			others = append(others, d)
		}
	}
	if len(addressed) == 0 {
		var rep report
		s.miss(ctx, &rep, routing.Delivery{}, "", reasonNoAddressedRoute)
		return models.Broadcast{}, rep, nil
	}
	if s.bot == nil {
		return models.Broadcast{}, report{}, errNoBotConfigured
	}
	// First, so a failure is retried before anyone is queued twice.
	rep, err := s.deliverEvent(ctx, ev, others, nil, true)
	if err != nil {
		return models.Broadcast{}, rep, err
	}
	event, err := json.Marshal(ev)
	if err != nil {
		return models.Broadcast{}, rep, err
	}
	plan, err := json.Marshal(addressed)
	if err != nil {
		return models.Broadcast{}, rep, err
	}
	b, err := s.store.CreateBroadcast(ctx, models.Broadcast{
		RequestedBy: from.creator.Subject, TokenName: from.token.Name, Event: event, Plan: plan,
	})
	return b, rep, err
}

// runBroadcasts delivers queued broadcasts until ctx ends, one at a time per
// replica, and prunes finished ones.
func (s *Server) runBroadcasts(ctx context.Context) {
	owner := "teamster-" + uuid.NewString()
	// Jitter keeps replicas started together from ticking together.
	t := time.NewTimer(time.Duration(rand.Int64N(int64(broadcastTick))))
	defer t.Stop()
	lastPrune := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if _, err := s.broadcastStep(ctx, owner); err != nil && ctx.Err() == nil {
			s.log.Error("broadcast", "err", err)
		}
		if now := s.now(); now.Sub(lastPrune) > time.Hour {
			lastPrune = now
			if _, err := s.store.PruneBroadcasts(ctx, now.Add(-broadcastRetention)); err != nil && ctx.Err() == nil {
				s.log.Error("prune broadcasts", "err", err)
			}
		}
		t.Reset(broadcastTick)
	}
}

// broadcastStep claims the next broadcast and delivers it, reporting whether
// there was one.
func (s *Server) broadcastStep(ctx context.Context, owner string) (bool, error) {
	now := s.now()
	b, err := s.store.NextBroadcast(ctx, now.Add(-broadcastStale))
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	claimed, err := s.store.ClaimBroadcast(ctx, b.ID, owner, now, now.Add(-broadcastStale))
	if err != nil || !claimed {
		return false, err
	}
	return true, s.executeBroadcast(ctx, b, owner)
}

var (
	// errBroadcastLost stops a broadcast another replica has taken over.
	errBroadcastLost = errors.New("another replica took the broadcast over")
	// errBroadcastPaused stops one whose progress could not be recorded; it
	// stays running and is carried on once its heartbeat goes stale.
	errBroadcastPaused = errors.New("broadcast progress not recorded")
)

// executeBroadcast sends to everyone after the cursor, a chunk at a time, and
// records progress after each, so a takeover repeats at most one chunk.
func (s *Server) executeBroadcast(ctx context.Context, b models.Broadcast, owner string) error {
	counts, cursor := b.BroadcastCounts, b.Cursor
	err := s.deliverBroadcast(ctx, b, owner, &counts, &cursor)
	switch {
	case errors.Is(err, errBroadcastLost):
		s.log.Warn("broadcast taken over", "broadcast", b.ID)
		return nil
	case errors.Is(err, errBroadcastPaused):
		return err
	case ctx.Err() != nil:
		// Left running: the next replica to see it stale carries on.
		return nil
	}
	state, lastErr := models.RunDone, ""
	if err != nil {
		state, lastErr = models.RunFailed, err.Error()
	}
	if _, ferr := s.store.FinishBroadcast(context.WithoutCancel(ctx), b.ID, owner, state, cursor, counts, lastErr, s.now()); ferr != nil {
		err = errors.Join(err, ferr)
	}
	s.log.Info("broadcast finished", "broadcast", b.ID, "state", state, "delivered", counts.Delivered,
		"unreachable", counts.Unreachable, "failed", counts.Failed)
	return err
}

func (s *Server) deliverBroadcast(ctx context.Context, b models.Broadcast, owner string, counts *models.BroadcastCounts, cursor *string) error {
	var ev models.Event
	if err := json.Unmarshal(b.Event, &ev); err != nil {
		return fmt.Errorf("decode broadcast event: %w", err)
	}
	var plan []routing.Delivery
	if err := json.Unmarshal(b.Plan, &plan); err != nil {
		return fmt.Errorf("decode broadcast plan: %w", err)
	}
	audience, err := s.broadcastAudience(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", errBroadcastPaused, err)
	}
	counts.Total = int64(len(audience))
	start := sort.Search(len(audience), func(i int) bool { return audience[i].key > *cursor })
	for i := start; i < len(audience); i += broadcastChunk {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		part := audience[i:min(i+broadcastChunk, len(audience))]
		chunkCtx, cancel := context.WithTimeout(ctx, broadcastChunkBudget)
		s.deliverToAudience(chunkCtx, ev, plan, part, counts)
		cancel()
		*cursor = part[len(part)-1].key
		ok, err := s.store.HeartbeatBroadcast(ctx, b.ID, owner, *cursor, *counts, s.now())
		if err != nil {
			return fmt.Errorf("%w: %w", errBroadcastPaused, err)
		}
		if !ok {
			return errBroadcastLost
		}
	}
	return nil
}

// A member is one person a broadcast reaches: a directory user, or a linked
// recipient the directory does not know.
type member struct {
	key         string
	personID    string
	recipientID string
}

// broadcastAudience is everyone the bot can reach, each once, sorted by key.
func (s *Server) broadcastAudience(ctx context.Context) ([]member, error) {
	var out []member
	seen := map[string]bool{}
	for after := ""; ; {
		users, err := s.store.ListReachableDirectoryUsers(ctx, after, directoryPage)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			out = append(out, member{key: personKeyPrefix + u.AADObjectID, personID: u.AADObjectID})
			seen[strings.ToLower(u.AADObjectID)] = true
		}
		if len(users) < directoryPage {
			break
		}
		after = users[len(users)-1].AADObjectID
	}
	recipients, err := s.store.ListRecipients(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range recipients {
		if r.ConversationID == "" || (r.AADObjectID != "" && seen[strings.ToLower(r.AADObjectID)]) {
			continue
		}
		out = append(out, member{key: "rcp:" + r.ID, recipientID: r.ID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out, nil
}

// deliverToAudience sends every addressed delivery to each member, at most
// webhook.fanout-concurrency at a time. A person counts once, by their worst
// outcome.
func (s *Server) deliverToAudience(ctx context.Context, ev models.Event, plan []routing.Delivery, part []member, counts *models.BroadcastCounts) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(s.cfg.Webhook.FanoutConcurrency, 1))
	for _, m := range part {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			var failed, blocked bool
			for _, d := range plan {
				if m.personID != "" {
					d.PersonID = m.personID
				} else {
					d.Kind, d.RecipientID = routing.DeliveryRecipient, m.recipientID
				}
				err := s.deliverOnce(ctx, ev, d)
				switch {
				case err == nil:
				case recipientBlocked(err):
					blocked = true
				default:
					failed = true
					logError(ctx, "broadcast delivery", fmt.Errorf("route %s, %s: %w", d.RouteName, m.key, err))
				}
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case failed:
				counts.Failed++
			case blocked:
				counts.Unreachable++
			default:
				counts.Delivered++
			}
		}()
	}
	wg.Wait()
}

// handleBroadcastStatus is GET /webhook/broadcasts/{id}, for a token of the
// broadcast's creator. Anyone else is told there is no such broadcast.
func (s *Server) handleBroadcastStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	from, ok := s.authorizeWebhook(w, r, models.SourceUniversal)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/webhook/broadcasts/")
	b, err := s.store.GetBroadcast(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound), err == nil && (from.creator == nil || from.creator.Subject != b.RequestedBy):
		writeJSONError(w, http.StatusNotFound, "no such broadcast")
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, b)
	}
}

// visibleBroadcasts are everyone's for an admin, and the caller's own otherwise.
func (s *Server) visibleBroadcasts(r *http.Request) ([]models.Broadcast, error) {
	requestedBy := principalSubject(r)
	if s.allow(r, authz.ActionAdminister, authz.Resource{Type: "Broadcast"}) {
		requestedBy = ""
	}
	return s.store.ListBroadcasts(r.Context(), requestedBy, 100)
}

// handleBroadcasts is GET /api/broadcasts.
func (s *Server) handleBroadcasts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.visibleBroadcasts(r)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleBroadcastsPage is /admin/broadcasts.
func (s *Server) handleBroadcastsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	page := views.Broadcasts{Viewer: s.viewerFor(r)}
	items, err := s.visibleBroadcasts(r)
	if err != nil {
		page.Error = failureText(ctx, "list broadcasts", err)
	}
	page.Items = items
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.BroadcastsPage(page).Render(ctx, w); err != nil {
		logError(ctx, "render broadcasts page", err)
	}
}
