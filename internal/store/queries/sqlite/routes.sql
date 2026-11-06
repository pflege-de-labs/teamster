-- name: ListRoutes :many
SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at, recipient_id, addressed, webhook_endpoint_id, no_greedy_children
FROM routes
ORDER BY priority DESC, name;

-- name: GetRoute :one
SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at, recipient_id, addressed, webhook_endpoint_id, no_greedy_children
FROM routes
WHERE id = ?;

-- name: CreateRoute :exec
INSERT INTO routes (id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at, recipient_id, addressed, webhook_endpoint_id, no_greedy_children)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateRoute :exec
UPDATE routes
SET name = ?, parent_id = ?, greedy = ?, label_selector = ?, destination_id = ?, template_id = ?, is_default = ?, priority = ?, updated_at = ?, recipient_id = ?, addressed = ?, no_greedy_children = ?
WHERE id = ?;

-- name: DeleteRoute :exec
DELETE FROM routes WHERE id = ?;

-- name: UnlinkRoute :exec
UPDATE routes SET webhook_endpoint_id = '' WHERE id = ?;
