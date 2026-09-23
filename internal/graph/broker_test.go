package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewBrokerClient(t *testing.T) {
	t.Parallel()

	client := NewBrokerClient("https://graph.example/v1.0", uninstrumented{}, 5*time.Second)
	if client.graphBaseURL != "https://graph.example/v1.0" {
		t.Errorf("graphBaseURL = %q, want the configured URL", client.graphBaseURL)
	}
	if client.httpClient.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", client.httpClient.Timeout)
	}
}

func TestEntraToken(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"access_token":"entra-token-1","token_type":"bearer","expires_in":300}`))
	}))
	defer srv.Close()

	client := &BrokerClient{httpClient: srv.Client(), graphBaseURL: "unused"}
	token, err := client.EntraToken(context.Background(), srv.URL, "entra-broker", "keycloak-access-token")
	if err != nil {
		t.Fatalf("EntraToken: %v", err)
	}
	if token != "entra-token-1" {
		t.Errorf("token = %q, want entra-token-1", token)
	}
	if gotPath != "/broker/entra-broker/token" {
		t.Errorf("path = %q, want /broker/entra-broker/token", gotPath)
	}
	if gotAuth != "Bearer keycloak-access-token" {
		t.Errorf("Authorization = %q, want the Keycloak access token as bearer", gotAuth)
	}
}

// The issuer as discovered may carry a trailing slash; the alias may need
// escaping. Both must still produce a clean path.
func TestEntraTokenBuildsTheEndpointCarefully(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"access_token":"t"}`))
	}))
	defer srv.Close()

	client := &BrokerClient{httpClient: srv.Client()}
	if _, err := client.EntraToken(context.Background(), srv.URL+"/", "my alias", "kc-token"); err != nil {
		t.Fatalf("EntraToken: %v", err)
	}
	if !strings.Contains(gotPath, "my%20alias") || strings.Contains(gotPath, "//broker") {
		t.Errorf("path = %q, want the alias escaped and no double slash", gotPath)
	}
}

func TestEntraTokenRejectsAFailedRequest(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer srv.Close()

	client := &BrokerClient{httpClient: srv.Client()}
	if _, err := client.EntraToken(context.Background(), srv.URL, "alias", "bad-token"); err == nil {
		t.Error("EntraToken() with a 401 = nil error, want it refused")
	}
}

func TestEntraTokenRejectsAResponseWithoutAnAccessToken(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token_type":"bearer"}`))
	}))
	defer srv.Close()

	client := &BrokerClient{httpClient: srv.Client()}
	if _, err := client.EntraToken(context.Background(), srv.URL, "alias", "token"); err == nil {
		t.Error("EntraToken() with no access_token = nil error, want it refused")
	}
}

func TestMyTeams(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"value":[{"id":"t2","displayName":"Zulu"},{"id":"t1","displayName":"Alpha"}]}`))
	}))
	defer srv.Close()

	client := &BrokerClient{httpClient: srv.Client(), graphBaseURL: srv.URL}
	teams, err := client.MyTeams(context.Background(), "entra-token")
	if err != nil {
		t.Fatalf("MyTeams: %v", err)
	}
	if len(teams) != 2 || teams[0].Name != "Alpha" {
		t.Errorf("teams = %+v, want the two teams sorted by name", teams)
	}
	if gotPath != "/me/joinedTeams" {
		t.Errorf("path = %q, want /me/joinedTeams", gotPath)
	}
	if gotAuth != "Bearer entra-token" {
		t.Errorf("Authorization = %q, want the Entra token as bearer", gotAuth)
	}
}

func TestMyChannels(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"value":[{"id":"c1","displayName":"General"}]}`))
	}))
	defer srv.Close()

	client := &BrokerClient{httpClient: srv.Client(), graphBaseURL: srv.URL}
	channels, err := client.MyChannels(context.Background(), "entra-token", "team one")
	if err != nil {
		t.Fatalf("MyChannels: %v", err)
	}
	if len(channels) != 1 || channels[0].Name != "General" {
		t.Errorf("channels = %+v", channels)
	}
	if !strings.Contains(gotPath, "team%20one") {
		t.Errorf("path = %q, want the team id percent-escaped into it", gotPath)
	}
}

func TestMyTeamsAndMyChannelsReportFailures(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	client := &BrokerClient{httpClient: srv.Client(), graphBaseURL: srv.URL}
	if _, err := client.MyTeams(context.Background(), "token"); err == nil {
		t.Error("MyTeams() on a 403 = nil error, want it refused")
	}
	if _, err := client.MyChannels(context.Background(), "token", "team-1"); err == nil {
		t.Error("MyChannels() on a 403 = nil error, want it refused")
	}
}
