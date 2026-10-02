// Package asana implements port.AsanaClient against the real Asana REST API.
package asana

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"asana_extractor/internal/extractor/domain"
)

const (
	defaultBaseURL = "https://app.asana.com/api/1.0"
	pageLimit      = 100
	maxAttempts    = 3
)

// Client is an HTTP-backed Asana API client with a client-side token bucket
// rate limiter and 429/Retry-After backoff.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	limiter    *rateLimiter
}

// NewClient builds a Client. ratePerMinute caps outgoing requests; Asana's
// free-tier limit is 150 req/min, so a conservative default (see
// DefaultRatePerMinute) leaves headroom for other API consumers sharing the
// same token.
func NewClient(token string, ratePerMinute int) *Client {
	return &Client{
		baseURL:    defaultBaseURL,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		limiter:    newRateLimiter(ratePerMinute),
	}
}

// DefaultRatePerMinute is a safe default well under Asana's documented
// rate limits, leaving headroom for retries and other clients.
const DefaultRatePerMinute = 50

// Close releases the rate limiter's background goroutine and ticker.
// Callers that construct a Client should defer Close() to avoid leaking
// those resources once the Client is no longer needed.
func (c *Client) Close() {
	c.limiter.stop()
}

type usersResponse struct {
	Data     []userDTO `json:"data"`
	NextPage *nextPage `json:"next_page"`
}

type userDTO struct {
	GID   string `json:"gid"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type projectsResponse struct {
	Data     []projectDTO `json:"data"`
	NextPage *nextPage    `json:"next_page"`
}

type projectDTO struct {
	GID  string `json:"gid"`
	Name string `json:"name"`
}

type nextPage struct {
	Offset string `json:"offset"`
}

type errorsResponse struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListUsers fetches every user in the workspace, paginating until
// next_page is null.
func (c *Client) ListUsers(ctx context.Context, workspaceGID string) ([]domain.User, error) {
	var users []domain.User
	offset := ""

	for {
		q := url.Values{}
		q.Set("opt_fields", "name,email")
		q.Set("limit", strconv.Itoa(pageLimit))
		if offset != "" {
			q.Set("offset", offset)
		}

		path := fmt.Sprintf("/workspaces/%s/users?%s", workspaceGID, q.Encode())

		var page usersResponse
		if err := c.getJSON(ctx, path, &page); err != nil {
			return users, fmt.Errorf("asana: list users: %w", err)
		}

		for _, u := range page.Data {
			users = append(users, domain.User{GID: u.GID, Name: u.Name, Email: u.Email})
		}

		if page.NextPage == nil || page.NextPage.Offset == "" {
			break
		}
		offset = page.NextPage.Offset
	}

	return users, nil
}

// ListProjects fetches every project in the workspace, paginating until
// next_page is null.
func (c *Client) ListProjects(ctx context.Context, workspaceGID string) ([]domain.Project, error) {
	var projects []domain.Project
	offset := ""

	for {
		q := url.Values{}
		q.Set("workspace", workspaceGID)
		q.Set("opt_fields", "name")
		q.Set("limit", strconv.Itoa(pageLimit))
		if offset != "" {
			q.Set("offset", offset)
		}

		path := fmt.Sprintf("/projects?%s", q.Encode())

		var page projectsResponse
		if err := c.getJSON(ctx, path, &page); err != nil {
			return projects, fmt.Errorf("asana: list projects: %w", err)
		}

		for _, p := range page.Data {
			projects = append(projects, domain.Project{GID: p.GID, Name: p.Name, WorkspaceGID: workspaceGID})
		}

		if page.NextPage == nil || page.NextPage.Offset == "" {
			break
		}
		offset = page.NextPage.Offset
	}

	return projects, nil
}

// getJSON performs a rate-limited GET against path (relative to baseURL),
// retrying on HTTP 429 per Retry-After, and decodes the JSON body into out.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := c.limiter.wait(ctx); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("do request (attempt %d/%d): %w", attempt, maxAttempts, err)
			if attempt == maxAttempts {
				break
			}
			if err := sleepCtx(ctx, retryBackoff(attempt)); err != nil {
				return err
			}
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("rate limited (attempt %d/%d), retry after %s", attempt, maxAttempts, retryAfter)
			if attempt == maxAttempts {
				break
			}
			if err := sleepCtx(ctx, retryAfter); err != nil {
				return err
			}
			continue
		}

		if resp.StatusCode >= 500 {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			var errBody errorsResponse
			msg := string(body)
			if json.Unmarshal(body, &errBody) == nil && len(errBody.Errors) > 0 {
				msg = errBody.Errors[0].Message
			}
			lastErr = fmt.Errorf("asana api error: status %d (attempt %d/%d): %s", resp.StatusCode, attempt, maxAttempts, msg)
			if attempt == maxAttempts {
				break
			}
			if err := sleepCtx(ctx, retryBackoff(attempt)); err != nil {
				return err
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read response body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			var errBody errorsResponse
			msg := string(body)
			if json.Unmarshal(body, &errBody) == nil && len(errBody.Errors) > 0 {
				msg = errBody.Errors[0].Message
			}
			return fmt.Errorf("asana api error: status %d: %s", resp.StatusCode, msg)
		}

		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}

	return fmt.Errorf("asana api: exhausted retries: %w", lastErr)
}

// retryBackoff returns a short, fixed-step backoff for transport-error and
// 5xx retries (as opposed to 429s, which honor the server-supplied
// Retry-After instead). Kept small so transient blips don't eat into the
// per-cycle timeout budget (see cmd/backednsvc's cycleTimeout).
func retryBackoff(attempt int) time.Duration {
	return time.Duration(attempt) * 200 * time.Millisecond
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return time.Second
	}
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds < 0 {
		return time.Second
	}
	return time.Duration(seconds) * time.Second
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
