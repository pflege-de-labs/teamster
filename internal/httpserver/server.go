package httpserver

import (
	"net/http"

	"github.com/pflege-de/teamster/internal/config"
	"github.com/pflege-de/teamster/internal/graph"
	"github.com/pflege-de/teamster/internal/routing"
	"github.com/pflege-de/teamster/internal/store"
)

type Server struct {
	cfg        config.Config
	store      store.Store
	graph      *graph.Client
	router     *routing.Router
	httpServer *http.Server
}

func NewServer(cfg config.Config, store store.Store, graphClient *graph.Client) *http.Server {
	api := &Server{
		cfg:    cfg,
		store:  store,
		graph:  graphClient,
		router: routing.New(store),
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
	adminMux.HandleFunc("/admin", api.handleAdmin)
	adminMux.HandleFunc("/", api.handleAdmin)

	mux.Handle("/api/", api.basicAuth(adminMux))
	mux.Handle("/admin", api.basicAuth(adminMux))
	mux.Handle("/", api.basicAuth(adminMux))

	api.httpServer = &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: api.logging(mux),
	}

	return api.httpServer
}
