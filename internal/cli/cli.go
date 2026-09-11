// Package cli defines the command tree and its kong wiring.
package cli

import (
	"context"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// CLI is the command tree. Configuration is embedded at the root so every
// command sees the same flags, config file and environment variables.
type CLI struct {
	ConfigFile kong.ConfigFlag  `name:"config" short:"c" env:"TEAMSTER_CONFIG" placeholder:"FILE" help:"Load configuration from FILE, overriding the XDG locations."`
	Version    kong.VersionFlag `help:"Print the version and exit."`

	Serve  ServeCmd  `cmd:"" default:"1" help:"Run the webhook bridge HTTP server."`
	Export ExportCmd `cmd:"" help:"Write the configuration to a bundle."`
	Import ImportCmd `cmd:"" help:"Apply a configuration bundle."`

	Config config.Config `embed:""`
}

// New builds the parser for cli. Callers pass options to override the defaults,
// which is how tests keep kong from writing to stdout or exiting the process.
func New(cli *CLI, version string, options ...kong.Option) (*kong.Kong, error) {
	defaults := []kong.Option{
		kong.Name("teamster"),
		kong.Description("Webhook bridge that routes alerts to Microsoft Teams."),
		kong.Configuration(kongyaml.Loader, config.SearchPaths()...),
		kong.DefaultEnvars("TEAMSTER"),
		kong.Vars{"version": version},
		kong.UsageOnError(),
	}
	return kong.New(cli, append(defaults, options...)...)
}

// Run parses args and executes the selected command. ctx carries the process
// lifetime, so a command can abort as soon as the caller cancels it.
func Run(ctx context.Context, args []string, version string, options ...kong.Option) error {
	cli := &CLI{}

	parser, err := New(cli, version, options...)
	if err != nil {
		return err
	}

	kctx, err := parser.Parse(args)
	if err != nil {
		return err
	}

	kctx.BindTo(ctx, (*context.Context)(nil))
	kctx.Bind(&cli.Config)

	return kctx.Run()
}
