package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
)

// linkCodeShape is what a minted code must look like: three dashed groups of
// four symbols drawn from the unambiguous alphabet. Built from
// linkCodeAlphabet itself, not a hand-written range, so it cannot silently
// admit a symbol the alphabet excludes -- [A-HJ-NP-Z2-9] once did, for L.
var linkCodeShape = regexp.MustCompile(
	`^[` + regexp.QuoteMeta(linkCodeAlphabet) + `]{4}-[` + regexp.QuoteMeta(linkCodeAlphabet) + `]{4}-[` + regexp.QuoteMeta(linkCodeAlphabet) + `]{4}$`,
)

func TestLinkRecipientViewerIsPermitted(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := asRole(t, handler, http.MethodPost, "/api/recipients/link", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Code      string `json:"code"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !linkCodeShape.MatchString(body.Code) {
		t.Errorf("code = %q, does not match the expected shape", body.Code)
	}
	if body.ExpiresAt == "" {
		t.Error("expires_at is empty")
	}
}

func TestLinkRecipientNoRoleIsRefused(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore())
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := asRole(t, handler, http.MethodPost, "/api/recipients/link", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// A basic-auth caller authenticates as the *configured admin username*, not as
// themselves, so minting a code for that subject would bind a chat to "the
// admin account" rather than to whoever holds the shared password.
func TestLinkRecipientBasicAuthIsRefused(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	req := httptest.NewRequest(http.MethodPost, "/api/recipients/link", nil)
	req.SetBasicAuth("admin", "pass")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkRecipientRejectsGet(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := asRole(t, handler, http.MethodGet, "/api/recipients/link", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// Minting a new code retires whatever the subject already had outstanding, so
// an old code guessed later finds nothing rather than accumulating live guesses.
func TestLinkRecipientRetiresPriorCodes(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	first := asRole(t, handler, http.MethodPost, "/api/recipients/link", "")
	if first.Code != http.StatusOK {
		t.Fatalf("first mint status = %d", first.Code)
	}
	var firstBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode first body: %v", err)
	}

	second := asRole(t, handler, http.MethodPost, "/api/recipients/link", "")
	if second.Code != http.StatusOK {
		t.Fatalf("second mint status = %d", second.Code)
	}

	// The map is keyed by the stored, undashed code; the response carries the
	// grouped display form, so the dashes must come back off before this
	// lookup can ever hit.
	firstCode := strings.ReplaceAll(firstBody.Code, "-", "")

	st.mu.Lock()
	_, stillLive := st.linkFlows[firstCode]
	remaining := len(st.linkFlows)
	st.mu.Unlock()

	if stillLive {
		t.Error("the first code is still live after a second was minted")
	}
	if remaining != 1 {
		t.Errorf("outstanding codes = %d, want exactly 1", remaining)
	}
}
