-- ReapStaleClaim drops a claim a process took and never completed. It is
-- separate from the claim below rather than a WHERE clause on it because the
-- two need different timestamps -- the cutoff and the new claim's own -- and
-- sqlc's SQLite engine folds two parameters that infer the same column name
-- into one. Two statements in one transaction say the same thing and read
-- better: reap what is dead, then claim what is free.
-- name: ReapStaleClaim :execrows
DELETE FROM active_events
WHERE event_key = ? AND team_id = ? AND channel_id = ? AND posted_at IS NULL AND claimed_at <= ?;

-- ClaimActiveEvent takes the right to post the card for one channel. A row
-- comes back only when the claim is ours; a claim somebody else is still
-- working on, and a row that already carries a card, both leave this returning
-- nothing, and the caller reads the row to find out which.
-- name: ClaimActiveEvent :one
INSERT INTO active_events (
	event_key, team_id, channel_id, state, message_id,
	claim_owner, claimed_at, posted_at, last_update)
VALUES (?, ?, ?, ?, '', ?, ?, NULL, ?)
ON CONFLICT(event_key, team_id, channel_id) DO NOTHING
RETURNING event_key, state, team_id, channel_id, message_id, claim_owner, claimed_at, posted_at, last_update, conversation_id;

-- CompleteActiveEventClaim records the card the claim produced. The guard is
-- what makes a lost claim visible: zero rows means somebody else's card is
-- recorded under this key, so the one just posted is an orphan and the caller
-- has to say so rather than overwrite them.
-- name: CompleteActiveEventClaim :one
INSERT INTO active_events (
	event_key, team_id, channel_id, state, message_id,
	claim_owner, claimed_at, posted_at, last_update, conversation_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(event_key, team_id, channel_id) DO UPDATE SET
	message_id  = excluded.message_id,
	conversation_id = excluded.conversation_id,
	posted_at   = excluded.posted_at,
	state      = excluded.state,
	last_update = excluded.last_update
WHERE active_events.posted_at IS NULL
  AND active_events.claim_owner = excluded.claim_owner
RETURNING message_id;

-- ReleaseActiveEventClaim hands a claim back when the post failed, so the next
-- attempt does not have to wait out the staleness cutoff. Deleting is safe
-- because posted_at IS NULL says no card was ever created under it.
-- name: ReleaseActiveEventClaim :exec
DELETE FROM active_events
WHERE event_key = ? AND team_id = ? AND channel_id = ?
  AND claim_owner = ? AND posted_at IS NULL;

-- TouchActiveEvent records that an existing card was updated. It matches on the
-- message id so that a resolve-then-refire cycle, which replaces the card, does
-- not have its newer row stamped by an update to the older one.
-- name: TouchActiveEvent :exec
UPDATE active_events
SET state = ?, last_update = ?
WHERE event_key = ? AND team_id = ? AND channel_id = ? AND message_id = ?;

-- ListActiveEvents returns every card posted for an event, one per channel it
-- fanned out to, and any claim still in flight. The order is stable so that
-- delivery, and its tests, see them the same way every time.
-- name: ListActiveEvents :many
SELECT event_key, state, team_id, channel_id, message_id, claim_owner, claimed_at, posted_at, last_update, conversation_id
FROM active_events
WHERE event_key = ?
ORDER BY team_id, channel_id;

-- name: GetActiveEvent :one
SELECT event_key, state, team_id, channel_id, message_id, claim_owner, claimed_at, posted_at, last_update, conversation_id
FROM active_events
WHERE event_key = ? AND team_id = ? AND channel_id = ?;

-- DeleteActiveEventCard removes the row for one card, and only if it is still
-- that card: a resolve that raced a refire must not delete the new card's row.
-- name: DeleteActiveEventCard :exec
DELETE FROM active_events
WHERE event_key = ? AND team_id = ? AND channel_id = ? AND message_id = ?;

-- CountActiveEvents counts cards, not claims: a claim in flight is not yet
-- something this service is keeping up to date.
-- name: CountActiveEvents :one
SELECT COUNT(*) FROM active_events WHERE posted_at IS NOT NULL;
