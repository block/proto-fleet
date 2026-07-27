package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	githubAPIVersion  = "2022-11-28"
	githubMediaType   = "application/vnd.github+json"
	githubHTTPTimeout = 10 * time.Second
	// releasesPageSize is the GitHub API maximum. Daily nightlies share the
	// list with RCs, so a smaller page could push a month-old RC off the page.
	releasesPageSize = "100"
)

// githubRelease mirrors the subset of a GitHub release object we consume.
type githubRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
}

// githubClient fetches release metadata with a per-endpoint ETag cache. On
// 304 Not Modified it replays the previously parsed result: conditional
// requests still cost rate-limit quota, so the ETag saves re-parsing (a 304
// has no body to parse), not quota.
//
// Not safe for concurrent use; the checker's single goroutine serializes
// calls, and the lifecycle's done-channel handoff orders successive
// activations.
type githubClient struct {
	baseURL    string
	userAgent  string
	logger     *slog.Logger
	httpClient *http.Client

	latestETag   string
	latestCached githubRelease

	listETag   string
	listCached []githubRelease
}

func newGitHubClient(baseURL, serverVersion string, logger *slog.Logger) *githubClient {
	return &githubClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		userAgent:  "fleetd/" + serverVersion,
		logger:     logger,
		httpClient: &http.Client{Timeout: githubHTTPTimeout},
	}
}

// fetchLatestStable returns the newest non-prerelease release, exactly as
// GitHub's /releases/latest endpoint defines it.
func (c *githubClient) fetchLatestStable(ctx context.Context) (githubRelease, error) {
	resp, err := c.get(ctx, c.baseURL+"/releases/latest", c.latestETag)
	if err != nil {
		return githubRelease{}, err
	}
	defer closeBody(resp)

	switch {
	case resp.StatusCode == http.StatusNotModified && c.latestETag != "":
		return c.latestCached, nil
	case resp.StatusCode != http.StatusOK:
		return githubRelease{}, fmt.Errorf("GET /releases/latest: status %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return githubRelease{}, fmt.Errorf("decode /releases/latest: %w", err)
	}
	// Record the ETag only after a successful parse so a bad body can't pin a
	// zero-value cache behind future 304s.
	c.latestETag = resp.Header.Get("ETag")
	c.latestCached = rel
	return rel, nil
}

// fetchReleases returns the first page of releases. A malformed individual
// entry is skipped; only an unparseable page aborts the cycle.
func (c *githubClient) fetchReleases(ctx context.Context) ([]githubRelease, error) {
	resp, err := c.get(ctx, c.baseURL+"/releases?per_page="+releasesPageSize, c.listETag)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	switch {
	case resp.StatusCode == http.StatusNotModified && c.listETag != "":
		return c.listCached, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GET /releases: status %d", resp.StatusCode)
	}

	var raw []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode /releases: %w", err)
	}
	releases := make([]githubRelease, 0, len(raw))
	for _, entry := range raw {
		var rel githubRelease
		if err := json.Unmarshal(entry, &rel); err != nil {
			c.logger.Debug("skipping malformed release entry", "error", err)
			continue
		}
		releases = append(releases, rel)
	}
	c.listETag = resp.Header.Get("ETag")
	c.listCached = releases
	return releases, nil
}

func (c *githubClient) get(ctx context.Context, url, etag string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", githubMediaType)
	// GitHub documents "X-GitHub-Api-Version"; header names are
	// case-insensitive and Go canonicalizes to this form regardless.
	req.Header.Set("X-Github-Api-Version", githubAPIVersion)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http GET %s: %w", url, err)
	}
	return resp, nil
}

// closeBody drains before closing so the keep-alive connection is reusable.
func closeBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
