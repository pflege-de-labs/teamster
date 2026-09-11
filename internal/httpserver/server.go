package httpserver

import (
	"net/http"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/graph"
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

type Server struct {
	cfg        config.Config
	store      store.Store
	graph      messenger
	router     *routing.Router
	directory  *directoryCache
	oidc       *oidcProvider
	httpServer *http.Server
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

func NewServer(cfg config.Config, store store.Store, graphClient messenger) *http.Server {
	registerMIMETypes()

	api := &Server{
		cfg:       cfg,
		store:     store,
		graph:     graphClient,
		router:    routing.New(store),
		directory: newDirectoryCache(directoryTTL),
		oidc:      &oidcProvider{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/alertmanager", api.handleAlertmanager)
	mux.HandleFunc("/webhook/universal", api.handleUniversal)

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/api/templates", api.handleTemplates)
	adminMux.HandleFunc("/api/templates/", api.handleTemplateByID)
	adminMux.HandleFunc("/api/destinations", api.handleDestinations)
	adminMux.HandleFunc("/api/destinations/", api.handleDestinationByID)
	adminMux.HandleFunc("/api/routes", api.handleRoutes)
	adminMux.HandleFunc("/api/routes/", api.handleRouteByID)
	adminMux.HandleFunc("/api/templates/preview", api.handlePreview)
	adminMux.HandleFunc("/api/routing/graph", api.handleRoutingGraph)
	adminMux.HandleFunc("/api/routing/match", api.handleRoutingMatch)
	adminMux.HandleFunc("/api/graph/teams", api.handleGraphTeams)
	adminMux.HandleFunc("/api/graph/teams/", api.handleGraphChannels)
	adminMux.HandleFunc("/admin", api.handleAdminPage)
	adminMux.HandleFunc("/admin/routing", api.handleRoutingPage)
	adminMux.HandleFunc("/admin/templates", api.formPost(api.saveTemplate))
	adminMux.HandleFunc("/admin/templates/delete", api.formPost(api.deleteTemplate))
	adminMux.HandleFunc("/admin/destinations", api.formPost(api.saveDestination))
	adminMux.HandleFunc("/admin/destinations/delete", api.formPost(api.deleteDestination))
	adminMux.HandleFunc("/admin/routes", api.formPost(api.saveRoute))
	adminMux.HandleFunc("/admin/routes/delete", api.formPost(api.deleteRoute))
	adminMux.HandleFunc("/", api.handleAssets)

	authMux := http.NewServeMux()
	authMux.HandleFunc("/admin/login", api.handleLoginPage)
	authMux.HandleFunc("/admin/auth/start", api.handleAuthStart)
	authMux.HandleFunc("/admin/auth/callback", api.handleAuthCallback)
	authMux.HandleFunc("/admin/logout", api.handleLogout)

	// The stylesheet, icons and scripts are public: the login page needs them
	// before anyone has a session, and none of them is sensitive.
	for _, asset := range []string{
		"/favicon.ico", "/site.webmanifest", "/styles.css",
		"/preview.js", "/pickers.js", "/routing.js", "/icons/", "/vendor/",
	} {
		mux.HandleFunc(asset, api.handleAssets)
	}

	mux.Handle("/api/", api.basicAuth(adminMux))
	mux.Handle("/admin/login", authMux)
	mux.Handle("/admin/auth/", authMux)
	mux.Handle("/admin/logout", authMux)
	mux.Handle("/admin", api.requireSession(adminMux))
	mux.Handle("/admin/", api.requireSession(adminMux))
	mux.Handle("/", api.requireSession(adminMux))

	// No BaseContext: it would have to be the context that a signal cancels,
	// and cancelling every in-flight request the moment SIGTERM arrives is the
	// opposite of the draining shutdown in ADR 0003.
	api.httpServer = &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           api.logging(mux),
		ReadHeaderTimeout: readHeaderTimeout(cfg.Server.ReadTimeout),
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	return api.httpServer
}
