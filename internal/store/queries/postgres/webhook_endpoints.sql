-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListWebhookEndpoints :many
SELECT id, team_slug, channel_slug, destination_id, token_hash, created_at, updated_at
FROM webhook_endpoints
ORDER BY team_slug, channel_slug;

-- name: GetWebhookEndpoint :one
SELECT id, team_slug, channel_slug, destination_id, token_hash, created_at, updated_at
FROM webhook_endpoints
WHERE id = $1;

-- GetWebhookEndpointBySlug is the request path, turned into a row. It is the
-- only lookup a sender can reach, so it matches on the pair alone and leaves
-- the token to a constant-time comparison outside SQL.
-- name: GetWebhookEndpointBySlug :one
SELECT id, team_slug, channel_slug, destination_id, token_hash, created_at, updated_at
FROM webhook_endpoints
WHERE team_slug = $1 AND channel_slug = $2;

-- name: CreateWebhookEndpoint :exec
INSERT INTO webhook_endpoints (id, team_slug, channel_slug, destination_id, token_hash, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- The token is written by its own statement, so editing an endpoint cannot
-- silently rotate the secret the sender is using.
-- name: UpdateWebhookEndpoint :exec
UPDATE webhook_endpoints
SET team_slug = $1, channel_slug = $2, destination_id = $3, updated_at = $4
WHERE id = $5;

-- name: RotateWebhookEndpointToken :exec
UPDATE webhook_endpoints
SET token_hash = $1, updated_at = $2
WHERE id = $3;

-- name: DeleteWebhookEndpoint :exec
DELETE FROM webhook_endpoints WHERE id = $1;
