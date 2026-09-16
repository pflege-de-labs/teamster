package store

import (
	"context"
	"errors"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrClaimLost means the row this caller claimed now belongs to somebody
	// else's card. Whatever it posted is unreachable: no row names it, so
	// nothing will ever update or resolve it.
	ErrClaimLost = errors.New("alert claim was taken by another writer")
	// ErrConflict means a write collided with another row on a primary key or
	// unique index, so the caller minting the key -- a link code, say -- can
	// simply try again with a new one instead of surfacing a constraint
	// violation as a 500.
	ErrConflict = errors.New("conflicts with an existing row")
)

// A ClaimOutcome says what asking for the right to post found.
type ClaimOutcome int

const (
	// ClaimAcquired: nothing was there, and the card is ours to post.
	ClaimAcquired ClaimOutcome = iota
	// ClaimRecovered: a claim was there but its owner never posted and the
	// staleness cutoff has passed, so it has been taken over.
	ClaimRecovered
	// ClaimPosted: a card already exists, and this alert is an update to it.
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
	DeleteTemplate(ctx context.Context, id string) error
	GetTemplate(ctx context.Context, id string) (models.Template, error)

	ListDestinations(ctx context.Context) ([]models.Destination, error)
	CreateDestination(ctx context.Context, d models.Destination) (models.Destination, error)
	UpdateDestination(ctx context.Context, d models.Destination) (models.Destination, error)
	DeleteDestination(ctx context.Context, id string) error
	GetDestination(ctx context.Context, id string) (models.Destination, error)

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
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context) error

	CreateLoginFlow(ctx context.Context, f models.LoginFlow) error
	TakeLoginFlow(ctx context.Context, state string) (models.LoginFlow, error)

	// ClaimActiveAlert takes the right to post the card for one channel, or
	// reports who has it. The Graph call that follows happens outside any
	// transaction, so the claim row is the only record that a post is in
	// flight — which is what lets a process that dies mid-post be recovered
	// rather than leave the alert stuck.
	ClaimActiveAlert(ctx context.Context, claim models.AlertClaim) (models.ActiveAlert, ClaimOutcome, error)
	// CompleteActiveAlertClaim records the card the claim produced. It returns
	// ErrClaimLost when the claim is no longer the caller's, which means the
	// message just posted is an orphan and nothing can adopt it.
	CompleteActiveAlertClaim(ctx context.Context, claim models.AlertClaim, messageID string, at time.Time) error
	// ReleaseActiveAlertClaim hands back a claim whose post failed, so the next
	// attempt need not wait out the staleness cutoff.
	ReleaseActiveAlertClaim(ctx context.Context, claim models.AlertClaim) error
	// TouchActiveAlert records that an existing card was updated. It matches on
	// the message id, so an update to a card that has since been replaced does
	// not stamp its replacement.
	TouchActiveAlert(ctx context.Context, card models.ActiveAlert, status string, at time.Time) error
	ListActiveAlerts(ctx context.Context, fingerprint string) ([]models.ActiveAlert, error)
	// CountActiveAlerts is how many cards this service is currently keeping up
	// to date. It runs on every metrics collection, so it counts rather than
	// reads.
	CountActiveAlerts(ctx context.Context) (int64, error)
	GetActiveAlert(ctx context.Context, fingerprint, teamID, channelID string) (models.ActiveAlert, error)
	// DeleteActiveAlertCard forgets one card, and only if it is still that
	// card: a resolve racing a refire must not delete the new card's row.
	DeleteActiveAlertCard(ctx context.Context, fingerprint, teamID, channelID, messageID string) error

	// The six methods below are the claim protocol above, mirrored for a chat
	// delivery against active_alert_recipients rather than active_alerts: a
	// person has no Team or channel to key on, so it is a parallel table
	// rather than a wider key (ADR 0026). See ADR 0021 for the protocol these
	// mirror.
	ClaimActiveAlertRecipient(ctx context.Context, claim models.RecipientClaim) (models.ActiveAlertRecipient, ClaimOutcome, error)
	CompleteActiveAlertRecipientClaim(ctx context.Context, claim models.RecipientClaim, messageID string, at time.Time) error
	ReleaseActiveAlertRecipientClaim(ctx context.Context, claim models.RecipientClaim) error
	TouchActiveAlertRecipient(ctx context.Context, card models.ActiveAlertRecipient, status string, at time.Time) error
	ListActiveAlertRecipients(ctx context.Context, fingerprint string) ([]models.ActiveAlertRecipient, error)
	// DeleteActiveAlertRecipientCard forgets one card, and only if it is still
	// that card: a resolve racing a refire must not delete the new card's row.
	DeleteActiveAlertRecipientCard(ctx context.Context, fingerprint, recipientID, messageID string) error
}
