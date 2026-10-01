-- +goose Up
-- The SQLite 0025 change, in this dialect. See
-- migrations/sqlite/0025_authz_generation.sql for why it is shaped this way.
CREATE TABLE authz_generation (
	id         INTEGER PRIMARY KEY CHECK (id = 1),
	generation BIGINT NOT NULL
);
INSERT INTO authz_generation (id, generation) VALUES (1, 0);

-- +goose Down
DROP TABLE authz_generation;
