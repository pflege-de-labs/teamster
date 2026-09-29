-- +goose Up
-- The SQLite 0017 change, in this dialect. See
-- migrations/sqlite/0017_settings.sql for why it is shaped this way.
CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE settings;
