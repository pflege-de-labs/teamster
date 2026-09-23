-- +goose Up
-- The SQLite 0010 table, in this dialect. The numbering differs because the
-- two dialects share statements rather than history.
--
-- See migrations/sqlite/0010_broker_tokens.sql for why this holds ciphertext
-- keyed by session id with no SQL foreign key.
CREATE TABLE broker_tokens (
	session_id    TEXT PRIMARY KEY,
	access_token  TEXT NOT NULL,
	refresh_token TEXT NOT NULL,
	expires_at    TIMESTAMPTZ NOT NULL,
	updated_at    TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE broker_tokens;
