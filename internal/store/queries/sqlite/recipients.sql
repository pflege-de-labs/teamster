-- name: ListRecipients :many
SELECT id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at
FROM recipients
ORDER BY name;

-- name: GetRecipient :one
SELECT id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at
FROM recipients
WHERE id = ?;

-- GetRecipientBySubject is how the linking flow finds an existing binding for
-- the person redeeming a code, so re-linking updates their row rather than
-- failing on the unique index.
-- name: GetRecipientBySubject :one
SELECT id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at
FROM recipients
WHERE subject = ?;

-- name: CreateRecipient :exec
INSERT INTO recipients (id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- The update deliberately leaves subject alone: it is who this binding belongs
-- to, and moving it would point one person's link at another's alerts. What
-- changes on a re-link is the conversation reference.
-- name: UpdateRecipient :exec
UPDATE recipients
SET name = ?, aad_object_id = ?, conversation_id = ?, service_url = ?, bot_channel_id = ?, tenant_id = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteRecipient :exec
DELETE FROM recipients WHERE id = ?;
