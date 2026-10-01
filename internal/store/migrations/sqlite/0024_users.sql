-- +goose Up
-- users remembers everyone who signed in, so permissions can name them and an
-- admin can disable them (ADR 0072). Written at every sign-in: roles and
-- idp_groups are what the provider said last time. Disabling sets
-- disabled_at; a disabled user is refused at sign-in.
CREATE TABLE users (
	subject     TEXT PRIMARY KEY,
	source      TEXT NOT NULL,
	name        TEXT NOT NULL DEFAULT '',
	email       TEXT NOT NULL DEFAULT '',
	roles       TEXT NOT NULL DEFAULT '',
	idp_groups  TEXT NOT NULL DEFAULT '[]',
	first_seen  DATETIME NOT NULL,
	last_seen   DATETIME NOT NULL,
	disabled_at DATETIME,
	disabled_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX users_name ON users (name);

-- Disabling ends a user's sessions by subject.
CREATE INDEX sessions_subject ON sessions (subject);

-- +goose Down
DROP INDEX sessions_subject;
DROP TABLE users;
