package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func TestConformanceBroadcasts(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		t0 := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

		first, err := st.CreateBroadcast(ctx, models.Broadcast{RequestedBy: "olga", TokenName: "notices", RequestedAt: t0, Event: []byte(`{"text":"a"}`), Plan: []byte(`[]`)})
		if err != nil {
			t.Fatalf("CreateBroadcast: %v", err)
		}
		if first.ID == "" || first.State != models.RunRequested || string(first.Event) != `{"text":"a"}` {
			t.Fatalf("created = %+v", first)
		}
		second, err := st.CreateBroadcast(ctx, models.Broadcast{RequestedBy: "ada", RequestedAt: t0.Add(time.Minute), Event: []byte(`{}`), Plan: []byte(`[]`)})
		if err != nil {
			t.Fatal(err)
		}

		next, err := st.NextBroadcast(ctx, t0)
		if err != nil || next.ID != first.ID {
			t.Fatalf("NextBroadcast = %+v, %v; want the oldest", next, err)
		}
		if ok, err := st.ClaimBroadcast(ctx, first.ID, "r1", t0, t0.Add(-time.Minute)); !ok || err != nil {
			t.Fatalf("ClaimBroadcast = %v, %v", ok, err)
		}
		if ok, _ := st.ClaimBroadcast(ctx, first.ID, "r2", t0, t0.Add(-time.Minute)); ok {
			t.Error("a live broadcast was claimed twice")
		}
		counts := models.BroadcastCounts{Total: 3, Delivered: 1}
		if ok, err := st.HeartbeatBroadcast(ctx, first.ID, "r1", "aad:1", counts, t0.Add(time.Second)); !ok || err != nil {
			t.Fatalf("HeartbeatBroadcast = %v, %v", ok, err)
		}
		if ok, _ := st.HeartbeatBroadcast(ctx, first.ID, "r2", "aad:9", counts, t0); ok {
			t.Error("another owner's heartbeat was accepted")
		}

		// Gone stale, it is next again and can be taken over.
		next, _ = st.NextBroadcast(ctx, t0.Add(time.Hour))
		if next.ID != first.ID {
			t.Errorf("a stale broadcast is not next: %+v", next)
		}
		if ok, _ := st.ClaimBroadcast(ctx, first.ID, "r2", t0.Add(time.Hour), t0.Add(time.Hour)); !ok {
			t.Fatal("a stale broadcast was not taken over")
		}
		counts.Delivered = 3
		if ok, err := st.FinishBroadcast(ctx, first.ID, "r2", models.RunDone, "rcp:z", counts, "", t0.Add(2*time.Hour)); !ok || err != nil {
			t.Fatalf("FinishBroadcast = %v, %v", ok, err)
		}
		got, err := st.GetBroadcast(ctx, first.ID)
		if err != nil || got.State != models.RunDone || got.Cursor != "rcp:z" || got.Delivered != 3 || got.Owner != "r2" || !got.StartedAt.Equal(t0) {
			t.Errorf("GetBroadcast = %+v, %v", got, err)
		}

		mine, err := st.ListBroadcasts(ctx, "olga", 10)
		if err != nil || len(mine) != 1 || mine[0].ID != first.ID {
			t.Errorf("ListBroadcasts(olga) = %+v, %v", mine, err)
		}
		all, _ := st.ListBroadcasts(ctx, "", 10)
		if len(all) != 2 || all[0].ID != second.ID {
			t.Errorf("ListBroadcasts() = %+v, want newest first", all)
		}

		if n, err := st.PruneBroadcasts(ctx, t0.Add(3*time.Hour)); n != 1 || err != nil {
			t.Errorf("PruneBroadcasts = %d, %v; want the finished one", n, err)
		}
		if _, err := st.GetBroadcast(ctx, first.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("pruned broadcast = %v", err)
		}
	})
}

func TestConformanceReachableDirectoryUsers(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
		for _, id := range []string{"oid-a", "oid-b", "oid-c", "oid-d"} {
			if err := st.UpsertDirectoryUser(ctx, models.DirectoryUser{AADObjectID: id, TenantID: "t", UserPrincipalName: id + "@corp.example", Eligible: id != "oid-d"}); err != nil {
				t.Fatal(err)
			}
		}
		for _, id := range []string{"oid-a", "oid-b", "oid-d"} {
			if err := st.SetDirectoryUserInstalled(ctx, id, "a:"+id, "https://smba.example/", at); err != nil {
				t.Fatal(err)
			}
		}
		// Blocked is no gate (ADR 0026).
		if err := st.MarkDirectoryUserBlocked(ctx, "oid-b", at, "blocked"); err != nil {
			t.Fatal(err)
		}

		ids := func(after string, limit int) string {
			users, err := st.ListReachableDirectoryUsers(ctx, after, limit)
			if err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, u := range users {
				out = append(out, u.AADObjectID)
			}
			return strings.Join(out, ",")
		}
		if got := ids("", 10); got != "oid-a,oid-b" {
			t.Errorf("reachable = %s, want the installed eligible ones", got)
		}
		if got := ids("oid-a", 10); got != "oid-b" {
			t.Errorf("after oid-a = %s", got)
		}
		if got := ids("", 1); got != "oid-a" {
			t.Errorf("one page = %s", got)
		}
	})
}
