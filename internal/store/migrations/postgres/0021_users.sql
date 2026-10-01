-- +goose Up
-- The SQLite 0024 change, in this dialect. See
-- migrations/sqlite/0024_users.sql for why it is shaped this way.
CREATE TABLE users (
	subject     TEXT PRIMARY KEY,
	source      TEXT NOT NULL,
	name        TEXT NOT NULL DEFAULT '',
	email       TEXT NOT NULL DEFAULT '',
	roles       TEXT NOT NULL DEFAULT '',
	idp_groups  TEXT NOT NULL DEFAULT '[]',
	first_seen  TIMESTAMPTZ NOT NULL,
	last_seen   TIMESTAMPTZ NOT NULL,
	disabled_at TIMESTAMPTZ,
	disabled_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX users_name ON users (name);
CREATE INDEX sessions_subject ON sessions (subject);

-- +goose Down
DROP INDEX sessions_subject;
DROP TABLE users;
