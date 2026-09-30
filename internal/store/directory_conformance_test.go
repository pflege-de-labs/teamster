package store_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func alice(seen time.Time) models.DirectoryUser {
	return models.DirectoryUser{
		AADObjectID: "oid-alice", TenantID: "tenant",
		UserPrincipalName: "Alice@Corp.example", Mail: "alice.smith@corp.example",
		DisplayName: "Alice Smith", GivenName: "Alice", Surname: "Smith",
		Eligible: true, DirectorySeenAt: seen,
	}
}

func TestConformanceDirectoryUsers(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		seen := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

		if _, err := st.GetDirectoryUser(ctx, "oid-alice"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetDirectoryUser() before any upsert = %v, want ErrNotFound", err)
		}
		if err := st.UpsertDirectoryUser(ctx, alice(seen)); err != nil {
			t.Fatalf("UpsertDirectoryUser: %v", err)
		}

		got, err := st.GetDirectoryUser(ctx, "oid-alice")
		if err != nil {
			t.Fatalf("GetDirectoryUser: %v", err)
		}
		if got.InstallState != models.InstallUnknown || !got.Eligible || got.GivenName != "Alice" || !got.DirectorySeenAt.Equal(seen) {
			t.Errorf("GetDirectoryUser() = %+v, want a new, eligible, unknown user", got)
		}

		for _, address := range []string{"alice@corp.example", " ALICE@CORP.EXAMPLE ", "Alice.Smith@corp.example"} {
			if u, err := st.FindDirectoryUser(ctx, address); err != nil || u.AADObjectID != "oid-alice" {
				t.Errorf("FindDirectoryUser(%q) = %+v, %v, want alice", address, u, err)
			}
		}
		for _, address := range []string{"", "bob@corp.example"} {
			if _, err := st.FindDirectoryUser(ctx, address); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("FindDirectoryUser(%q) = %v, want ErrNotFound", address, err)
			}
		}

		at := seen.Add(time.Hour)
		if err := st.SetDirectoryUserInstalled(ctx, "oid-alice", "a:conv", "https://smba.example/emea/", at); err != nil {
			t.Fatalf("SetDirectoryUserInstalled: %v", err)
		}
		if err := st.SetDirectoryUserInstalled(ctx, "oid-nobody", "a:x", "https://smba.example/", at); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("SetDirectoryUserInstalled() for an unknown user = %v, want ErrNotFound", err)
		}
		got, err = st.GetDirectoryUserByConversation(ctx, "a:conv")
		if err != nil {
			t.Fatalf("GetDirectoryUserByConversation: %v", err)
		}
		if got.InstallState != models.InstallInstalled || got.ServiceURL != "https://smba.example/emea/" || !got.InstalledAt.Equal(at) {
			t.Errorf("after install = %+v", got)
		}
		if _, err := st.GetDirectoryUserByConversation(ctx, ""); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetDirectoryUserByConversation(\"\") = %v, want ErrNotFound", err)
		}

		// A later listing updates the profile and leaves the install alone.
		again := alice(seen.Add(2 * time.Hour))
		again.DisplayName = "Alice Jones"
		if err := st.UpsertDirectoryUser(ctx, again); err != nil {
			t.Fatalf("UpsertDirectoryUser again: %v", err)
		}
		if got, _ := st.GetDirectoryUser(ctx, "oid-alice"); got.DisplayName != "Alice Jones" || got.InstallState != models.InstallInstalled || got.ConversationID != "a:conv" {
			t.Errorf("after a second upsert = %+v, want the new name and the install kept", got)
		}

		if err := st.MarkDirectoryUserRemoved(ctx, "oid-alice", at); err != nil {
			t.Fatalf("MarkDirectoryUserRemoved: %v", err)
		}
		if got, _ := st.GetDirectoryUser(ctx, "oid-alice"); got.InstallState != models.InstallRemoved || got.ConversationID != "a:conv" || !got.NextAttemptAt.Equal(at) {
			t.Errorf("after removal = %+v, want removed, due now, conversation kept", got)
		}

		next := at.Add(time.Hour)
		for range 2 {
			if err := st.RecordDirectoryInstallFailure(ctx, "oid-alice", models.InstallFailed, "403 Forbidden", next, at); err != nil {
				t.Fatalf("RecordDirectoryInstallFailure: %v", err)
			}
		}
		if got, _ := st.GetDirectoryUser(ctx, "oid-alice"); got.InstallState != models.InstallFailed || got.Attempts != 2 || got.LastError != "403 Forbidden" || !got.NextAttemptAt.Equal(next) {
			t.Errorf("after two failures = %+v", got)
		}
		if err := st.MarkDirectoryUserRemoved(ctx, "oid-nobody", at); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("MarkDirectoryUserRemoved() for an unknown user = %v, want ErrNotFound", err)
		}
	})
}

func TestConformanceDirectoryUsersDue(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

		user := func(id string, eligible bool) {
			t.Helper()
			if err := st.UpsertDirectoryUser(ctx, models.DirectoryUser{AADObjectID: id, TenantID: "tenant", Eligible: eligible, DirectorySeenAt: now}); err != nil {
				t.Fatalf("UpsertDirectoryUser(%s): %v", id, err)
			}
		}
		user("a-unknown", true)
		user("b-guest", false)
		user("c-failed-later", true)
		user("d-failed-due", true)
		user("e-installed-fresh", true)
		user("f-installed-stale", true)
		user("g-ineligible", true)

		mustDo := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		mustDo(st.RecordDirectoryInstallFailure(ctx, "c-failed-later", models.InstallFailed, "x", now.Add(time.Hour), now))
		mustDo(st.RecordDirectoryInstallFailure(ctx, "d-failed-due", models.InstallFailed, "x", now.Add(-time.Minute), now))
		mustDo(st.SetDirectoryUserInstalled(ctx, "e-installed-fresh", "c1", "https://s/", now.Add(-time.Hour)))
		mustDo(st.SetDirectoryUserInstalled(ctx, "f-installed-stale", "c2", "https://s/", now.Add(-10*24*time.Hour)))
		mustDo(st.RecordDirectoryInstallFailure(ctx, "g-ineligible", models.InstallIneligible, "no licence", time.Time{}, now))

		due, err := st.ListDirectoryUsersDue(ctx, now, now.Add(-7*24*time.Hour), 10)
		if err != nil {
			t.Fatalf("ListDirectoryUsersDue: %v", err)
		}
		var ids []string
		for _, u := range due {
			ids = append(ids, u.AADObjectID)
		}
		if fmt.Sprint(ids) != "[a-unknown d-failed-due f-installed-stale]" {
			t.Errorf("due = %v", ids)
		}
		if due, _ := st.ListDirectoryUsersDue(ctx, now, now.Add(-7*24*time.Hour), 1); len(due) != 1 {
			t.Errorf("ListDirectoryUsersDue(limit 1) = %d users", len(due))
		}

		counts, err := st.CountDirectoryUsersByState(ctx)
		if err != nil {
			t.Fatalf("CountDirectoryUsersByState: %v", err)
		}
		want := map[models.InstallState]int64{models.InstallUnknown: 2, models.InstallFailed: 2, models.InstallInstalled: 2, models.InstallIneligible: 1}
		if fmt.Sprint(counts) != fmt.Sprint(want) {
			t.Errorf("counts = %v, want %v", counts, want)
		}
	})
}

func TestConformanceDirectoryUsersDepart(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		before := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		run := before.Add(24 * time.Hour)

		if err := st.UpsertDirectoryUser(ctx, models.DirectoryUser{AADObjectID: "left", TenantID: "t", Eligible: true, DirectorySeenAt: before}); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertDirectoryUser(ctx, models.DirectoryUser{AADObjectID: "stayed", TenantID: "t", Eligible: true, DirectorySeenAt: run}); err != nil {
			t.Fatal(err)
		}

		if n, err := st.MarkDirectoryUsersDeparted(ctx, run, run); err != nil || n != 1 {
			t.Fatalf("MarkDirectoryUsersDeparted() = %d, %v, want 1", n, err)
		}
		if n, _ := st.MarkDirectoryUsersDeparted(ctx, run, run); n != 0 {
			t.Errorf("a second MarkDirectoryUsersDeparted() = %d, want 0", n)
		}
		if got, _ := st.GetDirectoryUser(ctx, "left"); got.InstallState != models.InstallDeparted {
			t.Errorf("left = %s, want departed", got.InstallState)
		}

		// Seen again: back to unknown and due.
		if err := st.UpsertDirectoryUser(ctx, models.DirectoryUser{AADObjectID: "left", TenantID: "t", Eligible: true, DirectorySeenAt: run.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if got, _ := st.GetDirectoryUser(ctx, "left"); got.InstallState != models.InstallUnknown {
			t.Errorf("returning user = %s, want unknown", got.InstallState)
		}

		if _, err := st.MarkDirectoryUsersDeparted(ctx, run.Add(2*time.Hour), run.Add(2*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if n, err := st.PurgeDepartedDirectoryUsers(ctx, run.Add(time.Hour)); err != nil || n != 0 {
			t.Errorf("PurgeDepartedDirectoryUsers() of recent departures = %d, %v, want 0", n, err)
		}
		if n, err := st.PurgeDepartedDirectoryUsers(ctx, run.Add(3*time.Hour)); err != nil || n != 2 {
			t.Errorf("PurgeDepartedDirectoryUsers() = %d, %v, want 2", n, err)
		}
		if _, err := st.GetDirectoryUser(ctx, "stayed"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("purged user = %v, want ErrNotFound", err)
		}
	})
}

func TestConformanceDirectoryRuns(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

		if _, err := st.LatestDirectoryRun(ctx); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("LatestDirectoryRun() with none = %v, want ErrNotFound", err)
		}

		run, err := st.RequestDirectoryRun(ctx, models.DirectoryRun{Kind: models.RunManual, RequestedBy: "admin", RequestedAt: now})
		if err != nil {
			t.Fatalf("RequestDirectoryRun: %v", err)
		}
		if run.ID == "" || run.State != models.RunRequested || run.RequestedBy != "admin" {
			t.Errorf("requested run = %+v", run)
		}
		if _, err := st.RequestDirectoryRun(ctx, models.DirectoryRun{Kind: models.RunPeriodic, RequestedAt: now}); !errors.Is(err, store.ErrConflict) {
			t.Errorf("a second request while one is pending = %v, want ErrConflict", err)
		}

		if ok, err := st.ClaimDirectoryRun(ctx, run.ID, "replica-1", now, now.Add(-2*time.Minute)); err != nil || !ok {
			t.Fatalf("ClaimDirectoryRun() = %v, %v", ok, err)
		}
		if ok, _ := st.ClaimDirectoryRun(ctx, run.ID, "replica-2", now, now.Add(-2*time.Minute)); ok {
			t.Error("a second replica claimed a run with a fresh heartbeat")
		}
		if _, err := st.RequestDirectoryRun(ctx, models.DirectoryRun{Kind: models.RunPeriodic, RequestedAt: now}); !errors.Is(err, store.ErrConflict) {
			t.Errorf("a request while one is running = %v, want ErrConflict", err)
		}

		counts := models.RunCounts{Total: 10, Installed: 3, Already: 5, Failed: 1, Ineligible: 1}
		if ok, err := st.HeartbeatDirectoryRun(ctx, run.ID, "replica-1", counts, now.Add(time.Minute)); err != nil || !ok {
			t.Fatalf("HeartbeatDirectoryRun() = %v, %v", ok, err)
		}

		// replica-1 goes quiet; replica-2 takes over once the heartbeat is stale.
		later := now.Add(10 * time.Minute)
		if ok, err := st.ClaimDirectoryRun(ctx, run.ID, "replica-2", later, later.Add(-2*time.Minute)); err != nil || !ok {
			t.Fatalf("takeover ClaimDirectoryRun() = %v, %v", ok, err)
		}
		if ok, _ := st.HeartbeatDirectoryRun(ctx, run.ID, "replica-1", counts, later); ok {
			t.Error("the replica that lost the run could still write a heartbeat")
		}
		if ok, _ := st.FinishDirectoryRun(ctx, run.ID, "replica-1", models.RunDone, counts, "", later); ok {
			t.Error("the replica that lost the run could still finish it")
		}

		if ok, err := st.FinishDirectoryRun(ctx, run.ID, "replica-2", models.RunDone, counts, "", later); err != nil || !ok {
			t.Fatalf("FinishDirectoryRun() = %v, %v", ok, err)
		}
		got, err := st.LatestDirectoryRun(ctx)
		if err != nil {
			t.Fatalf("LatestDirectoryRun: %v", err)
		}
		if got.State != models.RunDone || got.Owner != "replica-2" || got.RunCounts != counts || !got.StartedAt.Equal(now) || !got.FinishedAt.Equal(later) {
			t.Errorf("finished run = %+v", got)
		}

		// Finished, so another can be requested.
		next, err := st.RequestDirectoryRun(ctx, models.DirectoryRun{Kind: models.RunPeriodic, RequestedAt: later.Add(time.Minute)})
		if err != nil {
			t.Fatalf("RequestDirectoryRun after finishing: %v", err)
		}
		if latest, _ := st.LatestDirectoryRun(ctx); latest.ID != next.ID {
			t.Errorf("LatestDirectoryRun() = %s, want the new run %s", latest.ID, next.ID)
		}

		if n, err := st.PruneDirectoryRuns(ctx, later.Add(time.Second)); err != nil || n != 1 {
			t.Errorf("PruneDirectoryRuns() = %d, %v, want the finished run only", n, err)
		}
		if _, err := st.GetDirectoryRun(ctx, run.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("pruned run = %v, want ErrNotFound", err)
		}
	})
}

func TestConformanceConcurrentDirectoryRunRequests(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		now := time.Now().UTC()

		const replicas = 8
		errs := make([]error, replicas)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range replicas {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, errs[i] = st.RequestDirectoryRun(ctx, models.DirectoryRun{Kind: models.RunPeriodic, RequestedBy: fmt.Sprintf("replica-%d", i), RequestedAt: now})
			}()
		}
		close(start)
		wg.Wait()

		var won int
		for i, err := range errs {
			switch {
			case err == nil:
				won++
			case !errors.Is(err, store.ErrConflict):
				t.Errorf("replica %d: %v", i, err)
			}
		}
		if won != 1 {
			t.Errorf("%d replicas requested a run, want exactly 1", won)
		}
	})
}
