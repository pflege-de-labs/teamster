-- name: UpsertUser :exec
-- A sign-in refreshes what the provider says; first_seen and the disabled
-- state are kept.
INSERT INTO users (subject, source, name, email, roles, idp_groups, first_seen, last_seen, object_id)
VALUES (sqlc.arg(subject), sqlc.arg(source), sqlc.arg(name), sqlc.arg(email), sqlc.arg(roles),
	sqlc.arg(idp_groups), sqlc.arg(seen_at), sqlc.arg(seen_at), sqlc.arg(object_id))
ON CONFLICT (subject) DO UPDATE SET
	source     = excluded.source,
	name       = excluded.name,
	email      = excluded.email,
	roles      = excluded.roles,
	idp_groups = excluded.idp_groups,
	last_seen  = excluded.last_seen,
	-- A sign-in without the claim keeps the id an earlier one recorded.
	object_id  = CASE WHEN excluded.object_id = '' THEN users.object_id ELSE excluded.object_id END;

-- name: GetUser :one
SELECT subject, source, name, email, roles, idp_groups, first_seen, last_seen, disabled_at, disabled_by, object_id
FROM users
WHERE subject = sqlc.arg(subject);

-- name: ListUsers :many
-- pattern is a lower-cased LIKE pattern; its % and _ stay wildcards.
SELECT subject, source, name, email, roles, idp_groups, first_seen, last_seen, disabled_at, disabled_by, object_id
FROM users
WHERE CAST(sqlc.arg(pattern) AS TEXT) = ''
	OR lower(subject) LIKE sqlc.arg(pattern)
	OR lower(name) LIKE sqlc.arg(pattern)
	OR lower(email) LIKE sqlc.arg(pattern)
ORDER BY lower(name), subject
LIMIT CAST(sqlc.arg(max_rows) AS BIGINT);

-- name: SetUserDisabled :execrows
UPDATE users
SET disabled_at = sqlc.narg(disabled_at), disabled_by = sqlc.arg(disabled_by)
WHERE subject = sqlc.arg(subject);

-- name: DeleteBrokerTokensForSubject :exec
DELETE FROM broker_tokens
WHERE session_id IN (SELECT id FROM sessions WHERE subject = sqlc.arg(subject));

-- name: DeleteSessionsForSubject :exec
DELETE FROM sessions WHERE subject = sqlc.arg(subject);
