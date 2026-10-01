package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

// The queryAdapter methods below are the statements; SQLiteStore and
// PostgresStore run each write in a transaction with the generation bump.

func (s queryAdapter) ListGroups(ctx context.Context) ([]models.Group, error) {
	rows, err := s.q.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	out := make([]models.Group, 0, len(rows))
	for _, row := range rows {
		out = append(out, groupFromRow(row))
	}
	return out, nil
}

func (s queryAdapter) GetGroup(ctx context.Context, id string) (models.Group, error) {
	row, err := s.q.GetGroup(ctx, id)
	if err != nil {
		return models.Group{}, notFound(err)
	}
	return groupFromRow(row), nil
}

func (s queryAdapter) CreateGroup(ctx context.Context, g models.Group) (models.Group, error) {
	g.ID = newID(g.ID)
	g.Name = strings.TrimSpace(g.Name)
	g.CreatedAt, g.UpdatedAt = nowUTC(), nowUTC()
	err := s.q.CreateGroup(ctx, sqlitedb.CreateGroupParams{
		ID: g.ID, Name: g.Name, Description: g.Description, CreatedBy: g.CreatedBy, At: g.CreatedAt,
	})
	if isPrimaryKeyConflict(err) {
		return models.Group{}, ErrConflict
	}
	if err != nil {
		return models.Group{}, fmt.Errorf("create group: %w", err)
	}
	return g, s.BumpAuthzGeneration(ctx)
}

func (s queryAdapter) UpdateGroup(ctx context.Context, g models.Group) (models.Group, error) {
	g.Name = strings.TrimSpace(g.Name)
	n, err := s.q.UpdateGroup(ctx, sqlitedb.UpdateGroupParams{ID: g.ID, Name: g.Name, Description: g.Description, At: nowUTC()})
	if isPrimaryKeyConflict(err) {
		return models.Group{}, ErrConflict
	}
	if err != nil {
		return models.Group{}, fmt.Errorf("update group: %w", err)
	}
	if n == 0 {
		return models.Group{}, ErrNotFound
	}
	updated, err := s.GetGroup(ctx, g.ID)
	if err != nil {
		return models.Group{}, err
	}
	return updated, s.BumpAuthzGeneration(ctx)
}

func (s queryAdapter) DeleteGroup(ctx context.Context, id string) error {
	if err := s.q.DeleteGroupMemberships(ctx, id); err != nil {
		return fmt.Errorf("delete group memberships: %w", err)
	}
	n, err := s.q.DeleteGroup(ctx, id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	// What the group held and what was held on it go with it.
	if err := s.q.DeletePermissionsForPrincipal(ctx, sqlitedb.DeletePermissionsForPrincipalParams{
		PrincipalType: string(models.PrincipalGroup), PrincipalID: id,
	}); err != nil {
		return fmt.Errorf("delete the group's permissions: %w", err)
	}
	if err := s.q.DeletePermissionsForResource(ctx, sqlitedb.DeletePermissionsForResourceParams{ResourceType: "Group", ResourceID: id}); err != nil {
		return fmt.Errorf("delete permissions on the group: %w", err)
	}
	return s.BumpAuthzGeneration(ctx)
}

func (s queryAdapter) ListGroupMembers(ctx context.Context, groupID string) ([]models.GroupMember, error) {
	rows, err := s.q.ListGroupMembers(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	return membersFromRows(rows), nil
}

func (s queryAdapter) ListAllGroupMembers(ctx context.Context) ([]models.GroupMember, error) {
	rows, err := s.q.ListAllGroupMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	return membersFromRows(rows), nil
}

// AddGroupMember checks the group, the member group and the cycle against the
// memberships this transaction sees; only a serializable one makes that hold.
func (s queryAdapter) AddGroupMember(ctx context.Context, m models.GroupMember) error {
	if !m.Type.Valid() || strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("a member needs a type of user, group or idp_group and an id")
	}
	if _, err := s.GetGroup(ctx, m.GroupID); err != nil {
		return err
	}
	if m.Type == models.MemberGroup {
		if _, err := s.GetGroup(ctx, m.ID); err != nil {
			return err
		}
		all, err := s.ListAllGroupMembers(ctx)
		if err != nil {
			return err
		}
		if wouldCycle(all, m.GroupID, m.ID) {
			return ErrGroupCycle
		}
	}
	err := s.q.AddGroupMember(ctx, sqlitedb.AddGroupMemberParams{
		GroupID: m.GroupID, MemberType: string(m.Type), MemberID: m.ID, AddedBy: m.AddedBy, AddedAt: nowUTC(),
	})
	if err != nil {
		return fmt.Errorf("add group member: %w", err)
	}
	return s.BumpAuthzGeneration(ctx)
}

func (s queryAdapter) RemoveGroupMember(ctx context.Context, m models.GroupMember) error {
	n, err := s.q.RemoveGroupMember(ctx, sqlitedb.RemoveGroupMemberParams{GroupID: m.GroupID, MemberType: string(m.Type), MemberID: m.ID})
	if err != nil {
		return fmt.Errorf("remove group member: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return s.BumpAuthzGeneration(ctx)
}

// wouldCycle reports whether making member a member of group closes a loop:
// whether group is already reachable from member through its member groups.
func wouldCycle(all []models.GroupMember, group, member string) bool {
	children := map[string][]string{}
	for _, m := range all {
		if m.Type == models.MemberGroup {
			children[m.GroupID] = append(children[m.GroupID], m.ID)
		}
	}
	seen := map[string]bool{}
	stack := []string{member}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == group {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		stack = append(stack, children[current]...)
	}
	return false
}

func groupFromRow(row sqlitedb.UserGroup) models.Group {
	return models.Group{
		ID: row.ID, Name: row.Name, Description: row.Description, CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
}

func membersFromRows(rows []sqlitedb.UserGroupMember) []models.GroupMember {
	out := make([]models.GroupMember, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.GroupMember{
			GroupID: row.GroupID, Type: models.MemberType(row.MemberType), ID: row.MemberID,
			AddedBy: row.AddedBy, AddedAt: row.AddedAt.UTC(),
		})
	}
	return out
}

// groupStore is the backend whose group writes each run in a transaction.
type groupStore interface {
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error
	WithSerializableTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error
}

func createGroupInTx(ctx context.Context, s groupStore, g models.Group) (models.Group, error) {
	var created models.Group
	err := s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		var err error
		created, err = tx.CreateGroup(ctx, g)
		return err
	})
	return created, err
}

func updateGroupInTx(ctx context.Context, s groupStore, g models.Group) (models.Group, error) {
	var updated models.Group
	err := s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		var err error
		updated, err = tx.UpdateGroup(ctx, g)
		return err
	})
	return updated, err
}

func deleteGroupInTx(ctx context.Context, s groupStore, id string) error {
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error { return tx.DeleteGroup(ctx, id) })
}

func addGroupMemberInTx(ctx context.Context, s groupStore, m models.GroupMember) error {
	return s.WithSerializableTx(ctx, func(ctx context.Context, tx Store) error { return tx.AddGroupMember(ctx, m) })
}

func removeGroupMemberInTx(ctx context.Context, s groupStore, m models.GroupMember) error {
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error { return tx.RemoveGroupMember(ctx, m) })
}
