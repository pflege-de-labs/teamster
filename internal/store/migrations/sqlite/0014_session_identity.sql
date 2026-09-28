-- +goose Up
-- What the identity provider said about a session's holder beyond its roles,
-- as JSON, for the user info page (ADR 0043). Additive with a default, so the
-- previous release keeps inserting sessions without it.
ALTER TABLE sessions ADD COLUMN identity TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN identity;
