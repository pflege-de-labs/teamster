-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListRecipients :many
SELECT id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at
FROM recipients
ORDER BY name;

-- name: GetRecipient :one
SELECT id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at
FROM recipients
WHERE id = $1;

-- GetRecipientBySubject is how the linking flow finds an existing binding for
-- the person redeeming a code, so re-linking updates their row rather than
-- failing on the unique index.
-- name: GetRecipientBySubject :one
SELECT id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at
FROM recipients
WHERE subject = $1;

-- name: CreateRecipient :exec
INSERT INTO recipients (id, subject, name, aad_object_id, conversation_id, service_url, bot_channel_id, tenant_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- The update deliberately leaves subject alone: it is who this binding belongs
-- to, and moving it would point one person's link at another's alerts. What
-- changes on a re-link is the conversation reference.
-- name: UpdateRecipient :exec
UPDATE recipients
SET name = $1, aad_object_id = $2, conversation_id = $3, service_url = $4, bot_channel_id = $5, tenant_id = $6, updated_at = $7
WHERE id = $8;

-- name: DeleteRecipient :exec
DELETE FROM recipients WHERE id = $1;
