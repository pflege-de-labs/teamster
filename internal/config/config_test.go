package config

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"
)

// testCLI mirrors the flag layout of cmd/teamster so the tests exercise the real
// kong wiring, including how kong-yaml maps nested YAML onto prefixed flags.
type testCLI struct {
	ConfigFile kong.ConfigFlag `name:"config" short:"c"`

	Config Config `embed:""`
}

func parse(t *testing.T, args []string, paths ...string) Config {
	t.Helper()

	cli := testCLI{}
	parser, err := kong.New(&cli,
		kong.Configuration(kongyaml.Loader, paths...),
		kong.DefaultEnvars("TEAMSTER"),
		kong.Writers(io.Discard, io.Discard),
		kong.Exit(func(int) { t.Fatal("kong exited") }),
	)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := parser.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return cli.Config
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestParseExampleConfig(t *testing.T) {
	t.Parallel()

	got := parse(t, nil, "../../config.example.yaml")
	want := Config{
		Server: ServerConfig{
			Addr:            ":8080",
			ShutdownTimeout: 15 * time.Second,
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    time.Minute,
			IdleTimeout:     2 * time.Minute,
		},
		Database: DatabaseConfig{Path: "teamster.db"},
		Webhook:  WebhookConfig{Token: "replace-with-shared-token"},
		Admin:    AdminConfig{Username: "admin", Password: "change-me"},
		Auth: AuthConfig{
			OIDCDiscoveryURL: "https://login.example/auth/realms/internal/.well-known/openid-configuration",
			OIDCClientID:     "teamster",
			OIDCRedirectURL:  "https://teamster.example/admin/auth/callback",
			OIDCScopes:       []string{"profile", "email", "roles"},
			Claim:            "realm_access.roles",
			Allowed:          []string{"admin"},
			AdminValues:      []string{"teamster-admins"},
			EditorValues:     []string{"teamster-editors"},
			ViewerValues:     []string{"teamster-viewers"},
			SessionTTL:       12 * time.Hour,
		},
		Graph: GraphConfig{
			TenantID:     "your-tenant-id",
			ClientID:     "your-client-id",
			ClientSecret: "your-client-secret",
			BaseURL:      "https://graph.microsoft.com/v1.0",
			TimeoutSec:   10,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsed example config = %+v, want %+v", got, want)
	}
	if err := Validate(got); err != nil {
		t.Errorf("example config must be valid: %v", err)
	}
}

func TestParseAppliesTagDefaults(t *testing.T) {
	t.Parallel()

	got := parse(t, nil, writeConfig(t, "webhook:\n  token: t\n"))
	if got.Server.Addr != ":8080" {
		t.Errorf("Server.Addr = %q, want %q", got.Server.Addr, ":8080")
	}
	if got.Server.ShutdownTimeout != 15*time.Second {
		t.Errorf("Server.ShutdownTimeout = %s, want 15s", got.Server.ShutdownTimeout)
	}
	if got.Database.Path != "teamster.db" {
		t.Errorf("Database.Path = %q, want %q", got.Database.Path, "teamster.db")
	}
	if got.Graph.BaseURL != "https://graph.microsoft.com/v1.0" {
		t.Errorf("Graph.BaseURL = %q, want the Graph v1.0 endpoint", got.Graph.BaseURL)
	}
	if got.Graph.TimeoutSec != 10 {
		t.Errorf("Graph.TimeoutSec = %d, want 10", got.Graph.TimeoutSec)
	}
}

// Guards the ordering contract SearchPaths relies on: kong keeps the value from
// the last matching resolver, so later paths must be the more specific ones.
func TestParsePrecedence(t *testing.T) {
	t.Parallel()

	system := writeConfig(t, "server:\n  addr: \":1111\"\n")
	user := writeConfig(t, "server:\n  addr: \":2222\"\n")
	explicit := writeConfig(t, "server:\n  addr: \":3333\"\n")

	tests := []struct {
		name  string
		args  []string
		paths []string
		want  string
	}{
		{name: "later search path wins", paths: []string{system, user}, want: ":2222"},
		{name: "missing paths are skipped", paths: []string{"does-not-exist.yaml", system}, want: ":1111"},
		{name: "config flag beats search paths", args: []string{"--config", explicit}, paths: []string{system, user}, want: ":3333"},
		{name: "command line beats every file", args: []string{"--server-addr", ":4444"}, paths: []string{system, user}, want: ":4444"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := parse(t, tt.args, tt.paths...).Server.Addr; got != tt.want {
				t.Errorf("Server.Addr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := Config{
		Webhook: WebhookConfig{Token: "token"},
		Admin:   AdminConfig{Username: "admin", Password: "secret"},
		Graph:   GraphConfig{TenantID: "tenant", ClientID: "client", ClientSecret: "secret"},
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "complete config", mutate: func(*Config) {}},
		{name: "missing tenant", mutate: func(c *Config) { c.Graph.TenantID = "" }, wantErr: "graph config is required"},
		{name: "missing client id", mutate: func(c *Config) { c.Graph.ClientID = "" }, wantErr: "graph config is required"},
		{name: "missing client secret", mutate: func(c *Config) { c.Graph.ClientSecret = "" }, wantErr: "graph config is required"},
		{name: "missing admin user", mutate: func(c *Config) { c.Admin.Username = "" }, wantErr: "admin username/password is required"},
		{name: "missing admin password", mutate: func(c *Config) { c.Admin.Password = "" }, wantErr: "admin username/password is required"},
		{name: "missing webhook token", mutate: func(c *Config) { c.Webhook.Token = "" }, wantErr: "webhook token is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			tt.mutate(&cfg)

			err := Validate(cfg)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tt.wantErr != "" && err == nil:
				t.Errorf("Validate() = nil, want %q", tt.wantErr)
			case tt.wantErr != "" && err.Error() != tt.wantErr:
				t.Errorf("Validate() = %q, want %q", err, tt.wantErr)
			}
		})
	}
}

// Documents where environment variables sit in the precedence chain: kong
// resolvers run before defaults, so a config file wins over $TEAMSTER_*.
func TestParseEnvPrecedence(t *testing.T) {
	file := writeConfig(t, "server:\n  addr: \":1111\"\n")
	t.Setenv("TEAMSTER_SERVER_ADDR", ":9999")

	if got := parse(t, nil, file).Server.Addr; got != ":1111" {
		t.Errorf("config file lost to the environment: Server.Addr = %q, want %q", got, ":1111")
	}
	if got := parse(t, nil).Server.Addr; got != ":9999" {
		t.Errorf("environment ignored without a config file: Server.Addr = %q, want %q", got, ":9999")
	}
	if got := parse(t, []string{"--server-addr", ":4444"}, file).Server.Addr; got != ":4444" {
		t.Errorf("command line lost: Server.Addr = %q, want %q", got, ":4444")
	}
}

// Failing closed: an issuer without accepted values would admit everyone the
// provider will authenticate.
func TestValidateRefusesIncompleteOIDC(t *testing.T) {
	t.Parallel()

	complete := Config{
		Webhook: WebhookConfig{Token: "token"},
		Admin:   AdminConfig{Username: "admin", Password: "secret"},
		Graph:   GraphConfig{TenantID: "tenant", ClientID: "client", ClientSecret: "secret"},
		Auth: AuthConfig{
			OIDCDiscoveryURL: "https://login.example/auth/realms/internal/.well-known/openid-configuration",
			OIDCClientID:     "teamster",
			OIDCRedirectURL:  "https://teamster.example/admin/auth/callback",
			Claim:            "realm_access.roles",
			Allowed:          []string{"admin"},
		},
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "complete", mutate: func(*Config) {}},
		{name: "no issuer means no OIDC, and the rest is ignored", mutate: func(c *Config) { c.Auth = AuthConfig{} }},
		{name: "issuer without accepted values", mutate: func(c *Config) { c.Auth.Allowed = nil }, wantErr: "auth allowed, or one of the role value lists, is required"},
		{
			// A role list names who may sign in just as much as auth-allowed does.
			name:   "a role list stands in for auth-allowed",
			mutate: func(c *Config) { c.Auth.Allowed, c.Auth.EditorValues = nil, []string{"teamster-editors"} },
		},
		{name: "issuer without a claim", mutate: func(c *Config) { c.Auth.Claim = "" }, wantErr: "auth claim is required"},
		{name: "issuer without a client id", mutate: func(c *Config) { c.Auth.OIDCClientID = "" }, wantErr: "oidc-client-id is required"},
		{name: "issuer without a redirect url", mutate: func(c *Config) { c.Auth.OIDCRedirectURL = "" }, wantErr: "oidc-redirect-url is required"},
		{name: "a public client needs no secret", mutate: func(c *Config) { c.Auth.OIDCClientSecret = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := complete
			tt.mutate(&cfg)

			err := Validate(cfg)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// A path alone reaches the provider verbatim and is refused there, far from the
// configuration that caused it.
func TestValidateRequiresAnAbsoluteRedirectURL(t *testing.T) {
	t.Parallel()

	base := Config{
		Webhook: WebhookConfig{Token: "token"},
		Admin:   AdminConfig{Username: "admin", Password: "secret"},
		Graph:   GraphConfig{TenantID: "tenant", ClientID: "client", ClientSecret: "secret"},
		Auth: AuthConfig{
			OIDCDiscoveryURL: "https://login.example/auth/realms/internal/.well-known/openid-configuration",
			OIDCClientID:     "teamster",
			Claim:            "realm_access.roles",
			Allowed:          []string{"admin"},
		},
	}

	tests := []struct {
		name     string
		redirect string
		wantErr  bool
	}{
		{name: "absolute https", redirect: "https://teamster.example/admin/auth/callback"},
		{name: "absolute http for local development", redirect: "http://localhost:8080/admin/auth/callback"},
		{name: "a path alone", redirect: "/admin/auth/callback", wantErr: true},
		{name: "host without a scheme", redirect: "teamster.example/admin/auth/callback", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := base
			cfg.Auth.OIDCRedirectURL = tt.redirect

			err := Validate(cfg)
			if tt.wantErr && err == nil {
				t.Errorf("Validate() = nil, want %q refused", tt.redirect)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate() = %v, want %q accepted", err, tt.redirect)
			}
		})
	}
}
