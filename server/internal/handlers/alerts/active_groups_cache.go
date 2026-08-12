package alerts

import (
	"context"
	"strconv"
	"time"

	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/sync/singleflight"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
)

const (
	// Under the dashboard's 15s poll, so a lone viewer never waits an extra interval for a change.
	activeGroupsTTL = 10 * time.Second
	// Orgs served by one process are few; this bounds an unbounded-growth path, it is not a working-set tune.
	activeGroupsCacheSize = 256
	// Bounds the detached fetch so a stuck DB connection can't leak the goroutine; not a latency target.
	activeGroupsFetchTimeout = 30 * time.Second
)

// activeGroupsCache serves the dashboard's rollup poll from one query per org per TTL: the rollup is org-global
// and moves only on re-assert. It holds shared store rows, not per-caller responses, so treat entries read-only.
type activeGroupsCache struct {
	entries *lru.LRU[int64, []notificationhistory.ActiveAlertGroup]
	single  singleflight.Group
	history notificationhistory.Lister
}

func newActiveGroupsCache(history notificationhistory.Lister) *activeGroupsCache {
	return &activeGroupsCache{
		entries: lru.NewLRU[int64, []notificationhistory.ActiveAlertGroup](activeGroupsCacheSize, nil, activeGroupsTTL),
		history: history,
	}
}

// get runs at most one aggregate per org per TTL. The page limit is fixed here, not taken from the caller: it
// isn't in the cache key, so a caller asking for another would silently get whatever the live flight fetched.
func (c *activeGroupsCache) get(ctx context.Context, orgID int64) ([]notificationhistory.ActiveAlertGroup, error) {
	if groups, ok := c.entries.Get(orgID); ok {
		return groups, nil
	}

	ch := c.single.DoChan(strconv.FormatInt(orgID, 10), func() (any, error) {
		// Detached, so whoever won the slot can't poison its siblings by navigating away mid-poll.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), activeGroupsFetchTimeout)
		defer cancel()

		// Re-check after acquiring the slot: a sibling may have populated the cache while we queued.
		if groups, ok := c.entries.Get(orgID); ok {
			return groups, nil
		}

		// Over-fetch by one so the caller can flag, rather than silently swallow, a fleet past the cap.
		groups, err := c.history.ListActiveGroups(fetchCtx, orgID, activeGroupsMaxPageSize+1)
		if err != nil {
			return nil, err
		}
		c.entries.Add(orgID, groups)
		return groups, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			// The loader returns store errors unwrapped; pass them through as-is.
			return nil, res.Err //nolint:wrapcheck
		}
		groups, ok := res.Val.([]notificationhistory.ActiveAlertGroup)
		if !ok {
			return nil, fleeterror.NewInternalErrorf("unexpected type from active groups singleflight: %T", res.Val)
		}
		return groups, nil
	case <-ctx.Done():
		// This caller gave up; the detached fetch keeps running and populates the cache for the next poll.
		return nil, ctx.Err() //nolint:wrapcheck
	}
}
