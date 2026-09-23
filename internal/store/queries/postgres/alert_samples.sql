-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: UpsertAlertSample :exec
-- Adds to the count rather than replacing it, so replicas sharing one
-- database each contribute what they saw. last_seen only moves forward, so a
-- replica with a slow clock cannot make a key look older than it is.
INSERT INTO alert_samples (kind, key, value, seen_count, first_seen, last_seen)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (kind, key, value) DO UPDATE
SET seen_count = alert_samples.seen_count + excluded.seen_count,
	last_seen = CASE WHEN excluded.last_seen > alert_samples.last_seen
		THEN excluded.last_seen ELSE alert_samples.last_seen END;

-- name: ListAlertSamples :many
-- The CAST keeps the parameter int64 in both dialects: Postgres would
-- otherwise infer int32 for a LIMIT.
SELECT kind, key, value, seen_count, first_seen, last_seen
FROM alert_samples
ORDER BY kind, key, last_seen DESC, value
LIMIT CAST(sqlc.arg(max_rows) AS BIGINT);

-- name: DeleteAlertSamplesSeenBefore :execrows
DELETE FROM alert_samples WHERE last_seen < $1;

-- name: DeleteExcessAlertSampleValues :execrows
-- Keeps the most recently seen values of each label key and deletes the rest.
-- A correlated count rather than a window function in a subquery, because the
-- same text has to run on SQLite and Postgres. The tie-break on value makes
-- two rows seen at the same instant rank the same way on every run.
DELETE FROM alert_samples
WHERE kind = 'label'
	AND (
		SELECT COUNT(*) FROM alert_samples AS newer
		WHERE newer.kind = alert_samples.kind
			AND newer.key = alert_samples.key
			AND (newer.last_seen > alert_samples.last_seen
				OR (newer.last_seen = alert_samples.last_seen AND newer.value < alert_samples.value))
	) >= CAST(sqlc.arg(keep) AS BIGINT);
