-- +goose Up
-- A webhook endpoint is one URL a sender already has pointed at Microsoft
-- Teams, re-pointed here. It names the channel by two readable slugs rather
-- than by the Graph identifiers, because the slugs are what somebody types
-- into a sender's configuration and reads back in an access log.
--
-- destination_id rather than a team and channel of its own: an endpoint is a
-- second way to reach a channel that is already configured, so it inherits the
-- Teams picker that filled the destination and the grants that bound who may
-- choose it. Two names for one channel would let the two disagree.
--
-- token_hash is a SHA-256 digest, never the token. The token travels in the
-- path, which is what lets a sender that can only change a URL migrate at all,
-- and it is shown once at creation because nothing can recover it afterwards.
-- The digest is unsalted on purpose: the token is 32 random bytes, not a
-- password, so there is no dictionary to stretch against.
CREATE TABLE webhook_endpoints (
	id TEXT PRIMARY KEY,
	team_slug TEXT NOT NULL,
	channel_slug TEXT NOT NULL,
	destination_id TEXT NOT NULL,
	token_hash TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

-- The pair is the URL, so the pair is unique.
CREATE UNIQUE INDEX webhook_endpoints_slug ON webhook_endpoints (team_slug, channel_slug);

-- +goose Down
DROP INDEX webhook_endpoints_slug;

DROP TABLE webhook_endpoints;
