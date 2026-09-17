-- +goose Up
-- The SQLite 0008 table, in this dialect. The numbering differs because the two
-- dialects share statements rather than history.
--
-- See migrations/sqlite/0008_webhook_endpoints.sql for why an endpoint names a
-- destination rather than a team and a channel, and why only a digest of the
-- token is kept.
CREATE TABLE webhook_endpoints (
	id TEXT PRIMARY KEY,
	team_slug TEXT NOT NULL,
	channel_slug TEXT NOT NULL,
	destination_id TEXT NOT NULL,
	token_hash TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX webhook_endpoints_slug ON webhook_endpoints (team_slug, channel_slug);

-- +goose Down
DROP INDEX webhook_endpoints_slug;

DROP TABLE webhook_endpoints;
