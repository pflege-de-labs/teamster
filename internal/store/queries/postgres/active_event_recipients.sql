-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- ReapStaleClaimRecipient is ReapStaleClaim mirrored for a chat delivery; see
-- active_events.sql for why it is a statement of its own rather than folded
-- into the claim below.
-- name: ReapStaleClaimRecipient :execrows
DELETE FROM active_event_recipients
WHERE event_key = $1 AND recipient_id = $2 AND posted_at IS NULL AND claimed_at <= $3;

-- ClaimActiveEventRecipient takes the right to send or update the message for
-- one recipient. A row comes back only when the claim is ours; see
-- ClaimActiveEvent for what a caller does with the two ways this can come back
-- empty.
-- name: ClaimActiveEventRecipient :one
INSERT INTO active_event_recipients (
	event_key, recipient_id, state, message_id,
	claim_owner, claimed_at, posted_at, last_update)
VALUES ($1, $2, $3, '', $4, $5, NULL, $6)
ON CONFLICT(event_key, recipient_id) DO NOTHING
RETURNING event_key, state, recipient_id, message_id, claim_owner, claimed_at, posted_at, last_update;

-- CompleteActiveEventRecipientClaim records the message the claim produced.
-- See CompleteActiveEventClaim: zero rows means somebody else's message is
-- recorded under this key, so the one just sent is an orphan.
-- name: CompleteActiveEventRecipientClaim :one
INSERT INTO active_event_recipients (
	event_key, recipient_id, state, message_id,
	claim_owner, claimed_at, posted_at, last_update)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT(event_key, recipient_id) DO UPDATE SET
	message_id  = excluded.message_id,
	posted_at   = excluded.posted_at,
	state      = excluded.state,
	last_update = excluded.last_update
WHERE active_event_recipients.posted_at IS NULL
  AND active_event_recipients.claim_owner = excluded.claim_owner
RETURNING message_id;

-- ReleaseActiveEventRecipientClaim hands a claim back when the send failed, so
-- the next attempt does not have to wait out the staleness cutoff.
-- name: ReleaseActiveEventRecipientClaim :exec
DELETE FROM active_event_recipients
WHERE event_key = $1 AND recipient_id = $2
  AND claim_owner = $3 AND posted_at IS NULL;

-- TouchActiveEventRecipient records that an existing message was updated. It
-- matches on the message id, like TouchActiveEvent, so a resolve-then-refire
-- cycle does not have its newer row stamped by an update meant for the older
-- one. Matching an empty message id against an empty message id is an
-- ordinary equality, not a NULL comparison, so a card whose send returned no
-- id is still reachable here.
-- name: TouchActiveEventRecipient :exec
UPDATE active_event_recipients
SET state = $1, last_update = $2
WHERE event_key = $3 AND recipient_id = $4 AND message_id = $5;

-- ListActiveEventRecipients returns every message sent for an event, one per
-- recipient it fanned out to, and any claim still in flight.
-- name: ListActiveEventRecipients :many
SELECT event_key, state, recipient_id, message_id, claim_owner, claimed_at, posted_at, last_update
FROM active_event_recipients
WHERE event_key = $1
ORDER BY recipient_id;

-- name: GetActiveEventRecipient :one
SELECT event_key, state, recipient_id, message_id, claim_owner, claimed_at, posted_at, last_update
FROM active_event_recipients
WHERE event_key = $1 AND recipient_id = $2;

-- DeleteActiveEventRecipientCard removes the row for one message, and only if
-- it is still that message: a resolve that raced a refire must not delete the
-- new message's row.
-- name: DeleteActiveEventRecipientCard :exec
DELETE FROM active_event_recipients
WHERE event_key = $1 AND recipient_id = $2 AND message_id = $3;

-- DeleteActiveEventRecipientsFor removes every in-flight or posted row for one
-- recipient, regardless of event_key or message id. It is what unlinking a
-- recipient runs inside the same transaction as the delete itself: an event
-- claimed or posted to a person who no longer exists has nobody left to
-- notify and nothing left to keep, and a stranded row would otherwise fail a
-- resolve forever (see recipientMissing in webhooks.go for the mirror-image
-- defence against a row that was stranded some other way).
-- name: DeleteActiveEventRecipientsFor :exec
DELETE FROM active_event_recipients WHERE recipient_id = $1;
