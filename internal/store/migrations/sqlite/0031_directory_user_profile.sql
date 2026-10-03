-- +goose Up
-- What Entra says about a person beyond their name, as JSON, for templates to
-- address them by (ADR 0086). Nothing filters on it, so one column rather
-- than one per field. The previous release ignores it.
ALTER TABLE directory_users ADD COLUMN profile TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE directory_users DROP COLUMN profile;
