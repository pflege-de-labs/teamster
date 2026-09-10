package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/oauth2/clientcredentials"

	"github.com/pflege-de-labs/teamster/internal/config"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type MessageResponse struct {
	ID string `json:"id"`
}

type ItemBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type Attachment struct {
	ID          string          `json:"id,omitempty"`
	ContentType string          `json:"contentType"`
	Content     json.RawMessage `json:"content"`
}

type MessageRequest struct {
	Body        ItemBody     `json:"body"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

func NewClient(cfg config.GraphConfig) (*Client, error) {
	oauthCfg := clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", cfg.TenantID),
		Scopes:       []string{"https://graph.microsoft.com/.default"},
	}

	httpClient := oauthCfg.Client(context.Background())
	httpClient.Timeout = time.Duration(cfg.TimeoutSec) * time.Second

	return &Client{
		baseURL:    cfg.BaseURL,
		httpClient: httpClient,
	}, nil
}

func (c *Client) PostMessage(teamID, channelID string, card json.RawMessage, summary string) (string, error) {
	escapedSummary := html.EscapeString(summary)
	payload := MessageRequest{
		Body: ItemBody{
			ContentType: "html",
			Content:     fmt.Sprintf("<p>%s</p>", escapedSummary),
		},
		Attachments: []Attachment{
			{
				ID:          "1",
				ContentType: "application/vnd.microsoft.card.adaptive",
				Content:     card,
			},
		},
	}

	endpoint := fmt.Sprintf("%s/teams/%s/channels/%s/messages", c.baseURL, teamID, channelID)
	resBody, err := c.doRequest(http.MethodPost, endpoint, payload)
	if err != nil {
		return "", err
	}

	var res MessageResponse
	if err := json.Unmarshal(resBody, &res); err != nil {
		return "", fmt.Errorf("decode post response: %w", err)
	}
	if res.ID == "" {
		return "", fmt.Errorf("graph response missing id")
	}

	return res.ID, nil
}

func (c *Client) UpdateMessage(teamID, channelID, messageID string, card json.RawMessage, summary string) error {
	escapedSummary := html.EscapeString(summary)
	payload := MessageRequest{
		Body: ItemBody{
			ContentType: "html",
			Content:     fmt.Sprintf("<p>%s</p>", escapedSummary),
		},
		Attachments: []Attachment{
			{
				ID:          "1",
				ContentType: "application/vnd.microsoft.card.adaptive",
				Content:     card,
			},
		},
	}

	endpoint := fmt.Sprintf("%s/teams/%s/channels/%s/messages/%s", c.baseURL, teamID, channelID, messageID)
	_, err := c.doRequest(http.MethodPatch, endpoint, payload)
	return err
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

// doRequest always marshals a body, which Graph rejects on GET.
// Graph returns teams and channels in no useful order, and these end up in a
// dropdown someone has to find a name in. Case-insensitive, so "ops" and "Ops"
// do not end up in separate halves of the list.
func sortByName[T any](items []T, name func(T) string) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := strings.ToLower(name(items[i])), strings.ToLower(name(items[j]))
		if left == right {
			return name(items[i]) < name(items[j])
		}
		return left < right
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

func (c *Client) doRequest(method, url string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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
		return nil, fmt.Errorf("graph %s %s failed: %s", method, url, string(resBody))
	}

	return resBody, nil
}
