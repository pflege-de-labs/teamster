package graph

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// noSleep records the waits a throttled client would have taken.
func noSleep(waits *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*waits = append(*waits, d)
		return nil
	}
}

func TestGetUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		status       int
		body         string
		wantNotFound bool
		wantErr      bool
		wantID       string
	}{
		{name: "found", body: `{"id":"u1","userPrincipalName":"alice@corp.example","givenName":"Alice","accountEnabled":true,"userType":"Member"}`, wantID: "u1"},
		{name: "unknown", status: http.StatusNotFound, body: `{"error":{"code":"Request_ResourceNotFound","message":"no"}}`, wantNotFound: true, wantErr: true},
		{name: "undecodable", body: `nope`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotPath, gotSelect string
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotSelect = r.URL.EscapedPath(), r.URL.Query().Get("$select")
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				_, _ = w.Write([]byte(tt.body))
			})

			u, err := client.GetUser(context.Background(), "alice@corp.example")
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetUser() error = %v, want error %v", err, tt.wantErr)
			}
			if got := errors.Is(err, ErrNotFound); got != tt.wantNotFound {
				t.Errorf("errors.Is(ErrNotFound) = %v, want %v", got, tt.wantNotFound)
			}
			if u.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", u.ID, tt.wantID)
			}
			if gotPath != "/users/alice@corp.example" {
				t.Errorf("path = %q", gotPath)
			}
			if gotSelect != userFields {
				t.Errorf("$select = %q", gotSelect)
			}
		})
	}
}

func TestFindUserByMail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		body         string
		wantID       string
		wantNotFound bool
		wantAmbig    bool
		wantErr      bool
	}{
		{name: "one match", body: `{"value":[{"id":"u1"}]}`, wantID: "u1"},
		{name: "no match", body: `{"value":[]}`, wantNotFound: true, wantErr: true},
		{name: "ambiguous", body: `{"value":[{"id":"u1"},{"id":"u2"}]}`, wantAmbig: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotFilter, gotConsistency string
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotFilter = r.URL.Query().Get("$filter")
				gotConsistency = r.Header.Get("ConsistencyLevel")
				_, _ = w.Write([]byte(tt.body))
			})

			u, err := client.FindUserByMail(context.Background(), "o'brien@corp.example")
			if (err != nil) != tt.wantErr || errors.Is(err, ErrNotFound) != tt.wantNotFound || errors.Is(err, ErrAmbiguous) != tt.wantAmbig {
				t.Fatalf("FindUserByMail() error = %v", err)
			}
			if u.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", u.ID, tt.wantID)
			}
			want := "mail eq 'o''brien@corp.example' or proxyAddresses/any(p:p eq 'smtp:o''brien@corp.example')"
			if gotFilter != want {
				t.Errorf("$filter = %q, want %q", gotFilter, want)
			}
			if gotConsistency != "eventual" {
				t.Errorf("ConsistencyLevel = %q, want eventual", gotConsistency)
			}
		})
	}
}

func TestListMemberUsers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pages     map[string]string
		stopAfter int
		wantIDs   []string
		wantErr   bool
	}{
		{name: "one page", pages: map[string]string{"": `{"value":[{"id":"a"},{"id":"b"}]}`}, wantIDs: []string{"a", "b"}},
		{name: "two pages", pages: map[string]string{"": `{"value":[{"id":"a"}],"@odata.nextLink":"BASE/users?page=2"}`, "2": `{"value":[{"id":"b"}]}`}, wantIDs: []string{"a", "b"}},
		{name: "a link elsewhere is not followed", pages: map[string]string{"": `{"value":[{"id":"a"}],"@odata.nextLink":"https://evil.example/users"}`}, wantIDs: []string{"a"}},
		{name: "callback stops the walk", pages: map[string]string{"": `{"value":[{"id":"a"}],"@odata.nextLink":"BASE/users?page=2"}`, "2": `{"value":[{"id":"b"}]}`}, stopAfter: 1, wantIDs: []string{"a"}, wantErr: true},
		{name: "undecodable", pages: map[string]string{"": `nope`}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var base, gotFilter string
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				page := r.URL.Query().Get("page")
				if page == "" {
					gotFilter = r.URL.Query().Get("$filter")
				}
				_, _ = w.Write([]byte(strings.ReplaceAll(tt.pages[page], "BASE", base)))
			})
			base = client.baseURL

			var ids []string
			calls := 0
			err := client.ListMemberUsers(context.Background(), func(users []User) error {
				calls++
				for _, u := range users {
					ids = append(ids, u.ID)
				}
				if tt.stopAfter != 0 && calls >= tt.stopAfter {
					return errors.New("stop")
				}
				return nil
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("ListMemberUsers() error = %v, want error %v", err, tt.wantErr)
			}
			if strings.Join(ids, ",") != strings.Join(tt.wantIDs, ",") {
				t.Errorf("ids = %v, want %v", ids, tt.wantIDs)
			}
			if gotFilter != "accountEnabled eq true and userType eq 'Member'" {
				t.Errorf("$filter = %q", gotFilter)
			}
		})
	}
}

func TestResolveCatalogApp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		manifestID   string
		botID        string
		byManifest   string
		byBot        string
		want         string
		wantNotFound bool
	}{
		{name: "by manifest id", manifestID: "m1", botID: "b1", byManifest: `{"value":[{"id":"cat-1","distributionMethod":"organization"}]}`, want: "cat-1"},
		{name: "falls back to the bot", manifestID: "m1", botID: "b1", byManifest: `{"value":[]}`, byBot: `{"value":[{"id":"cat-2","distributionMethod":"organization"}]}`, want: "cat-2"},
		{name: "a sideloaded app is not installable", botID: "b1", byBot: `{"value":[{"id":"cat-3","distributionMethod":"sideloaded"}]}`, wantNotFound: true},
		{name: "nowhere", manifestID: "m1", botID: "b1", byManifest: `{"value":[]}`, byBot: `{"value":[]}`, wantNotFound: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/appCatalogs/teamsApps" {
					t.Errorf("path = %q", r.URL.Path)
				}
				if strings.Contains(r.URL.Query().Get("$filter"), "externalId eq '"+tt.manifestID+"'") {
					_, _ = w.Write([]byte(tt.byManifest))
					return
				}
				_, _ = w.Write([]byte(tt.byBot))
			})

			got, err := client.ResolveCatalogApp(context.Background(), tt.manifestID, tt.botID)
			if errors.Is(err, ErrNotFound) != tt.wantNotFound || (err != nil && !tt.wantNotFound) {
				t.Fatalf("ResolveCatalogApp() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveCatalogApp() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstallAppForUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		wantAlready bool
		wantErr     bool
	}{
		{name: "installed", status: http.StatusCreated},
		{name: "already installed", status: http.StatusConflict, wantAlready: true},
		{name: "permission missing", status: http.StatusForbidden, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var base, gotMethod, gotPath, gotType string
			var gotBody map[string]string
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &gotBody)
				w.WriteHeader(tt.status)
			})
			base = client.baseURL

			already, err := client.InstallAppForUser(context.Background(), "u1", "cat-1")
			if (err != nil) != tt.wantErr || already != tt.wantAlready {
				t.Fatalf("InstallAppForUser() = %v, %v; want %v, error %v", already, err, tt.wantAlready, tt.wantErr)
			}
			if gotMethod != http.MethodPost || gotPath != "/users/u1/teamwork/installedApps" || gotType != "application/json" {
				t.Errorf("request = %s %s (%s)", gotMethod, gotPath, gotType)
			}
			if want := base + "/appCatalogs/teamsApps/cat-1"; gotBody["teamsApp@odata.bind"] != want {
				t.Errorf("bind = %q, want %q", gotBody["teamsApp@odata.bind"], want)
			}
		})
	}
}

func TestThrottledRequestsAreRetried(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		statuses  []int
		header    string
		wantCalls int32
		wantWaits []time.Duration
		wantErr   bool
	}{
		{name: "retry after, then ok", statuses: []int{429, 200}, header: "3", wantCalls: 2, wantWaits: []time.Duration{3 * time.Second}},
		{name: "no header doubles", statuses: []int{503, 503, 200}, wantCalls: 3, wantWaits: []time.Duration{time.Second, 2 * time.Second}},
		{name: "a long wait is capped", statuses: []int{429, 200}, header: "3600", wantCalls: 2, wantWaits: []time.Duration{maxRetryWait}},
		{name: "gives up", statuses: []int{429, 429, 429, 429, 200}, header: "1", wantCalls: maxAttempts, wantWaits: []time.Duration{time.Second, time.Second, time.Second}, wantErr: true},
		{name: "other errors are not retried", statuses: []int{500, 200}, wantCalls: 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				n := calls.Add(1)
				if tt.header != "" {
					w.Header().Set("Retry-After", tt.header)
				}
				w.WriteHeader(tt.statuses[n-1])
				_, _ = w.Write([]byte(`{"id":"u1"}`))
			})
			var waits []time.Duration
			client.sleep = noSleep(&waits)

			_, err := client.GetUser(context.Background(), "u1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetUser() error = %v, want error %v", err, tt.wantErr)
			}
			if calls.Load() != tt.wantCalls {
				t.Errorf("calls = %d, want %d", calls.Load(), tt.wantCalls)
			}
			if len(waits) != len(tt.wantWaits) {
				t.Fatalf("waits = %v, want %v", waits, tt.wantWaits)
			}
			for i := range waits {
				if waits[i] != tt.wantWaits[i] {
					t.Errorf("waits = %v, want %v", waits, tt.wantWaits)
					break
				}
			}
		})
	}
}

func TestThrottledWaitEndsWithContext(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := client.GetUser(ctx, "u1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetUser() error = %v, want the deadline", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("waited %v after the context ended", time.Since(start))
	}
}

func TestAPIErrorCarriesGraphCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		wantCode string
		wantMsg  string
	}{
		{name: "graph error body", body: `{"error":{"code":"Forbidden","message":"missing role"}}`, wantCode: "Forbidden", wantMsg: "missing role"},
		{name: "plain body", body: `gateway`, wantMsg: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := apiError(http.MethodGet, "https://graph.example/users", http.StatusForbidden, []byte(tt.body))
			if e.Code != tt.wantCode || e.Message != tt.wantMsg || e.Status != http.StatusForbidden {
				t.Errorf("apiError = %+v", e)
			}
			if !strings.Contains(e.Error(), "403") || errors.Is(e, ErrNotFound) {
				t.Errorf("Error() = %q", e.Error())
			}
		})
	}
}
