-- +goose Up
-- sources lists the webhooks a template handles, comma separated, from
-- alertmanager, universal and teamsv2 (ADR 0053). Empty means any, which is
-- what every existing template and the previous release's writes are.
ALTER TABLE templates ADD COLUMN sources TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE templates DROP COLUMN sources;
