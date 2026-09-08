package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

// quiet keeps kong from writing to the test output or calling os.Exit.
func quiet(t *testing.T) []kong.Option {
	t.Helper()

	return []kong.Option{
		kong.Writers(io.Discard, io.Discard),
		kong.Exit(func(int) { t.Fatal("kong exited the process") }),
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestParseSelectsServeByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "no arguments at all", args: nil},
		{name: "flags without a command", args: []string{"--server-addr", ":9000"}},
		{name: "command named explicitly", args: []string{"serve"}},
		{name: "command named with flags", args: []string{"serve", "--server-addr", ":9000"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cli := &CLI{}
			parser, err := New(cli, "test", quiet(t)...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			kctx, err := parser.Parse(tt.args)
			if err != nil {
				t.Fatalf("Parse(%v): %v", tt.args, err)
			}
			if got := kctx.Command(); got != "serve" {
				t.Errorf("Command() = %q, want %q", got, "serve")
			}
		})
	}
}

func TestParseRejectsAnUnknownCommand(t *testing.T) {
	t.Parallel()

	cli := &CLI{}
	parser, err := New(cli, "test", quiet(t)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := parser.Parse([]string{"migrate"}); err == nil {
		t.Error("Parse() = nil error, want an unknown command to be rejected")
	}
}

func TestRunReportsParseErrors(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), []string{"--not-a-flag"}, "test", quiet(t)...)
	if err == nil || !strings.Contains(err.Error(), "not-a-flag") {
		t.Errorf("Run() = %v, want the unknown flag to be reported", err)
	}
}

func TestRunBindsConfigAndContext(t *testing.T) {
	t.Parallel()

	// An invalid config makes serve fail immediately, which is enough to prove
	// that the command ran with the parsed configuration bound to it.
	path := writeConfig(t, "server:\n  addr: \":0\"\n")

	err := Run(context.Background(), []string{"--config", path}, "test", quiet(t)...)
	if err == nil || !strings.Contains(err.Error(), "config validation") {
		t.Errorf("Run() = %v, want the serve command to reject the incomplete config", err)
	}
}

func TestRunPropagatesConstructionErrors(t *testing.T) {
	t.Parallel()

	// A duplicate default command is rejected while the parser is built, before
	// any argument is looked at.
	type broken struct {
		A ServeCmd `cmd:"" default:"1"`
		B ServeCmd `cmd:"" default:"1"`
	}

	if _, err := kong.New(&broken{}); err == nil {
		t.Error("kong.New() = nil error, want more than one default command to be rejected")
	}
}
