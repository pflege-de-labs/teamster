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

// A Route decides where an alert goes. Routes form a tree: a child refines its
// parent's match, and delivers instead of its parent when Greedy, as well as it
// when not. A child leaves DestinationID or TemplateID empty to inherit the
// nearest ancestor's.
type Route struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	ParentID      string            `json:"parent_id"`
	LabelSelector map[string]string `json:"label_selector"`
	DestinationID string            `json:"destination_id"`
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
type ActiveAlert struct {
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"`
	TeamID      string    `json:"team_id"`
	ChannelID   string    `json:"channel_id"`
	MessageID   string    `json:"message_id"`
	LastUpdate  time.Time `json:"last_update"`
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
