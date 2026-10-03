-- +goose Up
-- The SQLite 0031 change, in this dialect. See
-- migrations/sqlite/0031_directory_user_profile.sql for what the column means.
ALTER TABLE directory_users ADD COLUMN profile TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE directory_users DROP COLUMN profile;
