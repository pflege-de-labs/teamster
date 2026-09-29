-- +goose Up
-- The SQLite 0018 change, in this dialect. See
-- migrations/sqlite/0018_template_sources.sql for why it is shaped this way.
ALTER TABLE templates ADD COLUMN sources TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE templates DROP COLUMN IF EXISTS sources;
