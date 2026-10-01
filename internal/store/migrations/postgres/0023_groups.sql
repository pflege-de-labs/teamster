-- +goose Up
-- The SQLite 0026 change, in this dialect. See
-- migrations/sqlite/0026_groups.sql for why it is shaped this way.
CREATE TABLE user_groups (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	created_by  TEXT NOT NULL DEFAULT '',
	created_at  TIMESTAMPTZ NOT NULL,
	updated_at  TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX user_groups_name ON user_groups (name);

CREATE TABLE user_group_members (
	group_id    TEXT NOT NULL,
	member_type TEXT NOT NULL CHECK (member_type IN ('user', 'group', 'idp_group')),
	member_id   TEXT NOT NULL,
	added_by    TEXT NOT NULL DEFAULT '',
	added_at    TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (group_id, member_type, member_id)
);
-- Which groups a principal is in: the direction authorization reads.
CREATE INDEX user_group_members_member ON user_group_members (member_type, member_id);

-- +goose Down
DROP TABLE user_group_members;
DROP TABLE user_groups;
