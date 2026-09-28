-- +goose Up
-- The SQLite 0014 change, in this dialect. See
-- migrations/sqlite/0014_session_identity.sql for why it is shaped this way.
ALTER TABLE sessions ADD COLUMN identity TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN IF EXISTS identity;
