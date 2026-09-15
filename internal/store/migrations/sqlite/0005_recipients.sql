-- +goose Up
-- A recipient is a person who asked for their alerts in a chat, and the Bot
-- Framework conversation reference needed to send one unprompted. Nothing
-- delivers to one yet: this is the storage the linking flow and the routing
-- change are built on.
--
-- subject is the admin-UI session subject that proved the link, and it is
-- unique: a person who re-links replaces their row rather than collecting a
-- second conversation that alerts would then be sent to twice. It is also what
-- ties a recipient back to a Grant: a recipient can only ever have been
-- created by a session that already existed.
--
-- bot_channel_id is a Bot Framework channel ("msteams"), not a Teams channel.
-- The name is deliberately not channel_id: that column means a Teams channel in
-- every other table here, and two meanings for one name is how the wrong one
-- gets read.
CREATE TABLE recipients (
	id TEXT PRIMARY KEY,
	subject TEXT NOT NULL,
	name TEXT NOT NULL,
	aad_object_id TEXT NOT NULL DEFAULT '',
	conversation_id TEXT NOT NULL,
	service_url TEXT NOT NULL,
	bot_channel_id TEXT NOT NULL,
	tenant_id TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE UNIQUE INDEX recipients_subject ON recipients (subject);

-- The same shape as login_flows, for the same reason: a code is redeemed once,
-- by deleting the row, so a replay finds nothing.
CREATE TABLE link_flows (
	code TEXT PRIMARY KEY,
	subject TEXT NOT NULL,
	expires_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE link_flows;

DROP INDEX recipients_subject;

DROP TABLE recipients;
