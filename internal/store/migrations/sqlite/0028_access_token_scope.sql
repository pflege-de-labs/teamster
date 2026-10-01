-- +goose Up
-- A scoped token names the webhooks it may send to, and is checked against
-- its creator's permissions on every use (ADR 0077). Its digest lives in
-- scoped_token_hash; token_hash holds a placeholder no digest equals, so the
-- previous release, which matches token_hash only, refuses it rather than
-- admitting it to every route. An empty scope is a token from before scopes.
ALTER TABLE access_tokens ADD COLUMN scope TEXT NOT NULL DEFAULT '';
ALTER TABLE access_tokens ADD COLUMN scoped_token_hash TEXT;
CREATE UNIQUE INDEX access_tokens_scoped_hash ON access_tokens (scoped_token_hash);

-- +goose Down
DROP INDEX access_tokens_scoped_hash;
ALTER TABLE access_tokens DROP COLUMN scoped_token_hash;
ALTER TABLE access_tokens DROP COLUMN scope;
