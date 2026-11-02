package sqlstores_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_LifecycleDurationGatesUseLockedRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	for _, state := range []models.EventState{models.EventStatePending, models.EventStateActive, models.EventStateRestoring} {
		t.Run(string(state), func(t *testing.T) {
			fixture := testutil.InitializeDBServiceInfrastructure(t)
			user := fixture.DatabaseService.CreateSuperAdminUser()
			database := fixture.DatabaseService.DB
			ctx := t.Context()
			store := sqlstores.NewSQLCurtailmentStore(database)
			eventUUID := uuid.New()
			params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, state, "locked-duration")
			inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{
				curtailmentStoreTestTarget("locked-duration-miner", models.TargetStatePending, models.DesiredStateCurtailed),
			})
			require.NoError(t, err)

			// Another writer changes the duration policy after the operator's
			// stale pre-read. The transition must wait and inspect its commit.
			tx, err := database.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			if state == models.EventStateRestoring {
				_, err = tx.ExecContext(ctx, `UPDATE curtailment_event SET max_duration_seconds=10, started_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, inserted.ID)
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE curtailment_event SET min_curtailed_duration_sec=600, created_at=NOW(), started_at=NULL WHERE id=$1`, inserted.ID)
			}
			require.NoError(t, err)
			result := make(chan error, 1)
			go func() {
				var transitionErr error
				if state == models.EventStateRestoring {
					_, transitionErr = store.BeginRecurtailTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRecurtailTransitionParams{})
				} else {
					_, transitionErr = store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{EnforceMinDuration: true})
				}
				result <- transitionErr
			}()
			select {
			case err := <-result:
				t.Fatalf("transition passed a locked concurrent duration update: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			require.NoError(t, tx.Commit())
			select {
			case err := <-result:
				require.Error(t, err)
				assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("transition did not complete after duration update committed")
			}
			current, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
			require.NoError(t, err)
			assert.Equal(t, state, current.State)
			if state != models.EventStateRestoring {
				// Safety/force restoration deliberately bypasses voluntary Stop's minimum.
				_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
				require.NoError(t, err)
			}
		})
	}
}

func TestSQLCurtailmentStore_FanIOAllowsMetadataUpdatesButSerializesLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	database := fixture.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(database)
	siteID, fanID := seedCurtailmentFacilityFan(t, database, user.OrganizationID, "fan-metadata")
	eventUUID := uuid.New()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "fan-metadata")
	params.FacilityFanDeviceIDs = []int64{fanID}
	params.ExpectedFacilityFanSites = map[int64]int64{fanID: siteID}
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{
		curtailmentStoreTestTarget("fan-metadata-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	entered, finish := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(finish) }) }
	defer release()
	commandDone := make(chan error, 1)
	go func() {
		_, err := store.CommandFanState(ctx, inserted.ID, interfaces.UpdateCurtailmentFanStateParams{ExpectedEventState: models.EventStateActive}, func(context.Context) *string {
			close(entered)
			<-finish
			return nil
		})
		commandDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("fan command did not start")
	}
	metadataCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	reason := "operator annotation during fan I/O"
	updated, err := store.UpdateOperatorFields(metadataCtx, inserted.ID, user.OrganizationID, interfaces.UpdateOperatorFieldsParams{Reason: &reason})
	require.NoError(t, err, "hardware I/O must not hold the event row lock")
	assert.Equal(t, reason, updated.Reason)
	transitionDone := make(chan error, 1)
	go func() {
		_, err := store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
		transitionDone <- err
	}()
	select {
	case err := <-transitionDone:
		t.Fatalf("lifecycle changed before physical fan command finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	require.NoError(t, <-commandDone)
	require.NoError(t, <-transitionDone)
}
