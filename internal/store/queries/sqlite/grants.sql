-- name: ListGrants :many
SELECT id, role, team_id, channel_id, created_at, updated_at
FROM grants
ORDER BY role, team_id, channel_id;

-- name: CreateGrant :exec
INSERT INTO grants (id, role, team_id, channel_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: DeleteGrant :exec
DELETE FROM grants WHERE id = ?;
