package sqlstores_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func newAlertMaintenanceWindowStore(t *testing.T) *sqlstores.SQLAlertMaintenanceWindowStore {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	return sqlstores.NewSQLAlertMaintenanceWindowStore(testutil.GetTestDB(t))
}

func TestAlertMaintenanceWindowStoreCRUD(t *testing.T) {
	store := newAlertMaintenanceWindowStore(t)
	ctx := context.Background()
	starts := time.Now().UTC().Truncate(time.Millisecond)
	ends := starts.Add(2 * time.Hour)

	created, err := store.Insert(ctx, alerts.MaintenanceWindowRecord{
		OrganizationID: 7,
		RuleUIDs:       []string{"rule-a", "rule-b"},
		ChannelIDs:     []int64{3, 5},
		StartsAt:       starts,
		EndsAt:         ends,
		Comment:        "planned work",
		CreatedBy:      "alice@example.com",
	})
	require.NoError(t, err)
	assert.NotZero(t, created.ID)
	assert.Equal(t, []string{"rule-a", "rule-b"}, created.RuleUIDs)
	assert.Equal(t, []int64{3, 5}, created.ChannelIDs)
	assert.False(t, created.CreatedAt.IsZero())

	// The empty-targets ("every rule/channel") form round-trips as empty, not null-ish garbage.
	allAll, err := store.Insert(ctx, alerts.MaintenanceWindowRecord{
		OrganizationID: 7,
		StartsAt:       starts.Add(24 * time.Hour),
		EndsAt:         ends.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	assert.Empty(t, allAll.RuleUIDs)
	assert.Empty(t, allAll.ChannelIDs)

	listed, err := store.List(ctx, 7)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, allAll.ID, listed[0].ID, "list orders newest window first")

	other, err := store.List(ctx, 8)
	require.NoError(t, err)
	assert.Empty(t, other, "another org sees nothing")

	// Update replaces scope and times but keeps the creator.
	created.RuleUIDs = nil
	created.ChannelIDs = []int64{5}
	created.EndsAt = ends.Add(time.Hour)
	created.CreatedBy = "mallory@example.com"
	updated, err := store.Update(ctx, created)
	require.NoError(t, err)
	assert.Empty(t, updated.RuleUIDs)
	assert.Equal(t, []int64{5}, updated.ChannelIDs)
	assert.Equal(t, "alice@example.com", updated.CreatedBy, "created_by is write-once")

	// Update and delete against the wrong org report NotFound instead of touching the row.
	created.OrganizationID = 8
	_, err = store.Update(ctx, created)
	require.ErrorIs(t, err, alerts.ErrNotFound)
	require.ErrorIs(t, store.Delete(ctx, 8, created.ID), alerts.ErrNotFound)

	require.NoError(t, store.Delete(ctx, 7, created.ID))
	require.ErrorIs(t, store.Delete(ctx, 7, created.ID), alerts.ErrNotFound)
}

func TestAlertMaintenanceWindowStoreListActive(t *testing.T) {
	store := newAlertMaintenanceWindowStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	active, err := store.Insert(ctx, alerts.MaintenanceWindowRecord{
		OrganizationID: 11, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
	})
	require.NoError(t, err)
	_, err = store.Insert(ctx, alerts.MaintenanceWindowRecord{
		OrganizationID: 11, StartsAt: now.Add(-3 * time.Hour), EndsAt: now.Add(-2 * time.Hour),
	})
	require.NoError(t, err)
	_, err = store.Insert(ctx, alerts.MaintenanceWindowRecord{
		OrganizationID: 11, StartsAt: now.Add(2 * time.Hour), EndsAt: now.Add(3 * time.Hour),
	})
	require.NoError(t, err)

	got, err := store.ListActive(ctx, 11, now)
	require.NoError(t, err)
	require.Len(t, got, 1, "only the window covering now is active")
	assert.Equal(t, active.ID, got[0].ID)

	// Boundary semantics: starts_at inclusive, ends_at exclusive.
	atStart, err := store.ListActive(ctx, 11, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Len(t, atStart, 1)
	atEnd, err := store.ListActive(ctx, 11, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Empty(t, atEnd)
}

func TestAlertMaintenanceWindowStoreCountAndPrune(t *testing.T) {
	store := newAlertMaintenanceWindowStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	insert := func(startsAt, endsAt time.Time) alerts.MaintenanceWindowRecord {
		rec, err := store.Insert(ctx, alerts.MaintenanceWindowRecord{
			OrganizationID: 21, StartsAt: startsAt, EndsAt: endsAt,
		})
		require.NoError(t, err)
		return rec
	}
	active := insert(now.Add(-time.Hour), now.Add(time.Hour))     // active
	insert(now.Add(24*time.Hour), now.Add(25*time.Hour))          // scheduled
	insert(now.Add(-2*time.Hour), now.Add(-time.Hour))            // freshly expired
	old := insert(now.Add(-72*time.Hour), now.Add(-48*time.Hour)) // expired past the cutoff

	n, err := store.CountUnexpired(ctx, 21, now, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "only active and scheduled windows count")

	n, err = store.CountUnexpired(ctx, 21, now, active.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "the row an update rewrites is excluded from its own quota check")

	deleted, err := store.PruneExpired(ctx, 21, now, 24*time.Hour, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	listed, err := store.List(ctx, 21)
	require.NoError(t, err)
	require.Len(t, listed, 3, "the prune removes only windows ended before the cutoff")
	for _, rec := range listed {
		assert.NotEqual(t, old.ID, rec.ID)
	}

	// Count backstop: with two expired rows and keep_newest=1, the older one is reclaimed even
	// though it ended well inside the retention cutoff.
	insert(now.Add(-4*time.Hour), now.Add(-3*time.Hour))
	deleted, err = store.PruneExpired(ctx, 21, now, 24*time.Hour, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted, "the expired row beyond keep_newest is reclaimed")

	listed, err = store.List(ctx, 21)
	require.NoError(t, err)
	assert.Len(t, listed, 3, "active, scheduled, and the newest expired row survive")
}
