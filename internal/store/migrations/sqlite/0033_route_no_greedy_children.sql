-- +goose Up
-- A route may keep its children from replacing it (ADR 0096). The default is
-- false, so every existing route keeps its behaviour, and the previous release,
-- which never reads or writes the column, leaves the stored value alone.
ALTER TABLE routes ADD COLUMN no_greedy_children INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE routes DROP COLUMN no_greedy_children;
