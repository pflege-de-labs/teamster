-- +goose Up
-- A permanent send failure -- MessageWritesBlocked, or ConversationBlockedByUser
-- one level in -- already stops delivery from retrying within an alert (see
-- recipientBlocked in internal/httpserver/webhooks.go), but until now it left
-- no trace an admin could read: only a metric. These two columns give it a
-- durable, visible home on the row itself.
--
-- Both are additive -- a nullable column and one with a default -- so the
-- previous release keeps reading and writing this table exactly as it did
-- before.
--
-- The flag is informational and self-healing, not a delivery gate: a
-- successful send or update clears it, and a blocked recipient is still
-- attempted on the next alert. Gating delivery on it would trade a visible
-- problem (a metric, this column) for an invisible one -- a person who
-- reinstalled the bot silently never receiving alerts again until an admin
-- notices and clears it by hand. See MarkRecipientBlocked and
-- ClearRecipientBlocked in queries/sqlite/recipients.sql, and ADR 0026's
-- consequences section.
ALTER TABLE recipients ADD COLUMN blocked_at DATETIME;
ALTER TABLE recipients ADD COLUMN blocked_reason TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE recipients DROP COLUMN blocked_reason;
ALTER TABLE recipients DROP COLUMN blocked_at;
