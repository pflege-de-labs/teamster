-- +goose Up
-- A broker_tokens row is the live Keycloak access and refresh token for one
-- session, kept only so the admin UI can ask Keycloak's broker endpoint for
-- the Entra token it stored when it federated that login (ADR 0037). The
-- session id is the primary key rather than a surrogate one: there is exactly
-- one live Keycloak token per session, and deleting the session is what ends
-- it (see deleteSessionCascade in internal/store/adapter.go, the same
-- Go-level cascade deleteRecipientCascade already uses -- this repo declares
-- no SQL foreign keys).
--
-- access_token and refresh_token are ciphertext, not the tokens themselves:
-- internal/httpserver/broker.go seals them with internal/cryptutil before
-- they reach this table, keyed to the session id as additional data so a row
-- cannot be replayed onto a different session.
CREATE TABLE broker_tokens (
	session_id    TEXT PRIMARY KEY,
	access_token  TEXT NOT NULL,
	refresh_token TEXT NOT NULL,
	expires_at    DATETIME NOT NULL,
	updated_at    DATETIME NOT NULL
);

-- +goose Down
DROP TABLE broker_tokens;
