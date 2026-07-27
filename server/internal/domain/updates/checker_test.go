package updates

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func (g *ghServer) setETags(latest, list string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.latestETag, g.listETag = latest, list
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
