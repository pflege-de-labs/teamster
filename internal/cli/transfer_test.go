package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/transfer"
)

// The commands open the database directly, which is what makes them work on a
// stopped installation — when a backup is most often wanted.
func seededDatabase(t *testing.T) *config.Config {
	t.Helper()

	path := filepath.Join(t.TempDir(), "teamster.db")
	st, err := store.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := st.CreateTemplate(models.Template{ID: "tmpl", Name: "Card", Body: "{}"}); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	return &config.Config{Database: config.DatabaseConfig{Path: path}}
}

func TestExportCommandWritesABundle(t *testing.T) {
	t.Parallel()

	cfg := seededDatabase(t)
	out := filepath.Join(t.TempDir(), "bundle.json")

	cmd := &ExportCmd{Output: out}
	if err := cmd.Run(t.Context(), cfg); err != nil {
		t.Fatalf("export: %v", err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}

	var bundle transfer.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if len(bundle.Templates) != 1 || bundle.Templates[0].ID != "tmpl" {
		t.Errorf("bundle = %+v, want the seeded template", bundle)
	}
}

// The round trip is the point: what one installation writes, another reads.
func TestImportCommandAppliesAnExport(t *testing.T) {
	t.Parallel()

	source := seededDatabase(t)
	bundlePath := filepath.Join(t.TempDir(), "bundle.json")
	if err := (&ExportCmd{Output: bundlePath}).Run(t.Context(), source); err != nil {
		t.Fatalf("export: %v", err)
	}

	target := &config.Config{Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "target.db")}}
	if err := (&ImportCmd{File: bundlePath, Mode: "replace"}).Run(t.Context(), target); err != nil {
		t.Fatalf("import: %v", err)
	}

	st, err := store.NewSQLiteStore(target.Database.Path)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer func() { _ = st.Close() }()

	templates, err := st.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) != 1 || templates[0].ID != "tmpl" {
		t.Errorf("templates = %+v, want the bundle applied with its ids", templates)
	}
}

func TestImportCommandDryRunChangesNothing(t *testing.T) {
	t.Parallel()

	cfg := seededDatabase(t)
	bundle := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(bundle, []byte(`{"version":1,"templates":[{"id":"other","name":"Other","body":"{}"}]}`), 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	if err := (&ImportCmd{File: bundle, Mode: "replace", DryRun: true}).Run(t.Context(), cfg); err != nil {
		t.Fatalf("dry run: %v", err)
	}

	st, _ := store.NewSQLiteStore(cfg.Database.Path)
	defer func() { _ = st.Close() }()
	templates, _ := st.ListTemplates()
	if len(templates) != 1 || templates[0].ID != "tmpl" {
		t.Errorf("templates = %+v, want the dry run to have changed nothing", templates)
	}
}

func TestImportCommandReportsWhatItCannotRead(t *testing.T) {
	t.Parallel()

	cfg := seededDatabase(t)
	missing := filepath.Join(t.TempDir(), "nope.json")

	err := (&ImportCmd{File: missing, Mode: "merge"}).Run(t.Context(), cfg)
	if err == nil || !strings.Contains(err.Error(), "open") {
		t.Errorf("import of a missing file = %v, want it named", err)
	}
}

// The diff is read by a person deciding whether to run it for real.
func TestReportReadsAsLines(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	report(&out, transfer.Result{
		Mode:   transfer.ModeReplace,
		DryRun: true,
		Changes: []transfer.Change{
			{Kind: "template", Action: "create", ID: "t1", Name: "Card"},
			{Kind: "route", Action: "delete", ID: "r1", Name: "Old"},
		},
	})

	got := out.String()
	for _, want := range []string{"create", "template", "Card", "delete", "Old", "would apply 2 changes", "replace"} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not mention %q:\n%s", want, got)
		}
	}

	out.Reset()
	report(&out, transfer.Result{Mode: transfer.ModeMerge})
	if !strings.Contains(out.String(), "nothing to change") {
		t.Errorf("report = %q, want it to say there is nothing to do", out.String())
	}
}
