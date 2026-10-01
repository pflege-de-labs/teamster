-- +goose Up
-- audit_events records who changed the configuration, and what it looked like
-- before and after (ADR 0070). It is append-only: nothing updates a row, and
-- the only delete is retention pruning, which is why occurred_at is indexed.
--
-- before and after are JSON snapshots of the record, or NULL for a create and
-- a delete respectively. Secrets never reach them: the models already leave
-- token digests out of their JSON.
CREATE TABLE audit_events (
	id             TEXT PRIMARY KEY,
	occurred_at    DATETIME NOT NULL,
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
