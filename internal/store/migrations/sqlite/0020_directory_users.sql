-- +goose Up
-- The people the bot can message without a link code (ADR 0059, ADR 0060).
--
-- Keyed by Entra object id, because that is what Graph and the Bot Connector
-- both name a person by. upn_key and mail_key are lower-cased copies, so a
-- case-insensitive lookup is a plain index scan in both dialects.
CREATE TABLE directory_users (
	aad_object_id       TEXT PRIMARY KEY,
	tenant_id           TEXT NOT NULL,
	user_principal_name TEXT NOT NULL DEFAULT '',
	mail                TEXT NOT NULL DEFAULT '',
	upn_key             TEXT NOT NULL DEFAULT '',
	mail_key            TEXT NOT NULL DEFAULT '',
	display_name        TEXT NOT NULL DEFAULT '',
	given_name          TEXT NOT NULL DEFAULT '',
	surname             TEXT NOT NULL DEFAULT '',
	-- An enabled member when last read from Graph.
	eligible            INTEGER NOT NULL DEFAULT 1,
	conversation_id     TEXT NOT NULL DEFAULT '',
	service_url         TEXT NOT NULL DEFAULT '',
	install_state       TEXT NOT NULL DEFAULT 'unknown'
		CHECK (install_state IN ('unknown', 'installed', 'removed', 'failed', 'ineligible', 'departed')),
	installed_at        DATETIME,
	next_attempt_at     DATETIME,
	attempts            INTEGER NOT NULL DEFAULT 0,
	last_error          TEXT NOT NULL DEFAULT '',
	blocked_at          DATETIME,
	blocked_reason      TEXT NOT NULL DEFAULT '',
	directory_seen_at   DATETIME NOT NULL,
	created_at          DATETIME NOT NULL,
	updated_at          DATETIME NOT NULL
);
CREATE INDEX directory_users_upn ON directory_users (upn_key);
CREATE INDEX directory_users_mail ON directory_users (mail_key);
CREATE INDEX directory_users_conversation ON directory_users (conversation_id);
CREATE INDEX directory_users_due ON directory_users (install_state, next_attempt_at);

-- One row per install run. The partial unique index admits a single run that
-- is requested or running, which is what makes the table a lease across
-- replicas: a second request, or a second replica's periodic run, collides.
CREATE TABLE directory_runs (
	id           TEXT PRIMARY KEY,
	kind         TEXT NOT NULL CHECK (kind IN ('manual', 'periodic')),
	requested_by TEXT NOT NULL DEFAULT '',
	requested_at DATETIME NOT NULL,
	state        TEXT NOT NULL CHECK (state IN ('requested', 'running', 'done', 'failed')),
	owner        TEXT NOT NULL DEFAULT '',
	heartbeat_at DATETIME,
	started_at   DATETIME,
	finished_at  DATETIME,
	total        INTEGER NOT NULL DEFAULT 0,
	installed    INTEGER NOT NULL DEFAULT 0,
	already      INTEGER NOT NULL DEFAULT 0,
	failed       INTEGER NOT NULL DEFAULT 0,
	ineligible   INTEGER NOT NULL DEFAULT 0,
	last_error   TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX directory_runs_active ON directory_runs ((1)) WHERE state IN ('requested', 'running');
CREATE INDEX directory_runs_requested ON directory_runs (requested_at);

-- +goose Down
DROP TABLE directory_runs;
DROP TABLE directory_users;
