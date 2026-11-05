-- +goose Up
-- The webhook endpoint a route was made for, or '' for a route someone wrote.
-- At most one route per endpoint. The previous release ignores the column.
ALTER TABLE routes ADD COLUMN webhook_endpoint_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX routes_webhook_endpoint_id ON routes (webhook_endpoint_id) WHERE webhook_endpoint_id <> '';

-- +goose Down
DROP INDEX routes_webhook_endpoint_id;
ALTER TABLE routes DROP COLUMN webhook_endpoint_id;
