-- +goose Up
-- A route gains a second, independent target: a person's chat beside a Team
-- channel. Both empty columns are additive -- see routes.destination_id for
-- the same "empty means unset" convention -- so the previous release keeps
-- running against this schema exactly as it did before.
ALTER TABLE routes ADD COLUMN recipient_id TEXT NOT NULL DEFAULT '';

-- The recipient equivalent of active_alerts: one row per chat message this
-- service is keeping up to date for one alert. It is a sibling table rather
-- than a wider key on active_alerts because a person has neither a team_id
-- nor a channel_id for that table's CHECK to hold; see
-- migrations/sqlite/0003_alert_claims.sql for the state machine this copies.
--
-- The CHECK differs from active_alerts on purpose. That one requires
-- (posted_at IS NULL) = (message_id = ''), which would reject a posted row
-- with an empty message id -- but bot.SendMessage returning ("", nil) is a
-- documented success ("delivered, not updatable"), not a claim in flight. This
-- CHECK only rules out the state that is actually invalid: a message id
-- recorded before anything was posted.
CREATE TABLE active_alert_recipients (
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	recipient_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	claim_owner TEXT NOT NULL DEFAULT '',
	claimed_at DATETIME,
	posted_at DATETIME,
	last_update DATETIME NOT NULL,
	PRIMARY KEY (fingerprint, recipient_id),
	CHECK (posted_at IS NOT NULL OR message_id = '')
);

-- +goose Down
DROP TABLE active_alert_recipients;

ALTER TABLE routes DROP COLUMN recipient_id;
