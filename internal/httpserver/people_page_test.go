package httpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// peopleServer is a server with the app installed for everyone.
func peopleServer(t *testing.T, st *fakeStore, global bool) http.Handler {
	t.Helper()
	bot := notificationsBotConfig()
	bot.GlobalInstall, bot.AppID = global, "app"
	return mustServer(t, config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     bot,
	}, st, &fakeMessenger{}).Handler
}

func TestPeoplePage(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	st.directory["a"] = models.DirectoryUser{AADObjectID: "a", InstallState: models.InstallInstalled}
	st.directory["b"] = models.DirectoryUser{AADObjectID: "b", DisplayName: "Bob Failing", InstallState: models.InstallFailed, LastError: "403 Forbidden"}
	st.runs = []models.DirectoryRun{{ID: "run-1", Kind: models.RunPeriodic, State: models.RunDone, RunCounts: models.RunCounts{Total: 2, Installed: 1, Failed: 1}}}
	handler := peopleServer(t, st, true)

	rec := asRole(t, handler, http.MethodGet, "/admin/people", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/people = %d", rec.Code)
	}
	for _, want := range []string{"Bob Failing", "403 Forbidden", "2 listed · 1 installed", `action="/admin/people/install"`, `href="/admin/people"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestPeopleInstallButton(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	handler := peopleServer(t, st, true)

	first := postForm(t, handler, "/admin/people/install", url.Values{}, nil)
	if first.Code != http.StatusSeeOther || !strings.Contains(first.Header().Get("Location"), "notice=") {
		t.Fatalf("first install = %d %s, want a notice", first.Code, first.Header().Get("Location"))
	}
	if len(st.runs) != 1 || st.runs[0].Kind != models.RunManual || st.runs[0].RequestedBy != "tester" {
		t.Fatalf("runs = %+v, want one manual run requested by the session", st.runs)
	}

	second := postForm(t, handler, "/admin/people/install", url.Values{}, nil)
	if !strings.Contains(second.Header().Get("Location"), "error=") {
		t.Errorf("second install = %s, want an error while the first is pending", second.Header().Get("Location"))
	}
	if page := asRole(t, handler, http.MethodGet, "/admin/people", "").Body.String(); strings.Contains(page, `action="/admin/people/install"`) {
		t.Error("the page offers the button while a run is pending")
	}
}

func TestPeopleAPI(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	st.directory["a"] = models.DirectoryUser{AADObjectID: "a", InstallState: models.InstallInstalled}
	handler := peopleServer(t, st, true)

	empty := asRole(t, handler, http.MethodGet, "/api/people/runs/latest", "")
	var before struct {
		Run    *models.DirectoryRun          `json:"run"`
		Counts map[models.InstallState]int64 `json:"counts"`
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &before); err != nil || before.Run != nil || before.Counts[models.InstallInstalled] != 1 {
		t.Errorf("latest before any run = %s, %v", empty.Body.String(), err)
	}

	if rec := asRole(t, handler, http.MethodPost, "/api/people/install", ""); rec.Code != http.StatusAccepted {
		t.Errorf("POST install = %d, want 202", rec.Code)
	}
	if rec := asRole(t, handler, http.MethodPost, "/api/people/install", ""); rec.Code != http.StatusConflict {
		t.Errorf("second POST install = %d, want 409", rec.Code)
	}
	if rec := asRole(t, handler, http.MethodGet, "/api/people/runs/latest", ""); !strings.Contains(rec.Body.String(), `"state":"requested"`) {
		t.Errorf("latest after a request = %s", rec.Body.String())
	}
	if rec := asRole(t, handler, http.MethodGet, "/api/people/install", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET install = %d, want 405", rec.Code)
	}
}

func TestPeoplePageIsTheAdmins(t *testing.T) {
	t.Parallel()

	for _, role := range []authz.Role{authz.RoleViewer, authz.RoleEditor} {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			st := sessionAs(newFakeStore(), role)
			handler := peopleServer(t, st, true)
			for _, req := range []struct{ method, path string }{
				{http.MethodGet, "/admin/people"},
				{http.MethodPost, "/api/people/install"},
				{http.MethodGet, "/api/people/runs/latest"},
			} {
				if rec := asRole(t, handler, req.method, req.path, ""); rec.Code != http.StatusForbidden {
					t.Errorf("%s %s as %s = %d, want 403", req.method, req.path, role, rec.Code)
				}
			}
			if len(st.runs) != 0 {
				t.Error("a non-admin requested a run")
			}
			if page := asRole(t, handler, http.MethodGet, "/admin", "").Body.String(); strings.Contains(page, `href="/admin/people"`) {
				t.Error("the nav offers the people page to a non-admin")
			}
		})
	}
}

func TestPeoplePageNeedsGlobalInstall(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	handler := peopleServer(t, st, false)

	if rec := asRole(t, handler, http.MethodPost, "/api/people/install", ""); rec.Code == http.StatusAccepted {
		t.Error("a run was accepted without global install")
	}
	if page := asRole(t, handler, http.MethodGet, "/admin", "").Body.String(); strings.Contains(page, `href="/admin/people"`) {
		t.Error("the nav offers the people page without global install")
	}
}

func TestPeoplePageReportsStoreFailures(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	st.failOn = map[string]bool{"CountDirectoryUsersByState": true, "LatestDirectoryRun": true}
	handler := peopleServer(t, st, true)

	if rec := asRole(t, handler, http.MethodGet, "/admin/people", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Reference") {
		t.Errorf("page with a failing store = %d, want it to say so with a reference", rec.Code)
	}
	if rec := asRole(t, handler, http.MethodGet, "/api/people/runs/latest", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("API with a failing store = %d, want 500", rec.Code)
	}
}
