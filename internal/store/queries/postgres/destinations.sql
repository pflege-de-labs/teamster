-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListDestinations :many
SELECT id, name, team_id, channel_id, created_at, updated_at
FROM destinations
ORDER BY name;

-- name: GetDestination :one
SELECT id, name, team_id, channel_id, created_at, updated_at
FROM destinations
WHERE id = $1;

-- name: CreateDestination :exec
INSERT INTO destinations (id, name, team_id, channel_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: UpdateDestination :exec
UPDATE destinations
SET name = $1, team_id = $2, channel_id = $3, updated_at = $4
WHERE id = $5;

-- name: DeleteDestination :exec
DELETE FROM destinations WHERE id = $1;
