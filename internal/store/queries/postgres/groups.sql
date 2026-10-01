-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: ListGroups :many
SELECT id, name, description, created_by, created_at, updated_at
FROM user_groups
ORDER BY lower(name), id;

-- name: GetGroup :one
SELECT id, name, description, created_by, created_at, updated_at
FROM user_groups
WHERE id = sqlc.arg(id);

-- name: CreateGroup :exec
INSERT INTO user_groups (id, name, description, created_by, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(description), sqlc.arg(created_by), sqlc.arg(at), sqlc.arg(at));

-- name: UpdateGroup :execrows
UPDATE user_groups
SET name = sqlc.arg(name), description = sqlc.arg(description), updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id);

-- name: DeleteGroup :execrows
DELETE FROM user_groups WHERE id = sqlc.arg(id);

-- name: DeleteGroupMemberships :exec
-- Both directions: the group's own members, and the group as someone's member.
DELETE FROM user_group_members
WHERE group_id = sqlc.arg(id) OR (member_type = 'group' AND member_id = sqlc.arg(id));

-- name: ListGroupMembers :many
SELECT group_id, member_type, member_id, added_by, added_at
FROM user_group_members
WHERE group_id = sqlc.arg(group_id)
ORDER BY member_type, member_id;

-- name: ListAllGroupMembers :many
SELECT group_id, member_type, member_id, added_by, added_at
FROM user_group_members
ORDER BY group_id, member_type, member_id;

-- name: AddGroupMember :exec
INSERT INTO user_group_members (group_id, member_type, member_id, added_by, added_at)
VALUES (sqlc.arg(group_id), sqlc.arg(member_type), sqlc.arg(member_id), sqlc.arg(added_by), sqlc.arg(added_at))
ON CONFLICT (group_id, member_type, member_id) DO NOTHING;

-- name: RemoveGroupMember :execrows
DELETE FROM user_group_members
WHERE group_id = sqlc.arg(group_id) AND member_type = sqlc.arg(member_type) AND member_id = sqlc.arg(member_id);
