package store_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func TestConformancePermissions(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		generation := func() int64 {
			t.Helper()
			g, err := st.AuthzGeneration(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return g
		}

		owner, err := st.PutPermission(ctx, models.Permission{
			PrincipalType: models.PrincipalUser, PrincipalID: "alice", ResourceType: "Template", ResourceID: "t1",
			Actions: []string{"own"}, CreatedBy: "alice",
		})
		if err != nil || owner.ID == "" || generation() != 1 {
			t.Fatalf("PutPermission = %+v, %v at generation %d", owner, err, generation())
		}
		shared, err := st.PutPermission(ctx, models.Permission{
			PrincipalType: models.PrincipalGroup, PrincipalID: "g-sre", ResourceType: "Template", ResourceID: "t1",
			Actions: []string{"read"},
		})
		if err != nil {
			t.Fatal(err)
		}
		// Granting again replaces the actions on the same row.
		again, err := st.PutPermission(ctx, models.Permission{
			PrincipalType: models.PrincipalGroup, PrincipalID: "g-sre", ResourceType: "Template", ResourceID: "t1",
			Actions: []string{"read", "update"},
		})
		if err != nil || again.ID != shared.ID || !slices.Equal(again.Actions, []string{"read", "update"}) {
			t.Errorf("regranting = %+v, %v, want the same row with both actions", again, err)
		}
		if _, err := st.PutPermission(ctx, models.Permission{PrincipalType: models.PrincipalRole, PrincipalID: "editor", ResourceType: "Route", ResourceID: "*", Actions: []string{"create"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.PutPermission(ctx, models.Permission{PrincipalType: "robot", PrincipalID: "x", ResourceType: "Route", ResourceID: "*", Actions: []string{"create"}}); err == nil {
			t.Error("an unknown principal type was accepted")
		}

		rows, err := st.ListPermissionsFor(ctx, "Template", "t1")
		if err != nil || len(rows) != 2 || rows[0].ID != owner.ID {
			t.Errorf("ListPermissionsFor = %+v, %v", rows, err)
		}
		all, err := st.ListPermissions(ctx)
		if err != nil || len(all) != 3 {
			t.Errorf("ListPermissions = %d, %v", len(all), err)
		}
		got, err := st.GetPermission(ctx, owner.ID)
		if err != nil || got.PrincipalID != "alice" || got.CreatedBy != "alice" {
			t.Errorf("GetPermission = %+v, %v", got, err)
		}

		// No actions left deletes the row.
		before := generation()
		if _, err := st.PutPermission(ctx, models.Permission{PrincipalType: models.PrincipalGroup, PrincipalID: "g-sre", ResourceType: "Template", ResourceID: "t1"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.GetPermission(ctx, shared.ID); !errors.Is(err, store.ErrNotFound) || generation() != before+1 {
			t.Errorf("an empty grant left the row: %v", err)
		}

		if err := st.DeletePermission(ctx, owner.ID); err != nil {
			t.Fatal(err)
		}
		if err := st.DeletePermission(ctx, owner.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("deleting twice = %v", err)
		}

		if _, err := st.PutPermission(ctx, models.Permission{PrincipalType: models.PrincipalUser, PrincipalID: "bob", ResourceType: "Destination", ResourceID: "d1", Actions: []string{"read"}}); err != nil {
			t.Fatal(err)
		}
		if err := st.DeletePermissionsFor(ctx, "Destination", "d1"); err != nil {
			t.Fatal(err)
		}
		if rows, _ := st.ListPermissionsFor(ctx, "Destination", "d1"); len(rows) != 0 {
			t.Errorf("rows left after DeletePermissionsFor: %+v", rows)
		}
	})
}

func TestConformanceDeletingAGroupTakesItsPermissions(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		g, err := st.CreateGroup(ctx, models.Group{Name: "sre"})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []models.Permission{
			{PrincipalType: models.PrincipalGroup, PrincipalID: g.ID, ResourceType: "Template", ResourceID: "t1", Actions: []string{"read"}},
			{PrincipalType: models.PrincipalUser, PrincipalID: "alice", ResourceType: "Group", ResourceID: g.ID, Actions: []string{"own"}},
			{PrincipalType: models.PrincipalUser, PrincipalID: "alice", ResourceType: "Template", ResourceID: "t1", Actions: []string{"own"}},
		} {
			if _, err := st.PutPermission(ctx, p); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.DeleteGroup(ctx, g.ID); err != nil {
			t.Fatal(err)
		}
		all, err := st.ListPermissions(ctx)
		if err != nil || len(all) != 1 || all[0].PrincipalID != "alice" || all[0].ResourceType != "Template" {
			t.Errorf("left %+v, %v, want only alice's template", all, err)
		}
	})
}
