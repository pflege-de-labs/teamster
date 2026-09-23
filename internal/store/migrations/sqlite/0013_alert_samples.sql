-- +goose Up
-- alert_samples remembers which label keys and values, and which annotation
-- keys, recent alerts carried, so the admin UI can complete them while someone
-- writes a template or a route (ADR 0041). It is a cache of what was seen,
-- not a record of any alert: nothing reads it on the delivery path.
--
-- Annotation rows always carry an empty value. Annotation values are free
-- text and may hold detail nobody asked this service to keep, so only the key
-- is stored. The primary key is what the sampler upserts on, and last_seen is
-- indexed because retention pruning deletes by it.
CREATE TABLE alert_samples (
	kind       TEXT NOT NULL CHECK (kind IN ('label', 'annotation')),
	key        TEXT NOT NULL,
	value      TEXT NOT NULL,
	seen_count INTEGER NOT NULL,
	first_seen DATETIME NOT NULL,
	last_seen  DATETIME NOT NULL,
	PRIMARY KEY (kind, key, value)
);

CREATE INDEX alert_samples_last_seen ON alert_samples (last_seen);

-- The per-key prune counts newer values of the same key for every row; without
-- this that count is a table scan, and on SQLite it holds the one connection.
CREATE INDEX alert_samples_key_recency ON alert_samples (kind, key, last_seen, value);

-- +goose Down
DROP TABLE alert_samples;
