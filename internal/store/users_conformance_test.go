package store_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func TestConformanceUsersSignIn(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		t0 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

		first := models.User{Subject: "s-1", Source: "oidc", Name: "Alice", Email: "alice@example.com", Roles: []string{"editor"}, IdPGroups: []string{"sre"}, LastSeen: t0, ObjectID: "oid-1"}
		if err := st.RecordSignIn(ctx, first); err != nil {
			t.Fatalf("RecordSignIn: %v", err)
		}
		again := first
		again.Name, again.Roles, again.IdPGroups, again.LastSeen = "Alice A.", []string{"admin", "viewer"}, nil, t0.Add(time.Hour)
		// A sign-in without the claim must not forget the object id.
		again.ObjectID = ""
		if err := st.RecordSignIn(ctx, again); err != nil {
			t.Fatalf("RecordSignIn again: %v", err)
		}

		got, err := st.GetUser(ctx, "s-1")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		tests := []struct {
			name      string
			got, want any
		}{
			{"name refreshed", got.Name, "Alice A."},
			{"roles refreshed", slices.Equal(got.Roles, []string{"admin", "viewer"}), true},
			{"groups cleared", len(got.IdPGroups), 0},
			{"first_seen kept", got.FirstSeen.Equal(t0), true},
			{"last_seen moved", got.LastSeen.Equal(t0.Add(time.Hour)), true},
			{"not disabled", got.Disabled(), false},
			{"object id kept", got.ObjectID, "oid-1"},
		}
		for _, tt := range tests {
			if tt.got != tt.want {
				t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.want)
			}
		}

		if _, err := st.GetUser(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetUser(nobody) = %v, want ErrNotFound", err)
		}
	})
}

func TestConformanceUsersList(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		for _, u := range []models.User{
			{Subject: "s-b", Source: "oidc", Name: "Bob", Email: "bob@example.com"},
			{Subject: "s-a", Source: "oidc", Name: "alice", Email: "alice@corp.example"},
			{Subject: "admin", Source: "local", Name: "admin"},
		} {
			if err := st.RecordSignIn(ctx, u); err != nil {
				t.Fatalf("RecordSignIn(%s): %v", u.Subject, err)
			}
		}

		tests := []struct {
			search string
			limit  int
			want   []string
		}{
			{"", 0, []string{"admin", "s-a", "s-b"}},
			{"", 2, []string{"admin", "s-a"}},
			{"BOB", 0, []string{"s-b"}},
			{"corp", 0, []string{"s-a"}},
			{"s-", 0, []string{"s-a", "s-b"}},
			{"nobody", 0, []string{}},
		}
		for _, tt := range tests {
			users, err := st.ListUsers(ctx, tt.search, tt.limit)
			if err != nil {
				t.Fatalf("ListUsers(%q): %v", tt.search, err)
			}
			got := []string{}
			for _, u := range users {
				got = append(got, u.Subject)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ListUsers(%q, %d) = %v, want %v", tt.search, tt.limit, got, tt.want)
			}
		}
	})
}

func TestConformanceDisableUserEndsSessions(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		now := time.Now().UTC()

		if err := st.RecordSignIn(ctx, models.User{Subject: "s-1", Source: "oidc"}); err != nil {
			t.Fatal(err)
		}
		for _, s := range []models.Session{
			{ID: "mine", Subject: "s-1", Source: "oidc", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
			{ID: "theirs", Subject: "s-2", Source: "oidc", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		} {
			if err := st.CreateSession(ctx, s); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateBrokerToken(ctx, models.BrokerToken{SessionID: s.ID, AccessToken: "a", RefreshToken: "r", ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
		}

		if err := st.DisableUser(ctx, "s-1", "admin"); err != nil {
			t.Fatalf("DisableUser: %v", err)
		}
		u, err := st.GetUser(ctx, "s-1")
		if err != nil || !u.Disabled() || u.DisabledBy != "admin" {
			t.Errorf("after disable: %+v, %v", u, err)
		}
		if _, err := st.GetSession(ctx, "mine"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the disabled user's session survived: %v", err)
		}
		if _, err := st.GetBrokerToken(ctx, "mine"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the disabled user's broker token survived: %v", err)
		}
		if _, err := st.GetSession(ctx, "theirs"); err != nil {
			t.Errorf("another user's session ended: %v", err)
		}
		if _, err := st.GetBrokerToken(ctx, "theirs"); err != nil {
			t.Errorf("another user's broker token went: %v", err)
		}

		// A sign-in after disabling keeps the user disabled.
		if err := st.RecordSignIn(ctx, models.User{Subject: "s-1", Source: "oidc", Name: "again"}); err != nil {
			t.Fatal(err)
		}
		if u, _ := st.GetUser(ctx, "s-1"); !u.Disabled() {
			t.Error("a sign-in re-enabled a disabled user")
		}

		if err := st.EnableUser(ctx, "s-1"); err != nil {
			t.Fatalf("EnableUser: %v", err)
		}
		if u, _ := st.GetUser(ctx, "s-1"); u.Disabled() || u.DisabledBy != "" {
			t.Errorf("after enable: %+v", u)
		}

		for name, err := range map[string]error{
			"disable": st.DisableUser(ctx, "nobody", "admin"),
			"enable":  st.EnableUser(ctx, "nobody"),
		} {
			if !errors.Is(err, store.ErrNotFound) {
				t.Errorf("%s nobody = %v, want ErrNotFound", name, err)
			}
		}
	})
}
