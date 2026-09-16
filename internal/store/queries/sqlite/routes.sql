-- name: ListRoutes :many
SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at, recipient_id
FROM routes
ORDER BY priority DESC, name;

-- name: GetRoute :one
SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at, recipient_id
FROM routes
WHERE id = ?;

-- name: CreateRoute :exec
INSERT INTO routes (id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at, recipient_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateRoute :exec
UPDATE routes
SET name = ?, parent_id = ?, greedy = ?, label_selector = ?, destination_id = ?, template_id = ?, is_default = ?, priority = ?, updated_at = ?, recipient_id = ?
WHERE id = ?;

-- name: DeleteRoute :exec
DELETE FROM routes WHERE id = ?;
