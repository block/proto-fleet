package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	releaseAPIBaseURL = "https://api.github.com/repos/block/proto-fleet"
	githubAPIVersion  = "2022-11-28"
	githubMediaType   = "application/vnd.github+json"
	githubHTTPTimeout = 10 * time.Second
	// releasesPageSize is the GitHub API maximum. The checker intentionally
	// inspects only the newest page to keep each cycle to two bounded calls.
	releasesPageSize = 100
	// maxResponseBytes caps how much of a response body a decode may
	// consume. per_page limits what we ask for, but the remote controls
	// what it actually sends; a page of 100 release objects is well under
	// 1 MiB, so 8 MiB is a generous ceiling. JSON truncated by the cap
	// fails to decode, degrading the cycle silently like any other bad
	// body.
	maxResponseBytes = 8 << 20
)

// githubRelease mirrors the subset of a GitHub release object we consume.
type githubRelease struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
}

// githubClient fetches release metadata. Production always uses the fixed
// Proto Fleet repository URL; tests inject an httptest URL directly.
type githubClient struct {
	baseURL    string
	userAgent  string
	logger     *slog.Logger
	httpClient *http.Client

	mu               sync.Mutex
	latestETag       string
	latestRelease    githubRelease
	latestCached     bool
	releasesETag     string
	cachedReleases   []githubRelease
	releasesCached   bool
	rateLimitedUntil time.Time
}

type githubRateLimitError struct {
	resetAt time.Time
}

func (e *githubRateLimitError) Error() string {
	return "GitHub API rate limit exceeded"
}

func newGitHubClient(baseURL, serverVersion string, logger *slog.Logger) *githubClient {
	return &githubClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		userAgent:  "fleetd/" + serverVersion,
		logger:     logger,
		httpClient: &http.Client{Timeout: githubHTTPTimeout},
	}
}

// fetchLatestStableFallback returns GitHub's created-at-based latest
// non-prerelease as a fallback candidate. The checker semver-maxes it with
// canonical stable tags from the first release-list page.
func (c *githubClient) fetchLatestStableFallback(ctx context.Context) (githubRelease, error) {
	etag, cached, cachedOK := c.latestCache()
	resp, err := c.get(ctx, c.baseURL+"/releases/latest", etag)
	if err != nil {
		return githubRelease{}, err
	}
	defer closeBody(resp)

	switch resp.StatusCode {
	case http.StatusNotModified:
		if !cachedOK {
			return githubRelease{}, fmt.Errorf("GET /releases/latest: 304 without cached response")
		}
		return cached, nil
	case http.StatusOK:
	default:
		return githubRelease{}, fmt.Errorf("GET /releases/latest: status %d", resp.StatusCode)
	}

	rel, err := decodeRelease(resp.Body, "/releases/latest")
	if err != nil {
		return githubRelease{}, err
	}
	c.storeLatest(resp.Header.Get("ETag"), rel)
	return rel, nil
}

// fetchReleaseByTag revalidates a cached release that no longer appears in
// the bounded discovery responses. A 404 is a successful confirmation that
// the release is no longer published; other failures leave its status
// unknown.
func (c *githubClient) fetchReleaseByTag(ctx context.Context, tag string) (githubRelease, bool, error) {
	endpoint := c.baseURL + "/releases/tags/" + url.PathEscape(tag)
	resp, err := c.get(ctx, endpoint, "")
	if err != nil {
		return githubRelease{}, false, err
	}
	defer closeBody(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		rel, err := decodeRelease(resp.Body, "/releases/tags/{tag}")
		if err != nil {
			return githubRelease{}, false, err
		}
		if rel.TagName != tag {
			return githubRelease{}, false, fmt.Errorf("decode /releases/tags/{tag}: response tag mismatch")
		}
		return rel, true, nil
	case http.StatusNotFound:
		return githubRelease{}, false, nil
	default:
		return githubRelease{}, false, fmt.Errorf("GET /releases/tags/{tag}: status %d", resp.StatusCode)
	}
}

func decodeRelease(body io.Reader, endpoint string) (githubRelease, error) {
	var rel githubRelease
	decoder := json.NewDecoder(io.LimitReader(body, maxResponseBytes))
	if err := decoder.Decode(&rel); err != nil {
		return githubRelease{}, fmt.Errorf("decode %s: %w", endpoint, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return githubRelease{}, fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return rel, nil
}

// fetchReleases fetches exactly the newest page. Responses are streamed so an
// upstream that ignores per_page cannot force allocation of an unbounded
// array.
func (c *githubClient) fetchReleases(ctx context.Context) ([]githubRelease, error) {
	endpoint := fmt.Sprintf("%s/releases?per_page=%d&page=1", c.baseURL, releasesPageSize)
	etag, cached, cachedOK := c.releasesCache()
	resp, err := c.get(ctx, endpoint, etag)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	switch resp.StatusCode {
	case http.StatusNotModified:
		if !cachedOK {
			return nil, fmt.Errorf("GET /releases: 304 without cached response")
		}
		return cached, nil
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("GET /releases: status %d", resp.StatusCode)
	}

	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	first, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode /releases: %w", err)
	}
	if delim, ok := first.(json.Delim); !ok || delim != '[' {
		return nil, fmt.Errorf("decode /releases: expected JSON array")
	}

	entries := make([]githubRelease, 0, releasesPageSize)
	rawCount := 0
	for decoder.More() {
		if rawCount == releasesPageSize {
			return nil, fmt.Errorf("decode /releases: release count exceeds per-page limit %d", releasesPageSize)
		}
		var entry json.RawMessage
		if err := decoder.Decode(&entry); err != nil {
			return nil, fmt.Errorf("decode /releases entry %d: %w", rawCount+1, err)
		}
		rawCount++

		var rel githubRelease
		if err := json.Unmarshal(entry, &rel); err != nil {
			c.logger.Debug("skipping malformed release entry", "error", err)
			continue
		}
		entries = append(entries, rel)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("decode /releases: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode /releases: %w", err)
	}
	c.storeReleases(resp.Header.Get("ETag"), entries)
	return entries, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	switch err := decoder.Decode(&trailing); {
	case err == io.EOF:
		return nil
	case err != nil:
		return fmt.Errorf("decode trailing JSON: %w", err)
	default:
		return fmt.Errorf("unexpected trailing JSON value")
	}
}

func (c *githubClient) get(ctx context.Context, endpoint, etag string) (*http.Response, error) {
	if resetAt := c.currentRateLimit(); !resetAt.IsZero() {
		return nil, &githubRateLimitError{resetAt: resetAt}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", githubMediaType)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	// GitHub documents "X-GitHub-Api-Version"; header names are
	// case-insensitive and Go canonicalizes to this form regardless.
	req.Header.Set("X-Github-Api-Version", githubAPIVersion)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http GET: %w", withoutRequestURL(err))
	}
	if resetAt, limited := responseRateLimit(resp); limited {
		closeBody(resp)
		c.setRateLimit(resetAt)
		return nil, &githubRateLimitError{resetAt: resetAt}
	}
	return resp, nil
}

func (c *githubClient) latestCache() (string, githubRelease, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.latestETag, c.latestRelease, c.latestCached
}

func (c *githubClient) storeLatest(etag string, rel githubRelease) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latestETag = etag
	c.latestRelease = rel
	c.latestCached = true
}

func (c *githubClient) releasesCache() (string, []githubRelease, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.releasesETag, append([]githubRelease(nil), c.cachedReleases...), c.releasesCached
}

func (c *githubClient) storeReleases(etag string, releases []githubRelease) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releasesETag = etag
	c.cachedReleases = append(c.cachedReleases[:0], releases...)
	c.releasesCached = true
}

func (c *githubClient) currentRateLimit() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rateLimitedUntil.After(time.Now()) {
		return c.rateLimitedUntil
	}
	c.rateLimitedUntil = time.Time{}
	return time.Time{}
}

func (c *githubClient) setRateLimit(resetAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rateLimitedUntil = resetAt
}

func responseRateLimit(resp *http.Response) (time.Time, bool) {
	remainingExhausted := resp.Header.Get("X-Ratelimit-Remaining") == "0"
	retryAfter := resp.Header.Get("Retry-After")
	if resp.StatusCode != http.StatusTooManyRequests &&
		!(resp.StatusCode == http.StatusForbidden && (remainingExhausted || retryAfter != "")) {
		return time.Time{}, false
	}

	now := time.Now()
	if raw := resp.Header.Get("X-Ratelimit-Reset"); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
			resetAt := time.Unix(seconds, 0)
			if resetAt.After(now) {
				return resetAt, true
			}
		}
	}
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	if resetAt, err := http.ParseTime(retryAfter); err == nil && resetAt.After(now) {
		return resetAt, true
	}
	return now.Add(time.Minute), true
}

// withoutRequestURL removes net/url wrappers before an error reaches logs.
// Those wrappers include the full requested URL, which may contain sensitive
// deployment configuration even though validated production URLs do not.
func withoutRequestURL(err error) error {
	for {
		urlErr, ok := err.(*url.Error)
		if !ok {
			return err
		}
		err = urlErr.Err
	}
}

// closeBody drains before closing so the keep-alive connection is reusable.
// The drain is capped: the remote controls the body size, and past the cap a
// fresh connection is cheaper than swallowing the rest.
func closeBody(resp *http.Response) {
	const maxDrainBytes = 1 << 20
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	_ = resp.Body.Close()
}
