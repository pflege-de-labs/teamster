package httpserver

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	jose "github.com/go-jose/go-jose/v4"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// botIDPKey is one signing key a test's identity provider advertises, with the
// channels it is endorsed for -- the JWKS extension go-oidc does not model.
type botIDPKey struct {
	kid          string
	priv         *rsa.PrivateKey
	endorsements []string
	// noEndorsements, when set, omits the "endorsements" member from this
	// key's JWKS entry entirely. Microsoft's live document does not do this on
	// any of its keys; the case exists to pin the fail-closed reading of it.
	// See keyEndorsement in bot_auth.go.
	noEndorsements bool
}

// botIDP is a hand-rolled Bot Framework metadata + JWKS server. oidctest.Server
// serves a discovery document and a JWKS, but not the endorsements extension
// every activity's channel is checked against, so this serves both itself
// rather than layering a second server on top for one field.
type botIDP struct {
	server *httptest.Server
	keys   map[string]botIDPKey
}

func newBotIDP(t *testing.T, keys ...botIDPKey) *botIDP {
	t.Helper()

	idp := &botIDP{keys: make(map[string]botIDPKey, len(keys))}
	for _, key := range keys {
		idp.keys[key.kid] = key
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":   idp.server.URL,
			"jwks_uri": idp.server.URL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(idp.jwks(t))
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// jwks renders the keys as a standard JWKS with one Bot Framework extension
// field, "endorsements", added to each entry after go-jose has marshalled the
// standard ones -- go-jose's JSONWebKey has no field for it.
func (idp *botIDP) jwks(t *testing.T) []byte {
	t.Helper()

	kids := make([]string, 0, len(idp.keys))
	for kid := range idp.keys {
		kids = append(kids, kid)
	}
	sort.Strings(kids)

	raw := make([]json.RawMessage, 0, len(kids))
	for _, kid := range kids {
		key := idp.keys[kid]
		jwk := jose.JSONWebKey{Key: &key.priv.PublicKey, Use: "sig", Algorithm: string(jose.RS256), KeyID: key.kid}
		data, err := jwk.MarshalJSON()
		if err != nil {
			t.Fatalf("marshal jwk: %v", err)
		}

		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatalf("unmarshal jwk: %v", err)
		}
		if !key.noEndorsements {
			fields["endorsements"] = key.endorsements
		}

		entry, err := json.Marshal(fields)
		if err != nil {
			t.Fatalf("marshal jwk with endorsements: %v", err)
		}
		raw = append(raw, entry)
	}

	body, err := json.Marshal(map[string]any{"keys": raw})
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}
	return body
}

func generateBotKey(t *testing.T, kid string, endorsements ...string) botIDPKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return botIDPKey{kid: kid, priv: priv, endorsements: endorsements}
}

// signBotToken signs a Bot Framework-shaped JWT: issuer, audience, serviceurl
// and the standard time claims. Fields left at their zero value are omitted
// entirely, which is how the "wrong issuer" and "wrong audience" cases produce
// a token that is otherwise well formed.
type botTokenClaims struct {
	Issuer     string
	Audience   string
	ServiceURL string
	IssuedAt   time.Time
	Expiry     time.Time
	NotBefore  time.Time
}

func signBotToken(t *testing.T, key botIDPKey, claims botTokenClaims) string {
	t.Helper()

	body := map[string]any{"sub": "botframework"}
	if claims.Issuer != "" {
		body["iss"] = claims.Issuer
	}
	if claims.Audience != "" {
		body["aud"] = claims.Audience
	}
	if claims.ServiceURL != "" {
		body["serviceurl"] = claims.ServiceURL
	}
	if !claims.IssuedAt.IsZero() {
		body["iat"] = claims.IssuedAt.Unix()
	}
	if !claims.Expiry.IsZero() {
		body["exp"] = claims.Expiry.Unix()
	}
	if !claims.NotBefore.IsZero() {
		body["nbf"] = claims.NotBefore.Unix()
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return oidctest.SignIDToken(key.priv, key.kid, oidc.RS256, string(raw))
}

// botActivityFields builds a minimal, valid Teams personal-message activity as
// a map, so a test can override exactly the field it cares about before
// marshalling.
func botActivityFields() map[string]any {
	return map[string]any{
		"type":       "message",
		"id":         "activity-1",
		"serviceUrl": "https://smba.trafficmanager.net/teams/",
		"channelId":  "msteams",
		"text":       "hello",
		"conversation": map[string]any{
			"id":               "conv-1",
			"conversationType": "personal",
		},
		"from": map[string]any{
			"id":          "29:user-1",
			"name":        "Alice",
			"aadObjectId": "aad-1",
		},
		"recipient": map[string]any{
			"id":   "28:bot-app-id",
			"name": "Teamster",
		},
	}
}

func marshalActivity(t *testing.T, fields map[string]any) string {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal activity: %v", err)
	}
	return string(data)
}

// botTestConfig is a fully configured single-tenant bot pointed at idp.
func botTestConfig(idp *botIDP, clientID string) config.BotConfig {
	return config.BotConfig{
		TenantID:     "tenant-1",
		ClientID:     clientID,
		ClientSecret: "secret",
		MetadataURL:  idp.server.URL + "/.well-known/openid-configuration",
	}
}

// primeBotKeySet sends one activity signed by key through handler so its kid
// is recorded as known by the shared rate-limited key set before a test goes
// on to exercise other, never-before-seen kids against the same handler --
// without it, whichever unfamiliar kid a t.Parallel() scheduler happens to
// run first wins the sole floor-window attempt, and every other one is denied
// regardless of what the subtest actually meant to check.
func primeBotKeySet(t *testing.T, handler http.Handler, idp *botIDP, clientID string, key botIDPKey) {
	t.Helper()
	activity := botActivityFields()
	now := time.Now()
	token := signBotToken(t, key, botTokenClaims{
		Issuer: idp.server.URL, Audience: clientID, ServiceURL: activity["serviceUrl"].(string),
		IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
	})
	postBotMessage(handler, marshalActivity(t, activity), "Bearer "+token)
}

func postBotMessage(handler http.Handler, body, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/bot/messages", strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestBotMessagesAuthentication(t *testing.T) {
	t.Parallel()

	const clientID = "bot-client-id"
	goodKey := generateBotKey(t, "good-key", "msteams")
	unendorsedKey := generateBotKey(t, "unendorsed-key", "directline")
	idp := newBotIDP(t, goodKey, unendorsedKey)

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     botTestConfig(idp, clientID),
	}
	srv, err := NewServer(cfg, newFakeStore(), &fakeMessenger{}, nil, newRecordingTelemetry())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.Handler

	activity := botActivityFields()
	serviceURL := activity["serviceUrl"].(string)
	now := time.Now()

	validClaims := botTokenClaims{
		Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
		IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
	}

	// Every subtest below shares this one handler and therefore one
	// rate-limited key set (item 1's floor on unrecognised kids). Priming
	// both real keys here, before the table's t.Parallel() subtests race each
	// other, is what lets each of them reach its own check instead of losing
	// the floor race to whichever unfamiliar kid happens to arrive first.
	primeBotKeySet(t, handler, idp, clientID, goodKey)
	primeBotKeySet(t, handler, idp, clientID, unendorsedKey)

	tests := []struct {
		name       string
		body       string
		bearer     string
		wantStatus int
	}{
		{
			name:       "a valid activity",
			body:       marshalActivity(t, activity),
			bearer:     "Bearer " + signBotToken(t, goodKey, validClaims),
			wantStatus: http.StatusOK,
		},
		{
			name: "expired token",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, goodKey, botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now.Add(-time.Hour), Expiry: now.Add(-time.Hour + time.Minute), NotBefore: now.Add(-time.Hour),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			// Inside the verifier's five-minute backdated-Now skew: must be
			// accepted, unlike the far-outside-any-skew case above.
			name: "expired within the clock-skew allowance",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, goodKey, botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now.Add(-10 * time.Minute), Expiry: now.Add(-2 * time.Minute), NotBefore: now.Add(-10 * time.Minute),
			}),
			wantStatus: http.StatusOK,
		},
		{
			name: "expired just outside the clock-skew allowance",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, goodKey, botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now.Add(-10 * time.Minute), Expiry: now.Add(-5*time.Minute - 30*time.Second), NotBefore: now.Add(-10 * time.Minute),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "serviceurl claim entirely absent",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, goodKey, botTokenClaims{
				// ServiceURL deliberately left zero: signBotToken omits it,
				// so "" == "" must not compare the missing claim equal to a
				// missing body field.
				Issuer: idp.server.URL, Audience: clientID,
				IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "alg none is rejected",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + unsignedAlgNoneToken(t, goodKey.kid, botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			// The algorithm-confusion attack: sign with HS256 using the
			// verifier's own RSA *public* key -- published in the JWKS,
			// hence attacker-known -- as the HMAC secret. SupportedSigningAlgs
			// restricting to RS256 must refuse this outright.
			name: "HS256 signed with the RSA public key is rejected",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + hs256WithPublicKey(t, &goodKey.priv.PublicKey, goodKey.kid, botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "token not yet valid",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, goodKey, botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now, Expiry: now.Add(time.Hour), NotBefore: now.Add(time.Hour),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "wrong issuer",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, goodKey, botTokenClaims{
				Issuer: "https://not-the-real-issuer.example", Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "wrong audience",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, goodKey, botTokenClaims{
				Issuer: idp.server.URL, Audience: "somebody-elses-app", ServiceURL: serviceURL,
				IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "no bearer header",
			body:       marshalActivity(t, activity),
			bearer:     "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed bearer",
			body:       marshalActivity(t, activity),
			bearer:     "Bearer",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "signature by an unknown key",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, generateBotKey(t, "stranger-key", "msteams"), botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "serviceUrl claim disagreeing with the body",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, goodKey, botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: "https://attacker.example/",
				IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
			}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "serviceUrl agreeing except for a trailing slash",
			body: marshalActivity(t, mapWith(activity, "serviceUrl", strings.TrimSuffix(serviceURL, "/"))),
			bearer: "Bearer " + signBotToken(t, goodKey, botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
			}),
			wantStatus: http.StatusOK,
		},
		{
			name: "a channelId the signing key does not endorse",
			body: marshalActivity(t, activity),
			bearer: "Bearer " + signBotToken(t, unendorsedKey, botTokenClaims{
				Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
				IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
			}),
			wantStatus: http.StatusForbidden,
		},
		{
			// Microsoft delivered and signed this correctly; refusing it with
			// anything but 2xx reads to its tooling as "this bot's auth is
			// broken" rather than "this bot ignores this activity".
			name:       "channelId not msteams",
			body:       marshalActivity(t, mapWith(activity, "channelId", "directline")),
			bearer:     "Bearer " + signBotToken(t, goodKey, validClaims),
			wantStatus: http.StatusOK,
		},
		{
			name: "conversationType not personal",
			body: marshalActivity(t, mapWith(activity, "conversation", map[string]any{
				"id": "conv-1", "conversationType": "groupChat",
			})),
			bearer:     "Bearer " + signBotToken(t, goodKey, validClaims),
			wantStatus: http.StatusOK,
		},
		{
			name: "tenant mismatch for a single-tenant registration",
			body: marshalActivity(t, mapWith(activity, "channelData", map[string]any{
				"tenant": map[string]any{"id": "some-other-tenant"},
			})),
			bearer:     "Bearer " + signBotToken(t, goodKey, validClaims),
			wantStatus: http.StatusOK,
		},
		{
			name:       "a body over the size limit",
			body:       marshalActivity(t, mapWith(activity, "text", strings.Repeat("a", botBodyLimit+1))),
			bearer:     "Bearer " + signBotToken(t, goodKey, validClaims),
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := postBotMessage(handler, tt.body, tt.bearer)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// mapWith returns a shallow copy of fields with key set to value, so a test
// can override one field of a shared base activity without the override
// leaking into other subtests sharing the same base map.
func mapWith(fields map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	out[key] = value
	return out
}

func TestBotMessagesRejectsGet(t *testing.T) {
	t.Parallel()

	goodKey := generateBotKey(t, "good-key", "msteams")
	idp := newBotIDP(t, goodKey)
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     botTestConfig(idp, "bot-client-id"),
	}
	srv, err := NewServer(cfg, newFakeStore(), &fakeMessenger{}, nil, newRecordingTelemetry())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/bot/messages", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// An unconfigured bot registers no route at all: the request falls through to
// the admin catch-all, which redirects an unauthenticated caller to the login
// page rather than answering with anything bot_auth.go produced.
func TestBotMessagesRouteAbsentWhenUnconfigured(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
	srv, err := NewServer(cfg, newFakeStore(), &fakeMessenger{}, nil, newRecordingTelemetry())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := postBotMessage(srv.Handler, marshalActivity(t, botActivityFields()), "")
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302 (the login redirect a missing route falls through to)", rec.Code)
	}
}

// The refusal buckets bullet 9 asks for must be separately countable, not
// merged into one generic "refused" status the way a single boolean would be.
func TestBotMessagesRefusalsAreCountedSeparately(t *testing.T) {
	t.Parallel()

	goodKey := generateBotKey(t, "good-key", "msteams")
	unendorsedKey := generateBotKey(t, "unendorsed-key", "other-channel")
	idp := newBotIDP(t, goodKey, unendorsedKey)
	const clientID = "bot-client-id"

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     botTestConfig(idp, clientID),
	}
	tel := newRecordingTelemetry()
	srv, err := NewServer(cfg, newFakeStore(), &fakeMessenger{}, nil, tel)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	activity := botActivityFields()
	serviceURL := activity["serviceUrl"].(string)
	now := time.Now()

	// Both keys below are never-before-seen kids on this handler's shared key
	// set; prime them first so the sequence of distinct-kid checks that
	// follows exercises what each is meant to, not the item 1 floor.
	primeBotKeySet(t, srv.Handler, idp, clientID, goodKey)
	primeBotKeySet(t, srv.Handler, idp, clientID, unendorsedKey)

	postBotMessage(srv.Handler, marshalActivity(t, activity), "")
	postBotMessage(srv.Handler, marshalActivity(t, activity), "Bearer "+signBotToken(t, goodKey, botTokenClaims{
		Issuer: idp.server.URL, Audience: "wrong-audience", ServiceURL: serviceURL,
		IssuedAt: now.Add(-time.Minute), Expiry: now.Add(time.Minute), NotBefore: now.Add(-time.Minute),
	}))
	postBotMessage(srv.Handler, marshalActivity(t, activity), "Bearer "+signBotToken(t, unendorsedKey, botTokenClaims{
		Issuer: idp.server.URL, Audience: clientID, ServiceURL: serviceURL,
		IssuedAt: now.Add(-time.Minute), Expiry: now.Add(time.Minute), NotBefore: now.Add(-time.Minute),
	}))

	_, receipts, _ := tel.calls()
	statuses := make(map[string]bool, len(receipts))
	for _, r := range receipts {
		if r[0] == "bot" {
			statuses[r[1]] = true
		}
	}

	for _, want := range []string{"no-bearer", "invalid-token", "endorsement-refused"} {
		if !statuses[want] {
			t.Errorf("status %q was not recorded among %v", want, statuses)
		}
	}
}

// unsignedAlgNoneToken builds a classic "alg: none" JWT: a header and payload
// with no signature segment at all. go-jose refuses to even parse "none" as
// an algorithm unless InsecureSkipSignatureCheck asks for it, which this
// service never sets, so this must never verify.
func unsignedAlgNoneToken(t *testing.T, kid string, claims botTokenClaims) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"alg":"none","kid":%q}`, kid)))
	body := fmt.Sprintf(
		`{"iss":%q,"aud":%q,"sub":"botframework","serviceurl":%q,"iat":%d,"exp":%d,"nbf":%d}`,
		claims.Issuer, claims.Audience, claims.ServiceURL,
		claims.IssuedAt.Unix(), claims.Expiry.Unix(), claims.NotBefore.Unix(),
	)
	payload := base64.RawURLEncoding.EncodeToString([]byte(body))
	return header + "." + payload + "."
}

// hs256WithPublicKey signs claims with HS256 using pub's DER encoding as the
// HMAC secret -- the classic RS256-to-HS256 algorithm-confusion attack, since
// an RSA public key is not a secret at all; it is published in the JWKS.
func hs256WithPublicKey(t *testing.T, pub *rsa.PublicKey, kid string, claims botTokenClaims) string {
	t.Helper()
	secret, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: secret},
		(&jose.SignerOptions{}).WithHeader("kid", kid),
	)
	if err != nil {
		t.Fatalf("new HS256 signer: %v", err)
	}
	body := fmt.Sprintf(
		`{"iss":%q,"aud":%q,"sub":"botframework","serviceurl":%q,"iat":%d,"exp":%d,"nbf":%d}`,
		claims.Issuer, claims.Audience, claims.ServiceURL,
		claims.IssuedAt.Unix(), claims.Expiry.Unix(), claims.NotBefore.Unix(),
	)
	jws, err := signer.Sign([]byte(body))
	if err != nil {
		t.Fatalf("HS256 sign: %v", err)
	}
	compact, err := jws.CompactSerialize()
	if err != nil {
		t.Fatalf("HS256 compact serialize: %v", err)
	}
	return compact
}

func TestJWTKeyIDRejectsMissingKid(t *testing.T) {
	t.Parallel()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	token := header + ".e30." + "sig"
	// go-oidc's RemoteKeySet matches EVERY cached key when a JWT carries no
	// kid at all; refusing that up front is what jwtKeyID's own empty-kid
	// check exists for.
	if _, err := jwtKeyID(token); err == nil {
		t.Error("jwtKeyID with no kid = nil error, want an error")
	}
}

func TestParseBearerEmptyTokenAfterTrim(t *testing.T) {
	t.Parallel()

	for _, header := range []string{"Bearer ", "Bearer    "} {
		if _, status := parseBearer(header); status != bearerMalformed {
			t.Errorf("parseBearer(%q) status = %v, want bearerMalformed", header, status)
		}
	}
}

func TestParseBearerCaseInsensitiveScheme(t *testing.T) {
	t.Parallel()

	for _, header := range []string{"bearer token", "BEARER token", "BeArEr token"} {
		token, status := parseBearer(header)
		if status != bearerOK || token != "token" {
			t.Errorf("parseBearer(%q) = (%q, %v), want (\"token\", bearerOK)", header, token, status)
		}
	}
}

// countingKeySet is a minimal oidc.KeySet that counts every call it receives
// and answers according to which kids it was told are valid -- standing in
// for the network fetch a real RemoteKeySet would make, without needing one.
type countingKeySet struct {
	mu    sync.Mutex
	calls int
	valid map[string]bool
}

func (c *countingKeySet) VerifySignature(_ context.Context, jwt string) ([]byte, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	kid, err := jwtKeyID(jwt)
	if err != nil {
		return nil, err
	}
	if c.valid[kid] {
		return []byte("payload"), nil
	}
	return nil, errors.New("unknown kid")
}

func (c *countingKeySet) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// fakeJWT builds a syntactically shaped compact JWT carrying kid in its
// header. jwtKeyID -- which both the real code and countingKeySet use to read
// the kid -- only ever looks at the header, so the payload and signature
// segments can be inert.
func fakeJWT(kid string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"alg":"RS256","kid":%q}`, kid)))
	return header + ".e30.sig"
}

// TestRateLimitedKeySetFloorsUnrecognisedKidFetches is item 1's core
// assertion: a flood of distinct, attacker-chosen kids must cost this process
// far fewer upstream fetches than there are requests, not one each.
func TestRateLimitedKeySetFloorsUnrecognisedKidFetches(t *testing.T) {
	t.Parallel()

	inner := &countingKeySet{valid: map[string]bool{}}
	wrapped := newRateLimitedKeySet(inner, time.Hour, 1)

	const attempts = 50
	for i := range attempts {
		_, _ = wrapped.VerifySignature(context.Background(), fakeJWT(fmt.Sprintf("bogus-%d", i)))
	}

	if got := inner.callCount(); got != 1 {
		t.Errorf("inner calls = %d, want exactly 1 for %d distinct bogus kids", got, attempts)
	}
}

// TestRateLimitedKeySetPicksUpRotationAfterFloor is item 1's other half: the
// floor must not be permanent. A genuine new kid still gets through once the
// floor window rolls over, and a second unfamiliar kid arriving inside the
// same window (with burst already spent) is refused without reaching inner.
func TestRateLimitedKeySetPicksUpRotationAfterFloor(t *testing.T) {
	t.Parallel()

	const floor = 50 * time.Millisecond
	inner := &countingKeySet{valid: map[string]bool{"old-kid": true}}
	wrapped := newRateLimitedKeySet(inner, floor, 1)

	if _, err := wrapped.VerifySignature(context.Background(), fakeJWT("old-kid")); err != nil {
		t.Fatalf("verify old-kid: %v", err)
	}

	if _, err := wrapped.VerifySignature(context.Background(), fakeJWT("too-soon")); err == nil {
		t.Error("a second unrecognised kid inside the same floor window (burst 1) was allowed through")
	}

	// A real rotation: a new kid appears server-side.
	inner.mu.Lock()
	inner.valid["new-kid"] = true
	inner.mu.Unlock()

	time.Sleep(floor + 20*time.Millisecond)

	if _, err := wrapped.VerifySignature(context.Background(), fakeJWT("new-kid")); err != nil {
		t.Errorf("rotation not picked up once the floor window rolled over: %v", err)
	}
	if got := inner.callCount(); got != 2 {
		t.Errorf("inner calls = %d, want 2 (old-kid and new-kid; too-soon never reached inner)", got)
	}
}

// TestBotMessagesMetadataUnreachableFailsClosed covers the previously-untested
// 502 branch: an unreachable trust anchor must refuse the activity, not fall
// through to processing it unauthenticated.
func TestBotMessagesMetadataUnreachableFailsClosed(t *testing.T) {
	t.Parallel()

	badMeta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(badMeta.Close)

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot: config.BotConfig{
			TenantID: "tenant-1", ClientID: "bot-client-id", ClientSecret: "secret",
			TenantType: "single", TimeoutSec: 10, MetadataURL: badMeta.URL,
		},
	}
	srv, err := NewServer(cfg, newFakeStore(), &fakeMessenger{}, nil, newRecordingTelemetry())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := postBotMessage(srv.Handler, marshalActivity(t, botActivityFields()), "Bearer some.bearer.token")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502: an unreachable metadata document must fail closed", rec.Code)
	}
}

// TestBotMessagesEndorsementsUnreachableFailsClosed isolates the other half of
// the same branch: the metadata document and the key used to sign the token
// are both fine -- go-oidc's own fetch succeeds -- but the separate
// endorsements fetch this service makes against the same JWKS URL fails. That
// must also refuse the activity, not treat a verified signature as enough.
func TestBotMessagesEndorsementsUnreachableFailsClosed(t *testing.T) {
	t.Parallel()

	key := generateBotKey(t, "flaky-key", "msteams")
	idp := &botIDP{keys: map[string]botIDPKey{key.kid: key}}

	var keyCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":   idp.server.URL,
			"jwks_uri": idp.server.URL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		// The first request is go-oidc's own signature-verification fetch;
		// only the second and later ones -- this service's separate
		// endorsements fetch -- are made to fail.
		if keyCalls.Add(1) > 1 {
			http.Error(w, "unreachable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(idp.jwks(t))
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)

	const clientID = "bot-client-id"
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     botTestConfig(idp, clientID),
	}
	srv, err := NewServer(cfg, newFakeStore(), &fakeMessenger{}, nil, newRecordingTelemetry())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	activity := botActivityFields()
	now := time.Now()
	token := signBotToken(t, key, botTokenClaims{
		Issuer: idp.server.URL, Audience: clientID, ServiceURL: activity["serviceUrl"].(string),
		IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
	})

	rec := postBotMessage(srv.Handler, marshalActivity(t, activity), "Bearer "+token)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502: an unreachable endorsements fetch must fail closed, not authenticate the activity", rec.Code)
	}
}

// TestBotAuthEndorsementsRefetchesAfterTTLExpiry exercises endorsementCacheTTL
// directly: a cache old enough to be stale must trigger exactly one refetch,
// not be trusted forever.
func TestBotAuthEndorsementsRefetchesAfterTTLExpiry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[{"kid":"k1","endorsements":["msteams"]}]}`))
	}))
	t.Cleanup(srv.Close)

	a := newBotAuthenticator(config.BotConfig{})
	a.endorsements = map[string]keyEndorsement{"k1": {channels: []string{"msteams"}}}
	a.endFetchedAt = time.Now().Add(-endorsementCacheTTL - time.Minute)

	ok, err := a.endorses(context.Background(), srv.URL, "k1", "msteams")
	if err != nil {
		t.Fatalf("endorses: %v", err)
	}
	if !ok {
		t.Error("endorses = false, want true")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("jwks fetches = %d, want exactly 1: a stale cache must trigger a refetch", got)
	}
}

// TestBotAuthEndorsementsUnknownKidTriggersOneRefetch is the key-rotation test
// item 5 asks for: a kid this process has never seen must trigger exactly one
// refetch, and be found once the refetch reflects the rotation.
func TestBotAuthEndorsementsUnknownKidTriggersOneRefetch(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			// Before the rotation: only the original key is published.
			_, _ = w.Write([]byte(`{"keys":[{"kid":"k1","endorsements":["msteams"]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"keys":[{"kid":"k1","endorsements":["msteams"]},{"kid":"k2","endorsements":["msteams"]}]}`))
	}))
	t.Cleanup(srv.Close)

	a := newBotAuthenticator(config.BotConfig{})

	if _, err := a.endorses(context.Background(), srv.URL, "k1", "msteams"); err != nil {
		t.Fatalf("endorses(k1): %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("jwks fetches after the first lookup = %d, want 1", got)
	}

	ok, err := a.endorses(context.Background(), srv.URL, "k2", "msteams")
	if err != nil {
		t.Fatalf("endorses(k2): %v", err)
	}
	if !ok {
		t.Error("endorses(k2) = false, want true: an unrecognised kid must trigger a refetch")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("jwks fetches after the rotation = %d, want exactly 2 (no double-fetch for the same unknown kid)", got)
	}
}

// A key carrying no "endorsements" member speaks for no channel, which is the
// fail-closed reading. Checked against the live document on 2026-09-17: all 220
// keys at login.botframework.com/v1/.well-known/keys carry the member, none of
// them empty, and 74 endorse msteams. The absent case is therefore not
// something Microsoft serves, and reading it as "valid for every channel" could
// only ever weaken the check the spec calls mandatory.
func TestBotMessagesKeyWithNoEndorsementsMemberIsRefused(t *testing.T) {
	t.Parallel()

	key := generateBotKey(t, "universal-key", "msteams")
	key.noEndorsements = true
	idp := newBotIDP(t, key)
	const clientID = "bot-client-id"

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     botTestConfig(idp, clientID),
	}
	srv, err := NewServer(cfg, newFakeStore(), &fakeMessenger{}, nil, newRecordingTelemetry())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	activity := botActivityFields()
	now := time.Now()
	token := signBotToken(t, key, botTokenClaims{
		Issuer: idp.server.URL, Audience: clientID, ServiceURL: activity["serviceUrl"].(string),
		IssuedAt: now.Add(-time.Minute), Expiry: now.Add(5 * time.Minute), NotBefore: now.Add(-time.Minute),
	})

	rec := postBotMessage(srv.Handler, marshalActivity(t, activity), "Bearer "+token)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403: a key with no endorsements member speaks for no channel", rec.Code)
	}
}
