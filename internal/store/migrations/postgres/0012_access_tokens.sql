-- +goose Up
-- The SQLite 0015 table, in this dialect. See
-- migrations/sqlite/0015_access_tokens.sql for why it is shaped this way.
CREATE TABLE access_tokens (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	token_hash TEXT NOT NULL,
	created_by TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	last_used_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX access_tokens_name ON access_tokens (name);

CREATE UNIQUE INDEX access_tokens_hash ON access_tokens (token_hash);

-- +goose Down
DROP INDEX access_tokens_hash;

DROP INDEX access_tokens_name;

DROP TABLE access_tokens;
