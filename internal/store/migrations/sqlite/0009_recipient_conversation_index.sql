-- +goose Up
-- Both ways a link now retires itself -- the bot's own unlink command, and the
-- bot being uninstalled -- arrive knowing only the conversation the activity
-- came from. Until now nothing could ask that question: the one caller that
-- needed it listed every recipient and scanned.
--
-- Not unique. A personal conversation is 1:1 with the bot, so in practice one
-- conversation belongs to one recipient, but subject is what this table is
-- keyed by: one Teams user holding two admin-UI subjects -- a local login and
-- an OIDC one -- can redeem a code for each from the same chat. A unique index
-- would refuse the second and break a link that is legitimate, so the lookup
-- orders and takes one instead.
CREATE INDEX recipients_conversation ON recipients (conversation_id);

-- +goose Down
DROP INDEX recipients_conversation;
