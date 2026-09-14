package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

// completionScript runs the real command and returns what it wrote, so these
// tests exercise the wiring rather than a generator called directly.
func completionScript(t *testing.T, shell string) string {
	t.Helper()

	var out bytes.Buffer
	err := Run(t.Context(), []string{"completion", shell}, "test",
		kong.Writers(&out, io.Discard),
		kong.Exit(func(int) { t.Fatal("kong exited the process") }))
	if err != nil {
		t.Fatalf("completion %s: %v", shell, err)
	}
	if out.Len() == 0 {
		t.Fatalf("completion %s wrote nothing", shell)
	}
	return out.String()
}

func TestCompletionCoversTheCommandTree(t *testing.T) {
	t.Parallel()

	// Every command has to appear, whatever the shell. These are the ones a
	// person types; if one is missing the script is quietly incomplete.
	commands := []string{"serve", "export", "import", "migrate", "up", "down", "status", "completion"}

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()

			script := completionScript(t, shell)
			for _, command := range commands {
				if !strings.Contains(script, command) {
					t.Errorf("the %s script never mentions %q", shell, command)
				}
			}
			// A flag from the root, which every subcommand inherits, and one
			// declared on a subcommand.
			for _, flag := range []string{"database-driver", "dry-run"} {
				if !strings.Contains(script, flag) {
					t.Errorf("the %s script never mentions --%s", shell, flag)
				}
			}
			// Enum values turn a documented set into a discoverable one, and
			// king emits them for zsh and fish. Its bash output completes an
			// enum flag with the value of the matching environment variable
			// instead, which is a gap rather than a wrong answer -- bash still
			// completes every command and flag. Reported upstream; asserted
			// here only where it is true, so this test cannot claim otherwise.
			if shell == "bash" {
				return
			}
			if !strings.Contains(script, "sqlite") || !strings.Contains(script, "postgres") {
				t.Errorf("the %s script does not offer the database drivers", shell)
			}
		})
	}
}

// The nesting fix, asserted on the text so a failure says which condition was
// wrong rather than only that fish offered the wrong thing.
func TestFishNestsSubcommandsUnderTheirParent(t *testing.T) {
	t.Parallel()

	script := completionScript(t, "fish")

	for _, command := range []string{"up", "down", "status"} {
		want := "-n '__fish_seen_subcommand_from migrate; and not __fish_seen_subcommand_from up down status' -a " + command
		if !strings.Contains(script, want) {
			t.Errorf("%q is not offered under migrate; want a line with %q", command, want)
		}
		if strings.Contains(script, "-n '__fish_use_subcommand' -a "+command+" ") {
			t.Errorf("%q is offered at the top level, where it means nothing", command)
		}
	}
	// The top-level commands must keep the condition they had.
	if !strings.Contains(script, "-n '__fish_use_subcommand' -a migrate ") {
		t.Error("migrate is no longer offered at the top level")
	}
}

func TestCompletionRejectsAnUnknownShell(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := Run(t.Context(), []string{"completion", "tcsh"}, "test",
		kong.Writers(&out, io.Discard),
		kong.Exit(func(int) { t.Fatal("kong exited the process") }))
	if err == nil {
		t.Fatal("completion tcsh = nil error, want a refusal")
	}
	if !strings.Contains(err.Error(), "bash") {
		t.Errorf("completion tcsh = %v, want an error naming the shells that work", err)
	}
}

// The scripts are shell code, so the shells get to say whether they parse.
// Skipped rather than failed when a shell is absent: this must not turn CI red
// on an image that happens not to ship fish.
func TestGeneratedScriptsParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		shell string
		// check is the argv that makes the shell parse a file without running it.
		check func(path string) []string
	}{
		{shell: "bash", check: func(path string) []string { return []string{"-n", path} }},
		{shell: "zsh", check: func(path string) []string { return []string{"-n", path} }},
		{shell: "fish", check: func(path string) []string { return []string{"--no-execute", path} }},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			t.Parallel()

			binary, err := exec.LookPath(tt.shell)
			if err != nil {
				t.Skipf("%s is not installed", tt.shell)
			}

			script := completionScript(t, tt.shell)
			path := filepath.Join(t.TempDir(), "completion."+tt.shell)
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatalf("write script: %v", err)
			}

			cmd := exec.CommandContext(t.Context(), binary, tt.check(path)...)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s rejected the script it is meant to read: %v\n%s", tt.shell, err, output)
			}
		})
	}
}

// What fish actually offers, asked of fish. This is the test that would have
// caught the nesting bug that shipped in the generator.
func TestFishOffersTheRightCommands(t *testing.T) {
	t.Parallel()

	binary, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish is not installed")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "teamster.fish")
	if err := os.WriteFile(path, []byte(completionScript(t, "fish")), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tests := []struct {
		name    string
		line    string
		want    []string
		refused []string
	}{
		{
			name:    "the top level offers the top-level commands",
			line:    "teamster ",
			want:    []string{"serve", "migrate", "export", "import", "completion"},
			refused: []string{"up", "down", "status"},
		},
		{
			name: "migrate offers its own",
			line: "teamster migrate ",
			want: []string{"up", "down", "status"},
		},
		{
			name: "an enum flag offers its values",
			line: "teamster --database-driver ",
			want: []string{"sqlite", "postgres"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := exec.CommandContext(t.Context(), binary, "-c",
				"source "+path+"; complete -C '"+tt.line+"'")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fish: %v\n%s", err, output)
			}

			offered := map[string]bool{}
			for _, line := range strings.Split(string(output), "\n") {
				if word, _, _ := strings.Cut(line, "\t"); word != "" {
					offered[word] = true
				}
			}
			for _, want := range tt.want {
				if !offered[want] {
					t.Errorf("fish did not offer %q for %q; got %v", want, tt.line, keys(offered))
				}
			}
			for _, refused := range tt.refused {
				if offered[refused] {
					t.Errorf("fish offered %q for %q, where it means nothing", refused, tt.line)
				}
			}
		})
	}
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}

// What bash actually offers, asked of bash. Driving the function the script
// registers is the only way to know the case statement it generates resolves
// the command path the way it reads as though it does.
func TestBashOffersTheRightCommands(t *testing.T) {
	t.Parallel()

	binary, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "teamster.bash")
	if err := os.WriteFile(path, []byte(completionScript(t, "bash")), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tests := []struct {
		name    string
		words   string
		cword   string
		cur     string
		prev    string
		want    []string
		refused []string
	}{
		{
			name:    "the top level offers the top-level commands",
			words:   `(teamster "")`,
			cword:   "1",
			want:    []string{"serve", "migrate", "export", "import", "completion"},
			refused: []string{"up", "down", "status"},
		},
		{
			name:  "migrate offers its own",
			words: `(teamster migrate "")`,
			cword: "2",
			prev:  "migrate",
			want:  []string{"up", "down", "status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			script := "source " + path + "\n" +
				"fn=$(complete -p teamster | sed -e 's/.*-F \\([^ ]*\\).*/\\1/')\n" +
				"COMP_WORDS=" + tt.words + "; COMP_CWORD=" + tt.cword + "; COMPREPLY=()\n" +
				"$fn teamster \"" + tt.cur + "\" \"" + tt.prev + "\"\n" +
				"printf '%s\\n' \"${COMPREPLY[@]}\""

			cmd := exec.CommandContext(t.Context(), binary, "-c", strings.ReplaceAll(script, "\\n", "\n"))
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("bash: %v\n%s", err, output)
			}

			offered := map[string]bool{}
			for _, line := range strings.Fields(string(output)) {
				offered[line] = true
			}
			for _, want := range tt.want {
				if !offered[want] {
					t.Errorf("bash did not offer %q; got %v", want, keys(offered))
				}
			}
			for _, refused := range tt.refused {
				if offered[refused] {
					t.Errorf("bash offered %q, where it means nothing", refused)
				}
			}
		})
	}
}
