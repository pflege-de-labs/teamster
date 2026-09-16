package models

import "time"

// A Template renders into a Teams message. Title is the line the activity feed
// previews, Text optional formatted prose, Body the Adaptive Card JSON — and a
// template needs only one of the three.
type Template struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Title     string    `json:"title"`
	Text      string    `json:"text"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Destination struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TeamID    string    `json:"team_id"`
	ChannelID string    `json:"channel_id"`
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
	ID            string    `json:"id"`
	TeamSlug      string    `json:"team_slug"`
	ChannelSlug   string    `json:"channel_slug"`
	DestinationID string    `json:"destination_id"`
	TokenHash     string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

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
	ID             string    `json:"id"`
	Subject        string    `json:"subject"`
	Name           string    `json:"name"`
	AADObjectID    string    `json:"aad_object_id"`
	ConversationID string    `json:"conversation_id"`
	ServiceURL     string    `json:"service_url"`
	BotChannelID   string    `json:"bot_channel_id"`
	TenantID       string    `json:"tenant_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

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
	TemplateID    string            `json:"template_id"`
	IsDefault     bool              `json:"is_default"`
	Greedy        bool              `json:"greedy"`
	Priority      int               `json:"priority"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// An ActiveAlert is one card this service posted. An alert that fans out to
// several channels has one of these per channel, which is why the key is the
// fingerprint together with the Team and channel.
// An ActiveAlert is one card: the message this service posted to one channel
// for one alert, and is keeping up to date until the alert resolves.
//
// A row exists from the moment delivery claims the right to post, which is
// before the card does. PostedAt is what tells the two apart: while it is zero
// the row is a claim in flight and MessageID is empty, because there is no
// message yet to name.
type ActiveAlert struct {
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"`
	TeamID      string    `json:"team_id"`
	ChannelID   string    `json:"channel_id"`
	MessageID   string    `json:"message_id"`
	ClaimOwner  string    `json:"claim_owner,omitempty"`
	ClaimedAt   time.Time `json:"claimed_at,omitempty"`
	PostedAt    time.Time `json:"posted_at,omitempty"`
	LastUpdate  time.Time `json:"last_update"`
}

// Posted reports whether a card exists for this row, as opposed to a claim on
// the right to post one.
func (a ActiveAlert) Posted() bool { return !a.PostedAt.IsZero() }

// An AlertClaim is the right to post one card, asked for before the Graph call
// and completed after it. Owner is unique to the attempt rather than to the
// process: it only has to answer "is this still mine to finish".
type AlertClaim struct {
	Fingerprint string
	TeamID      string
	ChannelID   string
	Status      string
	Owner       string
	At          time.Time
	// StaleBefore is the age at which a claim is presumed abandoned, because
	// whatever took it died between claiming and posting.
	StaleBefore time.Time
}

// An ActiveAlertRecipient is AlertClaim's card mirrored for a chat delivery: one
// message a bot sent to one person for one alert, kept up to date until it
// resolves. It is a parallel table to ActiveAlert rather than a wider key on
// it, because a person has neither a Team nor a channel for that table's CHECK
// to hold (see ADR 0026 and ADR 0021).
//
// Unlike ActiveAlert, an empty MessageID does not always mean "claimed, not
// posted": bot.SendMessage returning ("", nil) is a documented success --
// delivered, but with nothing to name it by for a later edit. PostedAt is the
// only reliable state machine here.
type ActiveAlertRecipient struct {
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"`
	RecipientID string    `json:"recipient_id"`
	MessageID   string    `json:"message_id"`
	ClaimOwner  string    `json:"claim_owner,omitempty"`
	ClaimedAt   time.Time `json:"claimed_at,omitempty"`
	PostedAt    time.Time `json:"posted_at,omitempty"`
	LastUpdate  time.Time `json:"last_update"`
}

// Posted reports whether a card exists for this row, as opposed to a claim on
// the right to post one.
func (a ActiveAlertRecipient) Posted() bool { return !a.PostedAt.IsZero() }

// A RecipientClaim is AlertClaim's counterpart for a chat delivery: the right
// to send or update one person's message, asked for before the Bot Connector
// call and completed after it.
type RecipientClaim struct {
	Fingerprint string
	RecipientID string
	Status      string
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
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
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

type Alert struct {
	Source      string            `json:"source"`
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"starts_at"`
	EndsAt      time.Time         `json:"ends_at"`
	Generator   string            `json:"generator"`
	Fingerprint string            `json:"fingerprint"`
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

type UniversalWebhookPayload struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"starts_at"`
	EndsAt      time.Time         `json:"ends_at"`
	Generator   string            `json:"generator"`
	Fingerprint string            `json:"fingerprint"`
}
