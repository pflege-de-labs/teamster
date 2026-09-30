-- UpsertDirectoryUser records what Graph says about a person. It never touches
-- the install columns, except that someone seen again after they were marked
-- departed is back to unknown and due at once.
-- name: UpsertDirectoryUser :exec
INSERT INTO directory_users (
	aad_object_id, tenant_id, user_principal_name, mail, upn_key, mail_key,
	display_name, given_name, surname, eligible, directory_seen_at, created_at, updated_at
) VALUES (
	sqlc.arg(aad_object_id), sqlc.arg(tenant_id), sqlc.arg(user_principal_name), sqlc.arg(mail),
	sqlc.arg(upn_key), sqlc.arg(mail_key), sqlc.arg(display_name), sqlc.arg(given_name),
	sqlc.arg(surname), sqlc.arg(eligible), sqlc.arg(seen_at), sqlc.arg(seen_at), sqlc.arg(seen_at)
)
ON CONFLICT(aad_object_id) DO UPDATE SET
	tenant_id           = excluded.tenant_id,
	user_principal_name = excluded.user_principal_name,
	mail                = excluded.mail,
	upn_key             = excluded.upn_key,
	mail_key            = excluded.mail_key,
	display_name        = excluded.display_name,
	given_name          = excluded.given_name,
	surname             = excluded.surname,
	eligible            = excluded.eligible,
	directory_seen_at   = excluded.directory_seen_at,
	updated_at          = excluded.updated_at,
	install_state       = CASE WHEN directory_users.install_state = 'departed' THEN 'unknown' ELSE directory_users.install_state END,
	next_attempt_at     = CASE WHEN directory_users.install_state = 'departed' THEN NULL ELSE directory_users.next_attempt_at END;

-- name: GetDirectoryUser :one
SELECT * FROM directory_users WHERE aad_object_id = sqlc.arg(aad_object_id);

-- FindDirectoryUserByUPNKey and FindDirectoryUserByMailKey take a lower-cased
-- address. The adapter asks for the UPN first, so a UPN that is also
-- somebody's alias wins.
-- name: FindDirectoryUserByUPNKey :one
SELECT * FROM directory_users
WHERE upn_key = sqlc.arg(address_key)
ORDER BY updated_at DESC
LIMIT 1;

-- name: FindDirectoryUserByMailKey :one
SELECT * FROM directory_users
WHERE mail_key = sqlc.arg(address_key)
ORDER BY updated_at DESC
LIMIT 1;

-- name: GetDirectoryUserByConversation :one
SELECT * FROM directory_users
WHERE conversation_id = sqlc.arg(conversation_id)
ORDER BY updated_at DESC
LIMIT 1;

-- SetDirectoryUserInstalled records the chat the bot has with a person, which
-- is proof the app is installed however it got there.
-- name: SetDirectoryUserInstalled :execrows
UPDATE directory_users
SET conversation_id = sqlc.arg(conversation_id),
	service_url     = sqlc.arg(service_url),
	install_state   = 'installed',
	installed_at    = sqlc.arg(at),
	next_attempt_at = NULL,
	attempts        = 0,
	last_error      = '',
	updated_at      = sqlc.arg(at)
WHERE aad_object_id = sqlc.arg(aad_object_id);

-- RecordDirectoryInstallFailure counts a failed attempt and says when the next
-- one is due. state is failed, or ineligible when Graph refused for a reason
-- retrying will not change.
-- name: RecordDirectoryInstallFailure :execrows
UPDATE directory_users
SET install_state   = sqlc.arg(state),
	last_error      = sqlc.arg(last_error),
	next_attempt_at = sqlc.arg(next_attempt_at),
	attempts        = attempts + 1,
	updated_at      = sqlc.arg(at)
WHERE aad_object_id = sqlc.arg(aad_object_id);

-- MarkDirectoryUserRemoved follows Teams telling the bot it was removed. The
-- conversation id stays: a reinstall reopens the same chat.
-- name: MarkDirectoryUserRemoved :execrows
UPDATE directory_users
SET install_state   = 'removed',
	next_attempt_at = sqlc.arg(at),
	updated_at      = sqlc.arg(at)
WHERE aad_object_id = sqlc.arg(aad_object_id);

-- ListDirectoryUsersDue is a reconcile's work list: eligible people whose app
-- is not known to be installed and whose next attempt has come, and installed
-- ones not verified since reverify_before. An install Graph refused for the
-- person is retried too: a licence or policy may change. ignore_backoff takes
-- them whenever their next attempt would be, for a run an admin asked for.
-- name: ListDirectoryUsersDue :many
SELECT * FROM directory_users
WHERE eligible
	AND (
		(install_state IN ('unknown', 'removed', 'failed', 'ineligible')
			AND (CAST(sqlc.arg(ignore_backoff) AS BOOLEAN) OR next_attempt_at IS NULL OR next_attempt_at <= sqlc.arg(now)))
		OR (install_state = 'installed' AND installed_at < sqlc.arg(reverify_before))
	)
ORDER BY aad_object_id
LIMIT CAST(sqlc.arg(max_rows) AS BIGINT);

-- MarkDirectoryUsersDeparted retires everyone a complete listing did not
-- return: they left, were disabled or stopped being members.
-- name: MarkDirectoryUsersDeparted :execrows
UPDATE directory_users
SET install_state = 'departed',
	updated_at    = sqlc.arg(at)
WHERE directory_seen_at < sqlc.arg(seen_before) AND install_state <> 'departed';

-- name: PurgeDepartedDirectoryUsers :execrows
DELETE FROM directory_users
WHERE install_state = 'departed' AND updated_at < sqlc.arg(before);

-- name: CountDirectoryUsersByState :many
SELECT install_state, COUNT(*) AS users
FROM directory_users
GROUP BY install_state
ORDER BY install_state;

-- ListDirectoryUserProblems is what the people page lists: installs that
-- failed or were refused, most recent first.
-- name: ListDirectoryUserProblems :many
SELECT * FROM directory_users
WHERE install_state IN ('failed', 'ineligible')
ORDER BY updated_at DESC, aad_object_id
LIMIT CAST(sqlc.arg(max_rows) AS BIGINT);

-- MarkDirectoryUserBlocked and ClearDirectoryUserBlocked are the directory's
-- counterparts of the recipient statements: informational, never a gate, and
-- cleared after every success (ADR 0026).
-- name: MarkDirectoryUserBlocked :exec
UPDATE directory_users
SET blocked_at = sqlc.arg(at), blocked_reason = sqlc.arg(reason)
WHERE aad_object_id = sqlc.arg(aad_object_id);

-- name: ClearDirectoryUserBlocked :exec
UPDATE directory_users
SET blocked_at = NULL, blocked_reason = ''
WHERE aad_object_id = sqlc.arg(aad_object_id) AND blocked_at IS NOT NULL;
