-- name: ListDestinations :many
SELECT id, name, team_id, channel_id, created_at, updated_at
FROM destinations
ORDER BY name;

-- name: GetDestination :one
SELECT id, name, team_id, channel_id, created_at, updated_at
FROM destinations
WHERE id = ?;

-- name: CreateDestination :exec
INSERT INTO destinations (id, name, team_id, channel_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateDestination :exec
UPDATE destinations
SET name = ?, team_id = ?, channel_id = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteDestination :exec
DELETE FROM destinations WHERE id = ?;
