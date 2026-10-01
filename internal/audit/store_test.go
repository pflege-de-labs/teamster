package audit

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func openStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), store.Options{
		Driver:  store.DriverSQLite,
		Path:    filepath.Join(t.TempDir(), "audit.db"),
		Migrate: store.MigrateAuto,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// audited returns a wrapped store whose events land in the same database,
// as serve wires it.
func audited(t *testing.T) (store.Store, store.Store) {
	t.Helper()
	inner := openStore(t)
	rec := NewRecorder(discard(), nil, 8, NewDBSink(inner))
	t.Cleanup(func() { _ = rec.Close(context.Background()) })
	return Wrap(inner, rec), inner
}

func trail(t *testing.T, st store.Store) []models.AuditEvent {
	t.Helper()
	events, err := st.ListAuditEvents(t.Context(), models.AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	// Oldest first reads like the steps a test took.
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events
}

func field(t *testing.T, raw json.RawMessage, key string) any {
	t.Helper()
	if raw == nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("snapshot %s: %v", raw, err)
	}
	return m[key]
}

type step struct {
	action, typ string
	before, aft any
}

func checkTrail(t *testing.T, events []models.AuditEvent, key string, want []step) {
	t.Helper()
	if len(events) != len(want) {
		actions := make([]string, 0, len(events))
		for _, e := range events {
			actions = append(actions, e.Action)
		}
		t.Fatalf("trail %v, want %d events", actions, len(want))
	}
	for i, w := range want {
		e := events[i]
		if e.Action != w.action || e.ResourceType != w.typ {
			t.Errorf("event %d: %s on %s, want %s on %s", i, e.Action, e.ResourceType, w.action, w.typ)
		}
		if got := field(t, e.Before, key); got != w.before {
			t.Errorf("event %d (%s): before.%s = %v, want %v", i, e.Action, key, got, w.before)
		}
		if got := field(t, e.After, key); got != w.aft {
			t.Errorf("event %d (%s): after.%s = %v, want %v", i, e.Action, key, got, w.aft)
		}
	}
}

func TestWrapRecordsChanges(t *testing.T) {
	t.Parallel()

	alice := models.Actor{Subject: "alice", Via: models.ViaSession}
	tests := []struct {
		name string
		key  string
		run  func(ctx context.Context, t *testing.T, st store.Store)
		want []step
	}{
		{
			name: "template lifecycle and defaults",
			key:  "name",
			run: func(ctx context.Context, t *testing.T, st store.Store) {
				tpl, err := st.CreateTemplate(ctx, models.Template{Name: "one", Body: `{}`})
				must(t, err)
				tpl.Name = "two"
				_, err = st.UpdateTemplate(ctx, tpl)
				must(t, err)
				must(t, st.SetGlobalDefaultTemplate(ctx, tpl.ID))
				must(t, st.SetSourceDefaultTemplate(ctx, "alertmanager", tpl.ID))
				must(t, st.SetGlobalDefaultTemplate(ctx, ""))
				must(t, st.SetSourceDefaultTemplate(ctx, "alertmanager", ""))
				must(t, st.DeleteTemplate(ctx, tpl.ID))
			},
			want: []step{
				{"template.create", TypeTemplate, nil, "one"},
				{"template.update", TypeTemplate, "one", "two"},
				{"setting.update", TypeSetting, nil, nil},
				{"setting.update", TypeSetting, nil, nil},
				{"setting.update", TypeSetting, nil, nil},
				{"setting.update", TypeSetting, nil, nil},
				{"template.delete", TypeTemplate, "two", nil},
			},
		},
		{
			name: "destinations, routes and the default",
			key:  "name",
			run: func(ctx context.Context, t *testing.T, st store.Store) {
				d, err := st.CreateDestination(ctx, models.Destination{Name: "ops", TeamID: "t", ChannelID: "c"})
				must(t, err)
				d.Name = "ops2"
				_, err = st.UpdateDestination(ctx, d)
				must(t, err)
				must(t, st.SetDefaultDestination(ctx, d.ID))
				r, err := st.CreateRoute(ctx, models.Route{Name: "r", DestinationID: d.ID, LabelSelector: map[string]string{"a": "b"}})
				must(t, err)
				r.Name = "r2"
				_, err = st.UpdateRoute(ctx, r)
				must(t, err)
				must(t, st.DeleteRoute(ctx, r.ID))
				must(t, st.DeleteDestination(ctx, d.ID))
			},
			want: []step{
				{"destination.create", TypeDestination, nil, "ops"},
				{"destination.update", TypeDestination, "ops", "ops2"},
				{"destination.default", TypeDestination, nil, nil},
				{"route.create", TypeRoute, nil, "r"},
				{"route.update", TypeRoute, "r", "r2"},
				{"route.delete", TypeRoute, "r2", nil},
				{"destination.delete", TypeDestination, "ops2", nil},
			},
		},
		{
			name: "credentials never reach the trail",
			key:  "token_hash",
			run: func(ctx context.Context, t *testing.T, st store.Store) {
				tok, err := st.CreateAccessToken(ctx, models.AccessToken{Name: "ci", TokenHash: "secret", CreatedBy: "alice"})
				must(t, err)
				must(t, st.DeleteAccessToken(ctx, tok.ID))
				d, err := st.CreateDestination(ctx, models.Destination{Name: "d", TeamID: "t", ChannelID: "c"})
				must(t, err)
				ep, err := st.CreateWebhookEndpoint(ctx, models.WebhookEndpoint{TeamSlug: "a", ChannelSlug: "b", DestinationID: d.ID, TokenHash: "secret"})
				must(t, err)
				_, err = st.UpdateWebhookEndpoint(ctx, ep)
				must(t, err)
				must(t, st.RotateWebhookEndpointToken(ctx, ep.ID, "secret2"))
				must(t, st.DeleteWebhookEndpoint(ctx, ep.ID))
			},
			want: []step{
				{"access_token.create", TypeAccessToken, nil, nil},
				{"access_token.delete", TypeAccessToken, nil, nil},
				{"destination.create", TypeDestination, nil, nil},
				{"webhook_endpoint.create", TypeWebhookEndpoint, nil, nil},
				{"webhook_endpoint.update", TypeWebhookEndpoint, nil, nil},
				{"webhook_endpoint.rotate", TypeWebhookEndpoint, nil, nil},
				{"webhook_endpoint.delete", TypeWebhookEndpoint, nil, nil},
			},
		},
		{
			name: "grants",
			key:  "role",
			run: func(ctx context.Context, t *testing.T, st store.Store) {
				g, err := st.CreateGrant(ctx, models.Grant{Role: "editor", TeamID: "t1"})
				must(t, err)
				_, err = st.CreateGrant(ctx, models.Grant{Role: "oncall", TeamID: "t1"})
				must(t, err)
				must(t, st.DeleteGrant(ctx, g.ID))
				must(t, st.DeleteGrantsForRole(ctx, "oncall"))
				must(t, st.DeleteGrantsForRole(ctx, "nobody"))
			},
			want: []step{
				{"grant.create", TypeGrant, nil, "editor"},
				{"grant.create", TypeGrant, nil, "oncall"},
				{"grant.delete", TypeGrant, "editor", nil},
				{"grant.delete", TypeGrant, "oncall", nil},
			},
		},
		{
			name: "recipients and manual runs, not periodic ones",
			key:  "subject",
			run: func(ctx context.Context, t *testing.T, st store.Store) {
				r, err := st.CreateRecipient(ctx, models.Recipient{Subject: "bob", Name: "Bob", ConversationID: "conv"})
				must(t, err)
				must(t, st.DeleteRecipient(ctx, r.ID))
				_, err = st.RequestDirectoryRun(ctx, models.DirectoryRun{Kind: models.RunPeriodic})
				must(t, err)
			},
			want: []step{
				{"recipient.create", TypeRecipient, nil, "bob"},
				{"recipient.delete", TypeRecipient, "bob", nil},
			},
		},
		{
			name: "disabling and enabling a user, not signing in",
			key:  "disabled_by",
			run: func(ctx context.Context, t *testing.T, st store.Store) {
				must(t, st.RecordSignIn(ctx, models.User{Subject: "bob", Source: "oidc"}))
				must(t, st.DisableUser(ctx, "bob", "alice"))
				must(t, st.EnableUser(ctx, "bob"))
			},
			want: []step{
				{"user.disable", TypeUser, nil, "alice"},
				{"user.enable", TypeUser, "alice", nil},
			},
		},
		{
			name: "writes that change nothing record nothing",
			key:  "name",
			run: func(ctx context.Context, t *testing.T, st store.Store) {
				// The store accepts these, but nothing changed.
				_, _ = st.UpdateTemplate(ctx, models.Template{ID: "missing", Name: "x", Body: `{}`})
				_ = st.DeleteRoute(ctx, "missing")
				_ = st.DeleteRecipient(ctx, "missing")
				if err := st.SetDefaultDestination(ctx, "missing"); err == nil {
					t.Error("defaulting to a missing destination succeeded")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st, inner := audited(t)
			ctx := WithActor(t.Context(), alice)
			tt.run(ctx, t, st)

			events := trail(t, inner)
			checkTrail(t, events, tt.key, tt.want)
			for _, e := range events {
				if e.Actor != alice {
					t.Errorf("%s: actor %+v", e.Action, e.Actor)
				}
			}
		})
	}
}

func TestWrapSeedsAsTeamster(t *testing.T) {
	t.Parallel()

	st, inner := audited(t)
	for range 2 {
		if _, err := st.SeedTemplates(t.Context(), []models.Template{{Name: "a", Body: `{}`}, {Name: "b", Body: `{}`}}); err != nil {
			t.Fatalf("SeedTemplates: %v", err)
		}
	}
	events := trail(t, inner)
	if len(events) != 1 || events[0].Action != "template.seed" || events[0].Actor.Via != models.ViaSystem {
		t.Fatalf("trail = %+v, want one seed by teamster", events)
	}
}

func TestWrapTransactions(t *testing.T) {
	t.Parallel()

	errAbort := errors.New("abort")
	tests := []struct {
		name string
		fail bool
		want int
	}{
		{"committed tx records", false, 2},
		{"rolled back tx records nothing", true, 0},
	}
	for _, tt := range tests {
		for _, serializable := range []bool{false, true} {
			name := tt.name
			if serializable {
				name += " (serializable)"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				// A memory sink: SQLite's one connection is the transaction's.
				sink := &memorySink{name: "memory"}
				st := Wrap(openStore(t), NewRecorder(discard(), nil, 1, sink))
				withTx := st.WithTx
				if serializable {
					withTx = st.WithSerializableTx
				}
				err := withTx(t.Context(), func(ctx context.Context, tx store.Store) error {
					if _, err := tx.CreateTemplate(ctx, models.Template{Name: "a", Body: `{}`}); err != nil {
						return err
					}
					if _, err := tx.CreateDestination(ctx, models.Destination{Name: "d", TeamID: "t", ChannelID: "c"}); err != nil {
						return err
					}
					if len(sink.recorded()) != 0 {
						t.Error("events were written before the commit")
					}
					if tt.fail {
						return errAbort
					}
					return nil
				})
				if tt.fail != errors.Is(err, errAbort) {
					t.Fatalf("tx = %v", err)
				}
				if got := len(sink.recorded()); got != tt.want {
					t.Errorf("%d events, want %d", got, tt.want)
				}
			})
		}
	}
}

func TestWrapWithoutRecorder(t *testing.T) {
	t.Parallel()

	inner := openStore(t)
	if Wrap(inner, nil) != inner {
		t.Error("Wrap without a recorder should return the store unchanged")
	}
}

func TestSnapshotOfUnencodable(t *testing.T) {
	t.Parallel()

	if got := snapshot(func() {}); got != nil {
		t.Errorf("snapshot(func) = %s", got)
	}
	if got := found(models.Template{}, errors.New("missing")); got != nil {
		t.Errorf("found with an error = %s", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// failingTrail refuses every audit insert made inside a transaction.
type failingTrail struct{ store.Store }

func (f failingTrail) WithSerializableTx(ctx context.Context, fn func(context.Context, store.Store) error) error {
	return f.Store.WithSerializableTx(ctx, func(ctx context.Context, tx store.Store) error {
		return fn(ctx, failingInsert{tx})
	})
}

type failingInsert struct{ store.Store }

func (failingInsert) InsertAuditEvent(context.Context, models.AuditEvent) error {
	return errors.New("disk full")
}

func TestGroupChangesAreRecordedInTheirTransaction(t *testing.T) {
	t.Parallel()

	alice := models.Actor{Subject: "alice", Via: models.ViaSession}

	t.Run("every change has its record", func(t *testing.T) {
		t.Parallel()
		st, inner := audited(t)
		ctx := WithActor(t.Context(), alice)
		g, err := st.CreateGroup(ctx, models.Group{Name: "oncall"})
		must(t, err)
		other, err := st.CreateGroup(ctx, models.Group{Name: "sre"})
		must(t, err)
		g.Name = "on-call"
		_, err = st.UpdateGroup(ctx, g)
		must(t, err)
		must(t, st.AddGroupMember(ctx, models.GroupMember{GroupID: g.ID, Type: models.MemberGroup, ID: other.ID}))
		must(t, st.RemoveGroupMember(ctx, models.GroupMember{GroupID: g.ID, Type: models.MemberGroup, ID: other.ID}))
		must(t, st.DeleteGroup(ctx, g.ID))
		// A refused change records nothing.
		if err := st.DeleteGroup(ctx, g.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("deleting twice = %v", err)
		}

		checkTrail(t, trail(t, inner), "name", []step{
			{"group.create", TypeGroup, nil, "oncall"},
			{"group.create", TypeGroup, nil, "sre"},
			{"group.update", TypeGroup, "oncall", "on-call"},
			{"group.member.add", TypeGroup, nil, nil},
			{"group.member.remove", TypeGroup, nil, nil},
			{"group.delete", TypeGroup, nil, nil},
		})
	})

	t.Run("no record, no change", func(t *testing.T) {
		t.Parallel()
		inner := openStore(t)
		rec := NewRecorder(discard(), nil, 1, NewDBSink(inner))
		st := Wrap(failingTrail{inner}, rec)
		if _, err := st.CreateGroup(t.Context(), models.Group{Name: "oncall"}); err == nil {
			t.Fatal("a group was created without its record")
		}
		if groups, _ := inner.ListGroups(t.Context()); len(groups) != 0 {
			t.Errorf("groups = %+v, want the change rolled back", groups)
		}
	})

	t.Run("inside an outer transaction the record is written once", func(t *testing.T) {
		t.Parallel()
		st, inner := audited(t)
		err := st.WithTx(t.Context(), func(ctx context.Context, tx store.Store) error {
			_, err := tx.CreateGroup(ctx, models.Group{Name: "oncall"})
			return err
		})
		must(t, err)
		if events := trail(t, inner); len(events) != 1 {
			t.Errorf("trail has %d events, want 1", len(events))
		}
	})

	t.Run("without a database trail it is recorded after the commit", func(t *testing.T) {
		t.Parallel()
		sink := &memorySink{name: "memory"}
		st := Wrap(openStore(t), NewRecorder(discard(), nil, 1, sink))
		_, err := st.CreateGroup(t.Context(), models.Group{Name: "oncall"})
		must(t, err)
		if got := sink.recorded(); len(got) != 1 || got[0].Action != "group.create" {
			t.Errorf("recorded %+v", got)
		}
	})
}
