-- +goose Up
-- A Teams V2 endpoint may name the template its messages render with
-- (ADR 0040). Empty means none, the same convention routes.template_id
-- uses, so the previous release keeps inserting endpoints without one.
ALTER TABLE webhook_endpoints ADD COLUMN template_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE webhook_endpoints DROP COLUMN template_id;
