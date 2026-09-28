-- +goose Up
-- An access token is a named credential for /webhook/alertmanager and
-- /webhook/universal, issued from the admin UI (ADR 0044). One per sender, so
-- revoking one sender leaves the others working.
--
-- token_hash is a SHA-256 digest, never the token, for the reasons given in
-- 0008_webhook_endpoints.sql. It is unique because it is the lookup key: a
-- request carries the token and nothing else.
--
-- last_used_at is nullable because a token that was never used has no answer.
-- A new table is additive, so the previous release runs against it untouched.
CREATE TABLE access_tokens (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	token_hash TEXT NOT NULL,
	created_by TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	last_used_at DATETIME
);

CREATE UNIQUE INDEX access_tokens_name ON access_tokens (name);

CREATE UNIQUE INDEX access_tokens_hash ON access_tokens (token_hash);

-- +goose Down
DROP INDEX access_tokens_hash;

DROP INDEX access_tokens_name;

DROP TABLE access_tokens;
