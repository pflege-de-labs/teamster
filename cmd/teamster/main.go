package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pflege-de-labs/teamster/internal/cli"
)

// version is stamped at build time via -ldflags.
var version = "dev"

func main() {
	// SIGINT and SIGTERM cancel ctx, which every command uses to shut down.
	// A second signal restores the default behaviour and kills the process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.Run(ctx, os.Args[1:], version); err != nil {
		log.Fatalf("teamster: %v", err)
	}
}
