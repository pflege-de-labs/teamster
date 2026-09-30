package people

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

const (
	// tickInterval is how often a replica looks for a run to do.
	tickInterval = 30 * time.Second
	// staleAfter is how long a run's heartbeat may be silent before another
	// replica takes it over. Heartbeats come every heartbeatEvery users and
	// at least every heartbeatInterval.
	staleAfter        = 2 * time.Minute
	heartbeatEvery    = 25
	heartbeatInterval = 30 * time.Second
	// dueBatch is how many due people are read at once.
	dueBatch = 100
	// retention is how long departed people and finished runs are kept.
	retention = 30 * 24 * time.Hour
)

// errLeaseLost stops a run another replica has taken over.
var errLeaseLost = errors.New("another replica took the run over")

// ReconcilerConfig is what a Reconciler needs from the bot configuration.
type ReconcilerConfig struct {
	TenantID string
	// Interval between periodic runs; zero runs only on request.
	Interval    time.Duration
	Reverify    time.Duration
	Concurrency int
	// Owner names this replica in the run table.
	Owner string
}

// Reconciler installs the Teams app for every enabled member, one run at a
// time across all replicas (ADR 0059).
type Reconciler struct {
	store Store
	graph Directory
	inst  *Installer
	rec   Recorder
	log   *slog.Logger
	cfg   ReconcilerConfig
	now   func() time.Time
}

func NewReconciler(log *slog.Logger, st Store, dir Directory, inst *Installer, rec Recorder, cfg ReconcilerConfig) *Reconciler {
	return &Reconciler{
		store: st, graph: dir, inst: inst, rec: rec, log: log, cfg: cfg,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Run looks for work every tick until ctx ends.
func (r *Reconciler) Run(ctx context.Context) {
	// Jitter keeps replicas started together from ticking together.
	jitter := time.Duration(rand.Int64N(int64(tickInterval)))
	t := time.NewTimer(jitter)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := r.Tick(ctx); err != nil && ctx.Err() == nil {
			r.log.Error("directory reconcile", "err", err)
		}
		t.Reset(tickInterval)
	}
}

// Tick does one run if one is waiting or due. A run that was requested, or
// whose owner stopped writing heartbeats, is claimed; otherwise a periodic run
// is requested once the interval has passed since the last one.
func (r *Reconciler) Tick(ctx context.Context) error {
	now := r.now()
	latest, err := r.store.LatestDirectoryRun(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		latest = models.DirectoryRun{}
	case err != nil:
		return err
	}

	active := latest.State == models.RunRequested || latest.State == models.RunRunning
	if !active {
		if r.cfg.Interval <= 0 || (!latest.RequestedAt.IsZero() && now.Sub(latest.RequestedAt) < r.cfg.Interval) {
			return nil
		}
		latest, err = r.store.RequestDirectoryRun(ctx, models.DirectoryRun{Kind: models.RunPeriodic, RequestedAt: now})
		if errors.Is(err, store.ErrConflict) {
			// Another replica requested it first; one of us claims it next.
			return nil
		}
		if err != nil {
			return err
		}
	}

	claimed, err := r.store.ClaimDirectoryRun(ctx, latest.ID, r.cfg.Owner, now, now.Add(-staleAfter))
	if err != nil || !claimed {
		return err
	}
	return r.execute(ctx, latest)
}

// execute lists the tenant, installs for whoever is due, retires people who
// left, and records how it went.
func (r *Reconciler) execute(ctx context.Context, run models.DirectoryRun) error {
	r.log.Info("directory run started", "run", run.ID, "kind", run.Kind)
	r.inst.ResetPermission()
	p := &progress{r: r, run: run, last: r.now()}

	err := r.sync(ctx, p)
	if err == nil {
		err = r.install(ctx, p)
	}
	if err == nil {
		r.tidy(ctx)
	}
	if errors.Is(err, errLeaseLost) {
		r.log.Warn("directory run taken over", "run", run.ID)
		r.rec.ReconcileRun(ctx, string(run.Kind), "lost")
		return nil
	}

	state, lastErr := models.RunDone, ""
	if err != nil {
		state, lastErr = models.RunFailed, err.Error()
	}
	// A cancelled run still says so, or its row would wait for a takeover.
	finishCtx := context.WithoutCancel(ctx)
	if _, ferr := r.store.FinishDirectoryRun(finishCtx, run.ID, r.cfg.Owner, state, p.snapshot(), lastErr, r.now()); ferr != nil {
		err = errors.Join(err, ferr)
	}
	r.rec.ReconcileRun(ctx, string(run.Kind), string(state))
	r.log.Info("directory run finished", "run", run.ID, "state", state, "counts", p.snapshot())
	return err
}

// sync upserts every enabled member and marks the rest departed. A listing
// that fails part-way marks nobody: missing from it proves nothing.
func (r *Reconciler) sync(ctx context.Context, p *progress) error {
	start := r.now()
	err := r.graph.ListMemberUsers(ctx, func(users []graph.User) error {
		for _, u := range users {
			if err := r.store.UpsertDirectoryUser(ctx, directoryUserOf(u, r.cfg.TenantID, start)); err != nil {
				return err
			}
			p.count(func(c *models.RunCounts) { c.Total++ })
		}
		return p.beat(ctx, false)
	})
	if permissionDenied(err) {
		return fmt.Errorf("list members: the graph registration needs the User.Read.All application permission with admin consent: %w", err)
	}
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}
	if _, err := r.store.MarkDirectoryUsersDeparted(ctx, start, r.now()); err != nil {
		return err
	}
	return p.beat(ctx, true)
}

// install works through the due list with a bounded pool. Someone handled once
// in this run is not handled again, even if a store error left them due.
func (r *Reconciler) install(ctx context.Context, p *progress) error {
	seen := map[string]bool{}
	for {
		now := r.now()
		due, err := r.store.ListDirectoryUsersDue(ctx, now, now.Add(-r.cfg.Reverify), dueBatch)
		if err != nil {
			return err
		}
		var batch []models.DirectoryUser
		for _, u := range due {
			if !seen[u.AADObjectID] {
				seen[u.AADObjectID] = true
				batch = append(batch, u)
			}
		}
		if len(batch) == 0 {
			return nil
		}
		if err := r.installBatch(ctx, p, batch); err != nil {
			return err
		}
	}
}

func (r *Reconciler) installBatch(ctx context.Context, p *progress, batch []models.DirectoryUser) error {
	work := make(chan models.DirectoryUser)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for range max(r.cfg.Concurrency, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range work {
				_, outcome, _ := r.inst.Ensure(ctx, u, true, true)
				p.count(func(c *models.RunCounts) { tally(c, outcome) })
				if err := p.beat(ctx, false); err != nil {
					mu.Lock()
					firstErr = cmpErr(firstErr, err)
					mu.Unlock()
				}
			}
		}()
	}

	for _, u := range batch {
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop || ctx.Err() != nil {
			break
		}
		work <- u
	}
	close(work)
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// tidy purges what has been kept long enough. Failing here fails nothing.
func (r *Reconciler) tidy(ctx context.Context) {
	cutoff := r.now().Add(-retention)
	if _, err := r.store.PurgeDepartedDirectoryUsers(ctx, cutoff); err != nil {
		r.log.Warn("purge departed directory users", "err", err)
	}
	if _, err := r.store.PruneDirectoryRuns(ctx, cutoff); err != nil {
		r.log.Warn("prune directory runs", "err", err)
	}
}

func tally(c *models.RunCounts, outcome string) {
	switch outcome {
	case OutcomeInstalled:
		c.Installed++
	case OutcomeAlready:
		c.Already++
	case OutcomeIneligible:
		c.Ineligible++
	default:
		c.Failed++
	}
}

func cmpErr(first, next error) error {
	if first != nil {
		return first
	}
	return next
}

// progress holds a run's counters and writes them as heartbeats, which is
// also how the run finds out it was taken over.
type progress struct {
	r   *Reconciler
	run models.DirectoryRun

	mu     sync.Mutex
	counts models.RunCounts
	since  int
	last   time.Time
}

func (p *progress) count(f func(*models.RunCounts)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(&p.counts)
	p.since++
}

func (p *progress) snapshot() models.RunCounts {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts
}

// beat writes a heartbeat when forced, every heartbeatEvery people, or after
// heartbeatInterval.
func (p *progress) beat(ctx context.Context, force bool) error {
	p.mu.Lock()
	now := p.r.now()
	if !force && p.since < heartbeatEvery && now.Sub(p.last) < heartbeatInterval {
		p.mu.Unlock()
		return nil
	}
	counts := p.counts
	p.since, p.last = 0, now
	p.mu.Unlock()

	ok, err := p.r.store.HeartbeatDirectoryRun(ctx, p.run.ID, p.r.cfg.Owner, counts, now)
	if err != nil {
		return err
	}
	if !ok {
		return errLeaseLost
	}
	return nil
}
