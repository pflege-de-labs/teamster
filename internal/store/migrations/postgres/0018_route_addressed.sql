-- +goose Up
-- The SQLite 0021 change, in this dialect. See
-- migrations/sqlite/0021_route_addressed.sql for why it is shaped this way.
ALTER TABLE routes ADD COLUMN addressed BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE routes DROP COLUMN IF EXISTS addressed;
