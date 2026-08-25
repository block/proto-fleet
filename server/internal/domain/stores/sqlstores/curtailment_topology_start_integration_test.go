package sqlstores_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	buildingsmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	dbinfra "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_FrozenTopologyStartPersistsEnvelopeAndRejectsMembershipDrift(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	buildingStore := sqlstores.NewSQLBuildingStore(database)
	orgID := user.OrganizationID

	site, err := sqlstores.NewSQLSiteStore(database).CreateSite(ctx, sitesmodels.CreateSiteParams{
		OrgID: orgID,
		Name:  "Frozen topology start site",
	})
	require.NoError(t, err)
	building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID:  orgID,
		SiteID: &site.ID,
		Name:   "Frozen topology start building",
	})
	require.NoError(t, err)
	device := testContext.DatabaseService.CreateDevice(orgID, "proto")
	deviceIdentifiers := []string{device.ID}
	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, &building.ID, deviceIdentifiers)
	require.NoError(t, err)
	_, err = buildingStore.CascadeDevicesSiteForBuilding(ctx, orgID, deviceIdentifiers, &site.ID)
	require.NoError(t, err)
	dispatchSnapshot, err := store.ResolveCurtailmentTopologyDispatch(
		ctx,
		interfaces.ListCandidatesParams{OrgID: orgID, BuildingIDs: []int64{building.ID}},
		[]string{device.ID, "not-a-member"},
	)
	require.NoError(t, err)
	assert.Equal(t, []int64{site.ID}, dispatchSnapshot.Coverage.SelectedResourceSiteIDs)
	assert.Equal(t, []int64{site.ID}, dispatchSnapshot.Coverage.CurrentMemberSiteIDs)
	assert.Equal(t, []string{device.ID}, dispatchSnapshot.DispatchMemberDeviceIdentifiers)

	eventUUID := uuid.New()
	event := curtailmentStoreTestEvent(orgID, user.DatabaseID, eventUUID, models.EventStatePending, "frozen-topology-start")
	event.ScopeType = models.ScopeTypeMixed
	event.ScopeJSON = []byte(fmt.Sprintf(`{"scope_schema_version":1,"building_ids":[%d]}`, building.ID))
	target := curtailmentStoreTestTarget(device.ID, models.TargetStatePending, models.DesiredStateCurtailed)

	inserted, err := store.InsertEventWithTargets(ctx, event, []models.InsertTargetParams{target})
	require.NoError(t, err)
	require.NotNil(t, inserted)

	persisted, err := store.GetEventByUUID(ctx, orgID, eventUUID)
	require.NoError(t, err)
	assert.JSONEq(t, string(event.ScopeJSON), string(persisted.ScopeJSON))
	var envelope models.AuthorizationEnvelope
	require.NoError(t, json.Unmarshal(persisted.AuthorizationEnvelopeJSON, &envelope))
	assert.Equal(t, []int64{site.ID}, envelope.SelectedResourceSiteIDs)
	assert.Equal(t, []int64{site.ID}, envelope.CurrentMemberSiteIDs)
	assert.False(t, envelope.MinerScopeUnbounded)

	targets, err := store.ListTargetsByEvent(ctx, orgID, eventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, device.ID, targets[0].DeviceIdentifier)

	fenceSnapshot := make(chan interfaces.CurtailmentTopologyDispatchFenceSnapshot, 1)
	releaseFence := make(chan struct{})
	fenceDone := make(chan error, 1)
	go func() {
		fenceDone <- store.WithCurtailmentTopologyDispatchFence(
			ctx,
			persisted,
			interfaces.ListCandidatesParams{OrgID: orgID, BuildingIDs: []int64{building.ID}},
			[]string{device.ID},
			func(snapshot interfaces.CurtailmentTopologyDispatchFenceSnapshot) error {
				batchUUID := uuid.NewString()
				if commandErr := dbinfra.WithTransactionNoResult(ctx, database, func(q sqlc.Querier) error {
					if _, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
						Uuid:           batchUUID,
						Type:           "CURTAIL",
						CreatedBy:      user.DatabaseID,
						CreatedAt:      time.Now(),
						Status:         sqlc.BatchStatusEnumPENDING,
						DevicesCount:   1,
						OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
					}); err != nil {
						return err
					}
					return q.CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
						CommandBatchLogUuid: batchUUID,
						CommandType:         "CURTAIL",
						DeviceID:            device.DatabaseID,
						Status:              sqlc.QueueStatusEnumPENDING,
					})
				}); commandErr != nil {
					return commandErr
				}
				fenceSnapshot <- snapshot
				<-releaseFence
				return nil
			},
		)
	}()
	select {
	case snapshot := <-fenceSnapshot:
		assert.Equal(t, []string{device.ID}, snapshot.Topology.DispatchMemberDeviceIdentifiers)
	case fenceErr := <-fenceDone:
		require.NoError(t, fenceErr)
		t.Fatal("dispatch fence exited before invoking its callback")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for dispatch fence callback")
	}
	mutationStarted := make(chan struct{})
	mutationDone := make(chan error, 1)
	go func() {
		close(mutationStarted)
		_, mutationErr := buildingStore.AssignDevicesToBuilding(ctx, orgID, nil, deviceIdentifiers)
		mutationDone <- mutationErr
	}()
	<-mutationStarted
	select {
	case mutationErr := <-mutationDone:
		require.NoError(t, mutationErr)
		t.Fatal("topology mutation completed while the dispatch fence was held")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFence)
	require.NoError(t, <-fenceDone)
	require.NoError(t, <-mutationDone)

	dispatchSnapshot, err = store.ResolveCurtailmentTopologyDispatch(
		ctx,
		interfaces.ListCandidatesParams{OrgID: orgID, BuildingIDs: []int64{building.ID}},
		[]string{device.ID},
	)
	require.NoError(t, err)
	assert.Equal(t, []int64{site.ID}, dispatchSnapshot.Coverage.SelectedResourceSiteIDs)
	assert.Empty(t, dispatchSnapshot.Coverage.CurrentMemberSiteIDs)
	assert.Empty(t, dispatchSnapshot.DispatchMemberDeviceIdentifiers)

	driftedUUID := uuid.New()
	drifted := event
	drifted.EventUUID = driftedUUID
	_, err = store.InsertEventWithTargets(ctx, drifted, []models.InsertTargetParams{target})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "topology changed before save")

	_, err = store.GetEventByUUID(ctx, orgID, driftedUUID)
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err), "failed topology validation must roll back the event insert")

	_, err = store.BeginRestoreTransition(ctx, orgID, eventUUID, interfaces.BeginRestoreTransitionParams{
		KnownUnsentDeviceIdentifiers: []string{device.ID},
	})
	require.NoError(t, err)
	targets, err = store.ListTargetsByEvent(ctx, orgID, eventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, models.TargetStateReleased, targets[0].State,
		"a dispatch rejection must not queue Uncurtail for a miner that never received Curtail")
}
