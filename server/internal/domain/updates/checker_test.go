package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

// releasesJSON marshals entries in the GitHub list-response shape; pagination
// tests need full 100-entry pages, which would be unwieldy as testdata files.
func releasesJSON(t *testing.T, entries []githubRelease) []byte {
	t.Helper()
	body, err := json.Marshal(entries)
	require.NoError(t, err)
	return body
}

// nightlies fabricates n non-RC prerelease entries; label keeps tags unique
// across pages.
func nightlies(n int, label string) []githubRelease {
	out := make([]githubRelease, n)
	for i := range out {
		tag := fmt.Sprintf("nightly-%s-%03d", label, i)
		out[i] = githubRelease{
			TagName:     tag,
			HTMLURL:     "https://github.com/block/proto-fleet/releases/tag/" + tag,
			PublishedAt: time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC),
			Prerelease:  true,
		}
	}
	return out
}

func rcEntry(tag string) githubRelease {
	return githubRelease{
		TagName:     tag,
		HTMLURL:     "https://github.com/block/proto-fleet/releases/tag/" + tag,
		PublishedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		Prerelease:  true,
	}
}

type ghRequest struct {
	path   string
	query  url.Values
	header http.Header
}

// ghServer is an httptest fixture standing in for the GitHub releases API.
type ghServer struct {
	srv *httptest.Server

	mu           sync.Mutex
	latestStatus int
	latestBody   []byte
	latestETag   string
	listStatus   int
	listBody     []byte
	listPages    [][]byte // per-page list bodies; when set, overrides listBody
	listETag     string
	requests     []ghRequest
	notModified  int
}

func newGHServer(t *testing.T) *ghServer {
	t.Helper()
	g := &ghServer{
		latestStatus: http.StatusOK,
		latestBody:   fixture(t, "latest_stable.json"),
		listStatus:   http.StatusOK,
		listBody:     []byte("[]"),
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
	var etag string
	switch r.URL.Path {
	case "/releases/latest":
		status, body, etag = g.latestStatus, g.latestBody, g.latestETag
	case "/releases":
		status, body, etag = g.listStatus, g.listBody, g.listETag
		if g.listPages != nil {
			body = []byte("[]") // pages past the end: the list is exhausted
			if page, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && page >= 1 && page <= len(g.listPages) {
				body = g.listPages[page-1]
			}
		}
	default:
		http.NotFound(w, r)
		return
	}

	if etag != "" {
		w.Header().Set("ETag", etag)
		if status == http.StatusOK && r.Header.Get("If-None-Match") == etag {
			g.notModified++
			w.WriteHeader(http.StatusNotModified)
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

func (g *ghServer) setListPages(pages ...[]byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.listPages = pages
}

func (g *ghServer) setETags(latest, list string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.latestETag, g.listETag = latest, list
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

func (g *ghServer) notModifiedCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.notModified
}

func (g *ghServer) config() Config {
	return Config{
		CheckInterval:   24 * time.Hour,
		ReleasesAPIURL:  g.srv.URL,
		DownloadBaseURL: "https://github.com/block/proto-fleet/releases/download",
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

func newTestChecker(t *testing.T, cfg Config) (*Checker, *recordingHandler) {
	t.Helper()
	h := &recordingHandler{}
	return newChecker(cfg, "test-version", slog.New(h)), h
}

// A stable release from /releases/latest lands in the snapshot
// with version, notes URL, and publish time. Also pins the request headers.
func TestCheckCachesLatestStable(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	c, _ := newTestChecker(t, gh.config())

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v0.2.9", snap.LatestStable.Version)
	assert.Equal(t, "https://github.com/block/proto-fleet/releases/tag/v0.2.9", snap.LatestStable.NotesURL)
	assert.Equal(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC), snap.LatestStable.PublishedAt.UTC())
	assert.False(t, snap.LatestStable.Prerelease)
	assert.Nil(t, snap.LatestRC)
	assert.False(t, snap.FetchedAt.IsZero())

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

// The checker is channel-agnostic — it caches BOTH the
// latest stable and the latest RC; channel filtering happens at read time.
// The fixture contains semver-valid
// feature-branch builds (v0.3.0-pr800.1 outranks every RC under plain semver)
// that must never be selected because only vX.Y.Z-rc.N grammar qualifies.
func TestCheckCachesBothStableAndRC(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases.json"))
	c, _ := newTestChecker(t, gh.config())

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v0.2.9", snap.LatestStable.Version)
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.5", snap.LatestRC.Version)
	assert.Equal(t, "https://github.com/block/proto-fleet/releases/tag/v0.2.9-rc.5", snap.LatestRC.NotesURL)
	assert.True(t, snap.LatestRC.Prerelease)
}

// A prerelease-grammar tag served by /releases/latest — a release published
// by hand without the prerelease flag — must not become the stable offer;
// the tag grammar decides, not GitHub's flag.
func TestMisflaggedPrereleaseTagNeverStableOffer(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setLatest(http.StatusOK, fixture(t, "latest_misflagged_rc.json"))
	gh.setList(http.StatusOK, fixture(t, "releases.json"))
	c, _ := newTestChecker(t, gh.config())

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
	gh.setList(http.StatusOK, fixture(t, "releases.json"))
	c, _ := newTestChecker(t, gh.config())

	c.check(context.Background())

	snap := c.Snapshot()
	assert.Nil(t, snap.LatestStable, "non-semver /releases/latest tag must not become the stable offer")
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.5", snap.LatestRC.Version)
	assert.False(t, snap.FetchedAt.IsZero(), "an invalid tag is not a fetch failure; the cycle still succeeds")
}

// With per_page=100 requested (the API max), an RC preceded by a
// month of nightlies on the same page is still found.
func TestRCSelectedBehindManyNightlies(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases_nightlies.json"))
	c, _ := newTestChecker(t, gh.config())

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
}

// An RC crowded past page 1 by daily nightlies is still found: the crawl
// follows pagination until the RC predicate is satisfied.
func TestRCFoundBeyondFirstPage(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setListPages(
		releasesJSON(t, nightlies(releasesPageSize, "p1")),
		releasesJSON(t, append(nightlies(3, "p2"), rcEntry("v0.2.9-rc.6"))),
	)
	c, _ := newTestChecker(t, gh.config())

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.6", snap.LatestRC.Version)

	listReqs := gh.listRequests()
	require.Len(t, listReqs, 2)
	assert.Equal(t, "1", listReqs[0].query.Get("page"))
	assert.Equal(t, "2", listReqs[1].query.Get("page"))
}

// A full first page that already holds an RC stops the crawl: deeper pages
// only offer older releases, so no second request is spent.
func TestPaginationStopsOncePageOneHasRC(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setListPages(
		releasesJSON(t, append(nightlies(releasesPageSize-1, "p1"), rcEntry("v0.2.9-rc.5"))),
		releasesJSON(t, []githubRelease{rcEntry("v0.2.9-rc.1")}),
	)
	c, _ := newTestChecker(t, gh.config())

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.5", snap.LatestRC.Version)
	assert.Len(t, gh.listRequests(), 1,
		"page 1 already satisfied the RC predicate; no deeper page should be fetched")
}

// A 304 on page 1 replays the whole previously accumulated crawl — including
// entries that came from deeper pages — without any further requests.
func TestNotModifiedReplaysMultiPageCrawl(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setListPages(
		releasesJSON(t, nightlies(releasesPageSize, "p1")),
		releasesJSON(t, []githubRelease{rcEntry("v0.2.9-rc.6")}),
	)
	gh.setETags("", `"etag-list-1"`)
	c, _ := newTestChecker(t, gh.config())

	c.check(context.Background())
	first := c.Snapshot()
	require.NotNil(t, first.LatestRC)
	require.Len(t, gh.listRequests(), 2, "test needs a multi-page crawl to prime the cache")

	c.check(context.Background())

	second := c.Snapshot()
	require.NotNil(t, second.LatestRC)
	assert.Equal(t, first.LatestRC, second.LatestRC,
		"the 304 replay must include entries crawled from deeper pages")
	assert.Len(t, gh.listRequests(), 3,
		"a 304 on page 1 must answer the whole cycle without deeper-page requests")
	assert.Equal(t, 1, gh.notModifiedCount())
}

// The crawl is bounded: a list that never satisfies the predicate stops at
// maxReleasePages instead of walking the repository's entire history.
func TestPaginationBoundedAtMaxPages(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	pages := make([][]byte, maxReleasePages+1)
	for i := range pages {
		pages[i] = releasesJSON(t, nightlies(releasesPageSize, fmt.Sprintf("p%d", i+1)))
	}
	gh.setListPages(pages...)
	c, _ := newTestChecker(t, gh.config())

	c.check(context.Background())

	assert.Nil(t, c.Snapshot().LatestRC)
	assert.Len(t, gh.listRequests(), maxReleasePages)
}

// RC selection is a semver max-compare, never list position. All
// entries share a publish time and the newest RC is buried mid-list; rc.10
// must beat rc.9 (numeric, not lexicographic prerelease comparison).
func TestRCPickedBySemverMaxNotListOrder(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases_rc_order.json"))
	c, _ := newTestChecker(t, gh.config())

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.10", snap.LatestRC.Version)
}

// R4: one malformed entry in the list is skipped without aborting the cycle.
func TestMalformedListEntrySkipped(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases_malformed_entry.json"))
	c, h := newTestChecker(t, gh.config())

	c.check(context.Background())

	snap := c.Snapshot()
	require.NotNil(t, snap.LatestStable)
	assert.Equal(t, "v0.2.9", snap.LatestStable.Version)
	require.NotNil(t, snap.LatestRC)
	assert.Equal(t, "v0.2.9-rc.5", snap.LatestRC.Version)
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))
}

// Every failure mode degrades silently — the previous
// snapshot is retained, no error escapes, and nothing is logged above Debug.
func TestFailuresRetainSnapshotSilently(t *testing.T) {
	t.Parallel()

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
			// The remote controls the body size regardless of per_page; a
			// body past maxResponseBytes is truncated by the read cap and
			// fails the decode.
			name: "oversized latest body",
			induce: func(gh *ghServer) {
				gh.setLatest(http.StatusOK, []byte(`{"tag_name":"`+strings.Repeat("a", maxResponseBytes)+`"}`))
			},
		},
		{
			name: "oversized list body",
			induce: func(gh *ghServer) {
				gh.setList(http.StatusOK, []byte(`[{"tag_name":"`+strings.Repeat("a", maxResponseBytes)+`"}]`))
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
			c, h := newTestChecker(t, gh.config())

			c.check(context.Background())
			before := c.Snapshot()
			require.NotNil(t, before.LatestStable, "test needs a primed snapshot")
			require.NotNil(t, before.LatestRC)

			tt.induce(gh)
			c.check(context.Background())

			assert.Equal(t, before, c.Snapshot(), "failed cycle must retain the previous snapshot untouched")
			assert.Empty(t, h.recordsAbove(slog.LevelDebug),
				"failure paths must log at Debug only")
		})
	}
}

// A 304 Not Modified reuses the previously parsed result. The 304
// responses have empty bodies, so the snapshot content can only come from the
// cached parse; FetchedAt advancing proves the cycle counted as a success.
func TestNotModifiedReusesCachedParse(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases.json"))
	gh.setETags(`"etag-latest-1"`, `"etag-list-1"`)
	c, h := newTestChecker(t, gh.config())

	c.check(context.Background())
	first := c.Snapshot()
	require.NotNil(t, first.LatestStable)
	require.NotNil(t, first.LatestRC)

	time.Sleep(2 * time.Millisecond) // make FetchedAt strictly comparable
	c.check(context.Background())

	second := c.Snapshot()
	assert.Equal(t, 2, gh.notModifiedCount(), "second cycle should be served entirely from 304s")
	require.NotNil(t, second.LatestStable)
	require.NotNil(t, second.LatestRC)
	assert.Equal(t, first.LatestStable, second.LatestStable)
	assert.Equal(t, first.LatestRC, second.LatestRC)
	assert.True(t, second.FetchedAt.After(first.FetchedAt), "a 304 cycle is a successful check")
	assert.Empty(t, h.recordsAbove(slog.LevelDebug))

	reqs := gh.recorded()
	require.Len(t, reqs, 4)
	assert.Equal(t, `"etag-latest-1"`, reqs[2].header.Get("If-None-Match"))
	assert.Equal(t, `"etag-list-1"`, reqs[3].header.Get("If-None-Match"))
}

// A body-derived notes URL is kept only when it is an absolute https URL.
// The tag grammar guards the install command; this guards the rendered link:
// a compromised or misconfigured releases API must not smuggle a javascript:
// href into the UI. Dropped URLs come through empty and the client hides the
// link.
func TestNotesURLRequiresHTTPS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, htmlURL, want string
	}{
		{"https kept", "https://github.com/block/proto-fleet/releases/tag/v0.2.9", "https://github.com/block/proto-fleet/releases/tag/v0.2.9"},
		{"javascript scheme dropped", "javascript:alert(document.domain)", ""},
		{"http dropped", "http://github.com/block/proto-fleet/releases/tag/v0.2.9", ""},
		{"scheme-relative dropped", "//github.com/block/proto-fleet/releases/tag/v0.2.9", ""},
		{"missing host dropped", "https:///releases/tag/v0.2.9", ""},
		{"unparseable dropped", "https://%zz", ""},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rel := newRelease(githubRelease{TagName: "v0.2.9", HTMLURL: tt.htmlURL})
			assert.Equal(t, tt.want, rel.NotesURL)
		})
	}
}

// Idempotent Start, draining Stop, no-op second Stop, and a
// working Start-after-Stop.
func TestLifecycle(t *testing.T) {
	t.Parallel()

	gh := newGHServer(t)
	gh.setList(http.StatusOK, fixture(t, "releases.json"))
	c, _ := newTestChecker(t, gh.config())
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
	c, _ := newTestChecker(t, gh.config())

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
	c, _ := newTestChecker(t, gh.config())
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
	cfg.Disabled = true
	c, _ := newTestChecker(t, cfg)

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
	c, _ := newTestChecker(t, cfg)
	for range 200 {
		d := c.jitteredInterval()
		assert.GreaterOrEqual(t, d, 8*time.Hour)
		assert.LessOrEqual(t, d, 9*time.Hour)
	}

	cfg.CheckInterval = 0 // falls back to the 24h default
	zero, _ := newTestChecker(t, cfg)
	for range 200 {
		d := zero.jitteredInterval()
		assert.GreaterOrEqual(t, d, time.Duration(float64(24*time.Hour)*0.8))
		assert.LessOrEqual(t, d, time.Duration(float64(24*time.Hour)*0.9))
	}
}
