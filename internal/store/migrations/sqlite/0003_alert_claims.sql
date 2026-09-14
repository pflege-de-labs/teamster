-- +goose Up
-- Posting a card is a read, a network call and a write, and nothing tied the
-- three together: two requests for the same firing alert both found no row and
-- both posted, then the second write overwrote the first message id and
-- orphaned a card in Teams forever. Claiming the row before the Graph call is
-- what makes the three atomic enough.
--
--   claim_owner  a token unique to one attempt, so completing a claim can tell
--                whether it is still the caller's to complete
--   claimed_at   when the claim was taken, which is what lets a claim left
--                behind by a killed process be recovered rather than block the
--                alert forever
--   posted_at    non-NULL exactly when a card exists. This is the state
--                machine: posted_at IS NULL means claimed but not yet posted.
--
-- SQLite cannot add a CHECK to an existing table, so the table is rebuilt —
-- the same move 0002 makes for the primary key, and for the same reason. The
-- constraint is worth the rebuild: it is what stops any future code path from
-- recording a card with no message id, or a message id nobody posted.
CREATE TABLE active_alerts_claimed (
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	claim_owner TEXT NOT NULL DEFAULT '',
	claimed_at DATETIME,
	posted_at DATETIME,
	last_update DATETIME NOT NULL,
	PRIMARY KEY (fingerprint, team_id, channel_id),
	CHECK ((posted_at IS NULL) = (message_id = ''))
);

-- Every existing row was written after its card was posted, so the backfill is
-- total: last_update is the closest thing to a posting time that was recorded.
INSERT INTO active_alerts_claimed (fingerprint, status, team_id, channel_id, message_id, claim_owner, claimed_at, posted_at, last_update)
SELECT fingerprint, status, team_id, channel_id, message_id, '', NULL, last_update, last_update
FROM active_alerts;

DROP TABLE active_alerts;

ALTER TABLE active_alerts_claimed RENAME TO active_alerts;

-- +goose Down
CREATE TABLE active_alerts_unclaimed (
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	last_update DATETIME NOT NULL,
	PRIMARY KEY (fingerprint, team_id, channel_id)
);

-- A claim that never became a card has no message id, and the old schema has
-- nowhere to say so; those rows are dropped rather than resurrected as cards
-- that do not exist.
INSERT INTO active_alerts_unclaimed (fingerprint, status, team_id, channel_id, message_id, last_update)
SELECT fingerprint, status, team_id, channel_id, message_id, last_update
FROM active_alerts
WHERE posted_at IS NOT NULL;

DROP TABLE active_alerts;

ALTER TABLE active_alerts_unclaimed RENAME TO active_alerts;
