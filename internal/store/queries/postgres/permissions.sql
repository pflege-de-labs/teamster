-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListPermissions :many
SELECT id, principal_type, principal_id, resource_type, resource_id, actions, created_by, created_at, updated_at
FROM permissions
ORDER BY resource_type, resource_id, principal_type, principal_id;

-- name: ListPermissionsForResource :many
SELECT id, principal_type, principal_id, resource_type, resource_id, actions, created_by, created_at, updated_at
FROM permissions
WHERE resource_type = sqlc.arg(resource_type) AND resource_id = sqlc.arg(resource_id)
ORDER BY created_at, id;

-- name: GetPermission :one
SELECT id, principal_type, principal_id, resource_type, resource_id, actions, created_by, created_at, updated_at
FROM permissions
WHERE id = sqlc.arg(id);

-- name: UpsertPermission :one
-- One row per principal and resource: granting again replaces the actions.
INSERT INTO permissions (id, principal_type, principal_id, resource_type, resource_id, actions, created_by, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(principal_type), sqlc.arg(principal_id), sqlc.arg(resource_type), sqlc.arg(resource_id),
	sqlc.arg(actions), sqlc.arg(created_by), sqlc.arg(at), sqlc.arg(at))
ON CONFLICT (principal_type, principal_id, resource_type, resource_id) DO UPDATE SET
	actions    = excluded.actions,
	updated_at = excluded.updated_at
RETURNING id, principal_type, principal_id, resource_type, resource_id, actions, created_by, created_at, updated_at;

-- name: DeletePermission :execrows
DELETE FROM permissions WHERE id = sqlc.arg(id);

-- name: DeletePermissionsForResource :exec
DELETE FROM permissions WHERE resource_type = sqlc.arg(resource_type) AND resource_id = sqlc.arg(resource_id);

-- name: DeletePermissionsForPrincipal :exec
DELETE FROM permissions WHERE principal_type = sqlc.arg(principal_type) AND principal_id = sqlc.arg(principal_id);
