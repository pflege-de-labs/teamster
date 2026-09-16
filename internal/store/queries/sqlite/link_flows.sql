-- name: CreateLinkFlow :exec
INSERT INTO link_flows (code, subject, expires_at)
VALUES (?, ?, ?);

-- TakeLinkFlow redeems a code once: the row is gone whether or not it had
-- expired, so a code read over somebody's shoulder and typed twice binds
-- nothing the second time.
-- name: TakeLinkFlow :one
DELETE FROM link_flows
WHERE code = ?
RETURNING code, subject, expires_at;

-- name: DeleteExpiredLinkFlows :exec
DELETE FROM link_flows WHERE expires_at <= ?;

-- Minting a new code for a subject retires whatever it had outstanding, so a
-- guess only ever has to beat one live code rather than every one ever handed
-- out before the hourly sweep catches up.
-- name: DeleteLinkFlowsForSubject :exec
DELETE FROM link_flows WHERE subject = ?;
