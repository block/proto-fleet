package sqlstores_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	buildingsmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
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

	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, nil, deviceIdentifiers)
	require.NoError(t, err)
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
