package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/language"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func TestWriteError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		err      error
		wantBody string
		wantLog  string
	}{
		{name: "a client error keeps its message", status: http.StatusBadRequest, err: errors.New("name is required"), wantBody: "name is required", wantLog: "level=DEBUG"},
		{name: "a server error hides its cause", status: http.StatusInternalServerError, err: fmt.Errorf("list: %w", errStore), wantBody: "internal server error", wantLog: "level=ERROR"},
		{name: "an upstream error hides its cause", status: http.StatusBadGateway, err: errors.New("graph GET https://graph/teams failed: {secret}"), wantBody: "bad gateway", wantLog: "level=ERROR"},
		{name: "a missing bot is named", status: http.StatusBadGateway, err: fmt.Errorf("deliver: %w", errNoChannelTransport), wantBody: "deliver: " + errNoChannelTransport.Error(), wantLog: "level=ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logs := &logBuffer{}
			logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			var rec *httptest.ResponseRecorder
			handler := (&Server{log: logger}).logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeError(w, r, tt.status, tt.err)
			}))
			rec = httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))

			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body %q: %v", rec.Body.String(), err)
			}
			if rec.Code != tt.status || body["error"] != tt.wantBody {
				t.Errorf("answer = %d %q, want %d %q", rec.Code, body["error"], tt.status, tt.wantBody)
			}
			id := rec.Header().Get(requestIDHeader)
			if tt.status >= http.StatusInternalServerError && body["request_id"] != id {
				t.Errorf("request_id = %q, want the %s header %q", body["request_id"], requestIDHeader, id)
			}
			if !strings.Contains(logs.String(), tt.wantLog) || !strings.Contains(logs.String(), "request_id="+id) {
				t.Errorf("log = %q, want %s with request_id=%s", logs.String(), tt.wantLog, id)
			}
			if !strings.Contains(logs.String(), tt.err.Error()) {
				t.Errorf("log = %q, want the cause %q", logs.String(), tt.err)
			}
		})
	}
}

func TestVisibleError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		want    string
		wantLog string
	}{
		{name: "validation", err: userError{errors.New("priority must be a number")}, want: "priority must be a number", wantLog: "level=DEBUG"},
		{name: "a store sentinel", err: fmt.Errorf("template t: %w", store.ErrNotFound), want: "template t: not found", wantLog: "level=DEBUG"},
		{name: "a refused route", err: invalidRoute{errors.New("route loops")}, want: "route loops", wantLog: "level=DEBUG"},
		{name: "anything else", err: errStore, want: "Something went wrong. Reference: req-1", wantLog: "level=ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logs := &logBuffer{}
			logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			bundle, err := i18n.New("", language.English)
			if err != nil {
				t.Fatalf("i18n.New: %v", err)
			}
			ctx := withTestLogger(i18n.WithLanguage(t.Context(), bundle, language.English), logger, "req-1")
			if got := visibleError(ctx, "form post", tt.err); got != tt.want {
				t.Errorf("visibleError() = %q, want %q", got, tt.want)
			}
			if !strings.Contains(logs.String(), tt.wantLog) || !strings.Contains(logs.String(), tt.err.Error()) {
				t.Errorf("log = %q, want %s carrying %q", logs.String(), tt.wantLog, tt.err)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		status   int
		wantLine string
		wantNone bool
	}{
		{name: "a request", path: "/api/templates", status: http.StatusTeapot, wantLine: "method=GET path=/api/templates status=418"},
		{name: "the Teams V2 token is not logged", path: "/teamsv2/team/channel/s3cret", status: http.StatusOK, wantLine: "path=/teamsv2/team/channel/…"},
		{name: "a probe is debug only", path: "/healthz", status: http.StatusOK, wantNone: true},
		{name: "a handler that only writes is a 200", path: "/admin", wantLine: "status=200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logs := &logBuffer{}
			logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
			handler := (&Server{log: logger}).logging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				_, _ = w.Write([]byte("ok"))
			}))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Header().Get(requestIDHeader) == "" {
				t.Errorf("no %s header", requestIDHeader)
			}
			if tt.wantNone {
				if logs.String() != "" {
					t.Errorf("log = %q, want nothing at info", logs.String())
				}
				return
			}
			if !strings.Contains(logs.String(), tt.wantLine) {
				t.Errorf("log = %q, want it to contain %q", logs.String(), tt.wantLine)
			}
			if strings.Contains(logs.String(), "s3cret") {
				t.Errorf("log = %q carries the Teams V2 token", logs.String())
			}
		})
	}
}

func TestImportReportsAStoreFailureAsAServerError(t *testing.T) {
	t.Parallel()

	st := transferStore(t, authz.RoleAdmin).fail("WithTx")
	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/config/import", `{"version":1}`)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), errStore.Error()) {
		t.Errorf("POST = %d %s, want 500 without the store's error", rec.Code, rec.Body.String())
	}
}

func TestInstallLookupLogsAMissingApp(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.destinations["d1"] = models.Destination{ID: "d1", Name: "Ops", TeamID: "team-1", ChannelID: "c"}
	cfg := testConfig()
	cfg.Bot.ClientID = "bot-app-id"
	srv, logs := newLoggedServer(t, cfg, st, &fakeMessenger{installed: map[string]bool{}}, slog.LevelInfo)

	do(t, srv.Handler, http.MethodGet, "/admin/teams", "")
	for _, want := range []string{"Teams app not installed in team", "team=team-1", "bot_client_id=bot-app-id"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log = %q, want it to contain %q", logs.String(), want)
		}
	}
}

func TestBotMessagesLogWhyTheyWereRefused(t *testing.T) {
	t.Parallel()

	key := generateBotKey(t, "good-key", "msteams")
	idp := newBotIDP(t, key)
	const clientID = "bot-client-id"
	cfg := testConfig()
	cfg.Bot = botTestConfig(idp, clientID)
	srv, logs := newLoggedServer(t, cfg, newFakeStore(), &fakeMessenger{}, slog.LevelInfo)
	primeBotKeySet(t, srv.Handler, idp, clientID, key)

	activity := botActivityFields()
	now := time.Now()
	postBotMessage(srv.Handler, marshalActivity(t, activity), "")
	postBotMessage(srv.Handler, marshalActivity(t, activity), "Bearer "+signBotToken(t, key, botTokenClaims{
		Issuer: idp.server.URL, Audience: "old-app-id", ServiceURL: activity["serviceUrl"].(string),
		IssuedAt: now.Add(-time.Minute), Expiry: now.Add(time.Minute), NotBefore: now.Add(-time.Minute),
	}))

	for _, want := range []string{"reason=no-bearer", "bot token verify", "bot_client_id=bot-client-id", "reason=invalid-token"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log = %q, want it to contain %q", logs.String(), want)
		}
	}
}
