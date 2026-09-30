-- +goose Up
-- The SQLite 0020 change, in this dialect. See
-- migrations/sqlite/0020_directory_users.sql for why it is shaped this way.
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
	eligible            BOOLEAN NOT NULL DEFAULT TRUE,
	conversation_id     TEXT NOT NULL DEFAULT '',
	service_url         TEXT NOT NULL DEFAULT '',
	install_state       TEXT NOT NULL DEFAULT 'unknown'
		CHECK (install_state IN ('unknown', 'installed', 'removed', 'failed', 'ineligible', 'departed')),
	installed_at        TIMESTAMPTZ,
	next_attempt_at     TIMESTAMPTZ,
	attempts            BIGINT NOT NULL DEFAULT 0,
	last_error          TEXT NOT NULL DEFAULT '',
	blocked_at          TIMESTAMPTZ,
	blocked_reason      TEXT NOT NULL DEFAULT '',
	directory_seen_at   TIMESTAMPTZ NOT NULL,
	created_at          TIMESTAMPTZ NOT NULL,
	updated_at          TIMESTAMPTZ NOT NULL
);
CREATE INDEX directory_users_upn ON directory_users (upn_key);
CREATE INDEX directory_users_mail ON directory_users (mail_key);
CREATE INDEX directory_users_conversation ON directory_users (conversation_id);
CREATE INDEX directory_users_due ON directory_users (install_state, next_attempt_at);

CREATE TABLE directory_runs (
	id           TEXT PRIMARY KEY,
	kind         TEXT NOT NULL CHECK (kind IN ('manual', 'periodic')),
	requested_by TEXT NOT NULL DEFAULT '',
	requested_at TIMESTAMPTZ NOT NULL,
	state        TEXT NOT NULL CHECK (state IN ('requested', 'running', 'done', 'failed')),
	owner        TEXT NOT NULL DEFAULT '',
	heartbeat_at TIMESTAMPTZ,
	started_at   TIMESTAMPTZ,
	finished_at  TIMESTAMPTZ,
	total        BIGINT NOT NULL DEFAULT 0,
	installed    BIGINT NOT NULL DEFAULT 0,
	already      BIGINT NOT NULL DEFAULT 0,
	failed       BIGINT NOT NULL DEFAULT 0,
	ineligible   BIGINT NOT NULL DEFAULT 0,
	last_error   TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX directory_runs_active ON directory_runs ((1)) WHERE state IN ('requested', 'running');
CREATE INDEX directory_runs_requested ON directory_runs (requested_at);

-- +goose Down
DROP TABLE directory_runs;
DROP TABLE directory_users;
