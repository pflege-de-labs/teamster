-- name: CreateBrokerToken :exec
INSERT INTO broker_tokens (session_id, access_token, refresh_token, expires_at, updated_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetBrokerToken :one
SELECT session_id, access_token, refresh_token, expires_at, updated_at
FROM broker_tokens
WHERE session_id = ?;

-- name: UpdateBrokerToken :exec
UPDATE broker_tokens
SET access_token = ?, refresh_token = ?, expires_at = ?, updated_at = ?
WHERE session_id = ?;

-- name: DeleteBrokerToken :exec
DELETE FROM broker_tokens WHERE session_id = ?;
