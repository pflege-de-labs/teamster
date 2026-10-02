-- +goose Up
-- Messages to the people a message names need a permission (ADR 0082).
-- object_id is the Entra object id from the user's last sign-in, which is how
-- "only to yourself" is told apart from "to someone else". message_scope is
-- how far a token may address people: '' (not at all), self or anyone. The
-- previous release reads neither column.
ALTER TABLE users ADD COLUMN object_id TEXT NOT NULL DEFAULT '';
ALTER TABLE access_tokens ADD COLUMN message_scope TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE access_tokens DROP COLUMN message_scope;
ALTER TABLE users DROP COLUMN object_id;
