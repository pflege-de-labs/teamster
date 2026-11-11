-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListMCPTokens :many
SELECT id, name, token_hash, created_by, created_at, last_used_at, access
FROM mcp_tokens
ORDER BY created_at, id;

-- name: GetMCPTokenByHash :one
SELECT id, name, token_hash, created_by, created_at, last_used_at, access
FROM mcp_tokens
WHERE token_hash = $1;

-- name: CreateMCPToken :exec
INSERT INTO mcp_tokens (id, name, token_hash, created_by, created_at, access)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: TouchMCPToken :exec
UPDATE mcp_tokens
SET last_used_at = $1
WHERE id = $2;

-- name: DeleteMCPToken :execrows
DELETE FROM mcp_tokens WHERE id = $1;
