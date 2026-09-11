package httpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
)

// The shapes below are what Keycloak actually emits: realm roles nested under
// realm_access, client roles under resource_access.<client>.
func keycloakClaims(t *testing.T) map[string]any {
	t.Helper()

	const raw = `{
		"sub": "8f2a",
		"preferred_username": "jens",
		"realm_access": {"roles": ["default-roles-internal", "offline_access", "admin"]},
		"resource_access": {"teamster": {"roles": ["operator"]}},
		"groups": ["/ops"],
		"email": "jens@example.com"
	}`

	var claims map[string]any
	if err := json.Unmarshal([]byte(raw), &claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	return claims
}

func TestClaimValuesWalksDottedPaths(t *testing.T) {
	t.Parallel()

	claims := keycloakClaims(t)

	tests := []struct {
		name string
		path string
		want []string
	}{
		{name: "realm roles", path: "realm_access.roles", want: []string{"default-roles-internal", "offline_access", "admin"}},
		{name: "client roles", path: "resource_access.teamster.roles", want: []string{"operator"}},
		{name: "top level list", path: "groups", want: []string{"/ops"}},
		{name: "top level string", path: "preferred_username", want: []string{"jens"}},
		{name: "missing leaf", path: "realm_access.missing"},
		{name: "missing root", path: "nope.roles"},
		{name: "path through a non-object", path: "preferred_username.roles"},
		{name: "path to an object rather than values", path: "realm_access"},
		{name: "client that granted nothing", path: "resource_access.other.roles"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := claimValues(claims, tt.path)
			if len(got) != len(tt.want) {
				t.Fatalf("claimValues(%q) = %v, want %v", tt.path, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("claimValues(%q)[%d] = %q, want %q", tt.path, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// The claim names the role directly: a provider role called "editor" is the
// editor role here, with nothing to configure in between.
func TestRolesFromClaimValues(t *testing.T) {
	t.Parallel()

	claims := keycloakClaims(t)

	tests := []struct {
		name        string
		path        string
		defaultRole authz.Role
		want        []authz.Role
	}{
		{
			// Keycloak's own realm roles ride along beside the one that matters;
			// they reach the policies, where nothing mentions them.
			name: "a realm role named admin", path: "realm_access.roles",
			want: []authz.Role{"default-roles-internal", "offline_access", authz.RoleAdmin},
		},
		{
			// The fixture's client role is named for this deployment rather than
			// for Teamster; it is still handed to the policies, which is what
			// lets a deployment write its own rule for it.
			name: "a client role of the deployment's own", path: "resource_access.teamster.roles",
			want: []authz.Role{"operator"},
		},
		{
			// "operator" is not a Teamster role, so the default still applies
			// beside it.
			name: "a deployment role does not stop the default", path: "resource_access.teamster.roles",
			defaultRole: authz.RoleViewer, want: []authz.Role{"operator", authz.RoleViewer},
		},
		{
			name: "a claim that does not exist falls back", path: "does.not.exist",
			defaultRole: authz.RoleViewer, want: []authz.Role{authz.RoleViewer},
		},
		{name: "a claim that does not exist and no default", path: "does.not.exist", want: nil},
		{
			// A group is not a role, but it is a claim value: if the claim is
			// pointed at groups, the group names are what the policies see.
			name: "whatever the claim carries becomes a role name", path: "groups",
			want: []authz.Role{"/ops"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := authz.RolesFor(claimValues(claims, tt.path), tt.defaultRole)
			if !slices.Equal(got, tt.want) {
				t.Errorf("roles from %q = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// Keycloak's role mappers populate the access token by default and leave the id
// token without roles, so reading only the id token rejects a realm that is
// configured correctly.
func TestAccessTokenClaims(t *testing.T) {
	t.Parallel()

	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"realm_access":{"roles":["admin"]},"resource_access":{"teamster":{"roles":["operator"]}}}`))
	token := "header." + payload + ".signature"

	tests := []struct {
		name  string
		token string
		path  string
		want  string
	}{
		{name: "realm roles", token: token, path: "realm_access.roles", want: "admin"},
		{name: "client roles", token: token, path: "resource_access.teamster.roles", want: "operator"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			values := claimValues(accessTokenClaims(tt.token), tt.path)
			if len(values) != 1 || values[0] != tt.want {
				t.Errorf("claimValues(accessTokenClaims(...), %q) = %v, want [%s]", tt.path, values, tt.want)
			}
		})
	}
}

func TestAccessTokenClaimsRejectsRubbish(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty"},
		{name: "not a jwt", token: "opaque-access-token"},
		{name: "wrong segment count", token: "only.two"},
		{name: "payload is not base64", token: "header.!!!!.signature"},
		{name: "payload is not json", token: "header." + base64.RawURLEncoding.EncodeToString([]byte("nope")) + ".sig"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := accessTokenClaims(tt.token); got != nil {
				t.Errorf("accessTokenClaims(%q) = %v, want nil", tt.token, got)
			}
		})
	}
}

// Roles in the id token are used in preference: the nil provider would panic if
// the lookup carried on to userinfo or the access token.
func TestMembershipPrefersTheIDToken(t *testing.T) {
	t.Parallel()

	server := &Server{cfg: config.Config{Auth: config.AuthConfig{
		Claim: "realm_access.roles",
	}}}

	claims := map[string]any{
		"realm_access": map[string]any{"roles": []any{"viewer", "admin"}},
	}

	values, source := server.membership(context.Background(), nil, &oauth2.Token{}, claims)
	if source != "the id token" {
		t.Errorf("source = %q, want the id token", source)
	}
	if !authz.HasBuiltin(authz.RolesFor(values, "")) {
		t.Errorf("values = %v, want the id token roles", values)
	}
}

// With nothing in the id token the lookup falls through, and an access token
// carrying the role is enough. Keycloak's built-in mappers produce exactly this.
func TestMembershipFallsBackToTheAccessToken(t *testing.T) {
	t.Parallel()

	server := &Server{cfg: config.Config{Auth: config.AuthConfig{
		Claim: "resource_access.teamster.roles",
	}}}

	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"resource_access":{"teamster":{"roles":["admin"]}}}`))
	token := &oauth2.Token{AccessToken: "header." + payload + ".signature"}

	// An empty provider URL makes the userinfo step fail fast rather than dial.
	values, source := server.membership(context.Background(), &oidc.Provider{}, token, map[string]any{})
	if source != "the access token" {
		t.Errorf("source = %q, want the access token", source)
	}
	if !authz.HasBuiltin(authz.RolesFor(values, "")) {
		t.Errorf("values = %v, want the access token roles", values)
	}
}
