package main

import (
	"log"
	"os"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"

	"github.com/pflege-de/teamster/internal/config"
	"github.com/pflege-de/teamster/internal/graph"
	"github.com/pflege-de/teamster/internal/httpserver"
	"github.com/pflege-de/teamster/internal/store"
)

// version is stamped at build time via -ldflags.
var version = "dev"

type cli struct {
	ConfigFile kong.ConfigFlag `name:"config" help:"Load configuration from FILE, overriding the XDG locations." short:"c" env:"TEAMSTER_CONFIG" placeholder:"FILE"`

	Version kong.VersionFlag `help:"Print the version and exit."`

	Config config.Config `embed:""`
}

func main() {
	cliCfg := cli{}

	parser, err := kong.New(&cliCfg,
		kong.Name("teamster"),
		kong.Description("Webhook bridge that routes alerts to Microsoft Teams."),
		kong.Configuration(kongyaml.Loader, config.SearchPaths()...),
		kong.DefaultEnvars("TEAMSTER"),
		kong.Vars{"version": version},
	)
	if err != nil {
		log.Fatalf("config parser: %v", err)
	}

	_, err = parser.Parse(os.Args[1:])
	if err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	if err := cliCfg.Config.Validate(); err != nil {
		log.Fatalf("config validation: %v", err)
	}

	sqlStore, err := store.NewSQLiteStore(cliCfg.Config.Database.Path)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlStore.Close() }()

	graphClient, err := graph.NewClient(cliCfg.Config.Graph)
	if err != nil {
		log.Fatalf("graph client: %v", err)
	}

	srv := httpserver.NewServer(cliCfg.Config, sqlStore, graphClient)
	log.Printf("listening on %s", cliCfg.Config.Server.Addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
