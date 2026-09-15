-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: CreateLinkFlow :exec
INSERT INTO link_flows (code, subject, expires_at)
VALUES ($1, $2, $3);

-- TakeLinkFlow redeems a code once: the row is gone whether or not it had
-- expired, so a code read over somebody's shoulder and typed twice binds
-- nothing the second time.
-- name: TakeLinkFlow :one
DELETE FROM link_flows
WHERE code = $1
RETURNING code, subject, expires_at;

-- name: DeleteExpiredLinkFlows :exec
DELETE FROM link_flows WHERE expires_at <= $1;
