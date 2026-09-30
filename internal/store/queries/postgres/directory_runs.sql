-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- InsertDirectoryRun requests a run. It collides with the partial unique index
-- while another run is requested or running.
-- name: InsertDirectoryRun :exec
INSERT INTO directory_runs (id, kind, requested_by, requested_at, state)
VALUES (sqlc.arg(id), sqlc.arg(kind), sqlc.arg(requested_by), sqlc.arg(requested_at), 'requested');

-- name: GetDirectoryRun :one
SELECT * FROM directory_runs WHERE id = sqlc.arg(id);

-- name: LatestDirectoryRun :one
SELECT * FROM directory_runs ORDER BY requested_at DESC, id DESC LIMIT 1;

-- ClaimDirectoryRun takes a requested run, or a running one whose owner has
-- stopped writing heartbeats.
-- name: ClaimDirectoryRun :execrows
UPDATE directory_runs
SET state        = 'running',
	owner        = sqlc.arg(owner),
	heartbeat_at = sqlc.arg(now),
	started_at   = COALESCE(started_at, sqlc.arg(now))
WHERE id = sqlc.arg(id)
	AND (state = 'requested' OR (state = 'running' AND heartbeat_at < sqlc.arg(stale_before)));

-- HeartbeatDirectoryRun writes progress. No row changed means another replica
-- took the run over, and this one has to stop.
-- name: HeartbeatDirectoryRun :execrows
UPDATE directory_runs
SET heartbeat_at = sqlc.arg(now),
	total        = sqlc.arg(total),
	installed    = sqlc.arg(installed),
	already      = sqlc.arg(already),
	failed       = sqlc.arg(failed),
	ineligible   = sqlc.arg(ineligible)
WHERE id = sqlc.arg(id) AND owner = sqlc.arg(owner) AND state = 'running';

-- name: FinishDirectoryRun :execrows
UPDATE directory_runs
SET state        = sqlc.arg(state),
	finished_at  = sqlc.arg(now),
	heartbeat_at = sqlc.arg(now),
	total        = sqlc.arg(total),
	installed    = sqlc.arg(installed),
	already      = sqlc.arg(already),
	failed       = sqlc.arg(failed),
	ineligible   = sqlc.arg(ineligible),
	last_error   = sqlc.arg(last_error)
WHERE id = sqlc.arg(id) AND owner = sqlc.arg(owner) AND state = 'running';

-- name: PruneDirectoryRuns :execrows
DELETE FROM directory_runs
WHERE state IN ('done', 'failed') AND finished_at < sqlc.arg(before);
