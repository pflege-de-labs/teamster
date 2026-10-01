-- +goose Up
-- One row per principal and resource: the actions the principal holds on it
-- (ADR 0075). Each row is rendered to a Cedar permit policy. An own row makes
-- an owner; a resource without one is the admins'. resource_id '*' is the
-- collection, which is where create is granted.
CREATE TABLE permissions (
	id             TEXT PRIMARY KEY,
	principal_type TEXT NOT NULL CHECK (principal_type IN ('user', 'group', 'idp_group', 'role')),
	principal_id   TEXT NOT NULL,
	resource_type  TEXT NOT NULL,
	resource_id    TEXT NOT NULL,
	actions        TEXT NOT NULL,
	created_by     TEXT NOT NULL DEFAULT '',
	created_at     DATETIME NOT NULL,
	updated_at     DATETIME NOT NULL
);
CREATE UNIQUE INDEX permissions_key ON permissions (principal_type, principal_id, resource_type, resource_id);
CREATE INDEX permissions_resource ON permissions (resource_type, resource_id);

-- +goose Down
DROP TABLE permissions;
