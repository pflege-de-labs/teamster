-- name: InsertAuditEvent :exec
INSERT INTO audit_events (
	id, occurred_at, actor_subject, actor_name, actor_via, actor_token_id,
	action, resource_type, resource_id, request_id, before, after
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListAuditEvents :many
-- Newest first, a page at a time. The cursor is the last row of the previous
-- page; an empty cursor id starts from the newest. Each filter left empty
-- matches everything, so one statement serves every combination of them.
SELECT id, occurred_at, actor_subject, actor_name, actor_via, actor_token_id,
	action, resource_type, resource_id, request_id, before, after
FROM audit_events
WHERE (CAST(sqlc.arg(actor) AS TEXT) = '' OR actor_subject = sqlc.arg(actor))
	AND (CAST(sqlc.arg(resource_type) AS TEXT) = '' OR resource_type = sqlc.arg(resource_type))
	AND (CAST(sqlc.arg(resource_id) AS TEXT) = '' OR resource_id = sqlc.arg(resource_id))
	AND (CAST(sqlc.arg(action) AS TEXT) = '' OR action = sqlc.arg(action))
	AND occurred_at >= sqlc.arg(since)
	AND occurred_at < sqlc.arg(until)
	AND (CAST(sqlc.arg(cursor_id) AS TEXT) = ''
		OR occurred_at < sqlc.arg(cursor_at)
		OR (occurred_at = sqlc.arg(cursor_at) AND id < sqlc.arg(cursor_id)))
ORDER BY occurred_at DESC, id DESC
LIMIT CAST(sqlc.arg(max_rows) AS BIGINT);

-- name: DeleteAuditEventsBefore :execrows
DELETE FROM audit_events WHERE occurred_at < ?;

-- name: DeleteAuditEventsBeyond :execrows
-- Keeps the newest keep rows. The offset finds the oldest row worth keeping;
-- everything strictly older goes, and so does anything at the same instant
-- with a smaller id, which is the order the list pages in.
DELETE FROM audit_events
WHERE EXISTS (
	SELECT 1 FROM (
		SELECT occurred_at AS at, id AS last_id FROM audit_events
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1 OFFSET CAST(sqlc.arg(keep) AS BIGINT) - 1
	) AS boundary
	WHERE audit_events.occurred_at < boundary.at
		OR (audit_events.occurred_at = boundary.at AND audit_events.id < boundary.last_id)
);
