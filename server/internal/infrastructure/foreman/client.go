package foreman

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultBaseURL     = "https://api.foreman.mn/api/v2"
	defaultTimeout     = 60 * time.Second
	maxMinersPerPage   = 100
	maxPaginationPages = 200      // safety cap to prevent infinite loops
	maxResponseBytes   = 10 << 20 // 10 MB max response body
	maxRetries         = 3
	defaultRetryDelay  = 2 * time.Second
	maxRetryDelay      = 30 * time.Second
)

// Client is an HTTP client for the Foreman REST API.
type Client struct {
	baseURL    string
	apiKey     string
	clientID   string
	httpClient *http.Client
}

// NewClient creates a Foreman API client.
// clientID must be numeric (Foreman client IDs are integers).
func NewClient(apiKey, clientID string) *Client {
	// Sanitize clientID to prevent path traversal — must be numeric.
	if _, err := strconv.Atoi(clientID); err != nil {
		clientID = "0" // will fail auth cleanly
	}

	return &Client{
		baseURL:  defaultBaseURL,
		apiKey:   apiKey,
		clientID: clientID,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// ListMiners fetches all miners for the client, handling pagination.
func (c *Client) ListMiners(ctx context.Context) ([]Miner, error) {
	var all []Miner
	offset := 0

	var total int
	for range maxPaginationPages {
		path := fmt.Sprintf("/clients/%s/miners?limit=%d&offset=%d", c.clientID, maxMinersPerPage, offset)
		body, err := c.get(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("fetching miners (offset %d): %w", offset, err)
		}

		var resp PaginatedMinersResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decoding miners response: %w", err)
		}

		total = resp.Total
		all = append(all, resp.Results...)
		if len(all) >= resp.Total || len(resp.Results) == 0 {
			break
		}
		offset += len(resp.Results)
	}

	if len(all) < total {
		return nil, fmt.Errorf("miner list truncated: fetched %d of %d miners (pagination limit reached)", len(all), total)
	}

	return all, nil
}

// ListSiteMapGroups fetches all site map groups.
func (c *Client) ListSiteMapGroups(ctx context.Context) ([]SiteMapGroup, error) {
	body, err := c.get(ctx, fmt.Sprintf("/site-map/%s/groups", c.clientID))
	if err != nil {
		return nil, fmt.Errorf("fetching site map groups: %w", err)
	}

	var groups []SiteMapGroup
	if err := json.Unmarshal(body, &groups); err != nil {
		return nil, fmt.Errorf("decoding site map groups: %w", err)
	}

	return groups, nil
}

// ListSiteMapRacks fetches all site map racks.
func (c *Client) ListSiteMapRacks(ctx context.Context) ([]SiteMapRack, error) {
	body, err := c.get(ctx, fmt.Sprintf("/site-map/%s/racks", c.clientID))
	if err != nil {
		return nil, fmt.Errorf("fetching site map racks: %w", err)
	}

	var racks []SiteMapRack
	if err := json.Unmarshal(body, &racks); err != nil {
		return nil, fmt.Errorf("decoding site map racks: %w", err)
	}

	return racks, nil
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	reqURL := c.baseURL + path

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}

		req.Header.Set("Authorization", "Token "+c.apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Retry on transient network errors (timeout, EOF, connection reset)
			if attempt < maxRetries {
				select {
				case <-time.After(defaultRetryDelay):
					continue
				case <-ctx.Done():
					return nil, fmt.Errorf("request to %s cancelled: %w", path, ctx.Err())
				}
			}
			return nil, fmt.Errorf("executing request to %s: %w", path, err)
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if err != nil {
			// Retry on read errors (unexpected EOF)
			if attempt < maxRetries {
				select {
				case <-time.After(defaultRetryDelay):
					continue
				case <-ctx.Done():
					return nil, fmt.Errorf("request to %s cancelled: %w", path, ctx.Err())
				}
			}
			return nil, fmt.Errorf("reading response body: %w", err)
		}

		// Retry on 429 (rate limited) and 5xx (server errors)
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < maxRetries {
			delay := defaultRetryDelay
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, parseErr := strconv.Atoi(ra); parseErr == nil && secs > 0 {
					delay = time.Duration(secs) * time.Second
					if delay > maxRetryDelay {
						delay = maxRetryDelay
					}
				}
			}
			select {
			case <-time.After(delay):
				continue
			case <-ctx.Done():
				return nil, fmt.Errorf("request to %s cancelled: %w", path, ctx.Err())
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Path:       path,
				Body:       string(body),
			}
		}

		return body, nil
	}

	return nil, &APIError{StatusCode: http.StatusTooManyRequests, Path: path, Body: "retries exhausted"}
}

// APIError represents an error returned by the Foreman API.
type APIError struct {
	StatusCode int
	Path       string
	Body       string
}

func (e *APIError) Error() string {
	body := e.Body
	if len(body) > 200 {
		body = body[:200] + "..."
	}
	return "foreman API error: HTTP " + strconv.Itoa(e.StatusCode) + " on " + e.Path + ": " + body
}

// IsUnauthorized returns true if the error is a 401 Unauthorized.
func (e *APIError) IsUnauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized
}

// IsRateLimited returns true if the error is a 429 Too Many Requests.
func (e *APIError) IsRateLimited() bool {
	return e.StatusCode == http.StatusTooManyRequests
}
