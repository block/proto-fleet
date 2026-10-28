package sqlstores_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
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
	_, err = database.ExecContext(ctx, `UPDATE curtailment_event SET created_at=NOW()-INTERVAL '2 minutes' WHERE id=$1`, inserted.ID)
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
