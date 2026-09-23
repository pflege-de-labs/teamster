package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BrokerClient asks Microsoft Graph for what one signed-in admin can see --
// their own Teams and channels -- rather than the tenant-wide list Client
// asks for with its own app-only credential. It is unrelated to Client's
// oauth2.Config: there is no token of its own to refresh here, only a bearer
// header set per request from whatever Entra token the caller already
// obtained through Keycloak's broker endpoint. See ADR 0037.
//
// It lives in this package rather than internal/httpserver so it can share
// Team, Channel and the unexported instrumentation interface Client already
// declares, instead of duplicating either.
type BrokerClient struct {
	httpClient   *http.Client
	graphBaseURL string
}

// NewBrokerClient takes the same instrumentation interface Client's
// constructor does, so a Graph call made on an admin's behalf is measured the
// same way as one made with the service's own credential.
func NewBrokerClient(graphBaseURL string, tel instrumentation, timeout time.Duration) *BrokerClient {
	return &BrokerClient{
		httpClient: &http.Client{
			Transport: tel.ClientTransport(http.DefaultTransport),
			Timeout:   timeout,
		},
		graphBaseURL: graphBaseURL,
	}
}

// EntraToken asks Keycloak's broker endpoint for the Entra token stored
// against keycloakAccessToken's federated login. Keycloak refreshes it
// upstream itself if needed; Teamster never talks to Entra's own token
// endpoint for this. The response is Keycloak's token-endpoint-shaped JSON
// wrapping the stored provider token, so this only ever looks at the
// top-level access_token field.
func (b *BrokerClient) EntraToken(ctx context.Context, issuer, alias, keycloakAccessToken string) (string, error) {
	endpoint := strings.TrimSuffix(issuer, "/") + "/broker/" + url.PathEscape(alias) + "/token"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("new broker token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+keycloakAccessToken)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("broker token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read broker token response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("broker token request failed: %s", string(body))
	}

	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode broker token response: %w", err)
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("broker token response carried no access_token")
	}
	return payload.AccessToken, nil
}

// MyTeams lists the Teams the Entra token's own user has joined, as opposed
// to Client.ListTeams which lists every Team the tenant-wide app-only
// credential can see.
func (b *BrokerClient) MyTeams(ctx context.Context, entraToken string) ([]Team, error) {
	endpoint := fmt.Sprintf("%s/me/joinedTeams?$select=id,displayName", b.graphBaseURL)
	body, err := b.get(ctx, endpoint, entraToken)
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode joined teams: %w", err)
	}

	teams := make([]Team, 0, len(res.Value))
	for _, v := range res.Value {
		teams = append(teams, Team{ID: v.ID, Name: v.DisplayName})
	}
	sortByName(teams, func(t Team) string { return t.Name })
	return teams, nil
}

// MyChannels lists teamID's channels using the Entra token's own bearer
// header, the per-user counterpart of Client.ListChannels.
func (b *BrokerClient) MyChannels(ctx context.Context, entraToken, teamID string) ([]Channel, error) {
	endpoint := fmt.Sprintf("%s/teams/%s/channels?$select=id,displayName", b.graphBaseURL, url.PathEscape(teamID))
	body, err := b.get(ctx, endpoint, entraToken)
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode channels: %w", err)
	}

	channels := make([]Channel, 0, len(res.Value))
	for _, v := range res.Value {
		channels = append(channels, Channel{ID: v.ID, Name: v.DisplayName})
	}
	sortByName(channels, func(c Channel) string { return c.Name })
	return channels, nil
}

func (b *BrokerClient) get(ctx context.Context, endpoint, bearer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("graph GET %s failed: %s", endpoint, string(body))
	}
	return body, nil
}
