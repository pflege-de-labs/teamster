-- name: CreateLoginFlow :exec
INSERT INTO login_flows (state, verifier, nonce, expires_at)
VALUES (?, ?, ?, ?);

-- TakeLoginFlow redeems a state once: the row is gone whether or not it had
-- expired, so a replayed callback finds nothing.
-- name: TakeLoginFlow :one
DELETE FROM login_flows
WHERE state = ?
RETURNING state, verifier, nonce, expires_at;

-- name: DeleteExpiredLoginFlows :exec
DELETE FROM login_flows WHERE expires_at <= ?;
