-- name: CreateSession :exec
INSERT INTO sessions (id, subject, name, source, role, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT id, subject, name, source, role, created_at, expires_at
FROM sessions
WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= ?;
