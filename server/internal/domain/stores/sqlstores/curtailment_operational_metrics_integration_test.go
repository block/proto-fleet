package sqlstores_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSQLCurtailmentStore_PendingWorkAges(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	ctx := t.Context()
	eventAge, edgeAge, err := store.GetPendingWorkAges(ctx)
	require.NoError(t, err)
	require.Zero(t, eventAge)
	require.Zero(t, edgeAge)
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStatePending, "pending-age")
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{curtailmentStoreTestTarget("pending-age-miner", models.TargetStatePending, models.DesiredStateCurtailed)})
	require.NoError(t, err)
	eventAge, _, err = store.GetPendingWorkAges(ctx)
	require.NoError(t, err)
	require.InDelta(t, 0, eventAge.Seconds(), 5, "initial insertion starts the pending phase clock")
	_, err = database.ExecContext(ctx, `UPDATE curtailment_event SET created_at=NOW()-INTERVAL '2 minutes', pending_started_at=NOW()-INTERVAL '2 minutes' WHERE id=$1`, inserted.ID)
	require.NoError(t, err)
	sourceID := seedMQTTSourceConfig(t, database, user.OrganizationID, user.DatabaseID, "pending-age-source", true)
	disabledSourceID := seedMQTTSourceConfig(t, database, user.OrganizationID, user.DatabaseID, "disabled-pending-age-source", false)
	_, err = database.ExecContext(ctx, `INSERT INTO curtailment_mqtt_source_state (source_config_id,pending_direction,pending_received_at) VALUES ($1,'on_to_off',NOW()-INTERVAL '30 seconds'),($2,'on_to_off',NOW()-INTERVAL '1 hour')`, sourceID, disabledSourceID)
	require.NoError(t, err)
	eventAge, edgeAge, err = store.GetPendingWorkAges(ctx)
	require.NoError(t, err)
	require.InDelta(t, 120, eventAge.Seconds(), 5)
	require.InDelta(t, 30, edgeAge.Seconds(), 5, "disabled source must not make pending runtime work appear stuck")
	_, err = database.ExecContext(ctx, `UPDATE curtailment_event SET state='completed' WHERE id=$1`, inserted.ID)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `UPDATE curtailment_mqtt_source_state SET pending_direction=NULL,pending_received_at=NULL WHERE source_config_id=$1`, sourceID)
	require.NoError(t, err)
	eventAge, edgeAge, err = store.GetPendingWorkAges(ctx)
	require.NoError(t, err)
	require.Equal(t, time.Duration(0), eventAge)
	require.Equal(t, time.Duration(0), edgeAge)
}

func TestSQLCurtailmentStore_PendingPhaseClockMigrationBackfillsCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	ctx := t.Context()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStatePending, "upgrade-pending-age")
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{
		curtailmentStoreTestTarget("upgrade-pending-age-miner", models.TargetStatePending, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	created := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Hour)
	_, err = database.ExecContext(ctx, `UPDATE curtailment_event SET created_at=$2 WHERE id=$1`, inserted.ID, created)
	require.NoError(t, err)
	for _, direction := range []string{"down", "up"} {
		migration, err := migrations.Current.ReadFile("current/001044_shared_curtailment_pending_phase_clock." + direction + ".sql")
		require.NoError(t, err)
		tx, err := database.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(migration))
		if err != nil {
			_ = tx.Rollback()
		}
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	}
	var pendingStarted, preservedCreation time.Time
	require.NoError(t, database.QueryRowContext(ctx, `SELECT pending_started_at, created_at FROM curtailment_event WHERE id=$1`, inserted.ID).Scan(&pendingStarted, &preservedCreation))
	require.True(t, created.Equal(pendingStarted), "upgrade uses original creation when older phase history is unavailable")
	require.True(t, created.Equal(preservedCreation), "the event's finite-cap clock must not change")
}

func TestSQLCurtailmentStore_PendingWorkAges_RecurtailStartsNewPhase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	ctx := t.Context()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStateRestoring, "recurtail-pending-age")
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{
		curtailmentStoreTestTarget("recurtail-pending-age-miner", models.TargetStateDispatched, models.DesiredStateActive),
	})
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `UPDATE curtailment_event SET created_at=NOW()-INTERVAL '2 hours', started_at=NOW()-INTERVAL '1 hour', pending_started_at=NOW()-INTERVAL '2 hours' WHERE id=$1`, inserted.ID)
	require.NoError(t, err)
	before := time.Now().Add(-time.Second)
	recurtailed, err := store.BeginRecurtailTransition(ctx, user.OrganizationID, inserted.EventUUID, interfaces.BeginRecurtailTransitionParams{})
	require.NoError(t, err)
	require.Equal(t, models.EventStatePending, recurtailed.State)
	require.True(t, recurtailed.CreatedAt.Before(before), "the event's lifetime cap clock must be preserved")
	require.NotNil(t, recurtailed.StartedAt)
	require.True(t, recurtailed.StartedAt.Before(before), "the initial activation timestamp is retained")
	eventAge, _, err := store.GetPendingWorkAges(ctx)
	require.NoError(t, err)
	require.InDelta(t, 0, eventAge.Seconds(), 5, "age measures the renewed pending phase, not original creation or activation")

	// Retrying dispatch advances updated_at and the batching clock but must not
	// refresh pending_started_at, or a stuck event would never appear old.
	_, err = database.ExecContext(ctx, `UPDATE curtailment_event SET pending_started_at=NOW()-INTERVAL '30 seconds' WHERE id=$1`, inserted.ID)
	require.NoError(t, err)
	require.NoError(t, store.RecordCurtailPendingDispatch(ctx, inserted.ID, models.EventStatePending, time.Now()))
	eventAge, _, err = store.GetPendingWorkAges(ctx)
	require.NoError(t, err)
	require.InDelta(t, 30, eventAge.Seconds(), 5)
}
