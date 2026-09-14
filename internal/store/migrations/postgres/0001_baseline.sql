-- +goose Up
-- The schema as SQLite reached it after four migrations, written once. There is
-- no Postgres database in the field that predates this, so there is nothing to
-- adopt and no numbering to keep in step: the two dialects share statements,
-- not history.
--
-- Two type choices worth naming. Timestamps are TIMESTAMPTZ, which Postgres
-- stores as an instant rather than as whatever the writer's clock said, and
-- which round-trips to time.Time exactly as SQLite's DATETIME does. Integers
-- that carry a Go int are BIGINT, because sqlc maps Postgres INTEGER to int32
-- and SQLite INTEGER to int64 -- BIGINT is what makes both backends produce the
-- same Go type from the same column.
CREATE TABLE templates (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	message_text TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE destinations (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE routes (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	parent_id TEXT NOT NULL DEFAULT '',
	greedy BOOLEAN NOT NULL DEFAULT FALSE,
	label_selector TEXT NOT NULL,
	destination_id TEXT NOT NULL,
	template_id TEXT NOT NULL,
	is_default BOOLEAN NOT NULL,
	priority BIGINT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE grants (
	id TEXT PRIMARY KEY,
	role TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX grants_role_scope ON grants (role, team_id, channel_id);

CREATE TABLE sessions (
	id TEXT PRIMARY KEY,
	subject TEXT NOT NULL,
	name TEXT NOT NULL,
	source TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE login_flows (
	state TEXT PRIMARY KEY,
	verifier TEXT NOT NULL,
	nonce TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE active_alerts (
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	claim_owner TEXT NOT NULL DEFAULT '',
	claimed_at TIMESTAMPTZ,
	posted_at TIMESTAMPTZ,
	last_update TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (fingerprint, team_id, channel_id),
	CHECK ((posted_at IS NULL) = (message_id = ''))
);

-- +goose Down
DROP TABLE IF EXISTS active_alerts;
DROP TABLE IF EXISTS login_flows;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS grants;
DROP TABLE IF EXISTS routes;
DROP TABLE IF EXISTS destinations;
DROP TABLE IF EXISTS templates;
