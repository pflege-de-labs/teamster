-- name: GetAuthzGeneration :one
SELECT generation FROM authz_generation WHERE id = 1;

-- name: BumpAuthzGeneration :exec
UPDATE authz_generation SET generation = generation + 1 WHERE id = 1;
