package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	Server   ServerConfig   `embed:"" prefix:"server-"`
	UI       UIConfig       `embed:"" prefix:"ui-"`
	Metrics  MetricsConfig  `embed:"" prefix:"metrics-"`
	Database DatabaseConfig `embed:"" prefix:"database-"`
	Webhook  WebhookConfig  `embed:"" prefix:"webhook-"`
	Admin    AdminConfig    `embed:"" prefix:"admin-"`
	Auth     AuthConfig     `embed:"" prefix:"auth-"`
	Graph    GraphConfig    `embed:"" prefix:"graph-"`
	Bot      BotConfig      `embed:"" prefix:"bot-"`
}

type ServerConfig struct {
	Addr            string        `help:"Address the HTTP server listens on." default:":8080"`
	ShutdownTimeout time.Duration `help:"How long to wait for in-flight requests when shutting down." default:"15s"`
	ReadTimeout     time.Duration `help:"How long a client may take to send a request, headers and body." default:"15s"`
	WriteTimeout    time.Duration `help:"How long a handler may take to answer; must exceed graph-timeout-sec." default:"60s"`
	IdleTimeout     time.Duration `help:"How long an idle keep-alive connection is held open." default:"120s"`
}

// UIConfig is about the admin UI's text, not its behaviour.
type UIConfig struct {
	Language  string `help:"Language to use when a browser asks for none this build carries." default:"en"`
	LocaleDir string `help:"Directory of catalog files that override the built-in text." name:"locale-dir"`
}

// MetricsConfig decides whether this service says anything about itself, and to
// whom. It is off by default: the attributes name routes, templates and
// channels, and the endpoint that carries them authenticates nobody.
type MetricsConfig struct {
	Enabled bool `help:"Collect metrics and export them." default:"false"`
	// Loopback rather than every interface: the listener is unauthenticated and
	// every collection asks the database how many alerts are open, so reaching
	// it should take a deliberate act of plumbing.
	Addr            string        `help:"Address of the unauthenticated metrics listener." default:"127.0.0.1:9090"`
	Path            string        `help:"Path the Prometheus exporter is served at." default:"/metrics"`
	Prometheus      bool          `help:"Serve the Prometheus exporter on the metrics listener." default:"true"`
	OTLPEndpoint    string        `help:"OTLP collector endpoint; empty runs no OTLP exporter." name:"otlp-endpoint"`
	OTLPProtocol    string        `help:"OTLP transport." name:"otlp-protocol" enum:"grpc,http" default:"http"`
	OTLPInsecure    bool          `help:"Send OTLP without TLS." name:"otlp-insecure" default:"false"`
	OTLPInterval    time.Duration `help:"How often metrics are pushed over OTLP." name:"otlp-interval" default:"60s"`
	ShutdownTimeout time.Duration `help:"How long the final export and the listener drain may take." default:"5s"`
	ServiceName     string        `help:"service.name reported with every metric." default:"teamster"`
}

// DatabaseConfig chooses the backend and configures it. The keys belonging to
// the driver that is not selected are ignored rather than rejected: the
// container image sets TEAMSTER_DATABASE_PATH unconditionally, so rejecting a
// path under postgres would fail every containerised deployment over a value
// nobody wrote.
type DatabaseConfig struct {
	Driver string `help:"Storage backend: sqlite for a single instance, postgres for several." enum:"sqlite,postgres" default:"sqlite"`

	// Keeps its flat name rather than moving under a sqlite- prefix: renaming
	// it would break every existing --database-path, TEAMSTER_DATABASE_PATH
	// and the image's own ENV, to buy symmetry and nothing else.
	Path string `help:"Path to the SQLite database file. Used when database-driver is sqlite." default:"teamster.db"`

	Postgres PostgresConfig `embed:"" prefix:"postgres-"`

	MaxOpenConns    int           `help:"Maximum open connections; 0 lets the backend choose." name:"max-open-conns" default:"0"`
	MaxIdleConns    int           `help:"Maximum idle connections; 0 lets the backend choose." name:"max-idle-conns" default:"0"`
	ConnMaxLifetime time.Duration `help:"How long a pooled connection may be reused; 0 lets the backend choose." name:"conn-max-lifetime" default:"0s"`
	ConnectTimeout  time.Duration `help:"How long to wait for the first connection before giving up at startup." name:"connect-timeout" default:"10s"`
	// A deployment that would rather run migrations as a visible step sets
	// verify, and `teamster migrate up` becomes part of the upgrade.
	Migrate string `help:"What opening the database does about pending migrations: apply them, verify none are pending, or neither." enum:"auto,verify,off" default:"auto"`
}

// PostgresConfig is discrete fields rather than one connection string, because
// a string cannot be split between the config file and the environment. A
// config file value wins over an environment variable (ADR 0001), so a DSN in
// the file would take the password with it -- and a DSN in the environment
// would mean the chart could render none of the rest. This way exactly one key
// carries a secret, which is the same shape the other credentials already have.
type PostgresConfig struct {
	Host     string `help:"Postgres host name."`
	Port     int    `help:"Postgres port." default:"5432"`
	DBName   string `help:"Postgres database name." name:"dbname" default:"teamster"`
	User     string `help:"Postgres user." default:"teamster"`
	Password string `help:"Postgres password. Deliver it as TEAMSTER_DATABASE_POSTGRES_PASSWORD; a config file value would win over the environment."`
	SSLMode  string `help:"libpq sslmode." name:"sslmode" enum:"disable,allow,prefer,require,verify-ca,verify-full" default:"require"`
	// A managed Postgres publishes its own root, which the distroless image
	// does not carry, so verify-ca and verify-full need this pointed at a
	// mounted bundle.
	SSLRootCert string `help:"CA certificate file that verify-ca and verify-full check the server against." name:"sslrootcert"`

	// The escape hatch for what the fields above cannot say: a pooler, a
	// target_session_attrs, a search_path. It carries the password, so deliver
	// it as TEAMSTER_DATABASE_POSTGRES_URL.
	URL string `help:"Full postgres:// connection URL instead of the individual fields." name:"url"`
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
	OIDCDiscoveryURL string        `help:"URL of the provider's /.well-known/openid-configuration document." name:"oidc-discovery-url"`
	OIDCClientID     string        `help:"OIDC client ID." name:"oidc-client-id"`
	OIDCClientSecret string        `help:"OIDC client secret; omit for a public client using PKCE." name:"oidc-client-secret"`
	OIDCRedirectURL  string        `help:"Absolute URL of /admin/auth/callback as registered with the provider." name:"oidc-redirect-url"`
	OIDCScopes       []string      `help:"Extra scopes to request beyond openid." name:"oidc-scopes" default:"profile,email,roles"`
	Claim            string        `help:"Dotted path of the claim carrying membership, e.g. realm_access.roles." default:"realm_access.roles"`
	DefaultRole      string        `help:"Role for a user whose claim names none: admin, editor, viewer, or empty for no access." name:"default-role" enum:"admin,editor,viewer," default:""`
	SessionTTL       time.Duration `help:"How long a login lasts." default:"12h"`
}

type GraphConfig struct {
	TenantID     string `help:"Microsoft Entra tenant ID."`
	ClientID     string `help:"Microsoft Entra application (client) ID."`
	ClientSecret string `help:"Microsoft Entra client secret."`
	BaseURL      string `help:"Microsoft Graph API base URL." default:"https://graph.microsoft.com/v1.0"`
	TimeoutSec   int    `help:"Timeout in seconds for Graph API calls." default:"10"`

	// The token endpoint cannot have a static default because it carries the
	// tenant id, so an empty value means the public cloud's. A sovereign cloud
	// needs this and base-url and scope changed together; a test needs it to
	// point somewhere it controls.
	TokenURL string `help:"OAuth2 token endpoint. Empty derives the public Microsoft Entra endpoint for graph-tenant-id." name:"token-url"`
	Scope    string `help:"OAuth2 scope requested for Graph." default:"https://graph.microsoft.com/.default"`
}

// BotConfig is a second, separate Entra registration for the Bot Framework
// identity that sends chat messages, unrelated to GraphConfig's channel-posting
// credential. It is off by default: a deployment that does not want alerts in a
// person's chat configures nothing.
type BotConfig struct {
	TenantID     string `help:"Microsoft Entra tenant ID for the bot registration."`
	ClientID     string `help:"Microsoft Entra application (client) ID for the bot registration."`
	ClientSecret string `help:"Microsoft Entra client secret for the bot registration."`

	// Bot Framework issues its own multi-tenant token when a bot is registered
	// to accept any tenant's users, which single-tenant apps never need. The
	// trailing empty enum member, matching AuthConfig.DefaultRole, is what
	// lets TEAMSTER_BOT_TENANT_TYPE="" parse at all for a deployment that
	// leaves the feature off -- without it kong rejects the empty string
	// before validateBot's own "" case is ever reached.
	TenantType string `help:"Whether the bot registration is single or multi tenant." enum:"single,multi," default:"single"`

	// The token endpoint cannot have a static default because it carries the
	// tenant id, so an empty value means the one TenantType derives. A
	// sovereign cloud needs this and metadata-url changed together; a test
	// needs it to point somewhere it controls.
	TokenURL    string `help:"OAuth2 token endpoint. Empty derives one from bot-tenant-id and bot-tenant-type." name:"token-url"`
	Scope       string `help:"OAuth2 scope requested for the Bot Connector API." default:"https://api.botframework.com/.default"`
	MetadataURL string `help:"Bot Framework OpenID configuration document, used to validate inbound requests." name:"metadata-url" default:"https://login.botframework.com/v1/.well-known/openidconfiguration"`
	TimeoutSec  int    `help:"Timeout in seconds for Bot Connector API calls." default:"10"`
}

// Configured is the single place that decides whether the feature is on at
// all, shared by the HTTP layer (whether to register the inbound route, the
// notifications UI and its nav link) and ServeCmd (whether to construct a
// bot.Client in the first place, rather than one that would sit unused and
// still attempt a token request the moment something called it).
func (c BotConfig) Configured() bool {
	if c.ClientID == "" || c.ClientSecret == "" || c.MetadataURL == "" {
		return false
	}
	// A multi-tenant registration authenticates through the shared
	// botframework.com tenant and needs no tenant id of its own; see
	// validateBot for the same rule applied to config validation.
	return c.TenantType == "multi" || c.TenantID != ""
}

// validateDatabase checks what the chosen driver needs, and deliberately does
// not check the other one's settings. The container image sets
// TEAMSTER_DATABASE_PATH whatever the driver is, so rejecting a path under
// postgres would fail every containerised deployment over a value the operator
// never wrote.
//
// It also does not require a password: an empty one is a real Postgres auth
// choice -- trust on a localhost pooler, an IAM token minted elsewhere -- and
// refusing it here would be this service overruling the database's own
// configuration.
func validateDatabase(cfg DatabaseConfig) error {
	switch cfg.Driver {
	case "sqlite", "":
		if cfg.Path == "" {
			return fmt.Errorf("database path is required when database-driver is sqlite")
		}
		return nil
	case "postgres":
		if cfg.Postgres.URL != "" {
			if cfg.Postgres.Host != "" || cfg.Postgres.Password != "" || cfg.Postgres.SSLRootCert != "" {
				return fmt.Errorf("set either database-postgres-url or the individual database-postgres-* settings, not both")
			}
			return nil
		}
		if cfg.Postgres.Host == "" {
			return fmt.Errorf("database-postgres-host is required when database-driver is postgres")
		}
		if cfg.Postgres.DBName == "" || cfg.Postgres.User == "" {
			return fmt.Errorf("database-postgres-dbname and database-postgres-user are required when database-driver is postgres")
		}
		return nil
	default:
		return fmt.Errorf("database-driver must be sqlite or postgres, not %q", cfg.Driver)
	}
}

// validateMetrics refuses a configuration that would start a listener nobody
// can scrape, or none at all while claiming to be enabled. Everything here is
// gated on Enabled: a deployment that wants no metrics configures nothing.
func validateMetrics(cfg Config) error {
	if !cfg.Metrics.Enabled {
		return nil
	}
	if !cfg.Metrics.Prometheus && cfg.Metrics.OTLPEndpoint == "" {
		return fmt.Errorf("metrics are enabled with neither the Prometheus exporter nor an OTLP endpoint")
	}
	if cfg.Metrics.Prometheus {
		if cfg.Metrics.Addr == "" {
			return fmt.Errorf("metrics addr is required when the Prometheus exporter is enabled")
		}
		// Two listeners on one address fail at bind time with an error that
		// names neither of them.
		if cfg.Metrics.Addr == cfg.Server.Addr {
			return fmt.Errorf("metrics addr %q is the address the server already listens on", cfg.Metrics.Addr)
		}
		if !strings.HasPrefix(cfg.Metrics.Path, "/") {
			return fmt.Errorf("metrics path must start with a slash, not %q", cfg.Metrics.Path)
		}
	}
	if cfg.Metrics.OTLPEndpoint != "" && cfg.Metrics.OTLPInterval <= 0 {
		return fmt.Errorf("metrics otlp-interval must be positive, not %s", cfg.Metrics.OTLPInterval)
	}
	return nil
}

// validateBot gates the whole feature on all three bot credentials being set
// together, mirroring validateMetrics: a deployment that wants no chat
// delivery configures nothing, and a partial credential is refused rather than
// silently authenticating as nobody.
func validateBot(cfg BotConfig) error {
	if cfg.TenantID == "" && cfg.ClientID == "" && cfg.ClientSecret == "" {
		return nil
	}
	if cfg.ClientID == "" {
		return fmt.Errorf("bot-client-id is required when the bot is configured")
	}
	if cfg.ClientSecret == "" {
		return fmt.Errorf("bot-client-secret is required when the bot is configured")
	}
	switch cfg.TenantType {
	case "", "single", "multi":
	default:
		return fmt.Errorf("bot-tenant-type must be single or multi, not %q", cfg.TenantType)
	}
	// Only a single-tenant registration authenticates through its own tenant. A
	// multi-tenant bot goes through the shared botframework.com tenant, so
	// demanding a tenant id there would be asking for a value with no meaning.
	if cfg.TenantType != "multi" && cfg.TenantID == "" {
		return fmt.Errorf("bot-tenant-id is required when the bot is configured, unless bot-tenant-type is multi")
	}
	// Zero means http.Client{Timeout: 0}: no timeout at all, so a send that
	// never gets an answer hangs forever instead of failing. A negative value
	// gives a negative duration, which is equally meaningless.
	if cfg.TimeoutSec <= 0 {
		return fmt.Errorf("bot-timeout-sec must be positive when the bot is configured, not %d", cfg.TimeoutSec)
	}
	// The trust anchor for every inbound activity: an empty value would still
	// register the route (botConfigured checks it too) but 502 forever, and a
	// non-https one would send the discovery request, and everything derived
	// from its answer, in the clear.
	if cfg.MetadataURL == "" {
		return fmt.Errorf("bot-metadata-url is required when the bot is configured")
	}
	if parsed, err := url.Parse(cfg.MetadataURL); err != nil || parsed.Scheme != "https" {
		return fmt.Errorf("bot-metadata-url must be an https URL, not %q", cfg.MetadataURL)
	}
	return nil
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
	if err := validateDatabase(cfg.Database); err != nil {
		return err
	}
	if cfg.Webhook.Token == "" {
		return fmt.Errorf("webhook token is required")
	}
	if err := validateMetrics(cfg); err != nil {
		return err
	}
	if err := validateBot(cfg.Bot); err != nil {
		return err
	}
	// Failing closed: an issuer without accepted values would admit everyone the
	// provider will authenticate.
	if cfg.Auth.OIDCDiscoveryURL != "" {
		if cfg.Auth.OIDCClientID == "" {
			return fmt.Errorf("auth oidc-client-id is required when a discovery URL is configured")
		}
		if cfg.Auth.OIDCRedirectURL == "" {
			return fmt.Errorf("auth oidc-redirect-url is required when a discovery URL is configured")
		}
		// A path alone is sent to the provider verbatim and rejected there, far
		// from the configuration that caused it.
		if redirect, err := url.Parse(cfg.Auth.OIDCRedirectURL); err != nil || !redirect.IsAbs() {
			return fmt.Errorf("auth oidc-redirect-url must be an absolute URL, as registered with the provider, not %q", cfg.Auth.OIDCRedirectURL)
		}
		// Roles are named by the claim, so there is no list to require here. A
		// default role is optional on purpose: leaving it empty means a user the
		// claim says nothing about gets nothing.
		switch cfg.Auth.DefaultRole {
		case "", "admin", "editor", "viewer":
		default:
			return fmt.Errorf("auth default-role must be admin, editor, viewer or empty, not %q", cfg.Auth.DefaultRole)
		}
		if cfg.Auth.Claim == "" {
			return fmt.Errorf("auth claim is required when a discovery URL is configured")
		}
	}
	return nil
}
