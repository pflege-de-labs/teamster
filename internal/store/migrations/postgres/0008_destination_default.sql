-- +goose Up
-- The SQLite 0011 change, in this dialect. See
-- migrations/sqlite/0011_destination_default.sql for why it is shaped this way.
ALTER TABLE destinations ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX destinations_single_default ON destinations (is_default) WHERE is_default;

UPDATE destinations SET is_default = TRUE
WHERE id = (SELECT id FROM destinations ORDER BY created_at, id LIMIT 1);

-- +goose Down
DROP INDEX IF EXISTS destinations_single_default;

ALTER TABLE destinations DROP COLUMN IF EXISTS is_default;
