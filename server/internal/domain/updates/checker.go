package updates

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const defaultCheckInterval = 24 * time.Hour

// rcTagPattern is the only grammar ever offered as a release candidate. The
// release workflow also publishes semver-valid feature-branch test builds
// (e.g. v0.2.9-pr737.1) and nightly tags, so RC candidates are selected by
// tag grammar — not by semver validity or the prerelease flag.
var rcTagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+-rc\.\d+$`)

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
	LatestStable *Release
	LatestRC     *Release
	FetchedAt    time.Time
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
	return newChecker(cfg, serverVersion, slog.Default())
}

func newChecker(cfg Config, serverVersion string, logger *slog.Logger) *Checker {
	return &Checker{
		cfg:    cfg,
		logger: logger,
		client: newGitHubClient(cfg.ReleasesAPIURL, serverVersion, logger),
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
	if c.cfg.Disabled {
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

// check runs one fetch cycle. Any failure degrades silently: the previous
// snapshot stays in place and nothing is logged above Debug — update
// notification is best-effort and must never look like a server problem.
func (c *Checker) check(ctx context.Context) {
	latest, err := c.client.fetchLatestStable(ctx)
	if err != nil {
		c.logger.Debug("release check skipped", "error", err)
		return
	}
	list, err := c.client.fetchReleases(ctx)
	if err != nil {
		c.logger.Debug("release check skipped", "error", err)
		return
	}

	snapshot := Snapshot{
		LatestStable: stableRelease(latest),
		LatestRC:     latestRC(list),
		FetchedAt:    time.Now(),
	}

	c.mu.Lock()
	c.snapshot = snapshot
	c.mu.Unlock()
}

// stableRelease converts the /releases/latest payload, rejecting drafts and
// tags that are not canonical stable semver (vX.Y.Z): hand-made tags must
// never be offered. GitHub's /releases/latest already excludes releases
// flagged prerelease, but that flag is set by hand on out-of-band publishes —
// the tag grammar is the guard here, mirroring latestRC's stance.
func stableRelease(rel githubRelease) *Release {
	if rel.Draft || !semver.IsValid(rel.TagName) || semver.Prerelease(rel.TagName) != "" {
		return nil
	}
	return newRelease(rel)
}

// latestRC picks the newest release candidate from the list by semver
// max-compare; GitHub's list order is not a reliable recency signal.
func latestRC(list []githubRelease) *Release {
	var best *githubRelease
	for i := range list {
		rel := &list[i]
		if rel.Draft || !rcTagPattern.MatchString(rel.TagName) {
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

func newRelease(rel githubRelease) *Release {
	return &Release{
		Version:     rel.TagName,
		NotesURL:    rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
		Prerelease:  rel.Prerelease,
	}
}
