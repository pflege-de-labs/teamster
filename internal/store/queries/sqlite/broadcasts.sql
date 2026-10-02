-- name: InsertBroadcast :exec
INSERT INTO broadcasts (id, requested_by, token_name, requested_at, state, event, plan)
VALUES (sqlc.arg(id), sqlc.arg(requested_by), sqlc.arg(token_name), sqlc.arg(requested_at), 'requested',
	sqlc.arg(event), sqlc.arg(plan));

-- name: GetBroadcast :one
SELECT * FROM broadcasts WHERE id = sqlc.arg(id);

-- ListBroadcasts is newest first; an empty requested_by lists everyone's.
-- name: ListBroadcasts :many
SELECT * FROM broadcasts
WHERE CAST(sqlc.arg(requested_by) AS TEXT) = '' OR requested_by = sqlc.arg(requested_by)
ORDER BY requested_at DESC, id DESC
LIMIT CAST(sqlc.arg(max_rows) AS BIGINT);

-- NextBroadcast is the oldest waiting broadcast, or a running one whose owner
-- stopped writing heartbeats.
-- name: NextBroadcast :one
SELECT * FROM broadcasts
WHERE state = 'requested' OR (state = 'running' AND heartbeat_at < sqlc.arg(stale_before))
ORDER BY requested_at, id
LIMIT 1;

-- name: ClaimBroadcast :execrows
UPDATE broadcasts
SET state        = 'running',
	owner        = sqlc.arg(owner),
	heartbeat_at = sqlc.arg(now),
	started_at   = COALESCE(started_at, sqlc.arg(now))
WHERE id = sqlc.arg(id)
	AND (state = 'requested' OR (state = 'running' AND heartbeat_at < sqlc.arg(stale_before)));

-- HeartbeatBroadcast writes progress. No row changed means another replica
-- took the broadcast over, and this one has to stop.
-- name: HeartbeatBroadcast :execrows
UPDATE broadcasts
SET heartbeat_at = sqlc.arg(now),
	cursor       = sqlc.arg(cursor),
	total        = sqlc.arg(total),
	delivered    = sqlc.arg(delivered),
	unreachable  = sqlc.arg(unreachable),
	failed       = sqlc.arg(failed)
WHERE id = sqlc.arg(id) AND owner = sqlc.arg(owner) AND state = 'running';

-- name: FinishBroadcast :execrows
UPDATE broadcasts
SET state        = sqlc.arg(state),
	finished_at  = sqlc.arg(now),
	heartbeat_at = sqlc.arg(now),
	cursor       = sqlc.arg(cursor),
	total        = sqlc.arg(total),
	delivered    = sqlc.arg(delivered),
	unreachable  = sqlc.arg(unreachable),
	failed       = sqlc.arg(failed),
	last_error   = sqlc.arg(last_error)
WHERE id = sqlc.arg(id) AND owner = sqlc.arg(owner) AND state = 'running';

-- name: PruneBroadcasts :execrows
DELETE FROM broadcasts
WHERE state IN ('done', 'failed') AND finished_at < sqlc.arg(before);
