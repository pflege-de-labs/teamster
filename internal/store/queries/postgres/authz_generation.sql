-- Code generated from ../sqlite by internal/store/queries/gen. DO NOT EDIT.
--
-- The statements are the SQLite ones with ? replaced by $n. Edit the SQLite
-- file and run make generate.

-- name: GetAuthzGeneration :one
SELECT generation FROM authz_generation WHERE id = 1;

-- name: BumpAuthzGeneration :exec
UPDATE authz_generation SET generation = generation + 1 WHERE id = 1;
