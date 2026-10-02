package models

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// A Template renders into a Teams message. Title is the line the activity feed
// previews, Text optional formatted prose, Body the Adaptive Card JSON — and a
// template needs only one of the three.
type Template struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Body  string `json:"body"`
	// Sources are the webhooks whose payloads the template handles; none means
	// any (ADR 0053).
	Sources   []string  `json:"sources,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Handles reports whether the template can render an event from source.
func (t Template) Handles(source string) bool {
	return len(t.Sources) == 0 || slices.Contains(t.Sources, source)
}

// AllSources lists every value SourceLabel takes, in the order the UI shows them.
func AllSources() []string {
	return []string{SourceAlertmanager, SourceUniversal, SourceTeamsV2}
}

// NormalizeSources orders a template's sources as AllSources does and drops
// duplicates, refusing a value no webhook sets.
func NormalizeSources(in []string) ([]string, error) {
	var out []string
	for _, source := range AllSources() {
		if slices.Contains(in, source) {
			out = append(out, source)
		}
	}
	for _, source := range in {
		if !slices.Contains(AllSources(), source) {
			return nil, fmt.Errorf("unknown source %q; want one of %s", source, strings.Join(AllSources(), ", "))
		}
	}
	return out, nil
}

type Destination struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TeamID    string `json:"team_id"`
	ChannelID string `json:"channel_id"`
	// IsDefault marks the global default destination (ADR 0038). It is
	// changed only through Store.SetDefaultDestination, never by an update.
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// A WebhookEndpoint is a Teams V2 webhook URL this service answers on behalf
// of a channel. TeamSlug and ChannelSlug are the two readable path segments
// that name it, and the pair is unique because the pair is the URL.
//
// The channel itself comes from the Destination, so an endpoint cannot name a
// channel that was never configured. TokenHash is a SHA-256 digest of the
// secret the sender puts in the path; the secret itself is shown once, when it
// is generated, and is not recoverable from here.
type WebhookEndpoint struct {
	ID            string `json:"id"`
	TeamSlug      string `json:"team_slug"`
	ChannelSlug   string `json:"channel_slug"`
	DestinationID string `json:"destination_id"`
	// TemplateID is what the endpoint's messages render with; empty sends
	// the payload as given, followed by a hint card (ADR 0040).
	TemplateID string    `json:"template_id"`
	TokenHash  string    `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// A BotTeam is a team the bot is installed in, and the regional Bot Connector
// endpoint that reaches it (ADR 0045). TeamID is the Graph team id.
type BotTeam struct {
	TeamID     string    `json:"team_id"`
	TenantID   string    `json:"tenant_id"`
	ServiceURL string    `json:"service_url"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// InstallState is how far installing the Teams app for a directory user got
// (ADR 0059).
type InstallState string

const (
	InstallUnknown    InstallState = "unknown"
	InstallInstalled  InstallState = "installed"
	InstallRemoved    InstallState = "removed"
	InstallFailed     InstallState = "failed"
	InstallIneligible InstallState = "ineligible"
	InstallDeparted   InstallState = "departed"
)

// A DirectoryUser is a person the bot can message without a link code, as
// Graph last described them (ADR 0060). The zero time means unset.
type DirectoryUser struct {
	AADObjectID       string       `json:"aad_object_id"`
	TenantID          string       `json:"tenant_id"`
	UserPrincipalName string       `json:"user_principal_name"`
	Mail              string       `json:"mail"`
	DisplayName       string       `json:"display_name"`
	GivenName         string       `json:"given_name"`
	Surname           string       `json:"surname"`
	Eligible          bool         `json:"eligible"`
	ConversationID    string       `json:"conversation_id"`
	ServiceURL        string       `json:"service_url"`
	InstallState      InstallState `json:"install_state"`
	InstalledAt       time.Time    `json:"installed_at"`
	NextAttemptAt     time.Time    `json:"next_attempt_at"`
	Attempts          int64        `json:"attempts"`
	LastError         string       `json:"last_error"`
	BlockedAt         time.Time    `json:"blocked_at"`
	BlockedReason     string       `json:"blocked_reason"`
	DirectorySeenAt   time.Time    `json:"directory_seen_at"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
}

// RunKind says who asked for a directory run.
type RunKind string

const (
	RunManual   RunKind = "manual"
	RunPeriodic RunKind = "periodic"
)

// RunState is where a directory run is.
type RunState string

const (
	RunRequested RunState = "requested"
	RunRunning   RunState = "running"
	RunDone      RunState = "done"
	RunFailed    RunState = "failed"
)

// RunCounts is a directory run's progress.
type RunCounts struct {
	Total      int64 `json:"total"`
	Installed  int64 `json:"installed"`
	Already    int64 `json:"already"`
	Failed     int64 `json:"failed"`
	Ineligible int64 `json:"ineligible"`
}

// A DirectoryRun is one pass that installs the Teams app for the tenant. Only
// one can be requested or running at a time, and its owner holds it while its
// heartbeat is fresh.
type DirectoryRun struct {
	ID          string    `json:"id"`
	Kind        RunKind   `json:"kind"`
	RequestedBy string    `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
	State       RunState  `json:"state"`
	Owner       string    `json:"owner"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	RunCounts
	LastError string `json:"last_error"`
}

// A Broadcast is a message to everyone the bot can reach, delivered by a
// background run any replica may take over (ADR 0083). Event and Plan are the
// message and its addressed deliveries as JSON; Cursor is the last person
// delivered to.
type Broadcast struct {
	ID          string          `json:"id"`
	RequestedBy string          `json:"requested_by"`
	TokenName   string          `json:"token_name"`
	RequestedAt time.Time       `json:"requested_at"`
	State       RunState        `json:"state"`
	Event       json.RawMessage `json:"-"`
	Plan        json.RawMessage `json:"-"`
	Owner       string          `json:"-"`
	HeartbeatAt time.Time       `json:"heartbeat_at,omitzero"`
	StartedAt   time.Time       `json:"started_at,omitzero"`
	FinishedAt  time.Time       `json:"finished_at,omitzero"`
	Cursor      string          `json:"-"`
	BroadcastCounts
	LastError string `json:"last_error,omitempty"`
}

// BroadcastCounts is how far a broadcast has come. Total is everyone it
// reaches; unreachable people blocked or removed the bot.
type BroadcastCounts struct {
	Total       int64 `json:"total"`
	Delivered   int64 `json:"delivered"`
	Unreachable int64 `json:"unreachable"`
	Failed      int64 `json:"failed"`
}

// An AccessToken is a named credential for the alertmanager and universal
// webhooks (ADR 0044). TokenHash is a SHA-256 digest; the token is shown once,
// when it is issued. LastUsedAt is zero for a token never presented.
type AccessToken struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	TokenHash  string    `json:"-"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	// Scope is the webhooks the token may send to, checked against its
	// creator's permissions on every use (ADR 0077). Empty is a token from
	// before scopes, which reaches both webhooks whoever made it.
	Scope []string `json:"scope,omitempty"`
	// Messages is how far the token may address people: "" not at all, or an
	// authz message level, never beyond its creator's (ADR 0082).
	Messages string `json:"messages,omitempty"`
}

// Scoped reports whether the token is bound to its scope and its creator.
func (t AccessToken) Scoped() bool { return len(t.Scope) > 0 }

// A Recipient is a person who asked for their alerts as a chat message, and the
// Bot Framework conversation reference that makes it possible to send one
// unprompted. Subject is the admin-UI session subject that proved the link, so a
// recipient inherits whatever that session was already allowed to see; it is
// unique, and re-linking replaces the row rather than adding a second one.
//
// The rest is the conversation reference, as Bot Framework spells it.
// BotChannelID is a Bot Framework channel — "msteams" — and not a Teams channel,
// which is what ChannelID means everywhere else in this package.
type Recipient struct {
	ID             string `json:"id"`
	Subject        string `json:"subject"`
	Name           string `json:"name"`
	AADObjectID    string `json:"aad_object_id"`
	ConversationID string `json:"conversation_id"`
	ServiceURL     string `json:"service_url"`
	BotChannelID   string `json:"bot_channel_id"`
	TenantID       string `json:"tenant_id"`
	// BlockedAt and BlockedReason are informational and self-healing, not a
	// delivery gate (ADR 0026): a permanent send failure sets them, any
	// successful send or update clears them, and delivery is attempted either
	// way. Gating on this flag would trade a visible problem for an invisible
	// one -- a person who reinstalled the bot would silently never receive
	// alerts again until an admin noticed and cleared it by hand.
	BlockedAt     time.Time `json:"blocked_at,omitempty"`
	BlockedReason string    `json:"blocked_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Blocked reports whether this recipient's bot conversation is known broken,
// mirroring ActiveEvent.Posted.
func (r Recipient) Blocked() bool { return !r.BlockedAt.IsZero() }

// A LinkFlow is a one-time code an authenticated admin-UI session generated, to
// be typed at the bot in Teams. Redeeming it is what binds a conversation to
// Subject: without it, anyone able to install the bot could opt into alerts
// nobody granted them.
type LinkFlow struct {
	Code      string    `json:"code"`
	Subject   string    `json:"subject"`
	ExpiresAt time.Time `json:"expires_at"`
}

// A Route decides where an alert goes. Routes form a tree: a child refines its
// parent's match, and delivers instead of its parent when Greedy, as well as it
// when not. A child leaves DestinationID, RecipientID or TemplateID empty to
// inherit the nearest ancestor's. DestinationID and RecipientID are
// independent targets, not alternatives: a route naming both fans out to a
// channel card and a chat message, the same fan-out nested routes already
// produce (see ADR 0026).
type Route struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	ParentID      string            `json:"parent_id"`
	LabelSelector map[string]string `json:"label_selector"`
	DestinationID string            `json:"destination_id"`
	RecipientID   string            `json:"recipient_id"`
	// Addressed delivers to the people each message names (ADR 0062).
	Addressed  bool      `json:"addressed,omitempty"`
	TemplateID string    `json:"template_id"`
	IsDefault  bool      `json:"is_default"`
	Greedy     bool      `json:"greedy"`
	Priority   int       `json:"priority"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// An ActiveEvent is one card: the message this service posted to one channel
// for one event, and is keeping up to date until the event closes. An event
// that fans out to several channels has one per channel, which is why the key
// is the event key together with the Team and channel.
//
// A row exists from the moment delivery claims the right to post, which is
// before the card does. PostedAt is what tells the two apart: while it is zero
// the row is a claim in flight and MessageID is empty, because there is no
// message yet to name.
type ActiveEvent struct {
	Key       string     `json:"key"`
	State     EventState `json:"state"`
	TeamID    string     `json:"team_id"`
	ChannelID string     `json:"channel_id"`
	MessageID string     `json:"message_id"`
	// ConversationID is the channel conversation the bot created for the
	// card; empty for a card the bot cannot edit (ADR 0045).
	ConversationID string    `json:"conversation_id,omitempty"`
	ClaimOwner     string    `json:"claim_owner,omitempty"`
	ClaimedAt      time.Time `json:"claimed_at,omitempty"`
	PostedAt       time.Time `json:"posted_at,omitempty"`
	LastUpdate     time.Time `json:"last_update"`
}

// Posted reports whether a card exists for this row, as opposed to a claim on
// the right to post one.
func (a ActiveEvent) Posted() bool { return !a.PostedAt.IsZero() }

// An EventClaim is the right to post one card, asked for before the Graph call
// and completed after it. Owner is unique to the attempt rather than to the
// process: it only has to answer "is this still mine to finish".
type EventClaim struct {
	Key       string
	TeamID    string
	ChannelID string
	State     EventState
	Owner     string
	At        time.Time
	// StaleBefore is the age at which a claim is presumed abandoned, because
	// whatever took it died between claiming and posting.
	StaleBefore time.Time
}

// An ActiveEventRecipient is ActiveEvent mirrored for a chat delivery: one
// message a bot sent to one person for one event, kept up to date until it
// closes. It is a parallel table to ActiveEvent rather than a wider key on
// it, because a person has neither a Team nor a channel for that table's CHECK
// to hold (see ADR 0026 and ADR 0021).
//
// Unlike ActiveEvent, an empty MessageID does not always mean "claimed, not
// posted": bot.SendMessage returning ("", nil) is a documented success --
// delivered, but with nothing to name it by for a later edit. PostedAt is the
// only reliable state machine here.
type ActiveEventRecipient struct {
	Key         string     `json:"key"`
	State       EventState `json:"state"`
	RecipientID string     `json:"recipient_id"`
	MessageID   string     `json:"message_id"`
	ClaimOwner  string     `json:"claim_owner,omitempty"`
	ClaimedAt   time.Time  `json:"claimed_at,omitempty"`
	PostedAt    time.Time  `json:"posted_at,omitempty"`
	LastUpdate  time.Time  `json:"last_update"`
}

// Posted reports whether a card exists for this row, as opposed to a claim on
// the right to post one.
func (a ActiveEventRecipient) Posted() bool { return !a.PostedAt.IsZero() }

// A RecipientClaim is EventClaim's counterpart for a chat delivery: the right
// to send or update one person's message, asked for before the Bot Connector
// call and completed after it.
type RecipientClaim struct {
	Key         string
	RecipientID string
	State       EventState
	Owner       string
	At          time.Time
	StaleBefore time.Time
}

// A Grant scopes a role to a Team or to one channel in it: holders of that role
// may deliver there and see it. A role with no grant at all is unrestricted,
// which is what an installation looks like before an admin narrows anything.
//
// Role is any role name the provider hands out, so "the payments editors" is a
// role in the identity provider and a grant here, with nothing in between.
type Grant struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	// TeamID is required; an empty ChannelID means the whole Team.
	TeamID    string    `json:"team_id"`
	ChannelID string    `json:"channel_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Session is a signed-in administrator. Subject and Name are what the identity
// provider said; a local login records the configured username.
type Session struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	// Roles are the role names the provider gave this session, space separated,
	// or "none" for a user the claim named no role for.
	Roles     string    `json:"roles"`
	Identity  Identity  `json:"identity"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Identity is what the provider said about a session's holder beyond its
// roles, kept for the user info page. It holds no tokens (ADR 0043).
type Identity struct {
	Issuer string `json:"issuer,omitempty"`
	Email  string `json:"email,omitempty"`
	// EmailVerified is the provider saying Email belongs to this user, which
	// is what lets it find their Teams chat (ADR 0065).
	EmailVerified bool `json:"email_verified,omitempty"`
	// ObjectID is the Entra object id from auth.object-id-claim, and Username
	// the preferred_username; both find the user's own Teams chat.
	ObjectID string   `json:"object_id,omitempty"`
	Username string   `json:"username,omitempty"`
	Scopes   []string `json:"scopes,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	// GroupsSource and ClaimSource name the token the values were found in.
	GroupsSource string `json:"groups_source,omitempty"`
	// ClaimValues are the role claim as the provider sent it, before roles
	// were derived from it.
	ClaimValues []string `json:"claim_values,omitempty"`
	ClaimSource string   `json:"claim_source,omitempty"`
}

// LoginFlow is an authorization code flow this service started. Holding the
// verifier and nonce here, rather than in a cookie, is what lets the callback
// prove the flow is one we began.
type LoginFlow struct {
	State     string    `json:"state"`
	Verifier  string    `json:"verifier"`
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
}

// A BrokerToken is the live Keycloak access and refresh token for one admin
// session, custodied only because the delegated-Teams design (ADR 0037)
// explicitly chose live pass-through over a safer, more limited alternative:
// Teamster asks Keycloak's broker endpoint for the Entra token it already
// stored, using the admin's own still-valid Keycloak token, rather than
// talking to Entra's token endpoint directly. AccessToken and RefreshToken are
// ciphertext, sealed by internal/cryptutil before they reach the store and
// opened only in internal/httpserver/broker.go.
type BrokerToken struct {
	SessionID    string    `json:"-"`
	AccessToken  string    `json:"-"`
	RefreshToken string    `json:"-"`
	ExpiresAt    time.Time `json:"-"`
	UpdatedAt    time.Time `json:"-"`
}

// SourceLabel names the webhook an event arrived at (ADR 0052). The server
// sets it, overwriting whatever the sender put there, so a route can trust it.
const SourceLabel = "teamster_source"

// RecipientLabel names the people an addressed route delivers to, comma
// separated, for a sender with no recipients field of its own, such as
// Alertmanager (ADR 0063). Unlike SourceLabel the sender sets it.
const RecipientLabel = "teamster_recipient"

// AddressesOf is who an event names: the universal recipients list when it
// has one, else the RecipientLabel. Addresses are trimmed and deduplicated
// ignoring case, in the order given. Like AttributesOf it is a function, so
// templates cannot reach it.
func AddressesOf(ev Event) []string {
	var raw []string
	if ev.Universal != nil && len(ev.Universal.Recipients) > 0 {
		raw = ev.Universal.Recipients
	} else if label := ev.Labels[RecipientLabel]; label != "" {
		raw = strings.Split(label, ",")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, address := range raw {
		address = strings.TrimSpace(address)
		key := strings.ToLower(address)
		if address == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, address)
	}
	return out
}

// The values of SourceLabel, one per receiving webhook.
const (
	SourceAlertmanager = "alertmanager"
	SourceUniversal    = "universal"
	SourceTeamsV2      = "teamsv2"
)

// WithSourceLabel returns labels plus SourceLabel set to source. It copies
// rather than writes into the sender's map.
func WithSourceLabel(labels map[string]string, source string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	maps.Copy(out, labels)
	out[SourceLabel] = source
	return out
}

// EventState is where an event is in its lifecycle. StateNone is the normal
// case for a sender with no lifecycle: delivered once, tracked nowhere (ADR
// 0035). Open and closed opt into the claim protocol, which is what lets a
// repeat edit a card instead of posting a second one.
type EventState string

const (
	StateNone   EventState = ""
	StateOpen   EventState = "open"
	StateClosed EventState = "closed"
)

// Valid reports whether s is one of the three states.
func (s EventState) Valid() bool {
	return s == StateNone || s == StateOpen || s == StateClosed
}

// An Event is the internal shape of one webhook-delivered message (ADR 0056).
// The core holds what every webhook has; anything only one webhook knows lives
// in that webhook's extension, which is nil for every other source.
//
// Title/Text/Card let a sender supply the message directly rather than
// authoring a Template; a route's own Template wins when it has one, so these
// are the fallback for a route with none (ADR 0036). No adapter fills them
// from anything else.
type Event struct {
	Source string     `json:"source"`
	Key    string     `json:"key"`
	State  EventState `json:"state"`
	// Labels are what routes select on.
	Labels map[string]string `json:"labels"`
	Title  string            `json:"title"`
	Text   string            `json:"text"`
	Card   json.RawMessage   `json:"card"`

	Alertmanager *AlertmanagerEvent `json:"alertmanager,omitempty"`
	Universal    *UniversalEvent    `json:"universal,omitempty"`
}

// AlertmanagerEvent is one Alertmanager group notification beyond the core
// (ADR 0084). Alerts is the group as sent; the flat per-alert fields are set
// only when it holds exactly one alert.
type AlertmanagerEvent struct {
	Alerts       []AlertmanagerAlert `json:"alerts"`
	Annotations  map[string]string   `json:"annotations"`
	StartsAt     time.Time           `json:"starts_at"`
	EndsAt       time.Time           `json:"ends_at"`
	GeneratorURL string              `json:"generator_url"`

	Receiver          string            `json:"receiver"`
	GroupKey          string            `json:"group_key"`
	GroupLabels       map[string]string `json:"group_labels"`
	CommonLabels      map[string]string `json:"common_labels"`
	CommonAnnotations map[string]string `json:"common_annotations"`
	ExternalURL       string            `json:"external_url"`
}

// UniversalEvent is what a universal webhook payload carries beyond the core.
type UniversalEvent struct {
	// Attributes are free text; only their keys are ever sampled (ADR 0041).
	Attributes map[string]string `json:"attributes"`
	Time       time.Time         `json:"time"`
	URL        string            `json:"url"`
	// Recipients are the people the sender named (ADR 0063).
	Recipients []string `json:"recipients"`
	// Broadcast sends to everyone the bot can reach instead (ADR 0083).
	Broadcast bool `json:"broadcast,omitempty"`
}

// AttributesOf returns the free-text map of whichever extension ev carries. It
// is a function rather than a method so templates cannot reach it: a template
// names the source it reads.
func AttributesOf(ev Event) map[string]string {
	switch {
	case ev.Alertmanager != nil:
		if ev.Alertmanager.Annotations != nil {
			return ev.Alertmanager.Annotations
		}
		return ev.Alertmanager.CommonAnnotations
	case ev.Universal != nil:
		return ev.Universal.Attributes
	}
	return nil
}

type AlertmanagerPayload struct {
	Receiver          string              `json:"receiver"`
	Status            string              `json:"status"`
	Alerts            []AlertmanagerAlert `json:"alerts"`
	GroupLabels       map[string]string   `json:"groupLabels"`
	CommonLabels      map[string]string   `json:"commonLabels"`
	CommonAnnotations map[string]string   `json:"commonAnnotations"`
	ExternalURL       string              `json:"externalURL"`
	Version           string              `json:"version"`
	GroupKey          string              `json:"groupKey"`
}

type AlertmanagerAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// UniversalWebhookPayload is the universal webhook's wire format. Only Labels
// matters for routing; State opts into the tracked lifecycle, and Title/Text/
// Card let a sender supply the message directly (ADR 0036). The rest becomes
// the event's Universal extension.
type UniversalWebhookPayload struct {
	Key        string            `json:"key"`
	State      EventState        `json:"state"`
	Labels     map[string]string `json:"labels"`
	Title      string            `json:"title"`
	Text       string            `json:"text"`
	Card       json.RawMessage   `json:"card"`
	Attributes map[string]string `json:"attributes"`
	Time       time.Time         `json:"time"`
	URL        string            `json:"url"`
	// Recipients names the people an addressed route delivers to: UPNs, mail
	// addresses or Entra object ids (ADR 0063).
	Recipients []string `json:"recipients"`
	// Broadcast sends to everyone the bot can reach, in the background (ADR 0083).
	Broadcast bool `json:"broadcast"`
}

// SampleKind says which part of an event a sample came from.
type SampleKind string

const (
	SampleLabel     SampleKind = "label"
	SampleAttribute SampleKind = "attribute"
)

// An EventSample is one label key and value, or one attribute key, that recent
// events carried. The admin UI completes from these (ADR 0041). Value is always
// empty for an attribute: attribute values are free text and are never kept.
// Alertmanager annotations are attributes here.
type EventSample struct {
	Kind      SampleKind
	Key       string
	Value     string
	SeenCount int64
	FirstSeen time.Time
	LastSeen  time.Time
}

// How an actor proved who they are, recorded on every audit event.
const (
	ViaSession = "session"
	ViaBasic   = "basic"
	ViaToken   = "token"
	ViaCLI     = "cli"
	ViaSystem  = "system"
)

// An Actor is who caused an audit event. TokenID names the access token a
// request arrived with, when it arrived with one.
type Actor struct {
	Subject string `json:"subject"`
	Name    string `json:"name,omitempty"`
	Via     string `json:"via"`
	TokenID string `json:"token_id,omitempty"`
}

// An AuditEvent is one change to the configuration (ADR 0070). Action is
// "<resource>.<verb>", such as "template.update". Before and After are JSON
// snapshots of the record; Before is empty for a create, After for a delete.
type AuditEvent struct {
	ID           string          `json:"id"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Actor        Actor           `json:"actor"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id,omitempty"`
	RequestID    string          `json:"request_id,omitempty"`
	Before       json.RawMessage `json:"before,omitempty"`
	After        json.RawMessage `json:"after,omitempty"`
}

// An AuditFilter narrows ListAuditEvents. Empty fields match everything, and
// a zero Since or Until leaves that end of the range open. The cursor is the
// last event of the previous page.
type AuditFilter struct {
	Actor        string
	ResourceType string
	ResourceID   string
	Action       string
	Since        time.Time
	Until        time.Time
	CursorID     string
	CursorAt     time.Time
	Limit        int
}

// A User is someone who has signed in (ADR 0072). Roles and IdPGroups are what
// the provider said at their last sign-in. A disabled user is refused sign-in.
type User struct {
	Subject    string    `json:"subject"`
	Source     string    `json:"source"`
	Name       string    `json:"name"`
	Email      string    `json:"email,omitempty"`
	Roles      []string  `json:"roles"`
	IdPGroups  []string  `json:"idp_groups"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	DisabledAt time.Time `json:"disabled_at,omitzero"`
	DisabledBy string    `json:"disabled_by,omitempty"`
	// ObjectID is the Entra object id from the last sign-in that carried one,
	// which tells a message to oneself from one to someone else (ADR 0082).
	ObjectID string `json:"object_id,omitempty"`
}

// Disabled says whether the user is refused sign-in.
func (u User) Disabled() bool { return !u.DisabledAt.IsZero() }

// A Group is a local set of principals (ADR 0074).
type Group struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedBy   string    `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// A MemberType says what a group member's id names.
type MemberType string

const (
	MemberUser     MemberType = "user"
	MemberGroup    MemberType = "group"
	MemberIdPGroup MemberType = "idp_group"
)

// Valid reports whether t is one of the three member types.
func (t MemberType) Valid() bool {
	return t == MemberUser || t == MemberGroup || t == MemberIdPGroup
}

// A GroupMember puts a user, a group or an identity provider group in GroupID.
type GroupMember struct {
	GroupID string     `json:"group_id"`
	Type    MemberType `json:"type"`
	ID      string     `json:"id"`
	AddedBy string     `json:"added_by,omitempty"`
	AddedAt time.Time  `json:"added_at"`
}

// A PrincipalType says what a permission's principal id names (ADR 0075).
type PrincipalType string

const (
	PrincipalUser     PrincipalType = "user"
	PrincipalGroup    PrincipalType = "group"
	PrincipalIdPGroup PrincipalType = "idp_group"
	PrincipalRole     PrincipalType = "role"
)

// Valid reports whether t is one of the four principal types.
func (t PrincipalType) Valid() bool {
	return t == PrincipalUser || t == PrincipalGroup || t == PrincipalIdPGroup || t == PrincipalRole
}

// A Permission is the actions one principal holds on one resource. ResourceID
// "*" is the collection, which is where create is granted.
type Permission struct {
	ID            string        `json:"id"`
	PrincipalType PrincipalType `json:"principal_type"`
	PrincipalID   string        `json:"principal_id"`
	ResourceType  string        `json:"resource_type"`
	ResourceID    string        `json:"resource_id"`
	Actions       []string      `json:"actions"`
	CreatedBy     string        `json:"created_by,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// An AuditCursor is the last event a relay delivered, in the trail's order
// (ADR 0078). The zero cursor is before every event.
type AuditCursor struct {
	At time.Time
	ID string
}

// IsZero reports whether the cursor names no event.
func (c AuditCursor) IsZero() bool { return c.ID == "" && c.At.IsZero() }
