-- +goose Up
-- The SQLite 0027 change, in this dialect. See
-- migrations/sqlite/0027_permissions.sql for why it is shaped this way.
CREATE TABLE permissions (
	id             TEXT PRIMARY KEY,
	principal_type TEXT NOT NULL CHECK (principal_type IN ('user', 'group', 'idp_group', 'role')),
	principal_id   TEXT NOT NULL,
	resource_type  TEXT NOT NULL,
	resource_id    TEXT NOT NULL,
	actions        TEXT NOT NULL,
	created_by     TEXT NOT NULL DEFAULT '',
	created_at     TIMESTAMPTZ NOT NULL,
	updated_at     TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX permissions_key ON permissions (principal_type, principal_id, resource_type, resource_id);
CREATE INDEX permissions_resource ON permissions (resource_type, resource_id);

-- +goose Down
DROP TABLE permissions;
