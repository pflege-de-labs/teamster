-- +goose Up
-- The SQLite 0013 table, in this dialect. See migrations/sqlite/0013_alert_samples.sql
-- for what it holds and why annotation values are never stored. seen_count is
-- BIGINT so both backends produce the same Go type from it.
CREATE TABLE alert_samples (
	kind       TEXT NOT NULL CHECK (kind IN ('label', 'annotation')),
	key        TEXT NOT NULL,
	value      TEXT NOT NULL,
	seen_count BIGINT NOT NULL,
	first_seen TIMESTAMPTZ NOT NULL,
	last_seen  TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (kind, key, value)
);

CREATE INDEX alert_samples_last_seen ON alert_samples (last_seen);

-- See the SQLite migration: backs the per-key prune's correlated count.
CREATE INDEX alert_samples_key_recency ON alert_samples (kind, key, last_seen, value);

-- +goose Down
DROP TABLE alert_samples;
