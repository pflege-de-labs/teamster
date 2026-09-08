package config

import (
	"fmt"
)

type Config struct {
	Server   ServerConfig   `embed:"" prefix:"server-"`
	Database DatabaseConfig `embed:"" prefix:"database-"`
	Webhook  WebhookConfig  `embed:"" prefix:"webhook-"`
	Admin    AdminConfig    `embed:"" prefix:"admin-"`
	Graph    GraphConfig    `embed:"" prefix:"graph-"`
}

type ServerConfig struct {
	Addr string `help:"Address the HTTP server listens on." default:":8080"`
}

type DatabaseConfig struct {
	Path string `help:"Path to the SQLite database file." default:"teamster.db"`
}

type WebhookConfig struct {
	Token string `help:"Shared secret expected in the X-Teamster-Token header."`
}

type AdminConfig struct {
	Username string `help:"Basic auth user for the admin UI."`
	Password string `help:"Basic auth password for the admin UI."`
}

type GraphConfig struct {
	TenantID     string `help:"Microsoft Entra tenant ID."`
	ClientID     string `help:"Microsoft Entra application (client) ID."`
	ClientSecret string `help:"Microsoft Entra client secret."`
	BaseURL      string `help:"Microsoft Graph API base URL." default:"https://graph.microsoft.com/v1.0"`
	TimeoutSec   int    `help:"Timeout in seconds for Graph API calls." default:"10"`
}

func (cfg Config) Validate() error {
	if cfg.Graph.TenantID == "" || cfg.Graph.ClientID == "" || cfg.Graph.ClientSecret == "" {
		return fmt.Errorf("graph config is required")
	}
	if cfg.Admin.Username == "" || cfg.Admin.Password == "" {
		return fmt.Errorf("admin username/password is required")
	}
	if cfg.Webhook.Token == "" {
		return fmt.Errorf("webhook token is required")
	}
	return nil
}
