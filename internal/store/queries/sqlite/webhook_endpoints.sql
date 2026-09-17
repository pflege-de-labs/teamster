-- name: ListWebhookEndpoints :many
SELECT id, team_slug, channel_slug, destination_id, token_hash, created_at, updated_at
FROM webhook_endpoints
ORDER BY team_slug, channel_slug;

-- name: GetWebhookEndpoint :one
SELECT id, team_slug, channel_slug, destination_id, token_hash, created_at, updated_at
FROM webhook_endpoints
WHERE id = ?;

-- GetWebhookEndpointBySlug is the request path, turned into a row. It is the
-- only lookup a sender can reach, so it matches on the pair alone and leaves
-- the token to a constant-time comparison outside SQL.
-- name: GetWebhookEndpointBySlug :one
SELECT id, team_slug, channel_slug, destination_id, token_hash, created_at, updated_at
FROM webhook_endpoints
WHERE team_slug = ? AND channel_slug = ?;

-- name: CreateWebhookEndpoint :exec
INSERT INTO webhook_endpoints (id, team_slug, channel_slug, destination_id, token_hash, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- The token is written by its own statement, so editing an endpoint cannot
-- silently rotate the secret the sender is using.
-- name: UpdateWebhookEndpoint :exec
UPDATE webhook_endpoints
SET team_slug = ?, channel_slug = ?, destination_id = ?, updated_at = ?
WHERE id = ?;

-- name: RotateWebhookEndpointToken :exec
UPDATE webhook_endpoints
SET token_hash = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteWebhookEndpoint :exec
DELETE FROM webhook_endpoints WHERE id = ?;
