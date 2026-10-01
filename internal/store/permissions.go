package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

func (s queryAdapter) ListPermissions(ctx context.Context) ([]models.Permission, error) {
	rows, err := s.q.ListPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	return permissionsFromRows(rows), nil
}

func (s queryAdapter) ListPermissionsFor(ctx context.Context, resourceType, resourceID string) ([]models.Permission, error) {
	rows, err := s.q.ListPermissionsForResource(ctx, sqlitedb.ListPermissionsForResourceParams{ResourceType: resourceType, ResourceID: resourceID})
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	return permissionsFromRows(rows), nil
}

func (s queryAdapter) GetPermission(ctx context.Context, id string) (models.Permission, error) {
	row, err := s.q.GetPermission(ctx, id)
	if err != nil {
		return models.Permission{}, notFound(err)
	}
	return permissionFromRow(row), nil
}

func (s queryAdapter) PutPermission(ctx context.Context, p models.Permission) (models.Permission, error) {
	if !p.PrincipalType.Valid() || p.PrincipalID == "" || p.ResourceType == "" || p.ResourceID == "" {
		return models.Permission{}, fmt.Errorf("a permission needs a principal and a resource")
	}
	if len(p.Actions) == 0 {
		return models.Permission{}, s.deletePermissionByKey(ctx, p)
	}
	row, err := s.q.UpsertPermission(ctx, sqlitedb.UpsertPermissionParams{
		ID:            newID(p.ID),
		PrincipalType: string(p.PrincipalType),
		PrincipalID:   p.PrincipalID,
		ResourceType:  p.ResourceType,
		ResourceID:    p.ResourceID,
		Actions:       strings.Join(p.Actions, " "),
		CreatedBy:     p.CreatedBy,
		At:            nowUTC(),
	})
	if err != nil {
		return models.Permission{}, fmt.Errorf("put permission: %w", err)
	}
	return permissionFromRow(row), s.BumpAuthzGeneration(ctx)
}

// deletePermissionByKey removes the row for p's principal and resource, if any.
func (s queryAdapter) deletePermissionByKey(ctx context.Context, p models.Permission) error {
	rows, err := s.ListPermissionsFor(ctx, p.ResourceType, p.ResourceID)
	if err != nil {
		return err
	}
	for _, existing := range rows {
		if existing.PrincipalType == p.PrincipalType && existing.PrincipalID == p.PrincipalID {
			return s.DeletePermission(ctx, existing.ID)
		}
	}
	return nil
}

func (s queryAdapter) DeletePermission(ctx context.Context, id string) error {
	n, err := s.q.DeletePermission(ctx, id)
	if err != nil {
		return fmt.Errorf("delete permission: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return s.BumpAuthzGeneration(ctx)
}

func (s queryAdapter) DeletePermissionsFor(ctx context.Context, resourceType, resourceID string) error {
	if err := s.q.DeletePermissionsForResource(ctx, sqlitedb.DeletePermissionsForResourceParams{ResourceType: resourceType, ResourceID: resourceID}); err != nil {
		return fmt.Errorf("delete permissions: %w", err)
	}
	return s.BumpAuthzGeneration(ctx)
}

func permissionsFromRows(rows []sqlitedb.Permission) []models.Permission {
	out := make([]models.Permission, 0, len(rows))
	for _, row := range rows {
		out = append(out, permissionFromRow(row))
	}
	return out
}

func permissionFromRow(row sqlitedb.Permission) models.Permission {
	return models.Permission{
		ID:            row.ID,
		PrincipalType: models.PrincipalType(row.PrincipalType),
		PrincipalID:   row.PrincipalID,
		ResourceType:  row.ResourceType,
		ResourceID:    row.ResourceID,
		Actions:       strings.Fields(row.Actions),
		CreatedBy:     row.CreatedBy,
		CreatedAt:     row.CreatedAt.UTC(),
		UpdatedAt:     row.UpdatedAt.UTC(),
	}
}

func putPermissionInTx(ctx context.Context, s groupStore, p models.Permission) (models.Permission, error) {
	var saved models.Permission
	err := s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		var err error
		saved, err = tx.PutPermission(ctx, p)
		return err
	})
	return saved, err
}

func deletePermissionInTx(ctx context.Context, s groupStore, id string) error {
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error { return tx.DeletePermission(ctx, id) })
}

func deletePermissionsForInTx(ctx context.Context, s groupStore, resourceType, resourceID string) error {
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		return tx.DeletePermissionsFor(ctx, resourceType, resourceID)
	})
}
