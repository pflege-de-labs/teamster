-- name: UpsertActiveAlert :exec
INSERT INTO active_alerts (fingerprint, status, team_id, channel_id, message_id, last_update)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(fingerprint, team_id, channel_id) DO UPDATE SET
	status = excluded.status,
	message_id = excluded.message_id,
	last_update = excluded.last_update;

-- ListActiveAlerts returns every card posted for an alert, one per channel it
-- fanned out to. The order is stable so that delivery, and its tests, see the
-- cards the same way every time.
-- name: ListActiveAlerts :many
SELECT fingerprint, status, team_id, channel_id, message_id, last_update
FROM active_alerts
WHERE fingerprint = ?
ORDER BY team_id, channel_id;

-- name: GetActiveAlert :one
SELECT fingerprint, status, team_id, channel_id, message_id, last_update
FROM active_alerts
WHERE fingerprint = ? AND team_id = ? AND channel_id = ?;

-- name: DeleteActiveAlert :exec
DELETE FROM active_alerts
WHERE fingerprint = ? AND team_id = ? AND channel_id = ?;

-- name: CountActiveAlerts :one
SELECT COUNT(*) FROM active_alerts;
