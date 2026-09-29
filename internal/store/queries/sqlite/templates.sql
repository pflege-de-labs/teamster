-- name: ListTemplates :many
SELECT id, name, title, message_text, body, created_at, updated_at, sources
FROM templates
ORDER BY name;

-- name: GetTemplate :one
SELECT id, name, title, message_text, body, created_at, updated_at, sources
FROM templates
WHERE id = ?;

-- name: CreateTemplate :exec
INSERT INTO templates (id, name, title, message_text, body, sources, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateTemplate :exec
UPDATE templates
SET name = ?, title = ?, message_text = ?, body = ?, sources = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteTemplate :exec
DELETE FROM templates WHERE id = ?;
