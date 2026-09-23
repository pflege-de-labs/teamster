package httpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/cryptutil"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/routing"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// messenger is the slice of the Graph client the handlers depend on.
type messenger interface {
	PostMessage(teamID, channelID string, msg graph.Message) (string, error)
	UpdateMessage(teamID, channelID, messageID string, msg graph.Message) error
	ListTeams() ([]graph.Team, error)
	ListChannels(teamID string) ([]graph.Channel, error)
}

// brokerGraph is the slice of graph.BrokerClient the delegated Teams/Channels
// handlers depend on -- Graph calls made per admin, with a bearer token this
// service obtained through Keycloak's broker endpoint, rather than with the
// app-only credential messenger uses. See ADR 0037 and broker.go.
type brokerGraph interface {
	EntraToken(ctx context.Context, issuer, alias, keycloakAccessToken string) (string, error)
	MyTeams(ctx context.Context, entraToken string) ([]graph.Team, error)
	MyChannels(ctx context.Context, entraToken, teamID string) ([]graph.Channel, error)
}

// botSender is the slice of the Bot Connector client the inbound handlers and
// chat delivery depend on: replying in the same chat a code or an install came
// from, and sending or editing the message an alert produces. Taking an
// interface rather than *bot.Client keeps this package depending on the shape
// of a reply, not on how it is authenticated or sent.
type botSender interface {
	SendMessage(ctx context.Context, ref bot.ConversationReference, msg bot.Message) (string, error)
	UpdateMessage(ctx context.Context, ref bot.ConversationReference, activityID string, msg bot.Message) error
}

// telemetry is what this server records against. It is an interface so the
// package depends on the shape rather than the pipeline, and so a test can hand
// it one that remembers what it was told.
//
// ClientTransport is the same method internal/graph's own instrumentation
// interface declares, needed here so the broker client -- Graph calls made on
// an admin's own behalf, see broker.go -- is measured the same way as one
// made with the service's app-only credential.
type telemetry interface {
	ServerMiddleware(operation string) func(http.Handler) http.Handler
	RouteTag(next http.Handler) http.Handler
	DeliveryRecorded(ctx context.Context, route, outcome string)
	WebhookReceived(ctx context.Context, source, status string)
	RenderFailed(ctx context.Context, templateID, stage string)
	ClientTransport(base http.RoundTripper) http.RoundTripper
}

// sampler is what processAlert tells about every alert it sees, so the admin
// UI can complete label keys and values (ADR 0041). It must not block.
type sampler interface {
	Observe(labels, annotations map[string]string)
}

type Server struct {
	cfg        config.Config
	store      store.Store
	graph      messenger
	bot        botSender
	router     *routing.Router
	draining   atomic.Bool
	authz      *authz.Authorizer
	metrics    telemetry
	text       *i18n.Bundle
	directory  *directoryCache
	oidc       *oidcProvider
	botAuth    *botAuthenticator
	httpServer *http.Server

	// broker and sealer are nil unless auth.broker.enabled: a deployment that
	// wants no delegated Teams/Channels constructs neither, and every code
	// path that would use them checks the config flag first. See broker.go.
	broker brokerGraph
	sealer *cryptutil.Sealer

	// samples is nil when nothing samples, as in most tests.
	samples sampler

	// now is the clock delivery stamps claims from. It is a field so a test
	// can move time forward past a claim's staleness cutoff without waiting.
	now func() time.Time
}

// headerGrace bounds how long a client may dawdle over request headers, which
// is the cheapest slow-client attack to mount.
const headerGrace = 10 * time.Second

func readHeaderTimeout(readTimeout time.Duration) time.Duration {
	if readTimeout > 0 && readTimeout < headerGrace {
		return readTimeout
	}
	return headerGrace
}

// NewServer returns an error rather than starting without an authorizer: a
// policy file that does not parse would otherwise leave every check to fall
// through to whatever the zero value decides.
func NewServer(cfg config.Config, store store.Store, graphClient messenger, botClient botSender, tel telemetry, samples sampler) (*http.Server, error) {
	registerMIMETypes()

	authorizer, err := authz.New()
	if err != nil {
		return nil, err
	}

	text, err := i18n.New(cfg.UI.LocaleDir, i18n.Parse(cfg.UI.Language))
	if err != nil {
		return nil, err
	}

	// Both are nil unless the feature is on: config.Validate already refuses a
	// deployment that enables it without a usable key, but this constructor
	// has its own tests that build a Config by hand, so it checks again rather
	// than trusting a Validate call it cannot see happened.
	var sealer *cryptutil.Sealer
	var brokerClient brokerGraph
	if cfg.Auth.Broker.Enabled {
		key, err := base64.StdEncoding.DecodeString(cfg.Auth.Broker.TokenEncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("auth broker token encryption key: %w", err)
		}
		sealer, err = cryptutil.NewSealer(key)
		if err != nil {
			return nil, fmt.Errorf("auth broker sealer: %w", err)
		}
		brokerClient = graph.NewBrokerClient(cfg.Graph.BaseURL, tel, time.Duration(cfg.Graph.TimeoutSec)*time.Second)
	}

	api := &Server{
		cfg:       cfg,
		store:     store,
		graph:     graphClient,
		bot:       botClient,
		router:    routing.New(store),
		authz:     authorizer,
		metrics:   tel,
		text:      text,
		now:       func() time.Time { return time.Now().UTC() },
		directory: newDirectoryCache(directoryTTL),
		oidc:      &oidcProvider{},
		broker:    brokerClient,
		sealer:    sealer,
		samples:   samples,
	}

	mux := http.NewServeMux()
	// Probes answer before any authentication: a kubelet carries no credentials,
	// and a probe that needs them reports the wrong thing when they are wrong.
	mux.HandleFunc("/healthz", api.handleLive)
	mux.HandleFunc("/readyz", api.handleReady)
	mux.HandleFunc("/webhook/alertmanager", api.handleAlertmanager)
	mux.HandleFunc("/webhook/universal", api.handleUniversal)
	// The one wildcard pattern in this server. Everything else registers a
	// prefix and trims it, but this path has two meaningful segments and a
	// secret, and naming them keeps r.Pattern -- and so the route label on
	// every metric -- one string rather than one per endpoint.
	mux.HandleFunc("/teamsv2/{team}/{channel}/{token}", api.handleTeamsV2)
	mux.HandleFunc("/teamsv2/", api.handleTeamsV2Unknown)

	// Registered only when the bot is fully configured, so a deployment that
	// wants no chat delivery exposes no unauthenticated path at all. It sits on
	// the plain mux beside the webhooks, not behind requireSession: Microsoft
	// authenticates this endpoint with its own signed token, and the catch-all
	// "/" route below would otherwise answer with a redirect to the login page
	// instead of a 405 or a 200.
	//
	// Registered without a method in the pattern, like the two webhooks above
	// it and for the same reason: the plain mux's own catch-all "/" (below)
	// matches every method, so "POST /bot/messages" would be the more specific
	// pattern only for a POST -- a GET would match nothing but "/" and would
	// be redirected to the login page after all. A literal, method-less
	// "/bot/messages" is more specific than "/" for every method at that path,
	// which is what lets handleBotMessages answer a GET with 405 itself.
	if botConfigured(cfg.Bot) {
		api.botAuth = newBotAuthenticator(cfg.Bot)
		mux.HandleFunc("/bot/messages", api.handleBotMessages)
	}

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/api/templates", api.handleTemplates)
	adminMux.HandleFunc("/api/templates/", api.handleTemplateByID)
	adminMux.HandleFunc("/api/destinations", api.handleDestinations)
	adminMux.HandleFunc("/api/destinations/", api.handleDestinationByID)
	adminMux.HandleFunc("/api/routes", api.handleRoutes)
	adminMux.HandleFunc("/api/routes/", api.handleRouteByID)
	adminMux.HandleFunc("/api/webhooks", api.handleWebhookEndpoints)
	adminMux.HandleFunc("/api/webhooks/", api.handleWebhookEndpointByID)
	adminMux.HandleFunc("/api/templates/preview", api.handlePreview)
	adminMux.HandleFunc("/api/routing/graph", api.handleRoutingGraph)
	adminMux.HandleFunc("/api/routing/templates", api.handleTemplateGraph)
	adminMux.HandleFunc("/api/routing/match", api.handleRoutingMatch)
	adminMux.HandleFunc("/api/samples", api.handleSamples)
	adminMux.HandleFunc("/api/config/export", api.handleExport)
	adminMux.HandleFunc("/api/config/import", api.handleImport)
	adminMux.HandleFunc("/api/recipients/link", api.handleLinkRecipient)
	// Unconditional, unlike the self-service page below: turning the bot off
	// does not delete the recipients a deployment already linked, and an admin
	// still has to be able to see and unlink them.
	adminMux.HandleFunc("/api/recipients", api.handleRecipients)
	adminMux.HandleFunc("/api/recipients/", api.handleRecipientByID)
	// Registered only alongside /bot/messages: a code minted here can never be
	// redeemed on a deployment that never registered the endpoint it would be
	// typed into, so offering the page (and the nav link to it, in
	// layout.templ) would tell someone to talk to a bot that does not exist.
	if botConfigured(cfg.Bot) {
		adminMux.HandleFunc("/admin/notifications", api.handleNotificationsPage)
		adminMux.HandleFunc("/admin/notifications/link", api.handleMintLink)
		adminMux.HandleFunc("/admin/notifications/cancel", api.formPostTo("/admin/notifications", api.cancelLink))
		adminMux.HandleFunc("/admin/notifications/unlink", api.formPostTo("/admin/notifications", api.unlinkNotifications))
	}
	adminMux.HandleFunc("/admin/recipients", api.handleRecipientsPage)
	adminMux.HandleFunc("/admin/recipients/delete", api.formPostTo("/admin/recipients", api.deleteRecipientForm))
	adminMux.HandleFunc("/api/grants", api.handleGrants)
	adminMux.HandleFunc("/api/grants/role", api.handleRoleGrants)
	adminMux.HandleFunc("/api/grants/", api.handleGrantByID)
	adminMux.HandleFunc("/api/graph/teams", api.handleGraphTeams)
	adminMux.HandleFunc("/api/graph/teams/", api.handleGraphChannels)
	adminMux.HandleFunc("/api/graph/my-teams", api.handleMyTeams)
	adminMux.HandleFunc("/api/graph/my-teams/", api.handleMyChannels)
	adminMux.HandleFunc("/admin", api.handleAdminPage)
	adminMux.HandleFunc("/admin/routing", api.handleRoutingPage)
	adminMux.HandleFunc("/admin/permissions", api.handlePermissionsPage)
	adminMux.HandleFunc("/admin/templates", api.formPost(api.saveTemplate))
	adminMux.HandleFunc("/admin/templates/delete", api.formPost(api.deleteTemplate))
	adminMux.HandleFunc("/admin/destinations", api.formPost(api.saveDestination))
	adminMux.HandleFunc("/admin/destinations/delete", api.formPost(api.deleteDestination))
	adminMux.HandleFunc("/admin/destinations/default", api.formPost(api.setDefaultDestination))
	adminMux.HandleFunc("/admin/routes", api.formPost(api.saveRoute))
	adminMux.HandleFunc("/admin/routes/delete", api.formPost(api.deleteRoute))
	adminMux.HandleFunc("/admin/webhooks", api.handleWebhookForm)
	adminMux.HandleFunc("/admin/webhooks/rotate", api.handleWebhookRotate)
	adminMux.HandleFunc("/admin/webhooks/delete", api.formPost(api.deleteWebhookEndpoint))
	adminMux.HandleFunc("/admin/grants", api.formPost(api.saveGrant))
	adminMux.HandleFunc("/admin/grants/delete", api.formPost(api.deleteGrant))
	adminMux.HandleFunc("/", api.handleAssets)

	authMux := http.NewServeMux()
	authMux.HandleFunc("/admin/login", api.handleLoginPage)
	authMux.HandleFunc("/admin/auth/start", api.handleAuthStart)
	authMux.HandleFunc("/admin/auth/callback", api.handleAuthCallback)
	authMux.HandleFunc("/admin/logout", api.handleLogout)
	// Choosing a language needs no session: the login page is the first thing
	// somebody reads in the wrong one.
	authMux.HandleFunc("/admin/language", api.handleLanguage)

	// The stylesheet, icons and scripts are public: the login page needs them
	// before anyone has a session, and none of them is sensitive.
	for _, asset := range []string{
		"/favicon.ico", "/site.webmanifest", "/styles.css",
		"/preview.js", "/pickers.js", "/routing.js", "/language.js", "/permissions.js", "/editor.js", "/icons/", "/vendor/",
	} {
		mux.HandleFunc(asset, api.handleAssets)
	}

	mux.Handle("/api/", api.apiAuth(api.authorize(adminMux)))
	mux.Handle("/admin/login", authMux)
	mux.Handle("/admin/auth/", authMux)
	mux.Handle("/admin/logout", authMux)
	mux.Handle("/admin/language", authMux)
	mux.Handle("/admin", api.requireSession(api.authorize(adminMux)))
	mux.Handle("/admin/", api.requireSession(api.authorize(adminMux)))
	mux.Handle("/", api.requireSession(api.authorize(adminMux)))

	// No BaseContext: it would have to be the context that a signal cancels,
	// and cancelling every in-flight request the moment SIGTERM arrives is the
	// opposite of the draining shutdown in ADR 0003.
	api.httpServer = &http.Server{
		Addr: cfg.Server.Addr,
		// RouteTag sits beside the mux on purpose: see its comment. The
		// instrumentation on the outside cannot see a pattern the mux wrote
		// into a request that localized had already copied.
		Handler:           tel.ServerMiddleware("teamster")(api.logging(api.localized(tel.RouteTag(mux)))),
		ReadHeaderTimeout: readHeaderTimeout(cfg.Server.ReadTimeout),
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	// Shutdown flips readiness before it starts draining, so traffic stops
	// arriving while the in-flight requests finish.
	api.httpServer.RegisterOnShutdown(func() { api.draining.Store(true) })

	return api.httpServer, nil
}
