-- +goose Up
-- The SQLite 0028 change, in this dialect. See
-- migrations/sqlite/0028_access_token_scope.sql for why it is shaped this way.
ALTER TABLE access_tokens ADD COLUMN scope TEXT NOT NULL DEFAULT '';
ALTER TABLE access_tokens ADD COLUMN scoped_token_hash TEXT;
CREATE UNIQUE INDEX access_tokens_scoped_hash ON access_tokens (scoped_token_hash);

-- +goose Down
DROP INDEX access_tokens_scoped_hash;
ALTER TABLE access_tokens DROP COLUMN scoped_token_hash;
ALTER TABLE access_tokens DROP COLUMN scope;
