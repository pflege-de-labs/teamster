package store

import (
	"context"
	"errors"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrDefaultDestination refuses deleting the global default destination
	// while another destination could take its place.
	ErrDefaultDestination = errors.New("destination is the global default; choose another global default first")
	// ErrClaimLost means the row this caller claimed now belongs to somebody
	// else's card. Whatever it posted is unreachable: no row names it, so
	// nothing will ever update or close it.
	ErrClaimLost = errors.New("event claim was taken by another writer")
	// ErrConflict means a write collided with another row on a primary key or
	// unique index, so the caller minting the key -- a link code, say -- can
	// simply try again with a new one instead of surfacing a constraint
	// violation as a 500.
	ErrConflict = errors.New("conflicts with an existing row")
	// ErrGroupCycle refuses a membership that would make a group its own member.
	ErrGroupCycle = errors.New("a group cannot contain itself, directly or through other groups")
	// ErrTemplateSource refuses a source default whose template names other
	// sources, since every message it caught would fall back anyway.
	ErrTemplateSource = errors.New("template does not handle this source")
)

// A ClaimOutcome says what asking for the right to post found.
type ClaimOutcome int

const (
	// ClaimAcquired: nothing was there, and the card is ours to post.
	ClaimAcquired ClaimOutcome = iota
	// ClaimRecovered: a claim was there but its owner never posted and the
	// staleness cutoff has passed, so it has been taken over.
	ClaimRecovered
	// ClaimPosted: a card already exists, and this event is an update to it.
	ClaimPosted
	// ClaimHeld: another writer is inside its Graph call for this very card.
	ClaimHeld
)

func (o ClaimOutcome) String() string {
	switch o {
	case ClaimAcquired:
		return "acquired"
	case ClaimRecovered:
		return "recovered"
	case ClaimPosted:
		return "posted"
	case ClaimHeld:
		return "held"
	default:
		return "unknown"
	}
}

type Store interface {
	Close() error
	// Ping reports whether the database can still be reached, which is what
	// readiness turns on.
	Ping(ctx context.Context) error

	// WithTx runs fn against a store bound to one transaction. Everything fn
	// writes lands together or not at all, which is what an import that may be
	// rejected half way through needs.
	//
	// fn receives the context rather than closing over one, so a transaction
	// can be given its own deadline without touching every caller.
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error

	// WithSerializableTx is WithTx for the invariants no constraint can
	// express -- "this route tree has no cycle", "this parent still has
	// children" -- where a check and the write it guards must see the same
	// world. That is write skew, which only serializable isolation prevents.
	//
	// fn may be run more than once, because a backend that detects the
	// conflict rather than blocking reports it as a retryable failure. It must
	// therefore not accumulate anything outside the transaction.
	WithSerializableTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error

	ListTemplates(ctx context.Context) ([]models.Template, error)
	CreateTemplate(ctx context.Context, t models.Template) (models.Template, error)
	UpdateTemplate(ctx context.Context, t models.Template) (models.Template, error)
	// DeleteTemplate also forgets it as the catch-all's template.
	DeleteTemplate(ctx context.Context, id string) error
	GetTemplate(ctx context.Context, id string) (models.Template, error)

	ListDestinations(ctx context.Context) ([]models.Destination, error)
	CreateDestination(ctx context.Context, d models.Destination) (models.Destination, error)
	UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error)
	DeleteDestination(ctx context.Context, id string) error
	GetDestination(ctx context.Context, id string) (models.Destination, error)
	// GetDefaultDestination is where a message goes when no route claims it.
	// ErrNotFound means there is no destination at all.
	GetDefaultDestination(ctx context.Context) (models.Destination, error)
	// SetDefaultDestination makes id the one global default.
	SetDefaultDestination(ctx context.Context, id string) error
	// GetGlobalDefaultTemplate is the template the catch-all route renders
	// with; "" means the built-in default message (ADR 0050).
	GetGlobalDefaultTemplate(ctx context.Context) (string, error)
	// SetGlobalDefaultTemplate chooses it; "" goes back to the built-in one.
	// ErrNotFound means the template does not exist.
	SetGlobalDefaultTemplate(ctx context.Context, templateID string) error
	// GetSourceDefaultTemplate is what a message from source renders with when
	// its route or endpoint names no template it can use; "" means the
	// built-in message (ADR 0055).
	GetSourceDefaultTemplate(ctx context.Context, source string) (string, error)
	// SourceDefaultTemplates maps every source with a default to its template.
	SourceDefaultTemplates(ctx context.Context) (map[string]string, error)
	// SetSourceDefaultTemplate chooses it; "" goes back to the built-in one.
	// ErrNotFound means the template does not exist, ErrTemplateSource that
	// it does not handle source.
	SetSourceDefaultTemplate(ctx context.Context, source, templateID string) error
	// SeedTemplates creates templates and makes each the default of its one
	// source where none is set, the first time it is called and never again,
	// so a template deleted afterwards stays deleted. It reports whether it
	// seeded.
	SeedTemplates(ctx context.Context, templates []models.Template) (bool, error)

	ListRecipients(ctx context.Context) ([]models.Recipient, error)
	CreateRecipient(ctx context.Context, r models.Recipient) (models.Recipient, error)
	// UpdateRecipient writes everything but the subject: a re-link replaces the
	// conversation, never the person a binding belongs to.
	UpdateRecipient(ctx context.Context, r models.Recipient) (models.Recipient, error)
	DeleteRecipient(ctx context.Context, id string) error
	GetRecipient(ctx context.Context, id string) (models.Recipient, error)
	// GetRecipientBySubject is what makes re-linking an update rather than a
	// duplicate: the subject is unique, and somebody who reinstalls the bot
	// arrives with a new conversation and the same session.
	GetRecipientBySubject(ctx context.Context, subject string) (models.Recipient, error)
	// MarkRecipientBlocked and ClearRecipientBlocked write only the two
	// blocked columns, on purpose: they are called from delivery, which reads
	// a recipient to send to it and must not clobber a conversation reference
	// it never loaded by going through the full UpdateRecipient. The flag they
	// set is informational and self-healing, never a delivery gate -- see
	// ADR 0026. ClearRecipientBlocked is a no-op, not an error, on a recipient
	// that was never blocked.
	// GetRecipientByConversation resolves an inbound activity's conversation
	// back to the recipient it belongs to. It is what lets the bot retire a
	// link from the chat side -- an unlink command, or the bot being removed --
	// where the only identity on hand is the conversation itself.
	GetRecipientByConversation(ctx context.Context, conversationID string) (models.Recipient, error)
	MarkRecipientBlocked(ctx context.Context, id string, at time.Time, reason string) error
	ClearRecipientBlocked(ctx context.Context, id string) error

	ListWebhookEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error)
	// CreateWebhookEndpoint and UpdateWebhookEndpoint write the endpoint but
	// not its secret: rotating one is its own call, so editing a slug cannot
	// break a sender by accident.
	CreateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error)
	UpdateWebhookEndpoint(ctx context.Context, e models.WebhookEndpoint) (models.WebhookEndpoint, error)
	RotateWebhookEndpointToken(ctx context.Context, id, tokenHash string) error
	DeleteWebhookEndpoint(ctx context.Context, id string) error
	GetWebhookEndpoint(ctx context.Context, id string) (models.WebhookEndpoint, error)
	// GetWebhookEndpointBySlug resolves a request path. It is the one store
	// call an unauthenticated caller can reach, and it matches on the slug
	// pair alone -- the token is compared in constant time afterwards.
	GetWebhookEndpointBySlug(ctx context.Context, teamSlug, channelSlug string) (models.WebhookEndpoint, error)

	// UpsertBotTeam and DeleteBotTeam are written only from authenticated
	// inbound activities: the service URL is where the bot's token is sent.
	UpsertBotTeam(ctx context.Context, t models.BotTeam) error
	GetBotTeam(ctx context.Context, teamID string) (models.BotTeam, error)
	ListBotTeams(ctx context.Context) ([]models.BotTeam, error)
	DeleteBotTeam(ctx context.Context, teamID string) error
	// CountDestinationsWithoutBotTeam runs on every metrics collection.
	CountDestinationsWithoutBotTeam(ctx context.Context) (int64, error)

	// UpsertDirectoryUser records what Graph says about a person, stamped
	// DirectorySeenAt (now when zero). It leaves the install state alone.
	UpsertDirectoryUser(ctx context.Context, u models.DirectoryUser) error
	GetDirectoryUser(ctx context.Context, aadObjectID string) (models.DirectoryUser, error)
	// FindDirectoryUser matches an address against UPN, then mail, ignoring case.
	FindDirectoryUser(ctx context.Context, address string) (models.DirectoryUser, error)
	GetDirectoryUserByConversation(ctx context.Context, conversationID string) (models.DirectoryUser, error)
	SetDirectoryUserInstalled(ctx context.Context, aadObjectID, conversationID, serviceURL string, at time.Time) error
	// RecordDirectoryInstallFailure counts an attempt; next is when to retry,
	// zero for never.
	RecordDirectoryInstallFailure(ctx context.Context, aadObjectID string, state models.InstallState, lastError string, next, at time.Time) error
	MarkDirectoryUserRemoved(ctx context.Context, aadObjectID string, at time.Time) error
	// UpdateRecipientChatsForObjectID moves the recipients bound to a person
	// onto their current chat, reporting how many moved.
	UpdateRecipientChatsForObjectID(ctx context.Context, aadObjectID, conversationID, serviceURL string, at time.Time) (int64, error)
	// MarkDirectoryUserBlocked and ClearDirectoryUserBlocked keep the
	// informational blocked flag, as for recipients.
	MarkDirectoryUserBlocked(ctx context.Context, aadObjectID string, at time.Time, reason string) error
	ClearDirectoryUserBlocked(ctx context.Context, aadObjectID string) error
	// ListDirectoryUsersDue with ignoreBackoff takes everyone not installed,
	// whenever their next attempt would be.
	ListDirectoryUsersDue(ctx context.Context, now, reverifyBefore time.Time, ignoreBackoff bool, limit int) ([]models.DirectoryUser, error)
	// MarkDirectoryUsersDeparted retires everyone a complete listing that
	// started at seenBefore did not return.
	MarkDirectoryUsersDeparted(ctx context.Context, seenBefore, at time.Time) (int64, error)
	PurgeDepartedDirectoryUsers(ctx context.Context, before time.Time) (int64, error)
	CountDirectoryUsersByState(ctx context.Context) (map[models.InstallState]int64, error)
	// ListDirectoryUserProblems lists failed and refused installs, newest first.
	ListDirectoryUserProblems(ctx context.Context, limit int) ([]models.DirectoryUser, error)

	// RequestDirectoryRun reports ErrConflict while another run is requested or
	// running.
	RequestDirectoryRun(ctx context.Context, r models.DirectoryRun) (models.DirectoryRun, error)
	GetDirectoryRun(ctx context.Context, id string) (models.DirectoryRun, error)
	LatestDirectoryRun(ctx context.Context) (models.DirectoryRun, error)
	// ClaimDirectoryRun, HeartbeatDirectoryRun and FinishDirectoryRun report
	// false when the run is not, or no longer, this owner's.
	ClaimDirectoryRun(ctx context.Context, id, owner string, now, staleBefore time.Time) (bool, error)
	HeartbeatDirectoryRun(ctx context.Context, id, owner string, counts models.RunCounts, now time.Time) (bool, error)
	FinishDirectoryRun(ctx context.Context, id, owner string, state models.RunState, counts models.RunCounts, lastError string, now time.Time) (bool, error)
	PruneDirectoryRuns(ctx context.Context, before time.Time) (int64, error)

	ListAccessTokens(ctx context.Context) ([]models.AccessToken, error)
	// CreateAccessToken reports ErrConflict when the name is taken.
	CreateAccessToken(ctx context.Context, t models.AccessToken) (models.AccessToken, error)
	// GetAccessTokenByHash authenticates a webhook request, so like
	// GetWebhookEndpointBySlug it is reachable by an unauthenticated caller.
	GetAccessTokenByHash(ctx context.Context, tokenHash string) (models.AccessToken, error)
	TouchAccessToken(ctx context.Context, id string, at time.Time) error
	DeleteAccessToken(ctx context.Context, id string) error

	CreateLinkFlow(ctx context.Context, f models.LinkFlow) error
	// TakeLinkFlow redeems a code once, whether or not it had expired.
	TakeLinkFlow(ctx context.Context, code string) (models.LinkFlow, error)
	// DeleteLinkFlowsForSubject retires every code a subject has outstanding.
	// Minting a new one calls this first, so a guessing attacker only ever
	// faces the one code just handed out rather than every one issued since
	// the last hourly sweep.
	DeleteLinkFlowsForSubject(ctx context.Context, subject string) error

	ListRoutes(ctx context.Context) ([]models.Route, error)
	CreateRoute(ctx context.Context, r models.Route) (models.Route, error)
	UpdateRoute(ctx context.Context, r models.Route) (models.Route, error)
	DeleteRoute(ctx context.Context, id string) error
	GetRoute(ctx context.Context, id string) (models.Route, error)

	ListGrants(ctx context.Context) ([]models.Grant, error)
	CreateGrant(ctx context.Context, g models.Grant) (models.Grant, error)
	DeleteGrant(ctx context.Context, id string) error
	// DeleteGrantsForRole removes a role's whole scope in one statement, which
	// is what replacing it safely needs: listing and deleting one at a time
	// lets a concurrent replacement interleave into the union of both.
	DeleteGrantsForRole(ctx context.Context, role string) error

	CreateSession(ctx context.Context, s models.Session) error
	GetSession(ctx context.Context, id string) (models.Session, error)
	// DeleteSession is overridden by SQLiteStore and PostgresStore to cascade
	// into that session's broker_tokens row, if any: a live Keycloak token has
	// no reason to outlive the session that fetched it. See
	// deleteSessionCascade.
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context) error

	// CreateBrokerToken, GetBrokerToken and UpdateBrokerToken persist and
	// refresh the live Keycloak token behind delegated Teams/Channels (ADR
	// 0037). DeleteBrokerToken is called directly on a hard refresh failure --
	// a revoked or expired refresh token -- so the next attempt fails fast
	// rather than retrying it forever; deleteSessionCascade is what covers the
	// ordinary case of the session itself ending.
	CreateBrokerToken(ctx context.Context, t models.BrokerToken) error
	GetBrokerToken(ctx context.Context, sessionID string) (models.BrokerToken, error)
	UpdateBrokerToken(ctx context.Context, t models.BrokerToken) error
	DeleteBrokerToken(ctx context.Context, sessionID string) error

	CreateLoginFlow(ctx context.Context, f models.LoginFlow) error
	TakeLoginFlow(ctx context.Context, state string) (models.LoginFlow, error)

	// ClaimActiveEvent takes the right to post the card for one channel, or
	// reports who has it. The Graph call that follows happens outside any
	// transaction, so the claim row is the only record that a post is in
	// flight — which is what lets a process that dies mid-post be recovered
	// rather than leave the event stuck.
	ClaimActiveEvent(ctx context.Context, claim models.EventClaim) (models.ActiveEvent, ClaimOutcome, error)
	// CompleteActiveEventClaim records the card the claim produced. It returns
	// ErrClaimLost when the claim is no longer the caller's, which means the
	// message just posted is an orphan and nothing can adopt it.
	CompleteActiveEventClaim(ctx context.Context, claim models.EventClaim, messageID, conversationID string, at time.Time) error
	// ReleaseActiveEventClaim hands back a claim whose post failed, so the next
	// attempt need not wait out the staleness cutoff.
	ReleaseActiveEventClaim(ctx context.Context, claim models.EventClaim) error
	// TouchActiveEvent records that an existing card was updated. It matches on
	// the message id, so an update to a card that has since been replaced does
	// not stamp its replacement.
	TouchActiveEvent(ctx context.Context, card models.ActiveEvent, state models.EventState, at time.Time) error
	ListActiveEvents(ctx context.Context, key string) ([]models.ActiveEvent, error)
	// CountActiveEvents is how many cards this service is currently keeping up
	// to date. It runs on every metrics collection, so it counts rather than
	// reads.
	CountActiveEvents(ctx context.Context) (int64, error)
	GetActiveEvent(ctx context.Context, key, teamID, channelID string) (models.ActiveEvent, error)
	// DeleteActiveEventCard forgets one card, and only if it is still that
	// card: a close racing a reopen must not delete the new card's row.
	DeleteActiveEventCard(ctx context.Context, key, teamID, channelID, messageID string) error

	// The six methods below are the claim protocol above, mirrored for a chat
	// delivery against active_event_recipients rather than active_events: a
	// person has no Team or channel to key on, so it is a parallel table
	// rather than a wider key (ADR 0026). See ADR 0021 for the protocol these
	// mirror.
	ClaimActiveEventRecipient(ctx context.Context, claim models.RecipientClaim) (models.ActiveEventRecipient, ClaimOutcome, error)
	CompleteActiveEventRecipientClaim(ctx context.Context, claim models.RecipientClaim, messageID string, at time.Time) error
	ReleaseActiveEventRecipientClaim(ctx context.Context, claim models.RecipientClaim) error
	TouchActiveEventRecipient(ctx context.Context, card models.ActiveEventRecipient, state models.EventState, at time.Time) error
	ListActiveEventRecipients(ctx context.Context, key string) ([]models.ActiveEventRecipient, error)
	// DeleteActiveEventRecipientCard forgets one card, and only if it is still
	// that card: a close racing a reopen must not delete the new card's row.
	DeleteActiveEventRecipientCard(ctx context.Context, key, recipientID, messageID string) error
	// DeleteActiveEventRecipientsFor forgets every row for one recipient,
	// regardless of key or message id. SQLiteStore and PostgresStore
	// call it inside the same transaction as DeleteRecipient, so unlinking
	// someone cannot strand a claimed or posted row that a close would then
	// fail against forever.
	DeleteActiveEventRecipientsFor(ctx context.Context, recipientID string) error

	// AuthzGeneration moves whenever what authorization reads from the store
	// changes; BumpAuthzGeneration moves it, inside the change's transaction.
	AuthzGeneration(ctx context.Context) (int64, error)
	BumpAuthzGeneration(ctx context.Context) error

	// Groups (ADR 0074). Every write bumps the authz generation in its own
	// transaction; adding a member refuses a cycle with ErrGroupCycle.
	ListGroups(ctx context.Context) ([]models.Group, error)
	GetGroup(ctx context.Context, id string) (models.Group, error)
	CreateGroup(ctx context.Context, g models.Group) (models.Group, error)
	UpdateGroup(ctx context.Context, g models.Group) (models.Group, error)
	DeleteGroup(ctx context.Context, id string) error
	ListGroupMembers(ctx context.Context, groupID string) ([]models.GroupMember, error)
	ListAllGroupMembers(ctx context.Context) ([]models.GroupMember, error)
	AddGroupMember(ctx context.Context, m models.GroupMember) error
	RemoveGroupMember(ctx context.Context, m models.GroupMember) error

	// RecordSignIn creates or refreshes a user from a sign-in (ADR 0072).
	RecordSignIn(ctx context.Context, u models.User) error
	GetUser(ctx context.Context, subject string) (models.User, error)
	// ListUsers matches search against subject, name and email, ignoring case.
	ListUsers(ctx context.Context, search string, limit int) ([]models.User, error)
	// DisableUser refuses the user's next sign-in and ends their sessions,
	// together, so a disabled user keeps no way in.
	DisableUser(ctx context.Context, subject, by string) error
	EnableUser(ctx context.Context, subject string) error

	// InsertAuditEvent appends one event to the audit trail (ADR 0070).
	InsertAuditEvent(ctx context.Context, e models.AuditEvent) error
	// ListAuditEvents returns events newest first, at most filter.Limit of them.
	ListAuditEvents(ctx context.Context, filter models.AuditFilter) ([]models.AuditEvent, error)
	// PruneAuditEvents forgets events older than cutoff, unless cutoff is zero,
	// and all but the newest keep events, unless keep is zero. Replicas sharing
	// a database may all run it: a second run finds nothing to delete.
	PruneAuditEvents(ctx context.Context, cutoff time.Time, keep int) (int64, error)

	// RecordEventSamples adds each sample's SeenCount to what is stored for its
	// kind, key and value, creating the row if there is none (ADR 0041).
	// SQLiteStore and PostgresStore commit the whole batch in one transaction.
	RecordEventSamples(ctx context.Context, samples []models.EventSample) error
	// ListEventSamples returns at most limit samples, grouped by kind and key
	// and most recently seen first within each key.
	ListEventSamples(ctx context.Context, limit int) ([]models.EventSample, error)
	// PruneEventSamples forgets samples last seen before cutoff, and label
	// values beyond the keepPerKey most recently seen for each key. Replicas
	// sharing a database may all run it: a second run finds nothing to delete.
	PruneEventSamples(ctx context.Context, cutoff time.Time, keepPerKey int) (int64, error)
}
