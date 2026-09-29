-- name: GetSetting :one
SELECT value FROM settings WHERE key = ?;

-- name: UpsertSetting :exec
INSERT INTO settings (key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
	value      = excluded.value,
	updated_at = excluded.updated_at;

-- name: DeleteSetting :exec
DELETE FROM settings WHERE key = ?;

-- ClearSettingValue drops a key only while it still holds value, so deleting
-- a template forgets it as the catch-all's without touching another choice.
-- name: ClearSettingValue :exec
DELETE FROM settings WHERE key = ? AND value = ?;

-- InsertSettingIfAbsent claims a key: it affects no row when another writer
-- got there first, so a one-time step runs once even across replicas.
-- name: InsertSettingIfAbsent :execrows
INSERT INTO settings (key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO NOTHING;
