-- +goose Up
-- The SQLite 0006 change, in this dialect. See
-- migrations/sqlite/0006_recipient_routing.sql for why the CHECK differs from
-- active_alerts and why both new columns are additive.
ALTER TABLE routes ADD COLUMN recipient_id TEXT NOT NULL DEFAULT '';

CREATE TABLE active_alert_recipients (
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	recipient_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	claim_owner TEXT NOT NULL DEFAULT '',
	claimed_at TIMESTAMPTZ,
	posted_at TIMESTAMPTZ,
	last_update TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (fingerprint, recipient_id),
	CHECK (posted_at IS NOT NULL OR message_id = '')
);

-- +goose Down
DROP TABLE IF EXISTS active_alert_recipients;

ALTER TABLE routes DROP COLUMN IF EXISTS recipient_id;
