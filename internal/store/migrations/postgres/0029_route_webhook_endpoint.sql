-- +goose Up
-- The SQLite 0032 change, in this dialect. See
-- migrations/sqlite/0032_route_webhook_endpoint.sql for what the column means.
ALTER TABLE routes ADD COLUMN webhook_endpoint_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX routes_webhook_endpoint_id ON routes (webhook_endpoint_id) WHERE webhook_endpoint_id <> '';

-- +goose Down
DROP INDEX routes_webhook_endpoint_id;
ALTER TABLE routes DROP COLUMN webhook_endpoint_id;
