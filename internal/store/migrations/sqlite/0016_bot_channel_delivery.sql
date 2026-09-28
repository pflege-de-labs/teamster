-- +goose Up
-- Channel delivery moves onto the Bot Framework bot (ADR 0045).
--
-- conversation_id is the channel conversation the bot created for a card. It
-- is what an edit addresses, beside the activity id in message_id. The default
-- is empty, so the previous release keeps inserting rows without it, and a row
-- it wrote is one the bot cannot edit.
ALTER TABLE active_alerts ADD COLUMN conversation_id TEXT NOT NULL DEFAULT '';

-- bot_teams is where the bot is installed, keyed by the Graph team id a
-- destination stores (channelData.team.aadGroupId). service_url is regional
-- and only ever taken from an authenticated inbound activity.
CREATE TABLE bot_teams (
	team_id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	service_url TEXT NOT NULL,
	updated_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE bot_teams;

ALTER TABLE active_alerts DROP COLUMN conversation_id;
