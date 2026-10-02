-- name: ListAccessTokens :many
SELECT id, name, token_hash, created_by, created_at, last_used_at, scope, scoped_token_hash, message_scope
FROM access_tokens
ORDER BY name;

-- GetAccessTokenByHash is how a webhook request is authenticated. Matching on
-- the digest in SQL leaks nothing a timing attack could use: the digest of a
-- guess says nothing about the digest of the token.
-- name: GetAccessTokenByHash :one
SELECT id, name, token_hash, created_by, created_at, last_used_at, scope, scoped_token_hash, message_scope
FROM access_tokens
WHERE token_hash = sqlc.arg(token_hash) OR scoped_token_hash = sqlc.arg(token_hash);

-- name: CreateAccessToken :exec
INSERT INTO access_tokens (id, name, token_hash, created_by, created_at, scope, scoped_token_hash, message_scope)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: TouchAccessToken :exec
UPDATE access_tokens
SET last_used_at = ?
WHERE id = ?;

-- name: DeleteAccessToken :exec
DELETE FROM access_tokens WHERE id = ?;
