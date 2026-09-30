package people

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

type reconcilerFixture struct {
	r     *Reconciler
	st    store.Store
	g     *fakeGraph
	chats *fakeChats
	rec   *fakeRecorder
	clock *clock
}

func newReconcilerFixture(t *testing.T, st store.Store, interval time.Duration, users ...graph.User) reconcilerFixture {
	t.Helper()
	if st == nil {
		st = openStore(t)
	}
	g := newFakeGraph(users...)
	chats := newFakeChats()
	g.installFn = func(id string) (bool, error) {
		chats.install(id)
		return false, nil
	}
	rec := newFakeRecorder()
	c := &clock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}

	inst := NewInstaller(st, g, chats, rec, InstallerConfig{BotID: "bot", ServiceURL: botServiceURL, AppID: "app", Global: true})
	inst.sleep, inst.now = noSleep, c.now
	r := NewReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), st, g, inst, rec, ReconcilerConfig{
		TenantID: "tenant", Interval: interval, Reverify: 7 * 24 * time.Hour, Concurrency: 3, Owner: "replica-1",
	})
	r.now = c.now
	return reconcilerFixture{r: r, st: st, g: g, chats: chats, rec: rec, clock: c}
}

func TestTickRunsPeriodically(t *testing.T) {
	t.Parallel()

	f := newReconcilerFixture(t, nil, 6*time.Hour,
		member("oid-a", "a@corp.example", ""),
		member("oid-b", "b@corp.example", ""),
		member("oid-c", "c@corp.example", ""),
	)
	f.chats.install("oid-a") // a setup policy got there first

	if err := f.r.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	run, err := f.st.LatestDirectoryRun(t.Context())
	if err != nil {
		t.Fatalf("LatestDirectoryRun: %v", err)
	}
	want := models.RunCounts{Total: 3, Installed: 2, Already: 1}
	if run.Kind != models.RunPeriodic || run.State != models.RunDone || run.RunCounts != want || run.Owner != "replica-1" {
		t.Errorf("run = %+v, want a finished periodic run with %+v", run, want)
	}
	if f.g.installs != 2 {
		t.Errorf("installs = %d, want 2: oid-a already had the app", f.g.installs)
	}
	for _, id := range []string{"oid-a", "oid-b", "oid-c"} {
		if u, _ := f.st.GetDirectoryUser(t.Context(), id); u.InstallState != models.InstallInstalled || u.ConversationID == "" {
			t.Errorf("%s = %s %q, want installed with a chat", id, u.InstallState, u.ConversationID)
		}
	}
	if f.rec.runs["periodic/done"] != 1 {
		t.Errorf("recorded runs = %v", f.rec.runs)
	}

	// Within the interval nothing new is requested.
	f.clock.advance(time.Hour)
	if err := f.r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if latest, _ := f.st.LatestDirectoryRun(t.Context()); latest.ID != run.ID {
		t.Error("a second run started within the interval")
	}

	// After it, a new run finds nothing due and installs nothing.
	f.clock.advance(6 * time.Hour)
	if err := f.r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	latest, _ := f.st.LatestDirectoryRun(t.Context())
	if latest.ID == run.ID || latest.Total != 3 || latest.Installed != 0 || f.g.installs != 2 {
		t.Errorf("second run = %+v, installs %d; want a new run that installs nothing", latest, f.g.installs)
	}
}

func TestTickOnlyOnRequestWithoutAnInterval(t *testing.T) {
	t.Parallel()

	f := newReconcilerFixture(t, nil, 0, member("oid-a", "a@corp.example", ""))

	if err := f.r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.LatestDirectoryRun(t.Context()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a run started without an interval or a request: %v", err)
	}

	if _, err := f.st.RequestDirectoryRun(t.Context(), models.DirectoryRun{Kind: models.RunManual, RequestedBy: "admin", RequestedAt: f.clock.now()}); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	run, _ := f.st.LatestDirectoryRun(t.Context())
	if run.State != models.RunDone || run.Installed != 1 || run.RequestedBy != "admin" {
		t.Errorf("manual run = %+v", run)
	}
}

func TestRunRetiresPeopleWhoLeft(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	f := newReconcilerFixture(t, st, 6*time.Hour, member("oid-stays", "s@corp.example", ""))
	if err := st.UpsertDirectoryUser(t.Context(), models.DirectoryUser{AADObjectID: "oid-left", TenantID: "tenant", Eligible: true, DirectorySeenAt: f.clock.now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	if err := f.r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if u, _ := st.GetDirectoryUser(t.Context(), "oid-left"); u.InstallState != models.InstallDeparted {
		t.Errorf("oid-left = %s, want departed", u.InstallState)
	}
	if f.g.installs != 1 {
		t.Errorf("installs = %d, want only the member who stays", f.g.installs)
	}
}

func TestRunFailsWhenTheListingFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantHint bool
	}{
		{name: "missing permission", err: &graph.APIError{Status: 403, Code: "Authorization_RequestDenied"}, wantHint: true},
		{name: "graph failure", err: &graph.APIError{Status: 500, Code: "InternalServerError"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := openStore(t)
			f := newReconcilerFixture(t, st, 6*time.Hour)
			f.g.listErr = tt.err
			if err := st.UpsertDirectoryUser(t.Context(), models.DirectoryUser{AADObjectID: "oid-known", TenantID: "tenant", Eligible: true, DirectorySeenAt: f.clock.now().Add(-time.Hour)}); err != nil {
				t.Fatal(err)
			}

			err := f.r.Tick(t.Context())
			var apiErr *graph.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Tick() = %v, want the listing failure", err)
			}
			run, _ := st.LatestDirectoryRun(t.Context())
			if run.State != models.RunFailed || run.LastError == "" {
				t.Errorf("run = %+v, want failed with the reason", run)
			}
			if got := strings.Contains(run.LastError, "User.Read.All"); got != tt.wantHint {
				t.Errorf("run error %q names User.Read.All = %v, want %v", run.LastError, got, tt.wantHint)
			}
			if u, _ := st.GetDirectoryUser(t.Context(), "oid-known"); u.InstallState == models.InstallDeparted {
				t.Error("a failed listing marked somebody departed")
			}
		})
	}
}

// losingStore reports every heartbeat as lost, as if another replica had
// taken the run over.
type losingStore struct {
	store.Store
}

func (losingStore) HeartbeatDirectoryRun(context.Context, string, string, models.RunCounts, time.Time) (bool, error) {
	return false, nil
}

func TestRunStopsWhenTakenOver(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	f := newReconcilerFixture(t, losingStore{st}, 6*time.Hour, member("oid-a", "a@corp.example", ""))

	if err := f.r.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() = %v, want a lost run to end quietly", err)
	}
	run, _ := st.LatestDirectoryRun(t.Context())
	if run.State != models.RunRunning {
		t.Errorf("run = %s, want it left running for its new owner", run.State)
	}
	if f.rec.runs["periodic/lost"] != 1 || f.g.installs != 0 {
		t.Errorf("runs = %v, installs = %d; want a lost run that installed nothing", f.rec.runs, f.g.installs)
	}
}

func TestTickTakesOverAStaleRun(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	f := newReconcilerFixture(t, st, 6*time.Hour, member("oid-a", "a@corp.example", ""))
	now := f.clock.now()
	run, err := st.RequestDirectoryRun(t.Context(), models.DirectoryRun{Kind: models.RunManual, RequestedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := st.ClaimDirectoryRun(t.Context(), run.ID, "replica-gone", now.Add(-time.Hour), now.Add(-2*time.Hour)); err != nil || !ok {
		t.Fatalf("ClaimDirectoryRun: %v, %v", ok, err)
	}

	if err := f.r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetDirectoryRun(t.Context(), run.ID)
	if got.State != models.RunDone || got.Owner != "replica-1" || got.Installed != 1 {
		t.Errorf("taken-over run = %+v", got)
	}
}

func TestRunStopsWithTheContext(t *testing.T) {
	t.Parallel()

	f := newReconcilerFixture(t, nil, time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.r.Run(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

func TestTally(t *testing.T) {
	t.Parallel()

	var c models.RunCounts
	for _, outcome := range []string{OutcomeInstalled, OutcomeAlready, OutcomeAlready, OutcomeIneligible, OutcomeFailed, "other"} {
		tally(&c, outcome)
	}
	if want := (models.RunCounts{Installed: 1, Already: 2, Ineligible: 1, Failed: 2}); c != want {
		t.Errorf("tally = %+v, want %+v", c, want)
	}
}
