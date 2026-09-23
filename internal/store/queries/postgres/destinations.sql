-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListDestinations :many
SELECT id, name, team_id, channel_id, created_at, updated_at, is_default
FROM destinations
ORDER BY name;

-- name: GetDestination :one
SELECT id, name, team_id, channel_id, created_at, updated_at, is_default
FROM destinations
WHERE id = $1;

-- name: GetDefaultDestination :one
SELECT id, name, team_id, channel_id, created_at, updated_at, is_default
FROM destinations
WHERE is_default;

-- The first destination becomes the default in the same statement that
-- creates it, so there is no window in which one exists without a default.
-- name: CreateDestination :exec
INSERT INTO destinations (id, name, team_id, channel_id, created_at, updated_at, is_default)
VALUES ($1, $2, $3, $4, $5, $6, NOT EXISTS (SELECT 1 FROM destinations WHERE is_default));

-- name: UpdateDestination :exec
UPDATE destinations
SET name = $1, team_id = $2, channel_id = $3, updated_at = $4
WHERE id = $5;

-- name: ClearDefaultDestination :exec
UPDATE destinations SET is_default = FALSE WHERE is_default;

-- name: MarkDefaultDestination :execrows
UPDATE destinations SET is_default = TRUE WHERE id = $1;

-- name: CountDestinations :one
SELECT COUNT(*) FROM destinations;

-- name: DeleteDestination :exec
DELETE FROM destinations WHERE id = $1;
