package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/pflege-de-labs/teamster/internal/config"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Message is a rendered template on its way to a channel, which the bot posts
// (ADR 0045). Title is the line the Teams activity feed previews, so a message
// without one previews as "Card" and tells a reader nothing. Text is HTML and
// must already be sanitized by the caller.
//
// Cards is a slice because a Teams webhook payload may carry several
// attachments, and a sender being migrated onto this service should not have to
// find that out from a rejected request. A template renders exactly one.
type Message struct {
	Title string
	Text  string
	Cards []json.RawMessage
}

// TokenURL is where this client asks for a token: the configured endpoint, or
// the public Microsoft Entra one for the tenant when none is set. It is
// exported because "which endpoint am I actually authenticating against" is a
// question worth being able to answer from outside this package.
func TokenURL(cfg config.GraphConfig) string {
	if cfg.TokenURL != "" {
		return cfg.TokenURL
	}
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", cfg.TenantID)
}

func scopeOf(cfg config.GraphConfig) string {
	if cfg.Scope != "" {
		return cfg.Scope
	}
	return "https://graph.microsoft.com/.default"
}

// instrumentation wraps the transport this client calls Graph through. It lives
// here rather than in the metrics package so that graph depends on nothing but
// an interface — and so a test can pass one that does nothing.
type instrumentation interface {
	ClientTransport(base http.RoundTripper) http.RoundTripper
}

func NewClient(cfg config.GraphConfig, tel instrumentation) (*Client, error) {
	oauthCfg := clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     TokenURL(cfg),
		Scopes:       []string{scopeOf(cfg)},
	}

	// The instrumented transport goes underneath oauth2's, not around it. A
	// token refresh happens inside oauth2's RoundTrip, so measuring from the
	// outside would charge Entra's latency to Graph once an hour and would
	// report a token endpoint that is down as a Graph failure. Underneath, both
	// are measured and the address tells them apart.
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	base := &http.Client{
		Transport: tel.ClientTransport(http.DefaultTransport),
		Timeout:   timeout,
	}

	// The token source shares this context, so the token request goes through
	// the same instrumented client — and gains the timeout it never had.
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, base)

	httpClient := oauthCfg.Client(ctx)
	httpClient.Timeout = timeout

	return &Client{
		baseURL:    cfg.BaseURL,
		httpClient: httpClient,
	}, nil
}

// maxInstalledAppPages bounds how far HasInstalledApp follows a team's app
// list; a team with more apps than this is answered as not having the bot.
const maxInstalledAppPages = 10

// HasInstalledApp reports whether a Teams app carrying the bot botID -- the
// bot's Entra client id, bots[0].botId in the manifest -- is installed in the
// team. It needs TeamsAppInstallation.ReadForTeam.All.
func (c *Client) HasInstalledApp(teamID, botID string) (bool, error) {
	next := fmt.Sprintf("%s/teams/%s/installedApps?%s", c.baseURL, url.PathEscape(teamID),
		url.Values{"$expand": {"teamsAppDefinition($expand=bot)"}}.Encode())

	for range maxInstalledAppPages {
		resBody, err := c.get(next)
		if err != nil {
			return false, err
		}

		var res struct {
			Value []struct {
				Definition struct {
					Bot *struct {
						ID string `json:"id"`
					} `json:"bot"`
				} `json:"teamsAppDefinition"`
			} `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(resBody, &res); err != nil {
			return false, fmt.Errorf("decode installed apps: %w", err)
		}
		for _, app := range res.Value {
			if bot := app.Definition.Bot; bot != nil && strings.EqualFold(bot.ID, botID) {
				return true, nil
			}
		}

		// The token goes with every request, so only Graph's own links are
		// followed.
		if res.NextLink == "" || !strings.HasPrefix(res.NextLink, c.baseURL+"/") {
			return false, nil
		}
		next = res.NextLink
	}
	return false, nil
}

// Team is the subset of a Microsoft Teams team the admin UI needs.
type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) ListTeams() ([]Team, error) {
	endpoint := fmt.Sprintf("%s/teams?$select=id,displayName&$top=999", c.baseURL)
	resBody, err := c.get(endpoint)
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := json.Unmarshal(resBody, &res); err != nil {
		return nil, fmt.Errorf("decode teams: %w", err)
	}

	teams := make([]Team, 0, len(res.Value))
	for _, v := range res.Value {
		teams = append(teams, Team{ID: v.ID, Name: v.DisplayName})
	}

	sortByName(teams, func(t Team) string { return t.Name })
	return teams, nil
}

func (c *Client) ListChannels(teamID string) ([]Channel, error) {
	endpoint := fmt.Sprintf("%s/teams/%s/channels?$select=id,displayName", c.baseURL, url.PathEscape(teamID))
	resBody, err := c.get(endpoint)
	if err != nil {
		return nil, err
	}

	var res struct {
		Value []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := json.Unmarshal(resBody, &res); err != nil {
		return nil, fmt.Errorf("decode channels: %w", err)
	}

	channels := make([]Channel, 0, len(res.Value))
	for _, v := range res.Value {
		channels = append(channels, Channel{ID: v.ID, Name: v.DisplayName})
	}

	sortByName(channels, func(c Channel) string { return c.Name })
	return channels, nil
}

// Graph returns teams and channels in no useful order, and these end up in a
// dropdown someone has to find a name in. Collation rather than byte order,
// because the latter puts Ärzte and Überwachung behind Zentrale. A collator
// holds buffers and is not safe for concurrent use, hence one per sort.
func sortByName[T any](items []T, name func(T) string) {
	collator := collate.New(language.Und)
	sort.SliceStable(items, func(i, j int) bool {
		return collator.CompareString(name(items[i]), name(items[j])) < 0
	})
}

func (c *Client) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	resBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("graph GET %s failed: %s", url, string(resBody))
	}

	return resBody, nil
}
