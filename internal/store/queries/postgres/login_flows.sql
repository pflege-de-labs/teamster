-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: CreateLoginFlow :exec
INSERT INTO login_flows (state, verifier, nonce, expires_at)
VALUES ($1, $2, $3, $4);

-- TakeLoginFlow redeems a state once: the row is gone whether or not it had
-- expired, so a replayed callback finds nothing.
-- name: TakeLoginFlow :one
DELETE FROM login_flows
WHERE state = $1
RETURNING state, verifier, nonce, expires_at;

-- name: DeleteExpiredLoginFlows :exec
DELETE FROM login_flows WHERE expires_at <= $1;
