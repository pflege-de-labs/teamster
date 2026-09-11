package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/transfer"
)

func transferStore(t *testing.T, role authz.Role) *fakeStore {
	t.Helper()

	st := sessionAs(seededUIStore(), role)
	st.grants["g1"] = models.Grant{ID: "g1", Role: "editor", TeamID: "team"}
	return st
}

func TestExportCarriesTheConfiguration(t *testing.T) {
	t.Parallel()

	st := transferStore(t, authz.RoleAdmin)
	msg := &fakeMessenger{
		teams:    []graph.Team{{ID: "team", Name: "Platform"}},
		channels: map[string][]graph.Channel{"team": {{ID: "chan", Name: "Alerts"}}},
	}

	rec := asRole(t, newTestServer(t, st, msg).Handler, http.MethodGet, "/api/config/export", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET export = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "teamster-") {
		t.Errorf("Content-Disposition = %q, want a filename an operator recognises later",
			rec.Header().Get("Content-Disposition"))
	}

	var bundle transfer.Bundle
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if bundle.Version != transfer.Version || len(bundle.Templates) == 0 || len(bundle.Routes) == 0 {
		t.Errorf("bundle = %+v, want the configuration", bundle)
	}
	if bundle.Destinations[0].TeamName != "Platform" || bundle.Destinations[0].ChannelName != "Alerts" {
		t.Errorf("destination = %+v, want the names beside the ids", bundle.Destinations[0])
	}

	// A bundle is meant to be kept beside a deployment's other configuration,
	// so nothing in it may be a credential.
	for _, secret := range []string{"pass", "token", "client_secret", "password"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("the bundle mentions %q; it must carry no credentials", secret)
		}
	}
}

func TestImportAppliesABundle(t *testing.T) {
	t.Parallel()

	st := transferStore(t, authz.RoleAdmin)
	body := `{"version":1,"templates":[{"id":"new","name":"New card","body":"{}"}],
		"destinations":[],"routes":[],"grants":[]}`

	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/config/import", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST import = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if st.templates["new"].Name != "New card" {
		t.Errorf("templates = %+v, want the bundle applied", st.templates)
	}
	// Merge is the default, so what the bundle did not mention survives.
	if _, ok := st.templates["tmpl"]; !ok {
		t.Error("merge deleted a template the bundle did not mention")
	}
}

func TestImportDryRunChangesNothing(t *testing.T) {
	t.Parallel()

	st := transferStore(t, authz.RoleAdmin)
	body := `{"version":1,"templates":[{"id":"new","name":"New card","body":"{}"}]}`

	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost,
		"/api/config/import?dry-run=true", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("dry run = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var result transfer.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.DryRun || len(result.Changes) == 0 {
		t.Errorf("result = %+v, want a diff marked as a dry run", result)
	}
	if _, ok := st.templates["new"]; ok {
		t.Error("the dry run wrote the bundle")
	}
}

func TestImportRefusesABundleItCannotApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		path string
	}{
		{name: "a version this build does not know", body: `{"version":99}`, path: "/api/config/import"},
		{
			name: "a route pointing outside the bundle",
			body: `{"version":1,"routes":[{"id":"r","name":"R","template_id":"missing"}]}`,
			path: "/api/config/import",
		},
		{name: "not JSON at all", body: `{`, path: "/api/config/import"},
		{name: "a mode that does not exist", body: `{"version":1}`, path: "/api/config/import?mode=wipe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := transferStore(t, authz.RoleAdmin)
			rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, tt.path, tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST = %d, want 400 (%s)", rec.Code, rec.Body.String())
			}
			if len(st.templates) != 1 {
				t.Errorf("templates = %+v, want the refused bundle to have changed nothing", st.templates)
			}
		})
	}
}

// An export is the whole configuration in one file and an import rewrites it,
// including who may deliver where.
func TestTransferIsAdminOnly(t *testing.T) {
	t.Parallel()

	for _, role := range []authz.Role{authz.RoleEditor, authz.RoleViewer} {
		st := transferStore(t, role)
		handler := newTestServer(t, st, &fakeMessenger{}).Handler

		if rec := asRole(t, handler, http.MethodGet, "/api/config/export", ""); rec.Code != http.StatusForbidden {
			t.Errorf("export as %s = %d, want 403", role, rec.Code)
		}
		if rec := asRole(t, handler, http.MethodPost, "/api/config/import", `{"version":1}`); rec.Code != http.StatusForbidden {
			t.Errorf("import as %s = %d, want 403", role, rec.Code)
		}
	}
}

func TestTransferRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	st := transferStore(t, authz.RoleAdmin)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	if rec := asRole(t, handler, http.MethodPost, "/api/config/export", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST export = %d, want 405", rec.Code)
	}
	if rec := asRole(t, handler, http.MethodGet, "/api/config/import", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET import = %d, want 405", rec.Code)
	}
}

// Graph being unreachable is no reason to refuse to back a configuration up.
func TestExportWorksWithoutGraph(t *testing.T) {
	t.Parallel()

	st := transferStore(t, authz.RoleAdmin)
	msg := &fakeMessenger{directoryErr: errStore}

	rec := asRole(t, newTestServer(t, st, msg).Handler, http.MethodGet, "/api/config/export", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET export = %d, want 200 with Graph down (%s)", rec.Code, rec.Body.String())
	}

	var bundle transfer.Bundle
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if bundle.Destinations[0].TeamID == "" {
		t.Error("the bundle lost the ids it exists to carry")
	}
}

// Team and channel ids belong to one tenant, so a bundle carried to another
// imports cleanly and then delivers nowhere. The import names what will not
// resolve rather than leaving it to be found by an alert that never arrives.
func TestImportNamesDestinationsThatDoNotResolve(t *testing.T) {
	t.Parallel()

	st := transferStore(t, authz.RoleAdmin)
	msg := &fakeMessenger{
		teams:    []graph.Team{{ID: "team", Name: "Platform"}},
		channels: map[string][]graph.Channel{"team": {{ID: "chan", Name: "Alerts"}}},
	}

	body := `{"version":1,"destinations":[
		{"id":"here","name":"Ops","team_id":"team","channel_id":"chan"},
		{"id":"elsewhere","name":"Payments","team_id":"other-tenant","channel_id":"alerts","team_name":"Payments Team"},
		{"id":"gone-channel","name":"Retired","team_id":"team","channel_id":"deleted"}
	]}`

	rec := asRole(t, newTestServer(t, st, msg).Handler, http.MethodPost, "/api/config/import?dry-run=true", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("import = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var result transfer.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Unresolved) != 2 {
		t.Fatalf("unresolved = %v, want the two that this tenant does not have", result.Unresolved)
	}
	joined := strings.Join(result.Unresolved, " ")
	if !strings.Contains(joined, "Payments") || !strings.Contains(joined, "Retired") {
		t.Errorf("unresolved = %v, want them named recognisably", result.Unresolved)
	}
	if strings.Contains(joined, "Ops") {
		t.Errorf("unresolved = %v, want the destination that does resolve left out", result.Unresolved)
	}
}

// An unreachable directory is not evidence that a channel is missing.
func TestImportSaysNothingAboutResolutionWithoutGraph(t *testing.T) {
	t.Parallel()

	st := transferStore(t, authz.RoleAdmin)
	msg := &fakeMessenger{directoryErr: errStore}
	body := `{"version":1,"destinations":[{"id":"x","name":"Ops","team_id":"nowhere","channel_id":"nothing"}]}`

	rec := asRole(t, newTestServer(t, st, msg).Handler, http.MethodPost, "/api/config/import", body)

	var result transfer.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want nothing claimed while Graph is unreachable", result.Unresolved)
	}
}
