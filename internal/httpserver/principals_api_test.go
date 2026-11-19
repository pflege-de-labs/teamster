package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestPrincipalSearch(t *testing.T) {
	t.Parallel()

	newStore := func(roles ...authz.Role) *fakeStore {
		st := sessionAs(newFakeStore(), roles...)
		st.users["s-zoe"] = models.User{Subject: "s-zoe", Name: "Zoe Last", Email: "zoe@example.com"}
		st.users["s-bob"] = models.User{Subject: "s-bob", Name: "Bob"}
		return st
	}
	shared := newStore(authz.RoleViewer)
	grantTo(shared, models.PrincipalUser, "tester", "Template", "t1", "read", "share")

	tests := []struct {
		name     string
		store    *fakeStore
		method   string
		query    string
		wantCode int
		want     []principalHit
	}{
		{name: "admin", store: newStore(authz.RoleAdmin), query: "?q=zoe", wantCode: http.StatusOK, want: []principalHit{
			{Value: "user:s-zoe", Label: "Zoe Last", Search: "Zoe Last zoe@example.com s-zoe", Title: "s-zoe"},
		}},
		{name: "editor", store: newStore(authz.RoleEditor), query: "?q=bob", wantCode: http.StatusOK, want: []principalHit{
			{Value: "user:s-bob", Label: "Bob", Search: "Bob s-bob", Title: "s-bob"},
		}},
		{name: "someone a record was shared with", store: shared, query: "?q=bob", wantCode: http.StatusOK, want: []principalHit{
			{Value: "user:s-bob", Label: "Bob", Search: "Bob s-bob", Title: "s-bob"},
		}},
		{name: "empty query", store: newStore(authz.RoleAdmin), query: "?q=%20", wantCode: http.StatusOK, want: []principalHit{}},
		{name: "viewer without grants", store: newStore(authz.RoleViewer), query: "?q=zoe", wantCode: http.StatusForbidden},
		{name: "not a GET", store: newStore(authz.RoleAdmin), method: http.MethodPost, query: "?q=zoe", wantCode: http.StatusMethodNotAllowed},
		{name: "a failing store", store: newStore(authz.RoleAdmin).fail("ListUsers"), query: "?q=zoe", wantCode: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			rec := call(t, newTestServer(t, tt.store, &fakeMessenger{}).Handler, method, "/admin/principals"+tt.query, "")
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantCode, rec.Body)
			}
			if tt.want == nil {
				return
			}
			var got []principalHit
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode %q: %v", rec.Body, err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("hits = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A picker searches only when its page could not list every user.
func TestPickerSearchesPastTheFirstPage(t *testing.T) {
	t.Parallel()

	for _, n := range []int{usersPageSize - 1, usersPageSize} {
		st := sessionAs(newFakeStore(), authz.RoleAdmin)
		for i := range n {
			subject := fmt.Sprintf("s-%03d", i)
			st.users[subject] = models.User{Subject: subject, Name: subject}
		}
		body := call(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/access", "").Body.String()
		searches := strings.Contains(body, `data-search-url="/admin/principals"`)
		if want := n >= usersPageSize; searches != want {
			t.Errorf("%d users: picker searches = %v, want %v", n, searches, want)
		}
	}
}
