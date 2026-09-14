-- name: ListGrants :many
SELECT id, role, team_id, channel_id, created_at, updated_at
FROM grants
ORDER BY role, team_id, channel_id;

-- name: CreateGrant :exec
INSERT INTO grants (id, role, team_id, channel_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: DeleteGrant :exec
DELETE FROM grants WHERE id = ?;

-- DeleteGrantsForRole replaces the list-and-filter-in-Go that used to stand
-- here. A set delete takes the locks the isolation level needs and cannot
-- interleave with another writer's replacement into the union of both.
-- name: DeleteGrantsForRole :exec
DELETE FROM grants WHERE role = ?;
