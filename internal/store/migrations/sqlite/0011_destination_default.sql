-- +goose Up
-- The global default destination: where a message goes when no route claims it
-- (ADR 0038). The column is additive with a default, so the previous release
-- keeps inserting destinations it never marks.
ALTER TABLE destinations ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0;

-- At most one default. A partial index says so in both dialects, where a
-- CHECK could not see the other rows.
CREATE UNIQUE INDEX destinations_single_default ON destinations (is_default) WHERE is_default;

-- An existing installation gets the default it would have had if the rule
-- "the first destination created is the default" had always applied.
UPDATE destinations SET is_default = 1
WHERE id = (SELECT id FROM destinations ORDER BY created_at, id LIMIT 1);

-- +goose Down
DROP INDEX destinations_single_default;

ALTER TABLE destinations DROP COLUMN is_default;
