-- +goose Up
-- settings holds single installation-wide values edited in the admin UI, one
-- row per key. The first is global_default.template_id (ADR 0050): the
-- template the catch-all route renders with. No row means the built-in
-- default message, which is what the previous release always sends.
CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE settings;
