-- +goose Up
-- The previous release ignores this table: a new table is additive.
-- An MCP token is a credential for /mcp, kept apart from access_tokens so
-- neither table can authenticate the other's callers. token_hash is a SHA-256
-- digest, never the token. Names are unique per creator. access is the most the token may do.
CREATE TABLE mcp_tokens (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	token_hash TEXT NOT NULL,
	created_by TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	last_used_at TIMESTAMPTZ,
	access TEXT NOT NULL CHECK (access IN ('read', 'write'))
);

CREATE UNIQUE INDEX mcp_tokens_name ON mcp_tokens (created_by, name);

CREATE UNIQUE INDEX mcp_tokens_hash ON mcp_tokens (token_hash);

-- +goose Down
DROP INDEX mcp_tokens_hash;

DROP INDEX mcp_tokens_name;

DROP TABLE mcp_tokens;
