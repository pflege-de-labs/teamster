-- ReapStaleClaimRecipient is ReapStaleClaim mirrored for a chat delivery; see
-- active_alerts.sql for why it is a statement of its own rather than folded
-- into the claim below.
-- name: ReapStaleClaimRecipient :execrows
DELETE FROM active_alert_recipients
WHERE fingerprint = ? AND recipient_id = ? AND posted_at IS NULL AND claimed_at <= ?;

-- ClaimActiveAlertRecipient takes the right to send or update the message for
-- one recipient. A row comes back only when the claim is ours; see
-- ClaimActiveAlert for what a caller does with the two ways this can come back
-- empty.
-- name: ClaimActiveAlertRecipient :one
INSERT INTO active_alert_recipients (
	fingerprint, recipient_id, status, message_id,
	claim_owner, claimed_at, posted_at, last_update)
VALUES (?, ?, ?, '', ?, ?, NULL, ?)
ON CONFLICT(fingerprint, recipient_id) DO NOTHING
RETURNING fingerprint, status, recipient_id, message_id, claim_owner, claimed_at, posted_at, last_update;

-- CompleteActiveAlertRecipientClaim records the message the claim produced.
-- See CompleteActiveAlertClaim: zero rows means somebody else's message is
-- recorded under this key, so the one just sent is an orphan.
-- name: CompleteActiveAlertRecipientClaim :one
INSERT INTO active_alert_recipients (
	fingerprint, recipient_id, status, message_id,
	claim_owner, claimed_at, posted_at, last_update)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(fingerprint, recipient_id) DO UPDATE SET
	message_id  = excluded.message_id,
	posted_at   = excluded.posted_at,
	status      = excluded.status,
	last_update = excluded.last_update
WHERE active_alert_recipients.posted_at IS NULL
  AND active_alert_recipients.claim_owner = excluded.claim_owner
RETURNING message_id;

-- ReleaseActiveAlertRecipientClaim hands a claim back when the send failed, so
-- the next attempt does not have to wait out the staleness cutoff.
-- name: ReleaseActiveAlertRecipientClaim :exec
DELETE FROM active_alert_recipients
WHERE fingerprint = ? AND recipient_id = ?
  AND claim_owner = ? AND posted_at IS NULL;

-- TouchActiveAlertRecipient records that an existing message was updated. It
-- matches on the message id, like TouchActiveAlert, so a resolve-then-refire
-- cycle does not have its newer row stamped by an update meant for the older
-- one. Matching an empty message id against an empty message id is an
-- ordinary equality, not a NULL comparison, so a card whose send returned no
-- id is still reachable here.
-- name: TouchActiveAlertRecipient :exec
UPDATE active_alert_recipients
SET status = ?, last_update = ?
WHERE fingerprint = ? AND recipient_id = ? AND message_id = ?;

-- ListActiveAlertRecipients returns every message sent for an alert, one per
-- recipient it fanned out to, and any claim still in flight.
-- name: ListActiveAlertRecipients :many
SELECT fingerprint, status, recipient_id, message_id, claim_owner, claimed_at, posted_at, last_update
FROM active_alert_recipients
WHERE fingerprint = ?
ORDER BY recipient_id;

-- name: GetActiveAlertRecipient :one
SELECT fingerprint, status, recipient_id, message_id, claim_owner, claimed_at, posted_at, last_update
FROM active_alert_recipients
WHERE fingerprint = ? AND recipient_id = ?;

-- DeleteActiveAlertRecipientCard removes the row for one message, and only if
-- it is still that message: a resolve that raced a refire must not delete the
-- new message's row.
-- name: DeleteActiveAlertRecipientCard :exec
DELETE FROM active_alert_recipients
WHERE fingerprint = ? AND recipient_id = ? AND message_id = ?;

-- DeleteActiveAlertRecipientsFor removes every in-flight or posted row for one
-- recipient, regardless of fingerprint or message id. It is what unlinking a
-- recipient runs inside the same transaction as the delete itself: an alert
-- claimed or posted to a person who no longer exists has nobody left to
-- notify and nothing left to keep, and a stranded row would otherwise fail a
-- resolve forever (see recipientMissing in webhooks.go for the mirror-image
-- defence against a row that was stranded some other way).
-- name: DeleteActiveAlertRecipientsFor :exec
DELETE FROM active_alert_recipients WHERE recipient_id = ?;
