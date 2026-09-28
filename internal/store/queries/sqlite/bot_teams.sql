-- name: GetBotTeam :one
SELECT team_id, tenant_id, service_url, updated_at
FROM bot_teams
WHERE team_id = ?;

-- name: ListBotTeams :many
SELECT team_id, tenant_id, service_url, updated_at
FROM bot_teams
ORDER BY team_id;

-- UpsertBotTeam records an install, or a newer service URL for one: both come
-- from the same authenticated activity, and the latest one wins.
-- name: UpsertBotTeam :exec
INSERT INTO bot_teams (team_id, tenant_id, service_url, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(team_id) DO UPDATE SET
	tenant_id   = excluded.tenant_id,
	service_url = excluded.service_url,
	updated_at  = excluded.updated_at;

-- name: DeleteBotTeam :exec
DELETE FROM bot_teams WHERE team_id = ?;
