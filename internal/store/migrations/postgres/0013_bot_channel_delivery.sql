-- +goose Up
-- The SQLite 0016 change, in this dialect. See
-- migrations/sqlite/0016_bot_channel_delivery.sql for why it is shaped this way.
ALTER TABLE active_alerts ADD COLUMN conversation_id TEXT NOT NULL DEFAULT '';

CREATE TABLE bot_teams (
	team_id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	service_url TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE bot_teams;

ALTER TABLE active_alerts DROP COLUMN IF EXISTS conversation_id;
