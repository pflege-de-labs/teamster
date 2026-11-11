package httpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestATemplateRoutesStillUseIsNotDeleted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		store      func() *fakeStore
		delete     func(t *testing.T, h http.Handler) (status int, message string)
		wantStatus int
		want       []string
		notWant    []string
	}{
		{
			name:  "form",
			store: seededUIStore,
			delete: func(t *testing.T, h http.Handler) (int, string) {
				rec := postForm(t, h, "/admin/templates/delete", url.Values{"id": {"tmpl"}}, nil)
				loc, _ := url.Parse(rec.Header().Get("Location"))
				return rec.Code, loc.Query().Get("error")
			},
			wantStatus: http.StatusSeeOther,
			want:       []string{"still used by routes", "Critical to ops"},
		},
		{
			name:  "API",
			store: seededUIStore,
			delete: func(t *testing.T, h http.Handler) (int, string) {
				rec := do(t, h, http.MethodDelete, "/api/templates/tmpl", "")
				return rec.Code, rec.Body.String()
			},
			wantStatus: http.StatusConflict,
			want:       []string{"still used by routes", "Critical to ops"},
		},
		{
			name: "a route the session may not read is counted, not named",
			store: func() *fakeStore {
				st := sharedStore()
				grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "read", "delete")
				st.routes["seen"] = models.Route{ID: "seen", Name: "Visible route", TemplateID: "t1"}
				st.routes["hidden"] = models.Route{ID: "hidden", Name: "Secret route", TemplateID: "t1"}
				grantTo(st, models.PrincipalUser, "tester", "Route", "seen", "read")
				return st
			},
			delete: func(t *testing.T, h http.Handler) (int, string) {
				rec := call(t, h, http.MethodDelete, "/api/templates/t1", "")
				return rec.Code, rec.Body.String()
			},
			wantStatus: http.StatusConflict,
			want:       []string{"Visible route", "1 more you may not read"},
			notWant:    []string{"Secret route"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := tt.store()
			before := len(st.templates)
			_, h := capturedServer(t, st)
			status, message := tt.delete(t, h)

			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d (%s)", status, tt.wantStatus, message)
			}
			for _, w := range tt.want {
				if !strings.Contains(message, w) {
					t.Errorf("message %q does not say %q", message, w)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(message, w) {
					t.Errorf("message %q names %q", message, w)
				}
			}
			if len(st.templates) != before {
				t.Error("the template was deleted")
			}
		})
	}
}
