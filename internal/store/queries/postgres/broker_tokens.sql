-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: CreateBrokerToken :exec
INSERT INTO broker_tokens (session_id, access_token, refresh_token, expires_at, updated_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetBrokerToken :one
SELECT session_id, access_token, refresh_token, expires_at, updated_at
FROM broker_tokens
WHERE session_id = $1;

-- name: UpdateBrokerToken :exec
UPDATE broker_tokens
SET access_token = $1, refresh_token = $2, expires_at = $3, updated_at = $4
WHERE session_id = $5;

-- name: DeleteBrokerToken :exec
DELETE FROM broker_tokens WHERE session_id = $1;
