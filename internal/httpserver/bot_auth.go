package httpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// botBodyLimit bounds an inbound activity. Neither webhook has one -- their
// sender authenticates with a shared secret -- but this endpoint is reachable
// by anyone who can forge Microsoft's signature, or by nobody at all before
// they even try, and either way there is no reason to read past a normal
// activity's size.
const botBodyLimit = 256 << 10

// maxBotMetadataBytes and botHTTPTimeout mirror maxDiscoveryBytes and
// discoveryClient in oidc.go: this is the same kind of document, fetched the
// same defensive way, for a second, unrelated identity provider.
const maxBotMetadataBytes = 1 << 20

// botHTTPTimeout is a const, not a var: package-level mutable state per AGENTS.md.
const botHTTPTimeout = 15 * time.Second

// endorsementCacheTTL is the ceiling on how long a key's endorsements are
// trusted before a refetch, independent of whether its kid was recognised.
// Bot Framework rotates keys occasionally; this bounds how stale an
// endorsement list may be even for a kid this process has seen before.
const endorsementCacheTTL = 24 * time.Hour

// negativeCacheTTL bounds how often a failed metadata or endorsements fetch is
// retried, so a blackholed IdP costs one botHTTPTimeout every this often
// rather than one per inbound request.
const negativeCacheTTL = 30 * time.Second

// keyFetchFloor and keyFetchBurst bound how often an unrecognised kid may
// trigger the underlying RemoteKeySet's own remote fetch: go-oidc
// singleflights concurrent callers but never rate-limits sequential ones, so
// an attacker cycling a fresh kid on every request would otherwise force one
// outbound GET per request, throttling this process's real calls to Microsoft
// along with it. The allowance is a burst per floor window, not one kid ever,
// because Microsoft keeps more than one key valid at once during a rotation;
// a bare "one new kid per floor" would treat the second, equally legitimate
// key of an ordinary overlap as if it were the attack this exists to stop.
const (
	keyFetchFloor = 5 * time.Minute
	keyFetchBurst = 4
)

// botConfigured is config.BotConfig.Configured, named for its call sites
// here: a deployment that never set bot-client-id, bot-client-secret or
// bot-metadata-url gets no inbound route and none of the notifications UI
// either, so it exposes no unauthenticated path reachable from the internet
// and offers nobody a page instructing them to talk to a bot that was never
// registered.
func botConfigured(cfg config.BotConfig) bool {
	return cfg.Configured()
}

// botMetadata is the two fields this service needs out of the Bot Framework's
// OpenID configuration document. Unlike oidc.ProviderConfig's, the document
// advertises no authorization_endpoint or token_endpoint -- this bot never
// signs a user in -- so fetchProviderConfig's required-field loop would reject
// it, and a sibling that asks for less is simpler than teaching that one to
// want less.
type botMetadata struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

// fetchBotMetadata reads the document at the configured URL, which is the
// trust anchor: the issuer and jwks_uri verification actually runs against
// come from here, not from a value this service invents, the same reasoning
// fetchProviderConfig documents for the admin login's identity provider.
func fetchBotMetadata(ctx context.Context, client *http.Client, metadataURL string) (*botMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("bot metadata request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", metadataURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: %s", metadataURL, resp.Status)
	}

	var metadata botMetadata
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBotMetadataBytes)).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("decode %s: %w", metadataURL, err)
	}
	if metadata.Issuer == "" {
		return nil, fmt.Errorf("%s advertises no issuer", metadataURL)
	}
	if metadata.JWKSURI == "" {
		return nil, fmt.Errorf("%s advertises no jwks_uri", metadataURL)
	}
	return &metadata, nil
}

// botAuthenticator verifies an inbound activity against the Bot Framework
// authentication spec: a bearer JWT that Microsoft signed, checked against
// Microsoft's own metadata document -- never against any of teamster's own
// credentials. It is a struct field on Server rather than a package-level
// variable so its cached state belongs to one server instance, per AGENTS.md.
type botAuthenticator struct {
	cfg    config.BotConfig
	client *http.Client

	mu               sync.Mutex
	metadata         *botMetadata
	verifier         *oidc.IDTokenVerifier
	metadataErr      error
	metadataErrAt    time.Time
	metadataInFlight chan struct{}

	endMu        sync.Mutex
	endorsements map[string]keyEndorsement
	endFetchedAt time.Time
	endErr       error
	endErrAt     time.Time
	endInFlight  chan struct{}
}

func newBotAuthenticator(cfg config.BotConfig) *botAuthenticator {
	return &botAuthenticator{
		cfg:    cfg,
		client: &http.Client{Timeout: botHTTPTimeout},
	}
}

// verifierFor discovers the metadata document and builds the verifier at most
// once, then memoises it. The lock is never held across fetchBotMetadata: a
// mutex ignores ctx, so holding it across a 15s call would let disconnected
// clients pile up goroutines that cannot drain until the holder's fetch ends
// (AGENTS.md). Concurrent callers on a cold cache instead wait on
// metadataInFlight -- one goroutine fetches, the rest block on a channel that
// does respect ctx -- and a failed fetch is remembered for negativeCacheTTL so
// a blackholed IdP does not mean one full timeout per request.
func (a *botAuthenticator) verifierFor(ctx context.Context) (*oidc.IDTokenVerifier, *botMetadata, error) {
	a.mu.Lock()
	if a.verifier != nil {
		verifier, metadata := a.verifier, a.metadata
		a.mu.Unlock()
		return verifier, metadata, nil
	}
	if a.metadataInFlight != nil {
		done := a.metadataInFlight
		a.mu.Unlock()
		select {
		case <-done:
			return a.verifierFor(ctx)
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	if a.metadataErr != nil && time.Since(a.metadataErrAt) < negativeCacheTTL {
		err := a.metadataErr
		a.mu.Unlock()
		return nil, nil, err
	}
	done := make(chan struct{})
	a.metadataInFlight = done
	a.mu.Unlock()

	metadata, err := fetchBotMetadata(ctx, a.client, a.cfg.MetadataURL)

	a.mu.Lock()
	a.metadataInFlight = nil
	if err != nil {
		a.metadataErr, a.metadataErrAt = err, time.Now()
		a.mu.Unlock()
		close(done)
		return nil, nil, err
	}

	// A bag-of-values context, not the request's: updateKeys otherwise falls
	// back to http.DefaultClient with no timeout, and the request's own
	// deadline -- Server.WriteTimeout -- is not something the context of an
	// inbound request carries, so a hung metadata host would park the
	// goroutine that discovered the unknown kid forever.
	keySetCtx := oidc.ClientContext(context.Background(), a.client)
	keySet := oidc.NewRemoteKeySet(keySetCtx, metadata.JWKSURI)

	verifier := oidc.NewVerifier(metadata.Issuer, newRateLimitedKeySet(keySet, keyFetchFloor, keyFetchBurst), &oidc.Config{
		ClientID:             a.cfg.ClientID,
		SupportedSigningAlgs: []string{oidc.RS256},
		// go-oidc gives exp zero leeway -- its five-minute leeway applies only
		// to nbf -- and Microsoft's spec calls for five minutes of clock skew
		// on both. Backdating Now covers exp; the side effect is that nbf
		// tolerates ten minutes rather than five, which is the accepted cost
		// of the one knob go-oidc exposes.
		Now: func() time.Time { return time.Now().Add(-5 * time.Minute) },
	})

	a.metadata, a.verifier, a.metadataErr = metadata, verifier, nil
	a.mu.Unlock()
	close(done)
	return verifier, metadata, nil
}

// rawJWKS is the shape this service reads out of the JWKS document for
// endorsements. go-oidc's key set parses the keys themselves but drops this
// field, because it is a Bot Framework extension rather than part of the JWKS
// standard it implements. Endorsements is a pointer so a member that is
// absent from the JSON -- which most of Microsoft's real keys are -- can be
// told apart from one present with an empty list.
type rawJWKS struct {
	Keys []struct {
		Kid          string    `json:"kid"`
		Endorsements *[]string `json:"endorsements"`
	} `json:"keys"`
}

// keyEndorsement is one signing key's entry in the endorsements cache: the
// channels that key is allowed to speak for.
//
// A key carrying no "endorsements" member endorses nothing. That is the
// fail-closed reading, and it costs nothing in practice: the live document at
// login.botframework.com/v1/.well-known/keys carries the member on every one
// of its keys, so the absent case is not something Microsoft actually serves.
// Treating absent as "valid for every channel" would turn the one check the
// spec calls mandatory into a no-op for any key that ever showed up without it.
type keyEndorsement struct {
	channels []string
}

// endorses reports whether channelID is one of the channels this key may sign
// for. A key with no channels endorses none of them.
func (e keyEndorsement) endorses(channelID string) bool {
	for _, c := range e.channels {
		if c == channelID {
			return true
		}
	}
	return false
}

// fetchEndorsements reads the raw JWKS document and returns kid -> that key's
// endorsement.
func fetchEndorsements(ctx context.Context, client *http.Client, jwksURI string) (map[string]keyEndorsement, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, fmt.Errorf("jwks request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", jwksURI, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: %s", jwksURI, resp.Status)
	}

	var parsed rawJWKS
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBotMetadataBytes)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode %s: %w", jwksURI, err)
	}

	out := make(map[string]keyEndorsement, len(parsed.Keys))
	for _, key := range parsed.Keys {
		// A nil member and an explicit empty list mean the same thing here:
		// this key speaks for no channel. See keyEndorsement.
		if key.Endorsements == nil {
			out[key.Kid] = keyEndorsement{}
			continue
		}
		out[key.Kid] = keyEndorsement{channels: *key.Endorsements}
	}
	return out, nil
}

// endorses reports whether the key identified by kid endorses channelID. An
// unrecognised kid triggers one refetch before this says no -- skipped when
// endorsementsFor's own call already fetched, so a cold cache does not pay for
// the same fetch twice in a row for nothing. The cache still expires on its
// own after endorsementCacheTTL so a key that is rotated out loses its
// endorsement eventually even if its kid is never asked about again.
func (a *botAuthenticator) endorses(ctx context.Context, jwksURI, kid, channelID string) (bool, error) {
	endorsements, fetched, err := a.endorsementsFor(ctx, jwksURI, false)
	if err != nil {
		return false, err
	}

	entry, known := endorsements[kid]
	if !known && !fetched {
		endorsements, _, err = a.endorsementsFor(ctx, jwksURI, true)
		if err != nil {
			return false, err
		}
		entry, known = endorsements[kid]
	}
	if !known {
		return false, nil
	}
	return entry.endorses(channelID), nil
}

// endorsementsFor returns the cached endorsements map, refetching when it is
// cold, stale past endorsementCacheTTL, or force is set. fetched reports
// whether this call itself went to the network, which is what lets endorses
// above skip a second attempt for the same answer. The lock is never held
// across fetchEndorsements (AGENTS.md); concurrent cache-miss callers wait on
// endInFlight instead of each starting their own fetch, and a failure is
// remembered for negativeCacheTTL rather than retried on every request.
func (a *botAuthenticator) endorsementsFor(ctx context.Context, jwksURI string, force bool) (map[string]keyEndorsement, bool, error) {
	a.endMu.Lock()
	stale := a.endorsements == nil || time.Since(a.endFetchedAt) > endorsementCacheTTL
	if !force && !stale {
		endorsements := a.endorsements
		a.endMu.Unlock()
		return endorsements, false, nil
	}
	if a.endInFlight != nil {
		done := a.endInFlight
		a.endMu.Unlock()
		select {
		case <-done:
			return a.endorsementsFor(ctx, jwksURI, force)
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	if a.endErr != nil && time.Since(a.endErrAt) < negativeCacheTTL {
		err := a.endErr
		a.endMu.Unlock()
		return nil, false, err
	}
	done := make(chan struct{})
	a.endInFlight = done
	a.endMu.Unlock()

	fresh, err := fetchEndorsements(ctx, a.client, jwksURI)

	a.endMu.Lock()
	a.endInFlight = nil
	if err != nil {
		a.endErr, a.endErrAt = err, time.Now()
		a.endMu.Unlock()
		close(done)
		return nil, false, err
	}
	a.endorsements, a.endFetchedAt, a.endErr = fresh, time.Now(), nil
	a.endMu.Unlock()
	close(done)
	return fresh, true, nil
}

// bearerStatus distinguishes an absent Authorization header from a malformed
// one, which the metrics need to count separately -- "nobody is sending us
// anything" and "something is sending us garbage" are different incidents.
type bearerStatus int

const (
	bearerOK bearerStatus = iota
	bearerMissing
	bearerMalformed
)

func parseBearer(header string) (string, bearerStatus) {
	if header == "" {
		return "", bearerMissing
	}
	const prefix = "Bearer "
	// RFC 7235 makes the scheme name case-insensitive; HasPrefix would not.
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", bearerMalformed
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", bearerMalformed
	}
	return token, bearerOK
}

// jwtKeyID reads the kid out of a compact JWT's header without verifying
// anything -- the same "decode, do not trust" reasoning accessTokenClaims in
// oidc.go documents. It is used only to look the key up in the endorsements
// map after the signature over the whole token has already been checked.
func jwtKeyID(rawToken string) (string, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return "", errors.New("malformed token")
	}

	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode header: %w", err)
	}

	var parsed struct {
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(header, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal header: %w", err)
	}
	if parsed.Kid == "" {
		return "", errors.New("token header carries no kid")
	}
	return parsed.Kid, nil
}

// trimOneTrailingSlash removes exactly one trailing slash, unlike
// strings.TrimRight which would strip every one. A real Teams service URL
// carries at most one, and stopping at one is what makes the comparison exact
// rather than approximate.
func trimOneTrailingSlash(s string) string {
	return strings.TrimSuffix(s, "/")
}

// botClaims is the one Bot Framework claim this service checks itself; issuer,
// audience and expiry are the verifier's job. serviceurl is lowercase on the
// wire, unlike the activity body's ServiceURL.
type botClaims struct {
	ServiceURL string `json:"serviceurl"`
}

// verifyBotToken runs the whole chain the spec requires for one bearer token:
// signature, issuer and audience through the verifier, then the serviceurl
// claim against the activity, then the channel endorsement of the key that
// signed it. The status strings are what handleBotMessages counts under
// metrics.WebhookReceived, each distinct so a dashboard can tell "nobody sent
// a token" apart from "a token failed to verify" apart from "the key does not
// speak for this channel".
func (s *Server) verifyBotToken(ctx context.Context, rawToken, activityServiceURL, channelID string) (status string, ok bool, forbidden bool) {
	verifier, metadata, err := s.botAuth.verifierFor(ctx)
	if err != nil {
		logError("bot metadata discovery", err)
		return "metadata-unreachable", false, false
	}

	idToken, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		logError("bot token verify", err)
		return "invalid-token", false, false
	}

	var claims botClaims
	if err := idToken.Claims(&claims); err != nil {
		logError("bot token claims", err)
		return "invalid-token", false, false
	}

	// An absent claim must not compare equal to an absent body field: "" == ""
	// would make the one claim-to-body binding this design has a no-op for any
	// token that simply omits serviceurl.
	if claims.ServiceURL == "" {
		return "serviceurl-mismatch", false, false
	}
	// Never HasPrefix or Contains: ServiceURL is concatenated into the outbound
	// URL that carries our Bot Connector bearer token, so a prefix match would
	// happily send that token to
	// "https://smba.trafficmanager.net.attacker.example/".
	if trimOneTrailingSlash(claims.ServiceURL) != trimOneTrailingSlash(activityServiceURL) {
		return "serviceurl-mismatch", false, false
	}

	kid, err := jwtKeyID(rawToken)
	if err != nil {
		logError("bot token kid", err)
		return "invalid-token", false, false
	}

	endorsed, err := s.botAuth.endorses(ctx, metadata.JWKSURI, kid, channelID)
	if err != nil {
		logError("bot endorsements fetch", err)
		return "endorsements-unreachable", false, false
	}
	if !endorsed {
		return "endorsement-refused", false, true
	}

	return "accepted", true, false
}

// rateLimitedKeySet wraps an oidc.KeySet -- in practice always a
// *oidc.RemoteKeySet -- with a cap on how many kids this process has not
// already verified may reach the inner set's own fetch-on-unknown-kid logic
// within one floor window. go-oidc singleflights concurrent lookups for the
// same moment in time but applies no cooldown between sequential ones, so
// without this an attacker sending a fresh, syntactically valid kid on every
// request forces one outbound JWKS fetch per request -- enough to get this
// process throttled by Microsoft, taking genuine traffic down with it. A kid
// that verifies is remembered so real traffic never pays the cap again; the
// cap is a burst per window rather than a single one-ever attempt because
// Microsoft keeps more than one key valid at once during a rotation, and two
// genuinely different, simultaneously valid kids must not be treated as the
// flood this defends against.
type rateLimitedKeySet struct {
	inner oidc.KeySet
	floor time.Duration
	burst int

	mu          sync.Mutex
	knownKids   map[string]struct{}
	windowStart time.Time
	windowCount int
}

func newRateLimitedKeySet(inner oidc.KeySet, floor time.Duration, burst int) *rateLimitedKeySet {
	return &rateLimitedKeySet{inner: inner, floor: floor, burst: burst, knownKids: map[string]struct{}{}}
}

// VerifySignature implements oidc.KeySet. It never itself makes a network
// call -- that stays inner's job -- so the rate-limiting decision below only
// ever costs a header decode and a map lookup, not a fetch.
func (k *rateLimitedKeySet) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	// jwtKeyID also rejects a missing kid; folding both cases into one bucket
	// (kid == "") is fine here because neither can ever be "known".
	kid, kidErr := jwtKeyID(jwt)
	if kidErr != nil {
		kid = ""
	}

	k.mu.Lock()
	_, known := k.knownKids[kid]
	if !known {
		if k.windowStart.IsZero() || time.Since(k.windowStart) >= k.floor {
			k.windowStart, k.windowCount = time.Now(), 0
		}
		if k.windowCount >= k.burst {
			k.mu.Unlock()
			return nil, fmt.Errorf("bot key set: kid %q unrecognised, and this floor window already spent its %d attempts", kid, k.burst)
		}
		k.windowCount++
	}
	k.mu.Unlock()

	payload, err := k.inner.VerifySignature(ctx, jwt)
	if err == nil && kidErr == nil {
		k.mu.Lock()
		k.knownKids[kid] = struct{}{}
		k.mu.Unlock()
	}
	return payload, err
}
