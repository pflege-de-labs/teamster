-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListRoutes :many
SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at
FROM routes
ORDER BY priority DESC, name;

-- name: GetRoute :one
SELECT id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at
FROM routes
WHERE id = $1;

-- name: CreateRoute :exec
INSERT INTO routes (id, name, parent_id, greedy, label_selector, destination_id, template_id, is_default, priority, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: UpdateRoute :exec
UPDATE routes
SET name = $1, parent_id = $2, greedy = $3, label_selector = $4, destination_id = $5, template_id = $6, is_default = $7, priority = $8, updated_at = $9
WHERE id = $10;

-- name: DeleteRoute :exec
DELETE FROM routes WHERE id = $1;
