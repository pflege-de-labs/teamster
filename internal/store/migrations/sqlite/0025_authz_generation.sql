-- +goose Up
-- One row whose counter moves with every change to what authorization reads
-- beyond the embedded policies (ADR 0073). Each request compares it with the
-- snapshot it holds, which is how every replica notices a change at once.
CREATE TABLE authz_generation (
	id         INTEGER PRIMARY KEY CHECK (id = 1),
	generation INTEGER NOT NULL
);
INSERT INTO authz_generation (id, generation) VALUES (1, 0);

-- +goose Down
DROP TABLE authz_generation;
