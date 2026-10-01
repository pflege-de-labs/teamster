package store_test

import (
	"errors"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func TestConformanceGroups(t *testing.T) {
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

		oncall, err := st.CreateGroup(ctx, models.Group{Name: " On call ", Description: "pager", CreatedBy: "alice"})
		if err != nil {
			t.Fatalf("CreateGroup: %v", err)
		}
		if oncall.Name != "On call" || oncall.ID == "" || generation() != 1 {
			t.Fatalf("created %+v at generation %d", oncall, generation())
		}
		if _, err := st.CreateGroup(ctx, models.Group{Name: "On call"}); !errors.Is(err, store.ErrConflict) {
			t.Errorf("a duplicate name = %v, want ErrConflict", err)
		}
		sre, err := st.CreateGroup(ctx, models.Group{Name: "SRE"})
		if err != nil {
			t.Fatal(err)
		}

		members := []models.GroupMember{
			{GroupID: sre.ID, Type: models.MemberUser, ID: "bob", AddedBy: "alice"},
			{GroupID: sre.ID, Type: models.MemberIdPGroup, ID: "platform"},
			{GroupID: oncall.ID, Type: models.MemberGroup, ID: sre.ID},
		}
		for _, m := range members {
			if err := st.AddGroupMember(ctx, m); err != nil {
				t.Fatalf("AddGroupMember(%+v): %v", m, err)
			}
		}
		// Adding again is idempotent.
		if err := st.AddGroupMember(ctx, members[0]); err != nil {
			t.Errorf("adding a member twice = %v", err)
		}

		refusals := []struct {
			name string
			m    models.GroupMember
			want error
		}{
			{"itself", models.GroupMember{GroupID: sre.ID, Type: models.MemberGroup, ID: sre.ID}, store.ErrGroupCycle},
			{"through another", models.GroupMember{GroupID: sre.ID, Type: models.MemberGroup, ID: oncall.ID}, store.ErrGroupCycle},
			{"a missing group", models.GroupMember{GroupID: "nope", Type: models.MemberUser, ID: "bob"}, store.ErrNotFound},
			{"a missing member group", models.GroupMember{GroupID: sre.ID, Type: models.MemberGroup, ID: "nope"}, store.ErrNotFound},
		}
		for _, tt := range refusals {
			if err := st.AddGroupMember(ctx, tt.m); !errors.Is(err, tt.want) {
				t.Errorf("adding %s = %v, want %v", tt.name, err, tt.want)
			}
		}
		if err := st.AddGroupMember(ctx, models.GroupMember{GroupID: sre.ID, Type: "role", ID: "x"}); err == nil {
			t.Error("an unknown member type was accepted")
		}

		got, err := st.ListGroupMembers(ctx, sre.ID)
		if err != nil || len(got) != 2 || got[0].Type != models.MemberIdPGroup || got[1].AddedBy != "alice" {
			t.Errorf("SRE members = %+v, %v", got, err)
		}
		all, err := st.ListAllGroupMembers(ctx)
		if err != nil || len(all) != 3 {
			t.Errorf("all members = %+v, %v", all, err)
		}

		before := generation()
		sre.Name, sre.Description = "Site reliability", "renamed"
		updated, err := st.UpdateGroup(ctx, sre)
		if err != nil || updated.Name != "Site reliability" || generation() != before+1 {
			t.Errorf("UpdateGroup = %+v, %v", updated, err)
		}
		if _, err := st.UpdateGroup(ctx, models.Group{ID: "nope", Name: "x"}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("updating a missing group = %v", err)
		}
		if _, err := st.UpdateGroup(ctx, models.Group{ID: sre.ID, Name: "On call"}); !errors.Is(err, store.ErrConflict) {
			t.Errorf("renaming onto another name = %v, want ErrConflict", err)
		}

		if err := st.RemoveGroupMember(ctx, members[0]); err != nil {
			t.Errorf("RemoveGroupMember: %v", err)
		}
		if err := st.RemoveGroupMember(ctx, members[0]); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("removing twice = %v, want ErrNotFound", err)
		}

		// Deleting SRE takes its members and its membership in On call along.
		if err := st.DeleteGroup(ctx, sre.ID); err != nil {
			t.Fatalf("DeleteGroup: %v", err)
		}
		if all, _ := st.ListAllGroupMembers(ctx); len(all) != 0 {
			t.Errorf("memberships left after deleting the group: %+v", all)
		}
		if err := st.DeleteGroup(ctx, sre.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("deleting twice = %v", err)
		}
		if _, err := st.GetGroup(ctx, sre.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetGroup after delete = %v", err)
		}
		groups, err := st.ListGroups(ctx)
		if err != nil || len(groups) != 1 || groups[0].ID != oncall.ID {
			t.Errorf("ListGroups = %+v, %v", groups, err)
		}
	})
}
