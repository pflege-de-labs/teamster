-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListRecipients :many
SELECT id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at, blocked_at, blocked_reason
FROM recipients
ORDER BY name;

-- name: GetRecipient :one
SELECT id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at, blocked_at, blocked_reason
FROM recipients
WHERE id = $1;

-- GetRecipientBySubject is how the linking flow finds an existing binding for
-- the person redeeming a code, so re-linking updates their row rather than
-- failing on the unique index.
-- name: GetRecipientBySubject :one
SELECT id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at, blocked_at, blocked_reason
FROM recipients
WHERE subject = $1;

-- name: CreateRecipient :exec
INSERT INTO recipients (id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at, blocked_at, blocked_reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- The update deliberately leaves subject alone: it is who this binding belongs
-- to, and moving it would point one person's link at another's alerts. What
-- changes on a re-link is the conversation reference.
--
-- blocked_at and blocked_reason are written here too, not left to the narrow
-- statements below: a re-link is the person proving the chat works again, so
-- whatever redeems the code passes the zero value for both and the flag
-- clears itself, the same self-healing rule a successful delivery follows.
-- name: UpdateRecipient :exec
UPDATE recipients
SET name = $1, aad_object_id = $2, conversation_id = $3, service_url = $4, bot_channel_id = $5, tenant_id = $6, updated_at = $7, blocked_at = $8, blocked_reason = $9
WHERE id = $10;

-- name: DeleteRecipient :exec
DELETE FROM recipients WHERE id = $1;

-- MarkRecipientBlocked records a permanent send failure. It is a narrow
-- statement, not a call through UpdateRecipient, so a delivery failure --
-- which only ever reads the conversation reference, never the rest of the row
-- -- cannot clobber a field it never loaded.
-- name: MarkRecipientBlocked :exec
UPDATE recipients
SET blocked_at = $1, blocked_reason = $2
WHERE id = $3;

-- ClearRecipientBlocked is MarkRecipientBlocked's mirror, run after a
-- successful send or update. It is a no-op, not an error, on a recipient that
-- was never blocked: delivery calls it after every success, not only after a
-- recovery.
-- name: ClearRecipientBlocked :exec
UPDATE recipients
SET blocked_at = NULL, blocked_reason = ''
WHERE id = $1;
