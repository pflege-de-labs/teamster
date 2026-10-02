-- +goose Up
-- The SQLite 0029 change, in this dialect. See
-- migrations/sqlite/0029_message_permissions.sql for what the columns mean.
ALTER TABLE users ADD COLUMN object_id TEXT NOT NULL DEFAULT '';
ALTER TABLE access_tokens ADD COLUMN message_scope TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE access_tokens DROP COLUMN message_scope;
ALTER TABLE users DROP COLUMN object_id;
