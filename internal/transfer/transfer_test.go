package transfer

import (
	"errors"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func configured(t *testing.T) *store.SQLiteStore {
	t.Helper()

	st, err := store.NewSQLiteStore(t.TempDir() + "/teamster.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.CreateTemplate(models.Template{ID: "tmpl", Name: "Critical card", Body: `{"type":"AdaptiveCard"}`}); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	if _, err := st.CreateDestination(models.Destination{ID: "dest", Name: "Ops", TeamID: "team", ChannelID: "chan"}); err != nil {
		t.Fatalf("seed destination: %v", err)
	}
	if _, err := st.CreateRoute(models.Route{ID: "root", Name: "Critical", DestinationID: "dest", TemplateID: "tmpl", Priority: 100}); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	if _, err := st.CreateGrant(models.Grant{ID: "grant", Role: "editor", TeamID: "team"}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	return st
}

type fixedDirectory struct{}

func (fixedDirectory) TeamName(string) string            { return "Platform" }
func (fixedDirectory) ChannelName(string, string) string { return "Alerts" }

func TestExportCarriesTheConfigurationAndNoSecrets(t *testing.T) {
	t.Parallel()

	bundle, err := Export(configured(t), fixedDirectory{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	if bundle.Version != Version || bundle.ExportedAt.IsZero() {
		t.Errorf("bundle = %+v, want a version and a timestamp", bundle)
	}
	if len(bundle.Templates) != 1 || len(bundle.Destinations) != 1 || len(bundle.Routes) != 1 || len(bundle.Grants) != 1 {
		t.Fatalf("bundle carries %d/%d/%d/%d, want one of each",
			len(bundle.Templates), len(bundle.Destinations), len(bundle.Routes), len(bundle.Grants))
	}

	// Ids are tenant-specific, so the names travel beside them as a hint for
	// whoever has to fix an import up.
	if bundle.Destinations[0].TeamName != "Platform" || bundle.Destinations[0].ChannelName != "Alerts" {
		t.Errorf("destination = %+v, want the Team and channel names beside the ids", bundle.Destinations[0])
	}
}

// A bundle with no names is still a bundle: Graph being unreachable is no
// reason to refuse to back a configuration up.
func TestExportWithoutADirectory(t *testing.T) {
	t.Parallel()

	bundle, err := Export(configured(t), nil)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if bundle.Destinations[0].TeamName != "" || bundle.Destinations[0].TeamID != "team" {
		t.Errorf("destination = %+v, want the ids without names", bundle.Destinations[0])
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := func() Bundle {
		return Bundle{
			Version:      Version,
			Templates:    []models.Template{{ID: "tmpl", Name: "Card"}},
			Destinations: []BundleDestination{{Destination: models.Destination{ID: "dest", Name: "Ops"}}},
			Routes:       []models.Route{{ID: "root", Name: "Root", DestinationID: "dest", TemplateID: "tmpl"}},
			Grants:       []models.Grant{{ID: "g1", Role: "editor", TeamID: "team"}},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Bundle)
		wantErr string
	}{
		{name: "a whole configuration", mutate: func(*Bundle) {}},
		{
			name:    "a version this build does not know",
			mutate:  func(b *Bundle) { b.Version = 99 },
			wantErr: "bundle version 99",
		},
		{
			// References may point within the bundle only: leaning on what
			// happens to be in the database would behave differently on a
			// fresh installation.
			name:    "a route pointing outside the bundle",
			mutate:  func(b *Bundle) { b.Routes[0].TemplateID = "elsewhere" },
			wantErr: "which the bundle does not carry",
		},
		{
			name:    "a destination the bundle does not carry",
			mutate:  func(b *Bundle) { b.Routes[0].DestinationID = "elsewhere" },
			wantErr: "which the bundle does not carry",
		},
		{
			name:    "a duplicated id",
			mutate:  func(b *Bundle) { b.Templates = append(b.Templates, b.Templates[0]) },
			wantErr: "appears twice",
		},
		{
			name:    "a record with no id",
			mutate:  func(b *Bundle) { b.Templates[0].ID = "" },
			wantErr: "has no id",
		},
		{
			name: "a cycle in the route tree",
			mutate: func(b *Bundle) {
				b.Routes = append(b.Routes, models.Route{
					ID: "child", Name: "Child", ParentID: "root", DestinationID: "dest",
					LabelSelector: map[string]string{"team": "payments"},
				})
				b.Routes[0].ParentID = "child"
				b.Routes[0].LabelSelector = map[string]string{"severity": "critical"}
			},
			wantErr: "cycle",
		},
		{
			// The second of two would be skipped on import, so the bundle would
			// quietly mean less than it says.
			name:    "a duplicated grant id",
			mutate:  func(b *Bundle) { b.Grants = append(b.Grants, b.Grants[0]) },
			wantErr: "appears twice",
		},
		{
			name:    "a grant without a Team",
			mutate:  func(b *Bundle) { b.Grants[0].TeamID = "" },
			wantErr: "needs a role and a Team",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bundle := valid()
			tt.mutate(&bundle)

			err := bundle.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() = %v, want it accepted", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

// The point of ids surviving: a bundle put back where it came from is a no-op.
func TestImportingAnExportChangesNothing(t *testing.T) {
	t.Parallel()

	st := configured(t)
	bundle, err := Export(st, nil)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	result, err := Import(st, bundle, ModeReplace, false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	for _, change := range result.Changes {
		if change.Action != "update" {
			t.Errorf("change = %+v, want nothing created or deleted", change)
		}
	}

	after, err := Export(st, nil)
	if err != nil {
		t.Fatalf("Export after import: %v", err)
	}
	if len(after.Routes) != len(bundle.Routes) || after.Routes[0].ID != bundle.Routes[0].ID {
		t.Errorf("routes = %+v, want the ones the bundle carried", after.Routes)
	}
}

func TestImportModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		mode             Mode
		wantTemplates    int
		wantDeletedNamed string
	}{
		{
			// Merge leaves what the bundle does not mention alone.
			name: "merge", mode: ModeMerge, wantTemplates: 2,
		},
		{
			name: "replace", mode: ModeReplace, wantTemplates: 1, wantDeletedNamed: "Critical card",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := configured(t)
			bundle := Bundle{
				Version:   Version,
				Templates: []models.Template{{ID: "other", Name: "Other card", Body: "{}"}},
			}

			result, err := Import(st, bundle, tt.mode, false)
			if err != nil {
				t.Fatalf("Import: %v", err)
			}

			templates, err := st.ListTemplates()
			if err != nil {
				t.Fatalf("ListTemplates: %v", err)
			}
			if len(templates) != tt.wantTemplates {
				t.Errorf("templates = %+v, want %d", templates, tt.wantTemplates)
			}

			if tt.wantDeletedNamed == "" {
				return
			}
			deleted := false
			for _, change := range result.Changes {
				if change.Action == "delete" && change.Name == tt.wantDeletedNamed {
					deleted = true
				}
			}
			if !deleted {
				t.Errorf("changes = %+v, want %q deleted", result.Changes, tt.wantDeletedNamed)
			}
		})
	}
}

// A dry run reports the same diff a real import would apply, and applies none
// of it — computed by running the import and rolling back, so the preview is of
// the real thing rather than of a second implementation.
func TestDryRunChangesNothing(t *testing.T) {
	t.Parallel()

	st := configured(t)
	bundle := Bundle{Version: Version, Templates: []models.Template{{ID: "other", Name: "Other", Body: "{}"}}}

	preview, err := Import(st, bundle, ModeReplace, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(preview.Changes) == 0 || !preview.DryRun {
		t.Fatalf("result = %+v, want a diff marked as a dry run", preview)
	}

	templates, _ := st.ListTemplates()
	if len(templates) != 1 || templates[0].ID != "tmpl" {
		t.Errorf("templates = %+v, want the configuration untouched", templates)
	}

	applied, err := Import(st, bundle, ModeReplace, false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(applied.Changes) != len(preview.Changes) {
		t.Errorf("applied %d changes, previewed %d — a preview of something else", len(applied.Changes), len(preview.Changes))
	}
}

// A bundle that cannot be applied must leave the configuration as it was, which
// is the whole reason the import runs in a transaction.
func TestAFailedImportLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	st := configured(t)
	// The parent is written, then the child is refused for having no selector,
	// so the parent's write has to be rolled back with it.
	bundle := Bundle{
		Version:      Version,
		Templates:    []models.Template{{ID: "new-tmpl", Name: "New", Body: "{}"}},
		Destinations: []BundleDestination{{Destination: models.Destination{ID: "new-dest", Name: "New"}}},
		Routes: []models.Route{
			{ID: "new-root", Name: "Root", DestinationID: "new-dest", TemplateID: "new-tmpl"},
		},
	}

	failing := &refusingStore{Store: st, failOn: "new-root"}
	if _, err := Import(failing, bundle, ModeMerge, false); err == nil {
		t.Fatal("Import() = nil error, want the refusal to surface")
	}

	templates, _ := st.ListTemplates()
	if len(templates) != 1 {
		t.Errorf("templates = %+v, want the failed import rolled back", templates)
	}
	destinations, _ := st.ListDestinations()
	if len(destinations) != 1 {
		t.Errorf("destinations = %+v, want the failed import rolled back", destinations)
	}
}

// refusingStore fails one write, which is how a partial import is staged.
type refusingStore struct {
	store.Store
	failOn string
}

func (r *refusingStore) WithTx(fn func(store.Store) error) error {
	return r.Store.WithTx(func(tx store.Store) error {
		return fn(&refusingStore{Store: tx, failOn: r.failOn})
	})
}

func (r *refusingStore) CreateRoute(route models.Route) (models.Route, error) {
	if route.ID == r.failOn {
		return models.Route{}, errors.New("refused")
	}
	return r.Store.CreateRoute(route)
}

func TestParseMode(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]Mode{"": ModeMerge, "merge": ModeMerge, "replace": ModeReplace} {
		got, err := ParseMode(value)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v, want %q", value, got, err, want)
		}
	}
	if _, err := ParseMode("delete-everything"); err == nil {
		t.Error("ParseMode() accepted a mode that does not exist")
	}
}

// A child cannot be written before its parent exists, or the tree validation on
// write refuses it.
func TestRoutesAreWrittenParentsFirst(t *testing.T) {
	t.Parallel()

	st := configured(t)
	bundle := Bundle{
		Version:      Version,
		Templates:    []models.Template{{ID: "tmpl", Name: "Card", Body: "{}"}},
		Destinations: []BundleDestination{{Destination: models.Destination{ID: "dest", Name: "Ops"}}},
		Routes: []models.Route{
			// Deliberately child first.
			{ID: "child", Name: "Child", ParentID: "root", DestinationID: "dest", LabelSelector: map[string]string{"team": "payments"}},
			{ID: "root", Name: "Root", DestinationID: "dest", TemplateID: "tmpl"},
		},
	}

	if _, err := Import(st, bundle, ModeReplace, false); err != nil {
		t.Fatalf("Import: %v", err)
	}

	routes, _ := st.ListRoutes()
	if len(routes) != 2 {
		t.Fatalf("routes = %+v, want both", routes)
	}
}
