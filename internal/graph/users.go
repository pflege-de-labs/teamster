package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound is what a lookup answers when Graph has no such object. An
// *APIError with status 404 matches it through errors.Is.
var ErrNotFound = errors.New("graph: not found")

// APIError is a response Graph refused. Code is Graph's error code, such as
// Authorization_RequestDenied, when the body carried one.
type APIError struct {
	Method  string
	URL     string
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("graph %s %s: %d %s: %s", e.Method, e.URL, e.Status, e.Code, e.Message)
}

func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.Status == http.StatusNotFound
}

const (
	// maxAttempts bounds how often a throttled request is sent.
	maxAttempts = 4
	// maxRetryWait caps a Retry-After, so one answer cannot park a worker.
	maxRetryWait = 30 * time.Second
	// maxUserPages bounds a directory listing: 999 users a page.
	maxUserPages = 1000
)

// userFields are the properties every user read selects.
const userFields = "id,displayName,givenName,surname,userPrincipalName,mail,accountEnabled,userType"

// User is the subset of a directory user needed to address and greet a person.
type User struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	GivenName         string `json:"givenName"`
	Surname           string `json:"surname"`
	UserPrincipalName string `json:"userPrincipalName"`
	Mail              string `json:"mail"`
	AccountEnabled    bool   `json:"accountEnabled"`
	UserType          string `json:"userType"`
}

// GetUser reads one user by object id or user principal name. It needs
// User.Read.All.
func (c *Client) GetUser(ctx context.Context, idOrUPN string) (User, error) {
	endpoint := fmt.Sprintf("%s/users/%s?%s", c.baseURL, url.PathEscape(idOrUPN),
		url.Values{"$select": {userFields}}.Encode())
	body, _, err := c.do(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return User{}, err
	}
	var u User
	if err := json.Unmarshal(body, &u); err != nil {
		return User{}, fmt.Errorf("decode user: %w", err)
	}
	return u, nil
}

// FindUserByMail finds the one user whose mail or SMTP proxy address is mail,
// for an address that is not the user principal name.
func (c *Client) FindUserByMail(ctx context.Context, mail string) (User, error) {
	quoted := odataString(mail)
	endpoint := fmt.Sprintf("%s/users?%s", c.baseURL, url.Values{
		"$filter": {fmt.Sprintf("mail eq %s or proxyAddresses/any(p:p eq %s)", quoted, odataString("smtp:"+mail))},
		"$select": {userFields},
		"$count":  {"true"},
	}.Encode())
	// A lambda over proxyAddresses is an advanced query.
	body, _, err := c.do(ctx, http.MethodGet, endpoint, nil, eventual)
	if err != nil {
		return User{}, err
	}
	var res struct {
		Value []User `json:"value"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return User{}, fmt.Errorf("decode users: %w", err)
	}
	switch len(res.Value) {
	case 0:
		return User{}, fmt.Errorf("user %q: %w", mail, ErrNotFound)
	case 1:
		return res.Value[0], nil
	default:
		return User{}, fmt.Errorf("user %q: %d users carry this address", mail, len(res.Value))
	}
}

// ListMemberUsers pages through the enabled member accounts of the tenant and
// hands each page to fn, so a large tenant is never held in memory at once.
func (c *Client) ListMemberUsers(ctx context.Context, fn func([]User) error) error {
	next := fmt.Sprintf("%s/users?%s", c.baseURL, url.Values{
		"$filter": {"accountEnabled eq true and userType eq 'Member'"},
		"$select": {userFields},
		"$top":    {"999"},
		"$count":  {"true"},
	}.Encode())

	for range maxUserPages {
		body, _, err := c.do(ctx, http.MethodGet, next, nil, eventual)
		if err != nil {
			return err
		}
		var res struct {
			Value    []User `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return fmt.Errorf("decode users: %w", err)
		}
		if err := fn(res.Value); err != nil {
			return err
		}
		// The token goes with every request, so only Graph's own links are
		// followed.
		if res.NextLink == "" || !strings.HasPrefix(res.NextLink, c.baseURL+"/") {
			return nil
		}
		next = res.NextLink
	}
	return fmt.Errorf("list users: more than %d pages", maxUserPages)
}

// ResolveCatalogApp finds the organization catalog id of the Teams app, first
// by its manifest id, then by the bot it carries. It needs AppCatalog.Read.All.
func (c *Client) ResolveCatalogApp(ctx context.Context, manifestID, botID string) (string, error) {
	var filters []string
	if manifestID != "" {
		filters = append(filters, fmt.Sprintf("distributionMethod eq 'organization' and externalId eq %s", odataString(manifestID)))
	}
	if botID != "" {
		filters = append(filters, fmt.Sprintf("appDefinitions/any(a:a/bot/id eq %s)", odataString(botID)))
	}

	for _, filter := range filters {
		endpoint := fmt.Sprintf("%s/appCatalogs/teamsApps?%s", c.baseURL, url.Values{
			"$filter": {filter},
			"$select": {"id,distributionMethod"},
		}.Encode())
		body, _, err := c.do(ctx, http.MethodGet, endpoint, nil, nil)
		if err != nil {
			return "", err
		}
		var res struct {
			Value []struct {
				ID                 string `json:"id"`
				DistributionMethod string `json:"distributionMethod"`
			} `json:"value"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return "", fmt.Errorf("decode catalog apps: %w", err)
		}
		// Only an org-published app can be installed for a user.
		for _, app := range res.Value {
			if app.DistributionMethod == "organization" {
				return app.ID, nil
			}
		}
	}
	return "", fmt.Errorf("teams app (manifest %q, bot %q) in the organization catalog: %w", manifestID, botID, ErrNotFound)
}

// InstallAppForUser installs the catalog app in the user's personal scope. It
// reports already when the app was installed before, which Graph answers with
// 409. It needs TeamsAppInstallation.ReadWriteForUser.All.
func (c *Client) InstallAppForUser(ctx context.Context, userID, catalogAppID string) (already bool, err error) {
	endpoint := fmt.Sprintf("%s/users/%s/teamwork/installedApps", c.baseURL, url.PathEscape(userID))
	payload, err := json.Marshal(map[string]string{
		"teamsApp@odata.bind": fmt.Sprintf("%s/appCatalogs/teamsApps/%s", c.baseURL, url.PathEscape(catalogAppID)),
	})
	if err != nil {
		return false, fmt.Errorf("encode install: %w", err)
	}

	_, _, err = c.do(ctx, http.MethodPost, endpoint, payload, nil)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
		return true, nil
	}
	return false, err
}

// eventual is the header Graph requires for $count and lambda filters.
var eventual = http.Header{"ConsistencyLevel": {"eventual"}}

// odataString quotes s as an OData string literal.
func odataString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// do sends a request, retrying while Graph throttles (429) or is unavailable
// (503), and turns any other failure into an *APIError.
func (c *Client) do(ctx context.Context, method, endpoint string, payload []byte, header http.Header) ([]byte, int, error) {
	for attempt := 1; ; attempt++ {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return nil, 0, fmt.Errorf("new request: %w", err)
		}
		for k, v := range header {
			req.Header[k] = v
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, 0, fmt.Errorf("graph request: %w", err)
		}
		resBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
		}

		throttled := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable
		if throttled && attempt < maxAttempts {
			if err := c.wait(ctx, retryAfter(resp.Header, attempt)); err != nil {
				return nil, resp.StatusCode, err
			}
			continue
		}
		if resp.StatusCode >= 300 {
			return nil, resp.StatusCode, apiError(method, endpoint, resp.StatusCode, resBody)
		}
		return resBody, resp.StatusCode, nil
	}
}

// wait sleeps for d or until ctx ends. Tests replace it through c.sleep.
func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryAfter reads Graph's Retry-After in seconds, falling back to doubling.
func retryAfter(h http.Header, attempt int) time.Duration {
	d := time.Duration(1<<(attempt-1)) * time.Second
	if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil && secs >= 0 {
		d = time.Duration(secs) * time.Second
	}
	return min(d, maxRetryWait)
}

func apiError(method, endpoint string, status int, body []byte) *APIError {
	var res struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	e := &APIError{Method: method, URL: endpoint, Status: status}
	if json.Unmarshal(body, &res) == nil && res.Error.Code != "" {
		e.Code, e.Message = res.Error.Code, res.Error.Message
	} else {
		e.Message = string(body)
	}
	return e
}
