-- +goose Up
-- The SQLite 0023 change, in this dialect. See
-- migrations/sqlite/0023_audit_events.sql for why it is shaped this way.
CREATE TABLE audit_events (
	id             TEXT PRIMARY KEY,
	occurred_at    TIMESTAMPTZ NOT NULL,
	actor_subject  TEXT NOT NULL,
	actor_name     TEXT NOT NULL DEFAULT '',
	actor_via      TEXT NOT NULL,
	actor_token_id TEXT NOT NULL DEFAULT '',
	action         TEXT NOT NULL,
	resource_type  TEXT NOT NULL,
	resource_id    TEXT NOT NULL DEFAULT '',
	request_id     TEXT NOT NULL DEFAULT '',
	before         TEXT,
	after          TEXT
);
CREATE INDEX audit_events_occurred ON audit_events (occurred_at, id);
CREATE INDEX audit_events_resource ON audit_events (resource_type, resource_id);
CREATE INDEX audit_events_actor ON audit_events (actor_subject);

-- +goose Down
DROP TABLE audit_events;
