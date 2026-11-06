-- +goose Up
-- The SQLite 0033 change, in this dialect. See
-- migrations/sqlite/0033_route_no_greedy_children.sql for why it is shaped this way.
ALTER TABLE routes ADD COLUMN no_greedy_children BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE routes DROP COLUMN IF EXISTS no_greedy_children;
