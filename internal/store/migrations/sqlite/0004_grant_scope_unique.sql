-- +goose Up
-- A grant is a role and a scope, so the same role granted the same scope twice
-- is a duplicate rather than a second permission. Nothing stopped it: two
-- admins saving the permissions page at once, or one double-submitting, each
-- wrote their own row, and the page then showed the scope twice.
--
-- Existing duplicates are collapsed onto the oldest row before the index goes
-- on, because a migration that fails on data somebody already has is a
-- migration that cannot be applied.
DELETE FROM grants
WHERE id NOT IN (
	SELECT min(id) FROM grants GROUP BY role, team_id, channel_id
);

CREATE UNIQUE INDEX grants_role_scope ON grants (role, team_id, channel_id);

-- +goose Down
DROP INDEX grants_role_scope;
