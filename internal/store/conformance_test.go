package store_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// A backend that passes this is interchangeable with the one that shipped.
//
// The two implementations share an adapter, so what this actually exercises is
// the part that cannot be shared: the SQL each dialect runs, and what the
// database does with it under concurrency. Everything asserted here is
// behaviour something above the store depends on.

// postgresDSN is the Postgres to test against. Without it the Postgres cases
// skip, so `go test ./...` works on a laptop with nothing installed -- but not
// in CI, where a skip would quietly mean the second backend is untested.
func postgresDSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("TEAMSTER_TEST_POSTGRES_DSN")
	if dsn != "" {
		return dsn
	}
	if os.Getenv("CI") != "" {
		t.Fatal("TEAMSTER_TEST_POSTGRES_DSN is unset in CI: the Postgres backend would go untested")
	}
	t.Skip("set TEAMSTER_TEST_POSTGRES_DSN to run the Postgres conformance tests")
	return ""
}

// eachBackend runs fn against every backend this build carries.
func eachBackend(t *testing.T, fn func(t *testing.T, open func(t *testing.T) store.Store)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		fn(t, openSQLite)
	})
	t.Run("postgres", func(t *testing.T) {
		t.Parallel()
		fn(t, openPostgres)
	})
}

func openSQLite(t *testing.T) store.Store {
	t.Helper()

	st, err := store.Open(t.Context(), store.Options{
		Driver:  store.DriverSQLite,
		Path:    filepath.Join(t.TempDir(), "conformance.db"),
		Migrate: store.MigrateAuto,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// openPostgres gives each test a schema of its own, so the suite keeps its
// t.Parallel() against one server. Migrations then run per schema, which makes
// every test a migration test as well.
func openPostgres(t *testing.T) store.Store {
	t.Helper()

	dsn := postgresDSN(t)
	schema := fmt.Sprintf("t_%d_%d", time.Now().UnixNano(), rand.N(1000))

	admin, err := store.Open(t.Context(), store.Options{
		Driver: store.DriverPostgres, DSN: dsn, Migrate: store.MigrateOff,
	})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := store.CreateSchemaForTest(t.Context(), admin, schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_ = store.DropSchemaForTest(context.Background(), admin, schema)
		_ = admin.Close()
	})

	st, err := store.Open(t.Context(), store.Options{
		Driver:  store.DriverPostgres,
		DSN:     dsn + "&search_path=" + schema,
		Migrate: store.MigrateAuto,
	})
	if err != nil {
		t.Fatalf("open postgres schema: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestConformanceTemplates(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()

		created, err := st.CreateTemplate(ctx, models.Template{Name: "Card", Body: "{}"})
		if err != nil {
			t.Fatalf("CreateTemplate: %v", err)
		}
		if created.ID == "" {
			t.Error("CreateTemplate() returned no id, want one generated")
		}

		got, err := st.GetTemplate(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetTemplate: %v", err)
		}
		// Postgres keeps microseconds and SQLite keeps nanoseconds, so an exact
		// comparison fails on Postgres. Truncate rather than round: Postgres
		// truncates, so rounding disagrees with it whenever the digits it drops
		// come to half a microsecond or more -- which is a test that passes
		// most of the time, the worst kind.
		if !got.CreatedAt.Truncate(time.Microsecond).Equal(created.CreatedAt.Truncate(time.Microsecond)) {
			t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created.CreatedAt)
		}
		if got.Name != "Card" {
			t.Errorf("template = %+v, want the one created", got)
		}

		if _, err := st.GetTemplate(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetTemplate(missing) = %v, want ErrNotFound", err)
		}
	})
}

// A route carries the two things the dialects disagree about underneath: a
// boolean SQLite has no type for, and a selector stored as JSON text.
func TestConformanceRoutes(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()

		created, err := st.CreateRoute(ctx, models.Route{
			Name:          "critical",
			Greedy:        true,
			IsDefault:     false,
			Priority:      100,
			LabelSelector: map[string]string{"severity": "critical"},
		})
		if err != nil {
			t.Fatalf("CreateRoute: %v", err)
		}

		got, err := st.GetRoute(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetRoute: %v", err)
		}
		if !got.Greedy || got.IsDefault {
			t.Errorf("route = %+v, want greedy and not default", got)
		}
		if got.Priority != 100 {
			t.Errorf("Priority = %d, want 100", got.Priority)
		}
		if got.LabelSelector["severity"] != "critical" {
			t.Errorf("LabelSelector = %v, want the selector round-tripped", got.LabelSelector)
		}
	})
}

func TestConformanceGrantScopeIsUnique(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		grant := models.Grant{Role: "editor", TeamID: "team", ChannelID: "channel"}

		if _, err := st.CreateGrant(ctx, grant); err != nil {
			t.Fatalf("CreateGrant: %v", err)
		}
		if _, err := st.CreateGrant(ctx, grant); err == nil {
			t.Error("the same scope twice = nil error, want the constraint to refuse it")
		}
	})
}

func TestConformanceSessionsAndFlows(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()

		expired := models.Session{ID: "expired", Subject: "s", ExpiresAt: time.Now().Add(-time.Minute)}
		if err := st.CreateSession(ctx, expired); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if _, err := st.GetSession(ctx, "expired"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("an expired session = %v, want ErrNotFound", err)
		}

		flow := models.LoginFlow{State: "state", Verifier: "v", Nonce: "n", ExpiresAt: time.Now().Add(time.Minute)}
		if err := st.CreateLoginFlow(ctx, flow); err != nil {
			t.Fatalf("CreateLoginFlow: %v", err)
		}
		if _, err := st.TakeLoginFlow(ctx, "state"); err != nil {
			t.Fatalf("TakeLoginFlow: %v", err)
		}
		// Taking it spends it, whether or not it had expired.
		if _, err := st.TakeLoginFlow(ctx, "state"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("taking a flow twice = %v, want ErrNotFound", err)
		}
	})
}

func TestConformanceRecipients(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()

		created, err := st.CreateRecipient(ctx, models.Recipient{
			Subject:        "alice@example.com",
			Name:           "Alice",
			AADObjectID:    "aad-1",
			ConversationID: "conversation-1",
			ServiceURL:     "https://smba.example.invalid/emea/",
			BotChannelID:   "msteams",
			TenantID:       "tenant-1",
		})
		if err != nil {
			t.Fatalf("CreateRecipient: %v", err)
		}
		if created.ID == "" {
			t.Error("CreateRecipient() returned no id, want one generated")
		}

		got, err := st.GetRecipient(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetRecipient: %v", err)
		}
		if got.Name != created.Name || got.AADObjectID != created.AADObjectID ||
			got.ConversationID != created.ConversationID || got.ServiceURL != created.ServiceURL ||
			got.BotChannelID != created.BotChannelID || got.TenantID != created.TenantID {
			t.Errorf("recipient = %+v, want the conversation reference round-tripped from %+v", got, created)
		}
		// Postgres keeps microseconds and SQLite nanoseconds, so the stamps are
		// only equal once both are truncated -- and truncated rather than
		// rounded, because Postgres truncates.
		if !got.CreatedAt.Truncate(time.Microsecond).Equal(created.CreatedAt.Truncate(time.Microsecond)) {
			t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created.CreatedAt)
		}

		bySubject, err := st.GetRecipientBySubject(ctx, "alice@example.com")
		if err != nil {
			t.Fatalf("GetRecipientBySubject: %v", err)
		}
		if bySubject.ID != created.ID {
			t.Errorf("GetRecipientBySubject() = %q, want %q", bySubject.ID, created.ID)
		}

		// Re-linking replaces the conversation and leaves the person alone.
		relinked := got
		relinked.Subject = "someone-else@example.com"
		relinked.ConversationID = "conversation-2"
		if _, err := st.UpdateRecipient(ctx, relinked); err != nil {
			t.Fatalf("UpdateRecipient: %v", err)
		}
		after, err := st.GetRecipient(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetRecipient after update: %v", err)
		}
		if after.ConversationID != "conversation-2" {
			t.Errorf("ConversationID = %q, want the re-linked conversation", after.ConversationID)
		}
		if after.Subject != "alice@example.com" {
			t.Errorf("Subject = %q, want the update to have left it alone", after.Subject)
		}

		if err := st.DeleteRecipient(ctx, created.ID); err != nil {
			t.Fatalf("DeleteRecipient: %v", err)
		}
		if _, err := st.GetRecipient(ctx, created.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("after the delete = %v, want ErrNotFound", err)
		}
		if _, err := st.GetRecipientBySubject(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetRecipientBySubject(nobody) = %v, want ErrNotFound", err)
		}
	})
}

// One person, one binding: a second row for the same subject would deliver
// every alert twice.
func TestConformanceRecipientSubjectIsUnique(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		recipient := models.Recipient{Subject: "alice", ConversationID: "c", ServiceURL: "u", BotChannelID: "msteams"}

		if _, err := st.CreateRecipient(ctx, recipient); err != nil {
			t.Fatalf("CreateRecipient: %v", err)
		}
		if _, err := st.CreateRecipient(ctx, recipient); err == nil {
			t.Error("the same subject twice = nil error, want the constraint to refuse it")
		}
	})
}

func TestConformanceLinkFlows(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()

		flow := models.LinkFlow{Code: "ABC123", Subject: "alice", ExpiresAt: time.Now().Add(time.Minute)}
		if err := st.CreateLinkFlow(ctx, flow); err != nil {
			t.Fatalf("CreateLinkFlow: %v", err)
		}

		taken, err := st.TakeLinkFlow(ctx, "ABC123")
		if err != nil {
			t.Fatalf("TakeLinkFlow: %v", err)
		}
		if taken.Subject != "alice" {
			t.Errorf("Subject = %q, want the subject the code was issued to", taken.Subject)
		}

		// A code is spent by being taken, so one read over a shoulder binds
		// nothing the second time.
		if _, err := st.TakeLinkFlow(ctx, "ABC123"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("taking a code twice = %v, want ErrNotFound", err)
		}

		expired := models.LinkFlow{Code: "OLD", Subject: "alice", ExpiresAt: time.Now().Add(-time.Minute)}
		if err := st.CreateLinkFlow(ctx, expired); err != nil {
			t.Fatalf("CreateLinkFlow(expired): %v", err)
		}
		if _, err := st.TakeLinkFlow(ctx, "OLD"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("an expired code = %v, want ErrNotFound", err)
		}
		// Expired or not, taking it spent it: the row is gone.
		if _, err := st.TakeLinkFlow(ctx, "OLD"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the expired code survived being taken: %v", err)
		}
	})
}

// The claim is the reason a second backend has to behave identically: if
// Postgres let two writers acquire the same card, HA would post duplicates.
func TestConformanceConcurrentClaims(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		now := time.Now().UTC()

		const writers = 8
		outcomes := make([]store.ClaimOutcome, writers)
		errs := make([]error, writers)
		start := make(chan struct{})

		var wg sync.WaitGroup
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, outcome, err := st.ClaimActiveAlert(ctx, models.AlertClaim{
					Fingerprint: "fp",
					TeamID:      "team",
					ChannelID:   "channel",
					Status:      "firing",
					Owner:       fmt.Sprintf("owner-%d", i),
					At:          now,
					StaleBefore: now.Add(-time.Minute),
				})
				outcomes[i], errs[i] = outcome, err
			}()
		}
		close(start)
		wg.Wait()

		var acquired int
		for i, outcome := range outcomes {
			if errs[i] != nil {
				t.Errorf("writer %d: %v", i, errs[i])
				continue
			}
			if outcome == store.ClaimAcquired {
				acquired++
			}
		}
		if acquired != 1 {
			t.Errorf("%d writers acquired the card, want exactly 1", acquired)
		}
	})
}

func TestConformanceClaimLifecycle(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		now := time.Now().UTC()

		claim := models.AlertClaim{
			Fingerprint: "fp", TeamID: "team", ChannelID: "channel", Status: "firing",
			Owner: "mine", At: now, StaleBefore: now.Add(-time.Minute),
		}

		card, outcome, err := st.ClaimActiveAlert(ctx, claim)
		if err != nil || outcome != store.ClaimAcquired {
			t.Fatalf("claim = %v/%v, want acquired", outcome, err)
		}
		if card.Posted() {
			t.Error("a fresh claim reports a card, want none yet")
		}

		if err := st.CompleteActiveAlertClaim(ctx, claim, "message-1", now); err != nil {
			t.Fatalf("CompleteActiveAlertClaim: %v", err)
		}

		// A second writer now finds a card rather than a claim.
		theirs := claim
		theirs.Owner = "theirs"
		existing, outcome, err := st.ClaimActiveAlert(ctx, theirs)
		if err != nil || outcome != store.ClaimPosted {
			t.Fatalf("claim over a card = %v/%v, want posted", outcome, err)
		}
		if existing.MessageID != "message-1" {
			t.Errorf("MessageID = %q, want the card that exists", existing.MessageID)
		}

		// And completing the claim they never had must not take it from us.
		if err := st.CompleteActiveAlertClaim(ctx, theirs, "message-2", now); !errors.Is(err, store.ErrClaimLost) {
			t.Errorf("completing a lost claim = %v, want ErrClaimLost", err)
		}

		if err := st.DeleteActiveAlertCard(ctx, "fp", "team", "channel", "message-1"); err != nil {
			t.Fatalf("DeleteActiveAlertCard: %v", err)
		}
		if _, err := st.GetActiveAlert(ctx, "fp", "team", "channel"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("after the delete = %v, want ErrNotFound", err)
		}
	})
}

func TestConformanceTransactionsRollBack(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		refused := errors.New("no")

		err := st.WithTx(ctx, func(ctx context.Context, tx store.Store) error {
			if _, err := tx.CreateTemplate(ctx, models.Template{ID: "rolled-back", Name: "x", Body: "{}"}); err != nil {
				return err
			}
			return refused
		})
		if !errors.Is(err, refused) {
			t.Fatalf("WithTx = %v, want the error the function returned", err)
		}
		if _, err := st.GetTemplate(ctx, "rolled-back"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the template survived the rollback: %v", err)
		}

		// And the three things a transaction cannot do to its own store.
		err = st.WithTx(ctx, func(ctx context.Context, tx store.Store) error {
			if err := tx.Ping(ctx); err == nil {
				t.Error("Ping inside a transaction = nil, want a refusal")
			}
			if err := tx.Close(); err == nil {
				t.Error("Close inside a transaction = nil, want a refusal")
			}
			return tx.WithTx(ctx, func(context.Context, store.Store) error { return nil })
		})
		if err == nil {
			t.Error("a nested WithTx = nil, want a refusal")
		}
	})
}
