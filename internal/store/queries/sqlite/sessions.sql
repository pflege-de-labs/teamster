-- name: CreateSession :exec
INSERT INTO sessions (id, subject, name, source, role, created_at, expires_at, identity)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT id, subject, name, source, role, created_at, expires_at, identity
FROM sessions
WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= ?;
