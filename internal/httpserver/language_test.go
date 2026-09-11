package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
)

func pageIn(t *testing.T, handler http.Handler, path, acceptLanguage string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Body.String()
}

// The point of the milestone: the same page, in the language the browser asked
// for, from one set of components.
func TestPagesRenderInTheRequestedLanguage(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleAdmin)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	tests := []struct {
		name     string
		path     string
		language string
		want     []string
		absent   []string
	}{
		{
			name: "the configuration page in English", path: "/admin", language: "en",
			want:   []string{"Templates", "Destinations", "Routes", `lang="en"`},
			absent: []string{"Vorlagen"},
		},
		{
			name: "and in German", path: "/admin", language: "de",
			want:   []string{"Vorlagen", "Ziele", "Routen", `lang="de"`},
			absent: []string{">Templates<"},
		},
		{
			name: "the routing page in German", path: "/admin/routing", language: "de-AT,de;q=0.9",
			want: []string{"Welche Route nimmt ein Alarm?", `lang="de"`},
		},
		{
			// Nothing this build carries: the configured fallback, not whichever
			// catalog sorts first.
			name: "a language we do not have", path: "/admin", language: "fr-CA,fr;q=0.9",
			want: []string{"Templates", `lang="en"`},
		},
		{
			name: "no preference at all", path: "/admin",
			want: []string{"Templates", `lang="en"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := pageIn(t, handler, tt.path, tt.language)
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("the page does not contain %q", want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(body, absent) {
					t.Errorf("the page still contains %q", absent)
				}
			}
		})
	}
}

// The pages a signed-out or unpermitted visitor sees are translated too — they
// are the ones most likely to be read by someone who does not work here.
func TestLoginAndNoAccessAreTranslated(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	handler := authServer(t, st, authConfigFor("https://idp.example/.well-known/openid-configuration"))

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	req.Header.Set("Accept-Language", "de")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "Anmelden") {
		t.Errorf("the login page is not translated:\n%s", rec.Body.String())
	}

	noRole := sessionAs(seededUIStore())
	body := pageIn(t, newTestServer(t, noRole, &fakeMessenger{}).Handler, "/admin", "de")
	if !strings.Contains(body, "keine Teamster-Rolle") {
		t.Errorf("the no-access page is not translated:\n%s", body)
	}
}

// A value inside a sentence goes where the catalog puts it, which is not
// necessarily where English puts it.
func TestValuesAreSubstitutedIntoTranslatedSentences(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleViewer)
	body := pageIn(t, newTestServer(t, st, &fakeMessenger{}).Handler, "/admin", "de")

	if !strings.Contains(body, "Angemeldet mit der Rolle viewer") {
		t.Errorf("the read-only notice did not substitute the role:\n%s", body)
	}
	if strings.Contains(body, "{0}") {
		t.Error("a placeholder reached the page")
	}
}

// Buttons whose label is built from a verb and a noun: German puts them the
// other way round, which is why they are two catalog entries and not a
// concatenation.
func TestASentenceIsNotBuiltByConcatenation(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleEditor)
	body := pageIn(t, newTestServer(t, st, &fakeMessenger{}).Handler, "/admin", "de")

	if !strings.Contains(body, "Vorlage speichern") {
		t.Errorf("the submit button is not in German word order:\n%s", body)
	}
}
