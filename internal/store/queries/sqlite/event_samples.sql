-- name: UpsertEventSample :exec
-- Adds to the count rather than replacing it, so replicas sharing one
-- database each contribute what they saw. last_seen only moves forward, so a
-- replica with a slow clock cannot make a key look older than it is.
INSERT INTO event_samples (kind, key, value, seen_count, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (kind, key, value) DO UPDATE
SET seen_count = event_samples.seen_count + excluded.seen_count,
	last_seen = CASE WHEN excluded.last_seen > event_samples.last_seen
		THEN excluded.last_seen ELSE event_samples.last_seen END;

-- name: ListEventSamples :many
-- The CAST keeps the parameter int64 in both dialects: Postgres would
-- otherwise infer int32 for a LIMIT.
SELECT kind, key, value, seen_count, first_seen, last_seen
FROM event_samples
ORDER BY kind, key, last_seen DESC, value
LIMIT CAST(sqlc.arg(max_rows) AS BIGINT);

-- name: DeleteEventSamplesSeenBefore :execrows
DELETE FROM event_samples WHERE last_seen < ?;

-- name: DeleteExcessEventSampleValues :execrows
-- Keeps the most recently seen values of each label key and deletes the rest.
-- A correlated count rather than a window function in a subquery, because the
-- same text has to run on SQLite and Postgres. The tie-break on value makes
-- two rows seen at the same instant rank the same way on every run.
DELETE FROM event_samples
WHERE kind = 'label'
	AND (
		SELECT COUNT(*) FROM event_samples AS newer
		WHERE newer.kind = event_samples.kind
			AND newer.key = event_samples.key
			AND (newer.last_seen > event_samples.last_seen
				OR (newer.last_seen = event_samples.last_seen AND newer.value < event_samples.value))
	) >= CAST(sqlc.arg(keep) AS BIGINT);
