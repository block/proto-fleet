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
	// list with RCs, so a smaller page would just force more pages to reach
	// the newest RC.
	releasesPageSize = 100
	// maxReleasePages bounds the release-list crawl. Ten full pages is
	// roughly 2.7 years of daily nightlies; an RC older than every release
	// in that window is not a live upgrade offer, and the bound caps a
	// cycle's worst-case API cost at a handful of requests.
	maxReleasePages = 10
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

	// listETag is page 1's ETag; listCached is the whole accumulated crawl
	// it stands for, however many pages that took.
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&rel); err != nil {
		return githubRelease{}, fmt.Errorf("decode /releases/latest: %w", err)
	}
	// Record the ETag only after a successful parse so a bad body can't pin a
	// zero-value cache behind future 304s.
	c.latestETag = resp.Header.Get("ETag")
	c.latestCached = rel
	return rel, nil
}

// fetchReleases returns releases newest-first, following pagination until the
// caller's enough predicate is satisfied by the accumulated list, a short page
// signals the list is exhausted, or maxReleasePages is reached. Daily
// nightlies crowd the newest pages, so a still-current RC can sit hundreds of
// entries deep; the predicate tells the crawl when it may stop early.
//
// Only page 1 is requested conditionally: publishing a release prepends to the
// list, so an unchanged first page means nothing new and the whole previously
// accumulated crawl is replayed. (A deletion deep in the list can leave page 1
// untouched and the replayed tail stale until the ETag rotates — acceptable
// for a best-effort daily check.)
func (c *githubClient) fetchReleases(ctx context.Context, enough func([]githubRelease) bool) ([]githubRelease, error) {
	var accumulated []githubRelease
	var firstPageETag string
	for page := 1; page <= maxReleasePages; page++ {
		etag := ""
		if page == 1 {
			etag = c.listETag
		}
		pg, err := c.fetchReleasesPage(ctx, page, etag)
		if err != nil {
			return nil, err
		}
		if pg.notModified {
			return c.listCached, nil
		}
		if page == 1 {
			firstPageETag = pg.etag
		}
		accumulated = append(accumulated, pg.entries...)
		if pg.rawCount < releasesPageSize || enough(accumulated) {
			break
		}
	}
	// Record the ETag and cache only after every fetched page parsed, so a
	// bad body can't pin a partial crawl behind future 304s.
	c.listETag = firstPageETag
	c.listCached = accumulated
	return accumulated, nil
}

// releasesPage is one decoded page of the release list.
type releasesPage struct {
	entries []githubRelease
	// rawCount is the pre-skip entry count: whether the list continues past
	// this page depends on how many entries GitHub sent, not on how many
	// survived decoding.
	rawCount    int
	etag        string
	notModified bool
}

// fetchReleasesPage fetches one page of the release list. A malformed
// individual entry is skipped; only an unparseable page aborts the cycle.
func (c *githubClient) fetchReleasesPage(ctx context.Context, page int, etag string) (releasesPage, error) {
	url := fmt.Sprintf("%s/releases?per_page=%d&page=%d", c.baseURL, releasesPageSize, page)
	resp, err := c.get(ctx, url, etag)
	if err != nil {
		return releasesPage{}, err
	}
	defer closeBody(resp)

	switch {
	case resp.StatusCode == http.StatusNotModified && etag != "":
		return releasesPage{notModified: true}, nil
	case resp.StatusCode != http.StatusOK:
		return releasesPage{}, fmt.Errorf("GET /releases page %d: status %d", page, resp.StatusCode)
	}

	var raw []json.RawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&raw); err != nil {
		return releasesPage{}, fmt.Errorf("decode /releases page %d: %w", page, err)
	}
	entries := make([]githubRelease, 0, len(raw))
	for _, entry := range raw {
		var rel githubRelease
		if err := json.Unmarshal(entry, &rel); err != nil {
			c.logger.Debug("skipping malformed release entry", "error", err)
			continue
		}
		entries = append(entries, rel)
	}
	return releasesPage{entries: entries, rawCount: len(raw), etag: resp.Header.Get("ETag")}, nil
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
// The drain is capped: the remote controls the body size, and past the cap a
// fresh connection is cheaper than swallowing the rest.
func closeBody(resp *http.Response) {
	const maxDrainBytes = 1 << 20
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	_ = resp.Body.Close()
}
