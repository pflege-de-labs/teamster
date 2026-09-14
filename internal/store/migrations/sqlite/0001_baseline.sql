-- +goose Up
-- The schema as it stood when migrations gained a version ledger, with every
-- column a later release had added already present. It is written with IF NOT
-- EXISTS because the databases in the field are not all fresh: a database this
-- build has already opened carries this exact schema and no ledger, and the
-- migration has to be a no-op there rather than an error. 0002 finishes the
-- job for the ones that are genuinely older.
CREATE TABLE IF NOT EXISTS templates (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	message_text TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS destinations (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS routes (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	parent_id TEXT NOT NULL DEFAULT '',
	greedy INTEGER NOT NULL DEFAULT 0,
	label_selector TEXT NOT NULL,
	destination_id TEXT NOT NULL,
	template_id TEXT NOT NULL,
	is_default INTEGER NOT NULL,
	priority INTEGER NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS grants (
	id TEXT PRIMARY KEY,
	role TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	subject TEXT NOT NULL,
	name TEXT NOT NULL,
	source TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	expires_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS login_flows (
	state TEXT PRIMARY KEY,
	verifier TEXT NOT NULL,
	nonce TEXT NOT NULL,
	expires_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS active_alerts (
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	last_update DATETIME NOT NULL,
	PRIMARY KEY (fingerprint, team_id, channel_id)
);

-- +goose Down
DROP TABLE IF EXISTS active_alerts;
DROP TABLE IF EXISTS login_flows;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS grants;
DROP TABLE IF EXISTS routes;
DROP TABLE IF EXISTS destinations;
DROP TABLE IF EXISTS templates;
