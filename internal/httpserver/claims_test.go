package httpserver

import (
	"encoding/json"
	"testing"
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

func TestAllowedByClaim(t *testing.T) {
	t.Parallel()

	claims := keycloakClaims(t)

	tests := []struct {
		name    string
		path    string
		allowed []string
		want    bool
	}{
		{name: "the realm role we grant on", path: "realm_access.roles", allowed: []string{"admin"}, want: true},
		{name: "one of several accepted values", path: "realm_access.roles", allowed: []string{"nope", "admin"}, want: true},
		{name: "a client role at its own path", path: "resource_access.teamster.roles", allowed: []string{"operator"}, want: true},
		{name: "a client role is not a realm role", path: "realm_access.roles", allowed: []string{"operator"}},
		{name: "authenticated but unauthorised", path: "realm_access.roles", allowed: []string{"superuser"}},
		{name: "no accepted values at all", path: "realm_access.roles"},
		{name: "claim that does not exist", path: "does.not.exist", allowed: []string{"admin"}},
		{name: "values are matched exactly, not by prefix", path: "groups", allowed: []string{"ops"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := allowedByClaim(claims, tt.path, tt.allowed); got != tt.want {
				t.Errorf("allowedByClaim(%q, %v) = %v, want %v", tt.path, tt.allowed, got, tt.want)
			}
		})
	}
}
