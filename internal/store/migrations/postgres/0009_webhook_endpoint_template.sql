-- +goose Up
-- The SQLite 0012 change, in this dialect. See
-- migrations/sqlite/0012_webhook_endpoint_template.sql.
ALTER TABLE webhook_endpoints ADD COLUMN template_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE webhook_endpoints DROP COLUMN IF EXISTS template_id;
