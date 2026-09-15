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
