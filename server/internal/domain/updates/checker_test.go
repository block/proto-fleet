package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return body
}

// releasesJSON marshals entries in the GitHub list-response shape; response
// bound tests need 100-entry pages, which would be unwieldy as testdata files.
func releasesJSON(t *testing.T, entries []githubRelease) []byte {
	t.Helper()
	body, err := json.Marshal(entries)
	require.NoError(t, err)
	return body
}

func releaseJSON(t *testing.T, entry githubRelease) []byte {
	t.Helper()
	body, err := json.Marshal(entry)
	require.NoError(t, err)
	return body
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// nightlies fabricates n non-RC prerelease entries.
func nightlies(n int, label string) []githubRelease {
	out := make([]githubRelease, n)
	for i := range out {
		tag := fmt.Sprintf("nightly-%s-%03d", label, i)
		out[i] = githubRelease{
			TagName:     tag,
			PublishedAt: time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC),
			Prerelease:  true,
		}
	}
	return out
}

func rcEntry(tag string) githubRelease {
	return githubRelease{
		TagName:     tag,
		PublishedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		Prerelease:  true,
	}
}

func stableEntry(tag string) githubRelease {
	return githubRelease{
		TagName:     tag,
		PublishedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
	}
}

type ghRequest struct {
	path   string
	query  url.Values
	header http.Header
}

type ghResponse struct {
	status int
	body   []byte
}

// ghServer is an httptest fixture standing in for the GitHub releases API.
type ghServer struct {
	srv *httptest.Server

	mu           sync.Mutex
	latestStatus int
	latestBody   []byte
	listStatus   int
	listBody     []byte
	tags         map[string]ghResponse
	requests     []ghRequest
}

func newGHServer(t *testing.T) *ghServer {
	t.Helper()
	g := &ghServer{
		latestStatus: http.StatusOK,
		latestBody:   fixture(t, "latest_stable.json"),
		listStatus:   http.StatusOK,
		listBody:     []byte("[]"),
		tags:         make(map[string]ghResponse),
	}
	g.srv = httptest.NewServer(http.HandlerFunc(g.handle))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *ghServer) handle(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, ghRequest{
		path:   r.URL.Path,
		query:  r.URL.Query(),
		header: r.Header.Clone(),
	})

	var status int
	var body []byte
	switch r.URL.Path {
	case "/releases/latest":
		status, body = g.latestStatus, g.latestBody
	case "/releases":
		status, body = g.listStatus, g.listBody
	default:
		const tagsPrefix = "/releases/tags/"
		if strings.HasPrefix(r.URL.Path, tagsPrefix) {
			tag := strings.TrimPrefix(r.URL.Path, tagsPrefix)
			response, ok := g.tags[tag]
			if !ok {
				http.NotFound(w, r)
				return
			}
			status, body = response.status, response.body
		} else {
			http.NotFound(w, r)
			return
		}
	}

	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (g *ghServer) setLatest(status int, body []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.latestStatus, g.latestBody = status, body
}

func (g *ghServer) setList(status int, body []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.listStatus, g.listBody = status, body
}

func (g *ghServer) setTag(tag string, status int, body []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tags[tag] = ghResponse{status: status, body: body}
}

// listRequests returns the recorded requests against /releases.
func (g *ghServer) listRequests() []ghRequest {
	var out []ghRequest
	for _, req := range g.recorded() {
		if req.path == "/releases" {
			out = append(out, req)
		}
	}
	return out
}

func (g *ghServer) recorded() []ghRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]ghRequest, len(g.requests))
	copy(out, g.requests)
	return out
}

func (g *ghServer) config() Config {
	return Config{
		CheckInterval:   time.Hour,
		DownloadBaseURL: "https://github.com/block/proto-fleet/releases/download",
		// Kong's default:"true" only applies to parsed config, not literals.
		Enabled: true,
	}
}

// recordingHandler captures slog records so tests can assert the log-level
// ceiling on failure paths.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) recordsAbove(level slog.Level) []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []slog.Record
	for _, r := range h.records {
		if r.Level > level {
			out = append(out, r)
		}
	}
	return out
}

func newTestChecker(t *testing.T, cfg Config, releasesAPIURL string) (*Checker, *recordingHandler) {
	t.Helper()
	h := &recordingHandler{}
	return newChecker(cfg, releasesAPIURL, "test-version", slog.New(h)), h
}

// A stable release from /releases/latest lands in the snapshot
// with version, notes URL, and publish time. Also pins the request headers.
func TestCheckCachesLatestStable(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v0.2.9", snap.LatestStable.Version)
	assert.Equal(t, "https://github.com/block/proto-fleet/releases/tag/v0.2.9", snap.LatestStable.NotesURL)
	assert.Equal(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC), snap.LatestStable.PublishedAt.UTC())
	assert.False(t, snap.LatestStable.Prerelease)
	assert.Nil(t, snap.LatestRC)
	assert.False(t, snap.FetchedAt.IsZero())
	assert.True(t, snap.StableAvailable)
	assert.True(t, snap.RCAvailable)

	reqs := gh.recorded()
	require.Len(t, reqs, 2)
	assert.Equal(t, "/releases/latest", reqs[0].path)
	assert.Equal(t, "/releases", reqs[1].path)
	for _, req := range reqs {
		assert.Equal(t, "fleetd/test-version", req.header.Get("User-Agent"))
		assert.Equal(t, "2022-11-28", req.header.Get("X-Github-Api-Version"))
		assert.Equal(t, "application/vnd.github+json", req.header.Get("Accept"))
	}
}

func TestSnapshotEligibilityAccessorsEncodeAvailabilityInvariants(t *testing.T) {
	t.Parallel()

	stable := &Release{Version: "v2.0.0"}
	snap := Snapshot{
		LatestStable:    stable,
		StableAvailable: true,
		RCAvailable:     true,
		FetchedAt:       time.Now(),
	}

	eligibleStable, stableAvailable := snap.EligibleStable()
	require.True(t, stableAvailable)
	require.NotNil(t, eligibleStable)
	eligibleStable.Version = "v9.9.9"
	assert.Equal(t, "v2.0.0", snap.LatestStable.Version, "accessors must not expose mutable cached data")

	eligibleRC, rcAvailable := snap.EligibleRC()
	assert.True(t, rcAvailable, "an available RC view may legitimately contain no candidate")
	assert.Nil(t, eligibleRC)
	emptyStable, emptyStableAvailable := (Snapshot{StableAvailable: true}).EligibleStable()
	assert.True(t, emptyStableAvailable, "availability and candidate presence are independent for both channels")
	assert.Nil(t, emptyStable)

	snap.StableAvailable = false
	eligibleStable, stableAvailable = snap.EligibleStable()
	assert.False(t, stableAvailable, "a retained candidate is not eligible while its channel is unavailable")
	assert.Nil(t, eligibleStable)

	zeroStable, zeroStableAvailable := (Snapshot{}).EligibleStable()
	zeroRC, zeroRCAvailable := (Snapshot{}).EligibleRC()
	assert.Nil(t, zeroStable)
	assert.False(t, zeroStableAvailable)
	assert.Nil(t, zeroRC)
	assert.False(t, zeroRCAvailable)
}

// The checker is channel-agnostic — it caches BOTH the
// latest stable and the latest RC; channel filtering happens at read time.
// The fixture contains semver-valid
// feature-branch builds (v0.3.0-pr800.1 outranks every RC under plain semver)
// that must never be selected because only vX.Y.Z-rc.N grammar qualifies.
func TestCheckCachesBothStableAndRC(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases.json"))
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v0.2.9", snap.LatestStable.Version)
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.5", snap.LatestRC.Version)
	assert.Equal(t, "https://github.com/block/proto-fleet/releases/tag/v0.2.9-rc.5", snap.LatestRC.NotesURL)
	assert.True(t, snap.LatestRC.Prerelease)
	assert.True(t, snap.StableAvailable)
	assert.True(t, snap.RCAvailable)
}

// GitHub's /releases/latest endpoint is ordered by release creation time, not
// semantic version. A later-created maintenance release must not hide a
// higher stable version that remains on the current list page.
func TestStablePickedBySemverMaxAcrossLatestAndList(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v1.9.1")))
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{stableEntry("v2.0.0")}))
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v2.0.0", snap.LatestStable.Version)
}

func TestStableSelectionRejectsPrereleaseFlag(t *testing.T) {
	t.Parallel()

	markedPrerelease := stableEntry("v2.0.0")
	markedPrerelease.Prerelease = true

	t.Run("latest endpoint", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, latestStable(markedPrerelease, nil))
	})

	t.Run("release list", func(t *testing.T) {
		t.Parallel()

		got := latestStable(stableEntry("v1.9.1"), []githubRelease{markedPrerelease})
		require.NotNil(t, got)
		assert.Equal(t, "v1.9.1", got.Version)
	})
}

// A prerelease-grammar tag served by /releases/latest — a release published
// by hand without the prerelease flag — must not become the stable offer;
// canonical stable grammar remains required even when GitHub's flag is wrong.
func TestMisflaggedPrereleaseTagNeverStableOffer(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusOK, fixture(t, "latest_misflagged_rc.json"))
	gh.setList(http.StatusOK, []byte("[]"))
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	assert.Nil(t, snap.LatestStable, "a prerelease-suffixed tag must never be the stable offer, whatever GitHub's prerelease flag says")
	assert.False(t, snap.FetchedAt.IsZero(), "a mis-flagged tag is not a fetch failure; the cycle still succeeds")
}

// Nightly and hand-made non-semver tags are never selected,
// on either the stable or the RC side.
func TestNonSemverTagsNeverSelected(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusOK, fixture(t, "latest_invalid_tag.json"))
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v0.2.9-rc.5")}))
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	assert.Nil(t, snap.LatestStable, "non-semver /releases/latest tag must not become the stable offer")
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.5", snap.LatestRC.Version)
	assert.False(t, snap.FetchedAt.IsZero(), "an invalid tag is not a fetch failure; the cycle still succeeds")
	assert.False(t, snap.StableAvailable, "without a usable or cached stable release, stable status is unavailable")
	assert.True(t, snap.RCAvailable, "a successful list fetch keeps the RC channel available independently")
}

func TestStableTagRequiresCanonicalGrammar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag  string
		want bool
	}{
		{tag: "v1.2.3", want: true},
		{tag: "v1"},
		{tag: "v1.2"},
		{tag: "v1.2.3+meta"},
		{tag: "v01.2.3"},
		{tag: "v1.02.3"},
		{tag: "v1.2.03"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isCanonicalStableTag(tt.tag))
		})
	}
}

func TestRCTagRequiresCanonicalSemver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag  string
		want bool
	}{
		{tag: "v1.2.3-rc.1", want: true},
		{tag: "v01.2.3-rc.1"},
		{tag: "v1.02.3-rc.1"},
		{tag: "v1.2.03-rc.1"},
		{tag: "v1.2.3-rc.01"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isCanonicalRCTag(tt.tag))
			if tt.want {
				require.NotNil(t, latestRC([]githubRelease{rcEntry(tt.tag)}))
			} else {
				assert.Nil(t, latestRC([]githubRelease{rcEntry(tt.tag)}))
			}
		})
	}
}

// With per_page=100 requested (the API max), an RC preceded by a
// month of nightlies on the same page is still found.
func TestRCSelectedBehindManyNightlies(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases_nightlies.json"))
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.7", snap.LatestRC.Version)

	var listReq *ghRequest
	for i := range gh.recorded() {
		req := gh.recorded()[i]
		if req.path == "/releases" {
			listReq = &req
		}
	}
	require.NotNil(t, listReq)
	assert.Equal(t, "100", listReq.query.Get("per_page"))
	assert.Equal(t, "1", listReq.query.Get("page"))
}

// A full page never triggers a historical crawl. Release discovery is bounded
// to the fixed latest endpoint plus page 1 of the list.
func TestReleaseFetchStopsAfterFirstPage(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, releasesJSON(t, nightlies(releasesPageSize, "p1")))
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	listReqs := gh.listRequests()
	require.Len(t, listReqs, 1)
	assert.Equal(t, "1", listReqs[0].query.Get("page"))
}

// A releases endpoint must honor the requested per_page=100 contract. Decode
// one item at a time and reject the 101st instead of allocating the whole
// attacker-controlled array.
func TestReleasePageRejectsMoreThanPageSizeEntries(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, releasesJSON(t, nightlies(releasesPageSize+1, "oversized")))
	client := newGitHubClient(gh.srv.URL, "test-version", slog.Default())

	_, err := client.fetchReleases(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "release count exceeds per-page limit 100")
}

// The latest endpoint must contain exactly one JSON value, matching the strict
// trailing-data invariant enforced for release-list responses.
func TestLatestReleaseRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	body := append(fixture(t, "latest_stable.json"), []byte("\n{}")...)
	gh.setLatest(http.StatusOK, body)
	client := newGitHubClient(gh.srv.URL, "test-version", slog.Default())

	_, err := client.fetchLatestStableFallback(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "unexpected trailing JSON value")
}

func TestConditionalRequestsReuseCachedResponses(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]ghRequest, 0, 4)
	counts := make(map[string]int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, ghRequest{path: r.URL.Path, header: r.Header.Clone()})
		counts[r.URL.Path]++
		count := counts[r.URL.Path]
		mu.Unlock()

		etag := `"` + strings.TrimPrefix(r.URL.Path, "/") + `-etag"`
		if count > 1 {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		switch r.URL.Path {
		case "/releases/latest":
			_, _ = w.Write(releaseJSON(t, stableEntry("v2.0.0")))
		case "/releases":
			_, _ = w.Write(releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.1")}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := newGitHubClient(srv.URL, "test-version", slog.Default())
	firstLatest, err := client.fetchLatestStableFallback(context.Background())
	require.NoError(t, err)
	secondLatest, err := client.fetchLatestStableFallback(context.Background())
	require.NoError(t, err)
	assert.Equal(t, firstLatest, secondLatest)

	firstList, err := client.fetchReleases(context.Background())
	require.NoError(t, err)
	secondList, err := client.fetchReleases(context.Background())
	require.NoError(t, err)
	assert.Equal(t, firstList, secondList)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requests, 4)
	assert.Empty(t, requests[0].header.Get("If-None-Match"))
	assert.Equal(t, `"releases/latest-etag"`, requests[1].header.Get("If-None-Match"))
	assert.Empty(t, requests[2].header.Get("If-None-Match"))
	assert.Equal(t, `"releases-etag"`, requests[3].header.Get("If-None-Match"))
}

func TestRateLimitResponseStopsRequestsUntilResetAndWarns(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(time.Hour).Truncate(time.Second)
	var mu sync.Mutex
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.Header().Set("X-Ratelimit-Remaining", "0")
		w.Header().Set("X-Ratelimit-Reset", fmt.Sprintf("%d", resetAt.Unix()))
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{CheckInterval: time.Hour, DownloadBaseURL: downloadBaseURL, Enabled: true}
	c, h := newTestChecker(t, cfg, srv.URL)
	c.check(context.Background())

	mu.Lock()
	assert.Equal(t, 1, requestCount, "the list request must stop locally after the latest endpoint reports exhaustion")
	mu.Unlock()
	records := h.recordsAbove(slog.LevelDebug)
	require.Len(t, records, 1)
	assert.Equal(t, slog.LevelWarn, records[0].Level)
	assert.Equal(t, "release check skipped", records[0].Message)
}

func TestReleaseByTagRequiresMatchingStrictResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    []byte
		found   bool
		wantErr string
	}{
		{
			name:   "matching release",
			status: http.StatusOK,
			body:   releaseJSON(t, stableEntry("v2.0.0")),
			found:  true,
		},
		{
			name:   "withdrawn release",
			status: http.StatusNotFound,
			body:   []byte(`{"message":"Not Found"}`),
		},
		{
			name:    "mismatched tag",
			status:  http.StatusOK,
			body:    releaseJSON(t, stableEntry("v1.9.1")),
			wantErr: "response tag mismatch",
		},
		{
			name:    "trailing JSON",
			status:  http.StatusOK,
			body:    append(releaseJSON(t, stableEntry("v2.0.0")), []byte("\n{}")...),
			wantErr: "unexpected trailing JSON value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gh := newGHServer(t)
			gh.setTag("v2.0.0", tt.status, tt.body)
			client := newGitHubClient(gh.srv.URL, "test-version", slog.Default())

			rel, found, err := client.fetchReleaseByTag(context.Background(), "v2.0.0")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				assert.False(t, found)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.found, found)
			if found {
				assert.Equal(t, "v2.0.0", rel.TagName)
			}
		})
	}
}

func TestHTTPTransportErrorDoesNotExposeRequestURL(t *testing.T) {
	t.Parallel()

	const sensitiveURL = "https://user:password@example.com/releases?token=secret"
	client := newGitHubClient("https://example.com", "test-version", slog.Default())
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{
			Op:  http.MethodGet,
			URL: sensitiveURL,
			Err: fmt.Errorf("dial failed"),
		}
	})

	resp, err := client.get(context.Background(), sensitiveURL, "")
	if resp != nil {
		closeBody(resp)
	}
	require.Error(t, err)
	assert.ErrorContains(t, err, "dial failed")
	assert.NotContains(t, err.Error(), sensitiveURL)
	assert.NotContains(t, err.Error(), "password")
	assert.NotContains(t, err.Error(), "secret")
}

// RC selection is a semver max-compare, never list position. All
// entries share a publish time and the newest RC is buried mid-list; rc.10
// must beat rc.9 (numeric, not lexicographic prerelease comparison).
func TestRCPickedBySemverMaxNotListOrder(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases_rc_order.json"))
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.10", snap.LatestRC.Version)
}

func TestRCMetadataUsesCanonicalTagInsteadOfGitHubPrereleaseFlag(t *testing.T) {
	t.Parallel()

	misflagged := rcEntry("v1.2.3-rc.1")
	misflagged.Prerelease = false
	release := latestRC([]githubRelease{misflagged})

	require.NotNil(t, release)
	assert.True(t, release.Prerelease, "the UI warning must follow the grammar used to select the RC channel")
}

// R4: one malformed entry in the list is skipped without aborting the cycle.
func TestMalformedListEntrySkipped(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases_malformed_entry.json"))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v0.2.9", snap.LatestStable.Version)
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.5", snap.LatestRC.Version)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestLatestFallbackFailureStillUsesReleaseList(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusInternalServerError, []byte("boom"))
	gh.setList(http.StatusOK, fixture(t, "releases.json"))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v0.2.8", snap.LatestStable.Version)
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.5", snap.LatestRC.Version)
	assert.False(t, snap.FetchedAt.IsZero())
	assert.True(t, snap.StableAvailable)
	assert.True(t, snap.RCAvailable)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestLatestFallbackFailureRetainsCachedStable(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v0.3.0-rc.1")}))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())
	before := c.Snapshot()
	require.NotNil(t, before.LatestStable)
	assert.Equal(t, "v0.2.9", before.LatestStable.Version)

	gh.setTag("v0.2.9", http.StatusOK, releaseJSON(t, stableEntry("v0.2.9")))
	gh.setLatest(http.StatusInternalServerError, []byte("boom"))
	c.check(context.Background())

	after := c.Snapshot()
	require.NotNil(t, after.LatestStable)
	assert.Equal(t, before.LatestStable.Version, after.LatestStable.Version)
	require.NotNil(t, after.LatestRC)
	assert.Equal(t, "v0.3.0-rc.1", after.LatestRC.Version)
	assert.True(t, after.StableAvailable)
	assert.True(t, after.RCAvailable)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestUnusableLatestFallbackRetainsCachedStable(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v0.3.0-rc.1")}))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())
	before := c.Snapshot()
	require.NotNil(t, before.LatestStable)
	assert.Equal(t, "v0.2.9", before.LatestStable.Version)

	gh.setTag("v0.2.9", http.StatusOK, releaseJSON(t, stableEntry("v0.2.9")))
	gh.setLatest(http.StatusOK, fixture(t, "latest_invalid_tag.json"))
	c.check(context.Background())

	after := c.Snapshot()
	require.NotNil(t, after.LatestStable)
	assert.Equal(t, before.LatestStable.Version, after.LatestStable.Version)
	require.NotNil(t, after.LatestRC)
	assert.Equal(t, "v0.3.0-rc.1", after.LatestRC.Version)
	assert.True(t, after.StableAvailable)
	assert.True(t, after.RCAvailable)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestLowerLatestFallbackDoesNotReplaceHigherCachedStable(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v2.0.0")))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())
	before := c.Snapshot()
	require.NotNil(t, before.LatestStable)
	assert.Equal(t, "v2.0.0", before.LatestStable.Version)

	gh.setTag("v2.0.0", http.StatusOK, releaseJSON(t, stableEntry("v2.0.0")))
	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v1.9.1")))
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.1")}))
	c.check(context.Background())

	after := c.Snapshot()
	require.NotNil(t, after.LatestStable)
	assert.Equal(t, before.LatestStable, after.LatestStable)
	require.NotNil(t, after.LatestRC)
	assert.Equal(t, "v2.1.0-rc.1", after.LatestRC.Version)
	assert.True(t, after.StableAvailable)
	assert.True(t, after.RCAvailable)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestWithdrawnCachedStableFallsBackToCurrentRelease(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v2.0.0")))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())
	require.Equal(t, "v2.0.0", c.Snapshot().LatestStable.Version)

	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v1.9.1")))
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.1")}))
	gh.setTag("v2.0.0", http.StatusNotFound, []byte(`{"message":"Not Found"}`))
	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v1.9.1", snap.LatestStable.Version)
	assert.True(t, snap.StableAvailable)
	assert.True(t, snap.RCAvailable)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestReclassifiedCachedStableFallsBackToCurrentRelease(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v2.0.0")))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())
	require.Equal(t, "v2.0.0", c.Snapshot().LatestStable.Version)

	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v1.9.1")))
	reclassified := stableEntry("v2.0.0")
	reclassified.Prerelease = true
	gh.setTag("v2.0.0", http.StatusOK, releaseJSON(t, reclassified))
	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v1.9.1", snap.LatestStable.Version)
	assert.True(t, snap.StableAvailable)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestCachedStableRevalidationFailureMarksOnlyStableUnavailable(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v2.0.0")))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())
	require.Equal(t, "v2.0.0", c.Snapshot().LatestStable.Version)

	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v1.9.1")))
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.1")}))
	gh.setTag("v2.0.0", http.StatusInternalServerError, []byte("boom"))
	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v2.0.0", snap.LatestStable.Version, "retain data for a later retry")
	assert.False(t, snap.StableAvailable, "do not advertise a cached release that could not be revalidated")
	assert.True(t, snap.RCAvailable, "stable revalidation must not suppress a fresh RC view")
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestRepeatedRevalidationFailureWarnsWithoutDroppingCachedRelease(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v2.0.0")))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)
	c.check(context.Background())
	require.Equal(t, "v2.0.0", c.Snapshot().LatestStable.Version)

	gh.setLatest(http.StatusOK, releaseJSON(t, stableEntry("v1.9.1")))
	gh.setTag("v2.0.0", http.StatusInternalServerError, []byte("boom"))
	for range revalidationWarningThreshold + 1 {
		c.check(context.Background())
	}

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v2.0.0", snap.LatestStable.Version, "transient failures never prove the cached release was withdrawn")
	assert.False(t, snap.StableAvailable)
	warnings := h.recordsAbove(slog.LevelDebug)
	require.Len(t, warnings, 1, "warn once when the threshold is crossed, not on every later cycle")
	assert.Equal(t, slog.LevelWarn, warnings[0].Level)
	assert.Equal(t, "cached release revalidation repeatedly unavailable", warnings[0].Message)

	gh.setTag("v2.0.0", http.StatusOK, releaseJSON(t, stableEntry("v2.0.0")))
	c.check(context.Background())
	gh.setTag("v2.0.0", http.StatusInternalServerError, []byte("boom"))
	for range revalidationWarningThreshold {
		c.check(context.Background())
	}
	assert.Len(t, h.recordsAbove(slog.LevelDebug), 2, "a successful revalidation resets the warning threshold")
}

func TestHigherCachedRCIsRevalidatedAndPreserved(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.5")}))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())
	require.Equal(t, "v2.1.0-rc.5", c.Snapshot().LatestRC.Version)

	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.4")}))
	gh.setTag("v2.1.0-rc.5", http.StatusOK, releaseJSON(t, rcEntry("v2.1.0-rc.5")))
	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v2.1.0-rc.5", snap.LatestRC.Version)
	assert.True(t, snap.RCAvailable)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestWithdrawnCachedRCFallsBackToCurrentRelease(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.5")}))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())
	require.Equal(t, "v2.1.0-rc.5", c.Snapshot().LatestRC.Version)

	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.4")}))
	gh.setTag("v2.1.0-rc.5", http.StatusNotFound, []byte(`{"message":"Not Found"}`))
	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v2.1.0-rc.4", snap.LatestRC.Version)
	assert.True(t, snap.RCAvailable)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestCachedRCRevalidationFailureMarksOnlyRCUnavailable(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.5")}))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())
	require.Equal(t, "v2.1.0-rc.5", c.Snapshot().LatestRC.Version)

	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v2.1.0-rc.4")}))
	gh.setTag("v2.1.0-rc.5", http.StatusInternalServerError, []byte("boom"))
	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v2.1.0-rc.5", snap.LatestRC.Version, "retain data for a later retry")
	assert.True(t, snap.StableAvailable)
	assert.False(t, snap.RCAvailable, "do not advertise an incomplete RC view")
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

func TestLatestFallbackFailureWithoutStableMarksStatusUnavailable(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusInternalServerError, []byte("boom"))
	gh.setList(http.StatusOK, releasesJSON(t, []githubRelease{rcEntry("v0.3.0-rc.1")}))
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)

	c.check(context.Background())

	snap := c.Snapshot()
	assert.Nil(t, snap.LatestStable)
	require.NotNil(t, snap.LatestRC)
	assert.False(t, snap.StableAvailable)
	assert.True(t, snap.RCAvailable)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

// Every release-list failure mode degrades silently — the previous snapshot
// data is retained but marked unavailable, no error escapes, and nothing is
// logged above Debug.
func TestFailuresRetainSnapshotSilently(t *testing.T) {
	t.Parallel()

	tooManyListEntries := releasesJSON(t, nightlies(releasesPageSize+1, "too-many"))
	tests := []struct {
		name   string
		induce func(gh *ghServer)
	}{
		{
			name: "server error",
			induce: func(gh *ghServer) {
				gh.setLatest(http.StatusInternalServerError, []byte("boom"))
				gh.setList(http.StatusInternalServerError, []byte("boom"))
			},
		},
		{
			name: "rate limited",
			induce: func(gh *ghServer) {
				body := []byte(`{"message":"API rate limit exceeded"}`)
				gh.setLatest(http.StatusForbidden, body)
				gh.setList(http.StatusForbidden, body)
			},
		},
		{
			name: "malformed JSON",
			induce: func(gh *ghServer) {
				gh.setLatest(http.StatusOK, []byte(`{"tag_name":`))
				gh.setList(http.StatusOK, []byte(`[{"tag_name":`))
			},
		},
		{
			name: "partial failure keeps whole previous snapshot",
			induce: func(gh *ghServer) {
				gh.setList(http.StatusInternalServerError, []byte("boom"))
			},
		},
		{
			name: "oversized list body",
			induce: func(gh *ghServer) {
				gh.setList(http.StatusOK, []byte(`[{"tag_name":"`+strings.Repeat("a", maxResponseBytes)+`"}]`))
			},
		},
		{
			name: "too many list entries",
			induce: func(gh *ghServer) {
				gh.setList(http.StatusOK, tooManyListEntries)
			},
		},
		{
			name:   "connection failure",
			induce: func(gh *ghServer) { gh.srv.Close() },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gh := newGHServer(t)
			gh.setList(http.StatusOK, fixture(t, "releases.json"))
			c, h := newTestChecker(t, gh.config(), gh.srv.URL)

			c.check(context.Background())
			before := c.Snapshot()
			require.NotNil(t, before.LatestStable, "test needs a primed snapshot")
			require.NotNil(t, before.LatestRC)

			tt.induce(gh)
			c.check(context.Background())

			want := before
			want.StableAvailable = false
			want.RCAvailable = false
			assert.Equal(t, want, c.Snapshot(), "failed cycle must retain cached release data and mark status unavailable")
			assert.Empty(t, h.recordsAbove(slog.LevelDebug),
				"failure paths must log at Debug only")
		})
	}
}

// Release-note links are derived from the fixed repository and validated tag,
// never from the upstream html_url field rendered by the client.
func TestNotesURLUsesCanonicalRepositoryAndTag(t *testing.T) {
	t.Parallel()

	var upstream githubRelease
	require.NoError(t, json.Unmarshal([]byte(`{
		"tag_name":"v0.2.9",
		"html_url":"https://github.com@something.example/phishing"
	}`), &upstream))
	release := newRelease(upstream)
	assert.Equal(t, "https://github.com/block/proto-fleet/releases/tag/v0.2.9", release.NotesURL)

	assert.Empty(t, releaseNotesURL("nightly-20260731"))
	assert.Empty(t, releaseNotesURL("v1.2.3/../../phishing"))
}

func TestCheckSafelyInvalidatesPrimedSnapshotAndAllowsNextCycle(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	c, h := newTestChecker(t, gh.config(), gh.srv.URL)
	calls := 0
	c.client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 3 {
			panic("unexpected transport defect")
		}
		body := []byte("[]")
		if req.URL.Path == "/releases/latest" {
			body = releaseJSON(t, stableEntry("v2.0.0"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    req,
		}, nil
	})

	c.checkSafely(context.Background())
	primed := c.Snapshot()
	require.True(t, primed.StableAvailable)
	require.True(t, primed.RCAvailable)
	require.NotNil(t, primed.LatestStable)

	assert.NotPanics(t, func() { c.checkSafely(context.Background()) })
	records := h.recordsAbove(slog.LevelDebug)
	require.Len(t, records, 1)
	assert.Equal(t, slog.LevelError, records[0].Level)
	assert.Equal(t, "release check panicked", records[0].Message)
	afterPanic := c.Snapshot()
	assert.False(t, afterPanic.StableAvailable)
	assert.False(t, afterPanic.RCAvailable)
	assert.Equal(t, primed.LatestStable, afterPanic.LatestStable,
		"panic recovery should retain cached data while making it ineligible")

	c.checkSafely(context.Background())
	stable, available := c.Snapshot().EligibleStable()
	require.True(t, available, "a later cycle must still run after a contained panic")
	require.NotNil(t, stable)
	assert.Equal(t, "v2.0.0", stable.Version)
}

// Idempotent Start, draining Stop, no-op second Stop, and a
// working Start-after-Stop.
func TestLifecycle(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases.json"))
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)
	ctx := context.Background()

	require.NoError(t, c.Start(ctx))
	c.lifecycleMu.Lock()
	firstDone := c.done
	c.lifecycleMu.Unlock()
	require.NotNil(t, firstDone, "Start must launch the polling goroutine")

	require.NoError(t, c.Start(ctx), "double Start must be safe")
	c.lifecycleMu.Lock()
	secondDone := c.done
	c.lifecycleMu.Unlock()
	assert.True(t, firstDone == secondDone, "second Start must not spawn a second goroutine")

	require.Eventually(t, func() bool { return c.Snapshot().LatestStable != nil },
		5*time.Second, 5*time.Millisecond, "first check should happen shortly after Start")

	require.NoError(t, c.Stop(ctx))
	select {
	case <-firstDone:
	default:
		t.Fatal("Stop returned before the polling goroutine drained")
	}
	require.NoError(t, c.Stop(ctx), "second Stop must be a no-op")

	// Restartable: a fresh activation performs a fresh check.
	requestsBeforeRestart := len(gh.recorded())
	require.NoError(t, c.Start(ctx))
	require.Eventually(t, func() bool { return len(gh.recorded()) > requestsBeforeRestart },
		5*time.Second, 5*time.Millisecond, "Start after Stop should check again")
	require.NoError(t, c.Stop(ctx))
}

func TestStartWithCanceledContextFails(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, c.Start(ctx))

	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	assert.Nil(t, c.done, "failed Start must leave the checker stopped")
}

// Stop must honor its context while waiting for the goroutine to drain.
// White-box: simulate an activation that never finishes.
func TestStopHonorsContext(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	c, _ := newTestChecker(t, gh.config(), gh.srv.URL)
	c.cancel = func() {}
	c.done = make(chan struct{}) // never closed: a goroutine that refuses to drain

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, c.Stop(ctx), context.Canceled)
}

func TestStartDisabledIsNoOp(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	cfg := gh.config()
	cfg.Enabled = false
	c, _ := newTestChecker(t, cfg, gh.srv.URL)

	require.NoError(t, c.Start(context.Background()))
	c.lifecycleMu.Lock()
	done := c.done
	c.lifecycleMu.Unlock()
	assert.Nil(t, done, "disabled checker must not poll")
	assert.Empty(t, gh.recorded())
	require.NoError(t, c.Stop(context.Background()))
}

// Jitter is subtract-only 10-20% so the worst-case gap never exceeds the
// configured interval.
func TestJitteredIntervalBounds(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	cfg := gh.config()
	cfg.CheckInterval = 10 * time.Hour
	c, _ := newTestChecker(t, cfg, gh.srv.URL)
	for range 200 {
		d := c.jitteredInterval()
		assert.GreaterOrEqual(t, d, 8*time.Hour)
		assert.LessOrEqual(t, d, 9*time.Hour)
	}

	cfg.CheckInterval = 0 // falls back to the 1h default
	zero, _ := newTestChecker(t, cfg, gh.srv.URL)
	for range 200 {
		d := zero.jitteredInterval()
		assert.GreaterOrEqual(t, d, time.Duration(float64(time.Hour)*0.8))
		assert.LessOrEqual(t, d, time.Duration(float64(time.Hour)*0.9))
	}
}
