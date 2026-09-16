-- +goose Up
-- The SQLite 0007 change, in this dialect. See
-- migrations/sqlite/0007_recipient_blocked.sql for why both columns are
-- additive and what the flag means.
ALTER TABLE recipients ADD COLUMN blocked_at TIMESTAMPTZ;
ALTER TABLE recipients ADD COLUMN blocked_reason TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE recipients DROP COLUMN IF EXISTS blocked_reason;
ALTER TABLE recipients DROP COLUMN IF EXISTS blocked_at;
