package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BrokerClient asks Microsoft Graph for what one signed-in admin can see --
// their own Teams and channels -- rather than the tenant-wide list Client
// asks for with its own app-only credential. It has no Entra token of its own
// to refresh, only a bearer header set per request from whatever Entra token
// Keycloak's broker endpoint handed back. See ADR 0037 and ADR 0092.
//
// It lives in this package rather than internal/httpserver so it can share
// Team, Channel and the transport instrumentation Client already declares,
// instead of duplicating either.
type BrokerClient struct {
	httpClient   *http.Client
	graphBaseURL string
	// Teamster's own Keycloak client, which authenticates the V2 broker call.
	clientID     string
	clientSecret string
}

// NewBrokerClient takes the transport instrumentation Client's constructor
// does, so a Graph call made on an admin's behalf is measured the
// same way as one made with the service's own credential.
func NewBrokerClient(graphBaseURL, clientID, clientSecret string, tel transportInstrumentation, timeout time.Duration) *BrokerClient {
	return &BrokerClient{
		httpClient: &http.Client{
			Transport: tel.ClientTransport(http.DefaultTransport),
			Timeout:   timeout,
		},
		graphBaseURL: graphBaseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

// EntraToken asks Keycloak's broker endpoint for the Entra token stored
// against keycloakAccessToken's federated login. Keycloak refreshes it
// upstream itself if needed; Teamster never talks to Entra's own token
// endpoint for this.
//
// Keycloak enables exactly one Identity Brokering API version, so this tries
// V2 first and falls back to V1 when Keycloak refuses it (ADR 0092). V2 only
// admits a confidential client, so without a client secret it goes straight to V1.
func (b *BrokerClient) EntraToken(ctx context.Context, issuer, alias, keycloakAccessToken string) (string, error) {
	endpoint := strings.TrimSuffix(issuer, "/") + "/broker/" + url.PathEscape(alias) + "/token"

	if b.clientSecret == "" {
		return b.entraTokenV1(ctx, endpoint, keycloakAccessToken)
	}
	token, errV2 := b.entraTokenV2(ctx, endpoint, keycloakAccessToken)
	if errV2 == nil {
		return token, nil
	}
	// No HTTP answer at all: V1 would only wait out the same timeout.
	var transportErr *url.Error
	if errors.As(errV2, &transportErr) {
		return "", errV2
	}
	token, errV1 := b.entraTokenV1(ctx, endpoint, keycloakAccessToken)
	if errV1 != nil {
		return "", fmt.Errorf("identity brokering API v2: %w; v1: %w", errV2, errV1)
	}
	return token, nil
}

// entraTokenV2 authenticates as Teamster's own client and names the user by
// the token form parameter.
func (b *BrokerClient) entraTokenV2(ctx context.Context, endpoint, keycloakAccessToken string) (string, error) {
	form := url.Values{
		"client_id":     {b.clientID},
		"client_secret": {b.clientSecret},
		"token":         {keycloakAccessToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("new broker token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return b.brokerToken(req)
}

// entraTokenV1 authenticates as the user, whose token needs the broker
// client's read-token role.
func (b *BrokerClient) entraTokenV1(ctx context.Context, endpoint, keycloakAccessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("new broker token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+keycloakAccessToken)
	return b.brokerToken(req)
}

// brokerToken sends req and reads the top-level access_token both versions
// answer with, in Keycloak's token-endpoint-shaped JSON.
func (b *BrokerClient) brokerToken(req *http.Request) (string, error) {
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
