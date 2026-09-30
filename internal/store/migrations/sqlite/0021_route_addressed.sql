-- +goose Up
-- A route may deliver to the people a message names (ADR 0062). The default is
-- false, so every existing route keeps its target, and the previous release,
-- which never reads the column, sees an addressed route as one with no target
-- and delivers nothing through it rather than failing.
ALTER TABLE routes ADD COLUMN addressed INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE routes DROP COLUMN addressed;
