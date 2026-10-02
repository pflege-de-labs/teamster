-- +goose Up
-- A message to everyone the bot can reach, delivered by a background run that
-- any replica may take over (ADR 0083). event and plan are the message and
-- its addressed deliveries as JSON. cursor is the last person delivered to,
-- so a takeover carries on from there. The previous release ignores the table.
CREATE TABLE broadcasts (
	id           TEXT PRIMARY KEY,
	requested_by TEXT NOT NULL,
	token_name   TEXT NOT NULL DEFAULT '',
	requested_at DATETIME NOT NULL,
	state        TEXT NOT NULL CHECK (state IN ('requested', 'running', 'done', 'failed')),
	event        TEXT NOT NULL,
	plan         TEXT NOT NULL,
	owner        TEXT NOT NULL DEFAULT '',
	heartbeat_at DATETIME,
	started_at   DATETIME,
	finished_at  DATETIME,
	cursor       TEXT NOT NULL DEFAULT '',
	total        INTEGER NOT NULL DEFAULT 0,
	delivered    INTEGER NOT NULL DEFAULT 0,
	unreachable  INTEGER NOT NULL DEFAULT 0,
	failed       INTEGER NOT NULL DEFAULT 0,
	last_error   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX broadcasts_queue ON broadcasts (state, requested_at);
CREATE INDEX broadcasts_requested_by ON broadcasts (requested_by, requested_at);

-- +goose Down
DROP TABLE broadcasts;
