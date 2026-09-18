-- +goose Up
-- The SQLite 0009 index, in this dialect. See
-- migrations/sqlite/0009_recipient_conversation_index.sql for why it is not
-- unique.
CREATE INDEX recipients_conversation ON recipients (conversation_id);

-- +goose Down
DROP INDEX recipients_conversation;
