package updates

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"regexp"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const defaultCheckInterval = time.Hour

var (
	stableTagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	rcTagPattern     = regexp.MustCompile(`^v\d+\.\d+\.\d+-rc\.\d+$`)
)

// Release describes one published GitHub release.
type Release struct {
	Version     string // git tag, e.g. "v0.2.9"
	NotesURL    string // the release's html_url
	PublishedAt time.Time
	Prerelease  bool
}

// Snapshot is the checker's view of the newest available releases. Zero value
// until the first successful fetch.
type Snapshot struct {
	LatestStable    *Release
	LatestRC        *Release
	FetchedAt       time.Time
	StableAvailable bool
	RCAvailable     bool
}

// Checker periodically fetches release metadata from GitHub and caches both
// the latest stable release and the latest release candidate; readers pick a
// channel at read time. It implements runtimejobs.Lifecycle.
type Checker struct {
	cfg    Config
	logger *slog.Logger
	client *githubClient

	mu       sync.Mutex
	snapshot Snapshot

	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

// NewChecker creates a release checker; serverVersion identifies this fleetd
// build in the User-Agent header.
func NewChecker(cfg Config, serverVersion string) *Checker {
	return newChecker(cfg, releaseAPIBaseURL, serverVersion, slog.Default())
}

func newChecker(cfg Config, releasesAPIURL, serverVersion string, logger *slog.Logger) *Checker {
	return &Checker{
		cfg:    cfg,
		logger: logger,
		client: newGitHubClient(releasesAPIURL, serverVersion, logger),
	}
}

// Snapshot returns a copy of the latest fetch result. Zero value before the
// first successful fetch.
func (c *Checker) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap := c.snapshot
	if snap.LatestStable != nil {
		stable := *snap.LatestStable
		snap.LatestStable = &stable
	}
	if snap.LatestRC != nil {
		rc := *snap.LatestRC
		snap.LatestRC = &rc
	}
	return snap
}

// Start launches the polling goroutine: one non-blocking check shortly after
// start, then one per (jittered) interval. It is a no-op when the checker is
// disabled or already running.
func (c *Checker) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start release checker: %w", err)
	}
	if !c.cfg.Enabled {
		return nil
	}

	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	if c.cancel != nil {
		select {
		case <-c.done:
			return fmt.Errorf("release checker activation ended before stop")
		default:
			return nil
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.cancel = cancel
	c.done = done
	go func() {
		defer close(done)
		c.run(runCtx)
	}()
	return nil
}

// Stop cancels the polling goroutine and waits for it to drain, honoring ctx.
// It is a no-op when the checker is not running, and a stopped checker can be
// started again.
func (c *Checker) Stop(ctx context.Context) error {
	c.lifecycleMu.Lock()
	if c.cancel == nil {
		c.lifecycleMu.Unlock()
		return nil
	}
	cancel := c.cancel
	done := c.done
	c.lifecycleMu.Unlock()

	cancel()
	select {
	case <-done:
		c.lifecycleMu.Lock()
		if c.done == done {
			c.cancel = nil
			c.done = nil
		}
		c.lifecycleMu.Unlock()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop release checker: %w", ctx.Err())
	}
}

func (c *Checker) run(ctx context.Context) {
	c.check(ctx)

	timer := time.NewTimer(c.jitteredInterval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			c.check(ctx)
			timer.Reset(c.jitteredInterval())
		}
	}
}

// jitteredInterval subtracts 10-20% random jitter from the configured
// interval. Subtract-only: it staggers fleets restarted together without ever
// stretching the worst-case gap past the configured interval.
func (c *Checker) jitteredInterval() time.Duration {
	interval := c.cfg.CheckInterval
	if interval <= 0 {
		interval = defaultCheckInterval
	}
	jitter := 0.10 + 0.10*rand.Float64()
	return time.Duration(float64(interval) * (1 - jitter))
}

// check runs one fetch cycle. A list failure retains the previous release data
// but marks both channels unavailable. The latest endpoint is only a stable
// fallback, so its failure must not hide releases discovered from the
// authoritative list page. Cached releases that age out of those bounded
// responses are revalidated by exact tag before they remain eligible. Nothing
// is logged above Debug — update notification is best-effort and must never
// look like a server problem.
func (c *Checker) check(ctx context.Context) {
	latest, latestErr := c.client.fetchLatestStableFallback(ctx)
	list, err := c.client.fetchReleases(ctx)
	if err != nil {
		c.logger.Debug("release check skipped", "error", err)
		c.mu.Lock()
		c.snapshot.StableAvailable = false
		c.snapshot.RCAvailable = false
		c.mu.Unlock()
		return
	}
	previous := c.Snapshot()
	stable := latestStable(latest, list)
	if latestErr != nil {
		c.logger.Debug("stable release fallback unavailable", "error", latestErr)
	}
	stable, stableComplete := c.reconcileCachedRelease(
		ctx,
		"stable",
		stable,
		previous.LatestStable,
		isEligibleStableRelease,
	)
	rc, rcComplete := c.reconcileCachedRelease(
		ctx,
		"release candidate",
		latestRC(list),
		previous.LatestRC,
		isEligibleRCRelease,
	)

	snapshot := Snapshot{
		LatestStable:    stable,
		LatestRC:        rc,
		FetchedAt:       time.Now(),
		StableAvailable: stableComplete && stable != nil,
		RCAvailable:     rcComplete,
	}

	c.mu.Lock()
	c.snapshot = snapshot
	c.mu.Unlock()
}

// reconcileCachedRelease preserves a higher cached candidate only after the
// exact tag endpoint confirms it is still published and eligible for the same
// channel. Confirmed absence or reclassification drops it in favor of the
// current bounded result. A transient revalidation failure retains the cached
// data for a later retry but reports an incomplete channel so consumers cannot
// offer the unverified release.
func (c *Checker) reconcileCachedRelease(
	ctx context.Context,
	channel string,
	current *Release,
	cached *Release,
	eligible func(githubRelease) bool,
) (*Release, bool) {
	if cached == nil ||
		(current != nil && semver.Compare(cached.Version, current.Version) <= 0) {
		return current, true
	}

	rel, found, err := c.client.fetchReleaseByTag(ctx, cached.Version)
	if err != nil {
		c.logger.Debug("cached release revalidation unavailable", "channel", channel, "error", err)
		return cached, false
	}
	if !found || !eligible(rel) {
		return current, true
	}
	return newRelease(rel), true
}

// latestStable picks the semantic-version maximum canonical stable tag from
// both GitHub's created-at-based /releases/latest candidate and the first
// release-list page. The endpoint remains a fallback when prereleases crowd
// every stable release out of the list page.
func latestStable(latest githubRelease, list []githubRelease) *Release {
	var best *githubRelease
	if isEligibleStableRelease(latest) {
		best = &latest
	}
	for i := range list {
		rel := &list[i]
		if !isEligibleStableRelease(*rel) {
			continue
		}
		if best == nil || semver.Compare(rel.TagName, best.TagName) > 0 {
			best = rel
		}
	}
	if best == nil {
		return nil
	}
	return newRelease(*best)
}

// latestRC picks the newest release candidate from the list by semver
// max-compare; GitHub's list order is not a reliable recency signal.
func latestRC(list []githubRelease) *Release {
	var best *githubRelease
	for i := range list {
		rel := &list[i]
		if !isEligibleRCRelease(*rel) {
			continue
		}
		if best == nil || semver.Compare(rel.TagName, best.TagName) > 0 {
			best = rel
		}
	}
	if best == nil {
		return nil
	}
	return newRelease(*best)
}

func isEligibleStableRelease(rel githubRelease) bool {
	return !rel.Draft && !rel.Prerelease && isCanonicalStableTag(rel.TagName)
}

func isEligibleRCRelease(rel githubRelease) bool {
	return !rel.Draft && isCanonicalRCTag(rel.TagName)
}

// Canonical release tags require the full vMAJOR.MINOR.PATCH form with no
// leading zeros or build metadata. The grammar excludes other prerelease
// families; semver validation tightens numeric components the regex admits.
func isCanonicalStableTag(tag string) bool {
	return stableTagPattern.MatchString(tag) && semver.IsValid(tag)
}

func isCanonicalRCTag(tag string) bool {
	return rcTagPattern.MatchString(tag) && semver.IsValid(tag)
}

func isCanonicalReleaseTag(tag string) bool {
	return isCanonicalStableTag(tag) || isCanonicalRCTag(tag)
}

func newRelease(rel githubRelease) *Release {
	return &Release{
		Version:     rel.TagName,
		NotesURL:    safeNotesURL(rel.HTMLURL),
		PublishedAt: rel.PublishedAt,
		Prerelease:  rel.Prerelease,
	}
}

// safeNotesURL keeps a body-derived html_url only when it parses as an
// absolute https URL. Unlike the tag, this field never passes a grammar
// check, and the client renders it as a link — so a compromised or
// misconfigured releases API could otherwise smuggle a javascript: href into
// the UI. Anything short of https is dropped and the client hides the link.
// This extends Config.Validate's https-only stance to body-derived fields.
func safeNotesURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return raw
}
