package config

import (
	"fmt"
	"time"
)

type Config struct {
	Server   ServerConfig   `embed:"" prefix:"server-"`
	Database DatabaseConfig `embed:"" prefix:"database-"`
	Webhook  WebhookConfig  `embed:"" prefix:"webhook-"`
	Admin    AdminConfig    `embed:"" prefix:"admin-"`
	Auth     AuthConfig     `embed:"" prefix:"auth-"`
	Graph    GraphConfig    `embed:"" prefix:"graph-"`
}

type ServerConfig struct {
	Addr            string        `help:"Address the HTTP server listens on." default:":8080"`
	ShutdownTimeout time.Duration `help:"How long to wait for in-flight requests when shutting down." default:"15s"`
}

type DatabaseConfig struct {
	Path string `help:"Path to the SQLite database file." default:"teamster.db"`
}

type WebhookConfig struct {
	Token string `help:"Shared secret expected in the X-Teamster-Token header."`
}

type AdminConfig struct {
	Username string `help:"Local admin login name, also accepted as basic auth on /api."`
	Password string `help:"Local admin login password, also accepted as basic auth on /api."`
}

// AuthConfig configures who may administer the service. It is unrelated to
// GraphConfig: that is a machine credential for posting cards, this is how a
// person signs in.
type AuthConfig struct {
	OIDCIssuer       string        `help:"OIDC issuer URL; discovery decides the endpoints." name:"oidc-issuer"`
	OIDCClientID     string        `help:"OIDC client ID." name:"oidc-client-id"`
	OIDCClientSecret string        `help:"OIDC client secret; omit for a public client using PKCE." name:"oidc-client-secret"`
	OIDCRedirectURL  string        `help:"Absolute URL of /admin/auth/callback as registered with the provider." name:"oidc-redirect-url"`
	OIDCScopes       []string      `help:"Extra scopes to request beyond openid." name:"oidc-scopes" default:"profile,email,roles"`
	Claim            string        `help:"Dotted path of the claim carrying membership, e.g. realm_access.roles." default:"realm_access.roles"`
	Allowed          []string      `help:"Claim values granted admin access; required when an issuer is set."`
	SessionTTL       time.Duration `help:"How long a login lasts." default:"12h"`
}

type GraphConfig struct {
	TenantID     string `help:"Microsoft Entra tenant ID."`
	ClientID     string `help:"Microsoft Entra application (client) ID."`
	ClientSecret string `help:"Microsoft Entra client secret."`
	BaseURL      string `help:"Microsoft Graph API base URL." default:"https://graph.microsoft.com/v1.0"`
	TimeoutSec   int    `help:"Timeout in seconds for Graph API calls." default:"10"`
}

// Validate is a function rather than a method on Config: kong calls a
// Validate() method on any embedded struct during Parse, which would force
// every command to carry full credentials just to parse its flags.
func Validate(cfg Config) error {
	if cfg.Graph.TenantID == "" || cfg.Graph.ClientID == "" || cfg.Graph.ClientSecret == "" {
		return fmt.Errorf("graph config is required")
	}
	if cfg.Admin.Username == "" || cfg.Admin.Password == "" {
		return fmt.Errorf("admin username/password is required")
	}
	if cfg.Webhook.Token == "" {
		return fmt.Errorf("webhook token is required")
	}
	// Failing closed: an issuer without accepted values would admit everyone the
	// provider will authenticate.
	if cfg.Auth.OIDCIssuer != "" {
		if cfg.Auth.OIDCClientID == "" {
			return fmt.Errorf("auth oidc-client-id is required when an issuer is configured")
		}
		if cfg.Auth.OIDCRedirectURL == "" {
			return fmt.Errorf("auth oidc-redirect-url is required when an issuer is configured")
		}
		if len(cfg.Auth.Allowed) == 0 {
			return fmt.Errorf("auth allowed is required when an issuer is configured")
		}
		if cfg.Auth.Claim == "" {
			return fmt.Errorf("auth claim is required when an issuer is configured")
		}
	}
	return nil
}
