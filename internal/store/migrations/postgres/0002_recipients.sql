-- +goose Up
-- The SQLite 0005 tables, in this dialect. The numbering differs because the
-- two dialects share statements rather than history: the Postgres baseline was
-- written once, after SQLite had already reached it through four migrations.
--
-- See migrations/sqlite/0005_recipients.sql for why subject is unique and why
-- the conversation reference's channel is called bot_channel_id.
CREATE TABLE recipients (
	id TEXT PRIMARY KEY,
	subject TEXT NOT NULL,
	name TEXT NOT NULL,
	aad_object_id TEXT NOT NULL DEFAULT '',
	conversation_id TEXT NOT NULL,
	service_url TEXT NOT NULL,
	bot_channel_id TEXT NOT NULL,
	tenant_id TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX recipients_subject ON recipients (subject);

CREATE TABLE link_flows (
	code TEXT PRIMARY KEY,
	subject TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS link_flows;

DROP INDEX IF EXISTS recipients_subject;

DROP TABLE IF EXISTS recipients;
