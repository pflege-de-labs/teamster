-- +goose Up
-- The SQLite 0022 change, in this dialect. See
-- migrations/sqlite/0022_recipient_object_id_index.sql for why.
CREATE INDEX recipients_aad_object_id ON recipients (aad_object_id);

-- +goose Down
DROP INDEX IF EXISTS recipients_aad_object_id;
