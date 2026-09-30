package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestChoosingATimeZone(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	tests := []struct {
		name       string
		method     string
		form       url.Values
		site       string
		wantStatus int
		// wantCookie is the stored zone; "-" means the cookie is cleared.
		wantCookie string
	}{
		{name: "the browser's zone", method: http.MethodPost, form: url.Values{"timezone": {"browser"}, "zone": {"Europe/Berlin"}, "return": {"/admin/people"}}, wantStatus: http.StatusSeeOther, wantCookie: "Europe/Berlin"},
		{name: "back to UTC", method: http.MethodPost, form: url.Values{"timezone": {"utc"}}, wantStatus: http.StatusSeeOther, wantCookie: "-"},
		{name: "an unknown zone", method: http.MethodPost, form: url.Values{"timezone": {"browser"}, "zone": {"Mars/Olympus"}}, wantStatus: http.StatusBadRequest},
		{name: "the server's zone", method: http.MethodPost, form: url.Values{"timezone": {"browser"}, "zone": {"Local"}}, wantStatus: http.StatusBadRequest},
		{name: "no zone sent", method: http.MethodPost, form: url.Values{"timezone": {"browser"}}, wantStatus: http.StatusBadRequest},
		{name: "cross-site", method: http.MethodPost, form: url.Values{"timezone": {"utc"}}, site: "cross-site", wantStatus: http.StatusForbidden},
		{name: "a GET", method: http.MethodGet, wantStatus: http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, "/admin/timezone", strings.NewReader(tt.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			site := tt.site
			if site == "" {
				site = "same-origin"
			}
			req.Header.Set("Sec-Fetch-Site", site)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var got *http.Cookie
			for _, c := range rec.Result().Cookies() {
				if c.Name == timeZoneCookie {
					got = c
				}
			}
			switch tt.wantCookie {
			case "":
				if got != nil {
					t.Errorf("cookie = %+v, want none set", got)
				}
			case "-":
				if got == nil || got.MaxAge >= 0 {
					t.Errorf("cookie = %+v, want it cleared", got)
				}
			default:
				if got == nil || got.Value != tt.wantCookie || !got.HttpOnly {
					t.Errorf("cookie = %+v, want %q, HttpOnly", got, tt.wantCookie)
				}
			}
			if tt.form.Get("return") != "" && rec.Header().Get("Location") != tt.form.Get("return") {
				t.Errorf("Location = %q, want %q", rec.Header().Get("Location"), tt.form.Get("return"))
			}
		})
	}
}

func TestPagesShowTimesInTheChosenZone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cookie string
		want   []string
	}{
		{name: "UTC by default", want: []string{"2026-01-01 10:00 UTC"}},
		{name: "the browser's zone", cookie: "Europe/Berlin", want: []string{"2026-01-01 11:00 CET", "Europe/Berlin"}},
		{name: "a cookie naming no zone", cookie: "Mars/Olympus", want: []string{"2026-01-01 10:00 UTC"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(newFakeStore(), authz.RoleViewer)
			created := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
			st.recipients["own"] = models.Recipient{ID: "own", Subject: "tester", Name: "Jens", CreatedAt: created, UpdatedAt: created}
			handler := notificationsServer(t, st, &fakeMessenger{}).Handler

			req := httptest.NewRequest(http.MethodGet, "/admin/notifications", nil)
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: timeZoneCookie, Value: tt.cookie})
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			body := rec.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("page does not contain %q", want)
				}
			}
		})
	}
}
