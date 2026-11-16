package devicewrite_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestExclusiveBindingObservationSerializesReadsWithoutMutationAuthority(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := createDeletionBindingFixture(t, database, user.OrganizationID)
	service := devicewrite.NewService(database.DB)
	ctx := t.Context()
	entered, release := make(chan struct{}), make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	mutationErr := make(chan error, 1)
	var mutated atomic.Bool
	first := make(chan error, 1)
	go func() {
		first <- service.WithHostObservation(ctx, user.OrganizationID, device.DatabaseID, 0, func(ctx context.Context) error {
			return service.WithExclusiveBindingObservation(ctx, device.DatabaseID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
				mutationErr <- devicewrite.Dispatch(ctx, func(context.Context) error { mutated.Store(true); return nil })
				close(entered)
				<-release
				return nil
			})
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("exclusive observation did not start")
	}
	require.True(t, fleeterror.IsFailedPreconditionError(<-mutationErr))
	require.False(t, mutated.Load(), "an exclusive read lock cannot grant mutation authority")
	secondEntered, second := make(chan struct{}), make(chan error, 1)
	go func() {
		second <- service.WithHostObservation(ctx, user.OrganizationID, device.DatabaseID, 0, func(ctx context.Context) error {
			return service.WithBindingObservation(ctx, device.DatabaseID, func(sqlc.Querier) error { return nil }, func(context.Context) error {
				close(secondEntered)
				return nil
			})
		})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := database.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
		 WHERE datname=current_database() AND wait_event_type='Lock'
		 AND query LIKE '%SELECT discovered_device_id FROM device%FOR SHARE%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "ordinary reads must wait on the exclusive observation's binding")
	select {
	case <-secondEntered:
		t.Fatal("another read entered during an exclusive observation")
	default:
	}
	finish()
	require.NoError(t, <-first)
	require.NoError(t, <-second)
}
