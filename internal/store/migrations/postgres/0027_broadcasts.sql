-- +goose Up
-- The SQLite 0030 change, in this dialect. See
-- migrations/sqlite/0030_broadcasts.sql for what the columns mean.
CREATE TABLE broadcasts (
	id           TEXT PRIMARY KEY,
	requested_by TEXT NOT NULL,
	token_name   TEXT NOT NULL DEFAULT '',
	requested_at TIMESTAMPTZ NOT NULL,
	state        TEXT NOT NULL CHECK (state IN ('requested', 'running', 'done', 'failed')),
	event        TEXT NOT NULL,
	plan         TEXT NOT NULL,
	owner        TEXT NOT NULL DEFAULT '',
	heartbeat_at TIMESTAMPTZ,
	started_at   TIMESTAMPTZ,
	finished_at  TIMESTAMPTZ,
	cursor       TEXT NOT NULL DEFAULT '',
	total        BIGINT NOT NULL DEFAULT 0,
	delivered    BIGINT NOT NULL DEFAULT 0,
	unreachable  BIGINT NOT NULL DEFAULT 0,
	failed       BIGINT NOT NULL DEFAULT 0,
	last_error   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX broadcasts_queue ON broadcasts (state, requested_at);
CREATE INDEX broadcasts_requested_by ON broadcasts (requested_by, requested_at);

-- +goose Down
DROP TABLE broadcasts;
