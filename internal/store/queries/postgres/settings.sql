-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: GetSetting :one
SELECT value FROM settings WHERE key = $1;

-- name: UpsertSetting :exec
INSERT INTO settings (key, value, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT(key) DO UPDATE SET
	value      = excluded.value,
	updated_at = excluded.updated_at;

-- name: DeleteSetting :exec
DELETE FROM settings WHERE key = $1;

-- ClearSettingValue drops a key only while it still holds value, so deleting
-- a template forgets it as the catch-all's without touching another choice.
-- name: ClearSettingValue :exec
DELETE FROM settings WHERE key = $1 AND value = $2;

-- InsertSettingIfAbsent claims a key: it affects no row when another writer
-- got there first, so a one-time step runs once even across replicas.
-- name: InsertSettingIfAbsent :execrows
INSERT INTO settings (key, value, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT(key) DO NOTHING;

-- ReplaceSettingValue moves a key from one value to the next, and only from
-- that one: two replicas advancing the same cursor cannot both win.
-- name: ReplaceSettingValue :execrows
UPDATE settings SET value = sqlc.arg(next), updated_at = sqlc.arg(updated_at)
WHERE key = sqlc.arg(key) AND value = sqlc.arg(current);
