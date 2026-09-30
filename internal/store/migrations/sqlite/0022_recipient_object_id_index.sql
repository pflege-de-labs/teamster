-- +goose Up
-- A reinstall gives a person a new chat, and every recipient bound to them
-- follows it (ADR 0065); this finds those recipients.
CREATE INDEX recipients_aad_object_id ON recipients (aad_object_id);

-- +goose Down
DROP INDEX recipients_aad_object_id;
