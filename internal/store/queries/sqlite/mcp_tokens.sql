-- name: ListMCPTokens :many
SELECT id, name, token_hash, created_by, created_at, last_used_at, access
FROM mcp_tokens
ORDER BY created_at, id;

-- name: GetMCPTokenByHash :one
SELECT id, name, token_hash, created_by, created_at, last_used_at, access
FROM mcp_tokens
WHERE token_hash = ?;

-- name: CreateMCPToken :exec
INSERT INTO mcp_tokens (id, name, token_hash, created_by, created_at, access)
VALUES (?, ?, ?, ?, ?, ?);

-- name: TouchMCPToken :exec
UPDATE mcp_tokens
SET last_used_at = ?
WHERE id = ?;

-- name: DeleteMCPToken :execrows
DELETE FROM mcp_tokens WHERE id = ?;
