package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/transfer"
)

// ExportCmd writes the configuration as a bundle. It opens the database
// directly rather than talking to a running server, so it works on a stopped
// installation — which is when a backup is most often wanted.
type ExportCmd struct {
	Output string `short:"o" placeholder:"FILE" help:"Write to FILE instead of stdout."`
}

func (c *ExportCmd) Run(ctx context.Context, cfg *config.Config) error {
	sqlStore, err := store.NewSQLiteStore(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = sqlStore.Close() }()

	// No directory: resolving Team names needs Graph credentials, and an export
	// from the command line should not depend on them. The ids are what the
	// import reads; the names are a hint the server's export can add.
	bundle, err := transfer.Export(sqlStore, nil)
	if err != nil {
		return err
	}

	out := io.Writer(os.Stdout)
	if c.Output != "" {
		file, err := os.Create(c.Output)
		if err != nil {
			return fmt.Errorf("create %s: %w", c.Output, err)
		}
		defer func() { _ = file.Close() }()
		out = file
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bundle); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}
	return nil
}

// ImportCmd applies a bundle to the configured database.
type ImportCmd struct {
	File   string `arg:"" placeholder:"FILE" help:"Bundle to import; - reads stdin."`
	Mode   string `enum:"merge,replace" default:"merge" help:"merge upserts what the bundle carries; replace also deletes what it does not mention."`
	DryRun bool   `name:"dry-run" help:"Report what would change without changing it."`
}

func (c *ImportCmd) Run(ctx context.Context, cfg *config.Config) error {
	mode, err := transfer.ParseMode(c.Mode)
	if err != nil {
		return err
	}

	source := io.Reader(os.Stdin)
	if c.File != "-" {
		file, err := os.Open(c.File)
		if err != nil {
			return fmt.Errorf("open %s: %w", c.File, err)
		}
		defer func() { _ = file.Close() }()
		source = file
	}

	var bundle transfer.Bundle
	if err := json.NewDecoder(source).Decode(&bundle); err != nil {
		return fmt.Errorf("read bundle: %w", err)
	}

	sqlStore, err := store.NewSQLiteStore(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = sqlStore.Close() }()

	result, err := transfer.Import(sqlStore, bundle, mode, c.DryRun)
	if err != nil {
		return err
	}

	report(os.Stdout, result)
	return nil
}

// report prints the diff as lines rather than JSON: this is read by a person
// deciding whether to run it for real.
func report(out io.Writer, result transfer.Result) {
	if len(result.Changes) == 0 {
		_, _ = fmt.Fprintln(out, "nothing to change")
		return
	}

	for _, change := range result.Changes {
		_, _ = fmt.Fprintf(out, "%-7s %-12s %s (%s)\n", change.Action, change.Kind, change.Name, change.ID)
	}

	verb := "applied"
	if result.DryRun {
		verb = "would apply"
	}
	_, _ = fmt.Fprintf(out, "%s %d changes in %s mode\n", verb, len(result.Changes), result.Mode)
}
