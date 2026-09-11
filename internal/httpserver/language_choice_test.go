package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
)

func chooseLanguage(t *testing.T, handler http.Handler, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/admin/language", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func languageCookieOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == languageCookie {
			return cookie
		}
	}
	return nil
}

// A choice made in the UI outranks what the browser asks for, and lasts.
func TestChoosingALanguage(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleEditor)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := chooseLanguage(t, handler, url.Values{"language": {"de"}, "return": {"/admin/routing"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d, want 303 (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/admin/routing" {
		t.Errorf("Location = %q, want the page it was chosen on", got)
	}

	cookie := languageCookieOf(rec)
	if cookie == nil || cookie.Value != "de" {
		t.Fatalf("cookie = %+v, want the choice remembered", cookie)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie = %+v, want it HttpOnly and SameSite=Lax", cookie)
	}

	// And it decides the page, even against a contrary Accept-Language.
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Accept-Language", "en")
	req.AddCookie(cookie)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, req)
	if !strings.Contains(page.Body.String(), "Vorlagen") {
		t.Error("the chosen language did not outrank Accept-Language")
	}
}

// "Browser default" is a choice too, and clearing the cookie is how it is kept.
func TestChoosingTheBrowserDefault(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleEditor)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := chooseLanguage(t, handler, url.Values{"language": {""}, "return": {"/admin"}}, nil)
	cookie := languageCookieOf(rec)
	if cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("cookie = %+v, want it cleared", cookie)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Accept-Language", "de")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, req)

	if !strings.Contains(page.Body.String(), "Vorlagen") {
		t.Error("without a cookie the browser's preference does not decide")
	}
}

func TestLanguageChoiceIsRefusedWhenItCannotBeHonoured(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleEditor)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	tests := []struct {
		name       string
		form       url.Values
		headers    map[string]string
		wantStatus int
	}{
		{
			// Storing it would mean reading it on every request and matching it
			// to nothing.
			name: "a language this build does not carry", form: url.Values{"language": {"fr"}},
			wantStatus: http.StatusBadRequest,
		},
		{name: "nonsense", form: url.Values{"language": {"!!"}}, wantStatus: http.StatusBadRequest},
		{
			name: "a cross-site post", form: url.Values{"language": {"de"}},
			headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := chooseLanguage(t, handler, tt.form, tt.headers)
			if rec.Code != tt.wantStatus {
				t.Errorf("POST = %d, want %d", rec.Code, tt.wantStatus)
			}
			if languageCookieOf(rec) != nil {
				t.Error("a refused choice was still stored")
			}
		})
	}
}

// The return path comes from a form field, so it has to be a path here and not
// somewhere else.
func TestLanguageReturnCannotLeaveTheSite(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleEditor)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	for _, target := range []string{"https://evil.example/", "//evil.example/", "http://evil.example"} {
		rec := chooseLanguage(t, handler, url.Values{"language": {"de"}, "return": {target}}, nil)
		if got := rec.Header().Get("Location"); got != "/admin" {
			t.Errorf("return %q sent the operator to %q", target, got)
		}
	}
}

// Somebody reading the login page in a language they do not speak has nowhere
// else to change it, so the picker is there before any session exists.
func TestTheLanguagePickerNeedsNoSession(t *testing.T) {
	t.Parallel()

	handler := authServer(t, newFakeStore(), authConfigFor("https://idp.example/.well-known/openid-configuration"))

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `id="language-picker"`) || !strings.Contains(body, `value="de"`) {
		t.Errorf("the login page offers no language picker:\n%s", body)
	}

	// And choosing one works without signing in.
	chosen := chooseLanguage(t, handler, url.Values{"language": {"de"}, "return": {"/admin/login"}}, nil)
	if chosen.Code != http.StatusSeeOther {
		t.Errorf("POST without a session = %d, want 303", chosen.Code)
	}
}

func TestLanguagePickerRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	if rec := probe(t, handler, http.MethodGet, "/admin/language"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /admin/language = %d, want 405", rec.Code)
	}
}
