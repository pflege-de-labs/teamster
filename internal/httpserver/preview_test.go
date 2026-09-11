package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postPreview(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/templates/preview", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "pass")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodePreview(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode preview response %q: %v", rec.Body.String(), err)
	}
	return payload
}

// The feed line is the point of the milestone, so the preview has to return it
// alongside the card — and the text it returns has already been sanitized.
func decodeString(t *testing.T, raw json.RawMessage) string {
	t.Helper()

	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func TestPreviewRendersTheWholeMessage(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	rec := postPreview(t, handler, `{"title":"{{ .Alert.Labels.alertname }} is {{ .Alert.Status }}","text":"<p>{{ .Alert.Annotations.summary }}</p><script>steal()</script>","sample":"firing"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	payload := decodePreview(t, rec)
	if got := decodeString(t, payload["title"]); got != "HighMemory is firing" {
		t.Errorf("title = %q, want the rendered feed line", got)
	}
	if got := decodeString(t, payload["text"]); got != "<p>Memory usage is above 80%</p>" {
		t.Errorf("text = %q, want the sanitized text without the script", got)
	}
	if _, ok := payload["card"]; ok {
		t.Errorf("card = %s, want none for a template without one", payload["card"])
	}
}

func TestPreviewReportsATemplateThatSendsNothing(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	rec := postPreview(t, handler, `{"sample":"firing"}`)

	payload := decodePreview(t, rec)
	for _, key := range []string{"title", "text", "card"} {
		if _, ok := payload[key]; ok {
			t.Errorf("%s = %s, want an empty message rather than an invented one", key, payload[key])
		}
	}
}

func TestPreviewRendersTheTemplate(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	rec := postPreview(t, handler, `{"body":"{\"text\":\"{{ .Alert.Status }} {{ index .Alert.Labels \"alertname\" }}\"}","sample":"firing"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	card := string(decodePreview(t, rec)["card"])
	if !strings.Contains(card, "firing HighMemory") {
		t.Errorf("card = %s, want the sample alert rendered into it", card)
	}
}

func TestPreviewUsesTheSelectedSample(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	body := `{"body":"{\"text\":\"{{ .Alert.Status }}\"}","sample":%q}`

	tests := []struct {
		sample string
		want   string
	}{
		{sample: "firing", want: "firing"},
		{sample: "resolved", want: "resolved"},
		{sample: "", want: "firing"},
		{sample: "nonsense", want: "firing"},
	}

	for _, tt := range tests {
		t.Run("sample "+tt.sample, func(t *testing.T) {
			t.Parallel()

			rec := postPreview(t, handler, strings.Replace(body, "%q", `"`+tt.sample+`"`, 1))
			card := string(decodePreview(t, rec)["card"])
			if !strings.Contains(card, tt.want) {
				t.Errorf("card = %s, want status %q", card, tt.want)
			}
		})
	}
}

// A template that does not compile is the answer the operator asked for, so it
// comes back as a readable message rather than a failed request.
func TestPreviewReportsTemplateErrors(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "template does not parse", body: `{"body":"{{ .Alert"}`, wantErr: "parse template"},
		{name: "output is not JSON", body: `{"body":"not json"}`, wantErr: "not valid JSON"},
		{name: "field does not exist", body: `{"body":"{{ .Nope.Missing }}"}`, wantErr: "execute template"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := postPreview(t, handler, tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("preview = %d, want 200 carrying the error", rec.Code)
			}

			payload := decodePreview(t, rec)
			if _, ok := payload["card"]; ok {
				t.Error("a broken template still produced a card")
			}
			if got := string(payload["error"]); !strings.Contains(got, tt.wantErr) {
				t.Errorf("error = %s, want it to mention %q", got, tt.wantErr)
			}
		})
	}
}

func TestPreviewRejectsBadRequests(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	if rec := postPreview(t, handler, "not json"); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid JSON = %d, want 400", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/templates/preview", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", rec.Code)
	}
}

// /api/templates/ already routes to handleTemplateByID, which would otherwise
// treat "preview" as a template id and answer 404.
func TestPreviewPathBeatsTheByIDRoute(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	rec := postPreview(t, handler, `{"body":"{\"a\":1}"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d, want 200 — the by-id route swallowed it", rec.Code)
	}
	if _, ok := decodePreview(t, rec)["card"]; !ok {
		t.Error("no card in the response")
	}
}

func TestPreviewAssetsAreServed(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler

	for _, path := range []string{"/vendor/adaptivecards.min.js", "/preview.js"} {
		rec := do(t, handler, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") && !strings.HasPrefix(got, "application/javascript") {
			t.Errorf("GET %s Content-Type = %q, want a JavaScript type", path, got)
		}
	}
}

func TestAdminPageOffersThePreviewControls(t *testing.T) {
	t.Parallel()

	body := do(t, newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()

	for _, want := range []string{
		`id="preview-button"`, `id="preview-output"`, `id="template-form"`,
		`<option value="firing">`, `<option value="resolved">`,
		`src="/vendor/adaptivecards.min.js"`, `src="/preview.js"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("admin page is missing %q", want)
		}
	}
}

// An unbounded body would let an authenticated operator exhaust memory by
// accident as easily as on purpose.
func TestPreviewRejectsAnOversizedBody(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	huge := `{"body":"` + strings.Repeat("x", maxPreviewBytes+1) + `"}`

	rec := postPreview(t, handler, huge)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized preview = %d, want 413", rec.Code)
	}
}

func TestPreviewAcceptsABodyUnderTheLimit(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	card := `{"text":"` + strings.Repeat("x", 1024) + `"}`

	rec := postPreview(t, handler, `{"body":`+mustJSON(t, card)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d, want 200", rec.Code)
	}
	if _, ok := decodePreview(t, rec)["card"]; !ok {
		t.Error("no card returned for a body well under the limit")
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}
