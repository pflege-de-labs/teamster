-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListTemplates :many
SELECT id, name, title, message_text, body, created_at, updated_at
FROM templates
ORDER BY name;

-- name: GetTemplate :one
SELECT id, name, title, message_text, body, created_at, updated_at
FROM templates
WHERE id = $1;

-- name: CreateTemplate :exec
INSERT INTO templates (id, name, title, message_text, body, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: UpdateTemplate :exec
UPDATE templates
SET name = $1, title = $2, message_text = $3, body = $4, updated_at = $5
WHERE id = $6;

-- name: DeleteTemplate :exec
DELETE FROM templates WHERE id = $1;
