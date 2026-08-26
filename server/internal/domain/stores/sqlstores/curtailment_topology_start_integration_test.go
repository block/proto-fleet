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

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
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

func TestSQLCurtailmentStore_PairingInsertWaitsForTopologyRestoreLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := testContext.DatabaseService.DB
	device := testContext.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	_, err := database.ExecContext(ctx, `DELETE FROM device_pairing WHERE device_id = $1`, device.DatabaseID)
	require.NoError(t, err)

	locked := make(chan struct{})
	release := make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- dbinfra.WithTransactionNoResult(ctx, database, func(q sqlc.Querier) error {
			rows, err := q.LockCurtailmentTargetPairingStatusesForWrite(
				ctx,
				sqlc.LockCurtailmentTargetPairingStatusesForWriteParams{
					OrgID:             user.OrganizationID,
					DeviceIdentifiers: []string{device.ID},
				},
			)
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0].PairingStatus != "UNPAIRED" {
				return fmt.Errorf("unexpected locked pairing snapshot: %+v", rows)
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	pairDone := make(chan error, 1)
	go func() {
		pairDone <- sqlstores.NewSQLDeviceStore(database).UpsertDevicePairing(
			ctx,
			&pairingpb.Device{DeviceIdentifier: device.ID},
			user.OrganizationID,
			string(sqlc.PairingStatusEnumPAIRED),
		)
	}()
	select {
	case err := <-pairDone:
		require.NoError(t, err)
		t.Fatal("first pairing insert bypassed the topology restore device lock")
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-lockDone)
	require.NoError(t, <-pairDone)

	var pairingStatus string
	require.NoError(t, database.QueryRowContext(
		ctx,
		`SELECT pairing_status::TEXT FROM device_pairing WHERE device_id = $1`,
		device.DatabaseID,
	).Scan(&pairingStatus))
	assert.Equal(t, "PAIRED", pairingStatus)
}

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

	transitioned, err := store.BeginCurtailmentTopologyTargetRestore(
		ctx,
		persisted,
		[]string{device.ID},
	)
	require.NoError(t, err)
	assert.Zero(t, transitioned, "a stale departure snapshot must not restore a current topology member")
	targets, err = store.ListTargetsByEvent(ctx, orgID, eventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, models.DesiredStateCurtailed, targets[0].DesiredState)
	assert.Equal(t, models.TargetStatePending, targets[0].State)

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

func TestSQLCurtailmentStore_BeginTopologyRestoreReleasesUnsentAndQueuesAttemptedTargets(t *testing.T) {
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

	building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID,
		Name:  "Topology departure branches",
	})
	require.NoError(t, err)
	devices := testContext.DatabaseService.CreateTestMiners(orgID, 2, "https://172.17.0.1:80")
	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, &building.ID, devices)
	require.NoError(t, err)

	eventUUID := uuid.New()
	event := curtailmentStoreClosedLoopFullFleetEvent(
		orgID,
		user.DatabaseID,
		eventUUID,
		models.ScopeTypeMixed,
		0,
		"topology-departure-branches",
	)
	event.ScopeJSON = []byte(fmt.Sprintf(`{"scope_schema_version":1,"building_ids":[%d]}`, building.ID))
	inserted, err := store.InsertEventWithTargets(ctx, event, []models.InsertTargetParams{
		curtailmentStoreTestTarget(devices[0], models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(devices[1], models.TargetStatePending, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `
		UPDATE curtailment_target
		SET retry_count = 1,
		    last_dispatched_at = CURRENT_TIMESTAMP,
		    curtail_dispatched_at = CURRENT_TIMESTAMP
		WHERE curtailment_event_id = $1
		  AND device_identifier = $2
	`, inserted.ID, devices[1])
	require.NoError(t, err)

	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, nil, devices)
	require.NoError(t, err)
	persisted, err := store.GetEventByUUID(ctx, orgID, eventUUID)
	require.NoError(t, err)
	transitioned, err := store.BeginCurtailmentTopologyTargetRestore(ctx, persisted, devices)
	require.NoError(t, err)
	assert.Equal(t, int64(2), transitioned)

	targets, err := store.ListTargetsByEvent(ctx, orgID, eventUUID)
	require.NoError(t, err)
	byDevice := make(map[string]*models.Target, len(targets))
	for _, target := range targets {
		byDevice[target.DeviceIdentifier] = target
	}
	unsent := byDevice[devices[0]]
	require.NotNil(t, unsent)
	assert.Equal(t, models.TargetStateReleased, unsent.State)
	assert.Equal(t, models.DesiredStateCurtailed, unsent.DesiredState)
	assert.Nil(t, unsent.RestorePhase.StartedAt)

	attempted := byDevice[devices[1]]
	require.NotNil(t, attempted)
	assert.Equal(t, models.TargetStatePending, attempted.State)
	assert.Equal(t, models.DesiredStateActive, attempted.DesiredState)
	assert.Equal(t, models.TargetStatePending, attempted.RestorePhase.State)
	require.NotNil(t, attempted.RestorePhase.StartedAt)
}

func TestSQLCurtailmentStore_TargetlessTopologyWatchersReserveEmptyScopesAndFollowMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	buildingStore := sqlstores.NewSQLBuildingStore(database)
	collectionStore := sqlstores.NewSQLCollectionStore(database)
	siteStore := sqlstores.NewSQLSiteStore(database)
	orgID := user.OrganizationID

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{
		OrgID: orgID,
		Name:  "Topology watcher site",
	})
	require.NoError(t, err)
	building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID:  orgID,
		SiteID: &site.ID,
		Name:   "Topology watcher building",
	})
	require.NoError(t, err)
	rack, err := collectionStore.CreateCollection(
		ctx,
		orgID,
		collectionpb.CollectionType_COLLECTION_TYPE_RACK,
		"Topology watcher rack",
		"",
	)
	require.NoError(t, err)
	err = collectionStore.CreateRackExtension(ctx, interfaces.CreateRackExtensionParams{
		OrgID:        orgID,
		CollectionID: rack.Id,
		Rows:         1,
		Columns:      2,
		Zone:         "Watcher zone",
		SiteID:       &site.ID,
	})
	require.NoError(t, err)
	group, err := collectionStore.CreateCollection(
		ctx,
		orgID,
		collectionpb.CollectionType_COLLECTION_TYPE_GROUP,
		"Topology watcher group",
		"",
	)
	require.NoError(t, err)

	devices := testContext.DatabaseService.CreateTestMiners(orgID, 7, "https://172.17.0.1:80")
	_, err = siteStore.AssignDevicesToSite(ctx, orgID, &site.ID, devices)
	require.NoError(t, err)

	watchers := []struct {
		name      string
		scopeJSON []byte
	}{
		{
			name:      "building",
			scopeJSON: []byte(fmt.Sprintf(`{"scope_schema_version":1,"building_ids":[%d]}`, building.ID)),
		},
		{
			name:      "rack",
			scopeJSON: []byte(fmt.Sprintf(`{"scope_schema_version":1,"rack_ids":[%d]}`, rack.Id)),
		},
		{
			name:      "group",
			scopeJSON: []byte(fmt.Sprintf(`{"scope_schema_version":1,"group_ids":[%d]}`, group.Id)),
		},
	}
	wholeOrgBlocker := curtailmentStoreClosedLoopFullFleetEvent(
		orgID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeWholeOrg,
		0,
		"whole-org-topology-blocker",
	)
	_, err = store.InsertEventWithTargets(ctx, wholeOrgBlocker, nil)
	require.NoError(t, err)
	blockedTopology := curtailmentStoreClosedLoopFullFleetEvent(
		orgID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeMixed,
		0,
		"topology-against-whole-org-watcher",
	)
	blockedTopology.ScopeJSON = watchers[0].scopeJSON
	_, err = store.InsertEventWithTargets(ctx, blockedTopology, nil)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err))
	_, err = store.BeginRestoreTransition(ctx, orgID, wholeOrgBlocker.EventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)

	watcherEvents := make(map[string]*models.InsertEventResult, len(watchers))
	for _, watcher := range watchers {
		event := curtailmentStoreClosedLoopFullFleetEvent(
			orgID,
			user.DatabaseID,
			uuid.New(),
			models.ScopeTypeMixed,
			0,
			"topology-watcher-"+watcher.name,
		)
		event.ScopeJSON = watcher.scopeJSON
		event.ForceIncludeAllPairedMiners = watcher.name == "group"
		watcherEvents[watcher.name], err = store.InsertEventWithTargets(ctx, event, nil)
		require.NoError(t, err)

		duplicate := curtailmentStoreClosedLoopFullFleetEvent(
			orgID,
			user.DatabaseID,
			uuid.New(),
			models.ScopeTypeMixed,
			0,
			"duplicate-topology-watcher-"+watcher.name,
		)
		duplicate.ScopeJSON = watcher.scopeJSON
		_, err = store.InsertEventWithTargets(ctx, duplicate, nil)
		require.Error(t, err)
		assert.True(t, fleeterror.IsAlreadyExistsError(err))
	}

	wholeOrg := curtailmentStoreClosedLoopFullFleetEvent(
		orgID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeWholeOrg,
		0,
		"whole-org-against-topology-watchers",
	)
	_, err = store.InsertEventWithTargets(ctx, wholeOrg, nil)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err))

	active, err := store.ListActiveCurtailedDevices(ctx, orgID)
	require.NoError(t, err)
	assert.Empty(t, active)

	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, &building.ID, devices[0:1])
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, rack.Id, devices[2:3])
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, group.Id, devices[4:5])
	require.NoError(t, err)

	active, err = store.ListActiveCurtailedDevices(ctx, orgID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{devices[0], devices[2], devices[4]}, active)

	claimedAllPaired, err := store.ClaimAllPairedPolicyTargets(
		ctx,
		watcherEvents["group"].ID,
		orgID,
		1,
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget(devices[0], models.TargetStatePending, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget(devices[6], models.TargetStatePending, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), claimedAllPaired,
		"the bounded batch must skip an earlier reservation without starving the next available miner")
	var claimedDevice string
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT device_identifier
		FROM curtailment_target
		WHERE curtailment_event_id = $1
	`, watcherEvents["group"].ID).Scan(&claimedDevice))
	assert.Equal(t, devices[6], claimedDevice)
	_, err = database.ExecContext(ctx, `
		DELETE FROM curtailment_target
		WHERE curtailment_event_id = $1
	`, watcherEvents["group"].ID)
	require.NoError(t, err)

	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(orgID, user.DatabaseID, uuid.New(), models.EventStateActive, "direct-target-against-topology-watcher"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget(devices[0], models.TargetStateConfirmed, models.DesiredStateCurtailed),
		},
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err),
		"direct target insertion must honor the older building watcher's logical reservation")

	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, group.Id, devices[0:1])
	require.NoError(t, err)
	claimed, err := store.ClaimClosedLoopFullFleetTargets(
		ctx,
		watcherEvents["group"].ID,
		orgID,
		0,
		[]models.InsertTargetParams{curtailmentStoreTestTarget(
			devices[0],
			models.TargetStatePending,
			models.DesiredStateCurtailed,
		)},
	)
	require.NoError(t, err)
	assert.Empty(t, claimed, "the older building watcher must retain logical ownership")
	_, err = collectionStore.RemoveDevicesFromCollection(ctx, orgID, group.Id, devices[0:1])
	require.NoError(t, err)

	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, nil, devices[0:1])
	require.NoError(t, err)
	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, &building.ID, devices[1:2])
	require.NoError(t, err)
	_, err = collectionStore.RemoveDevicesFromCollection(ctx, orgID, rack.Id, devices[2:3])
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, rack.Id, devices[3:4])
	require.NoError(t, err)
	_, err = collectionStore.RemoveDevicesFromCollection(ctx, orgID, group.Id, devices[4:5])
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, group.Id, devices[5:6])
	require.NoError(t, err)

	active, err = store.ListActiveCurtailedDevices(ctx, orgID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{devices[1], devices[3], devices[5]}, active,
		"logical topology ownership must follow current membership even before target rows are admitted")
}

func TestSQLCurtailmentStore_ClaimWaitsForTopologyMoveBeforeCheckingEarlierReservation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	buildingStore := sqlstores.NewSQLBuildingStore(database)
	collectionStore := sqlstores.NewSQLCollectionStore(database)
	orgID := user.OrganizationID

	building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID,
		Name:  "Admission reservation building",
	})
	require.NoError(t, err)
	group, err := collectionStore.CreateCollection(
		ctx,
		orgID,
		collectionpb.CollectionType_COLLECTION_TYPE_GROUP,
		"Admission reservation group",
		"",
	)
	require.NoError(t, err)
	device := testContext.DatabaseService.CreateDevice(orgID, "proto")
	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, group.Id, []string{device.ID})
	require.NoError(t, err)

	older := curtailmentStoreClosedLoopFullFleetEvent(
		orgID, user.DatabaseID, uuid.New(), models.ScopeTypeMixed, 0, "older-building-reservation",
	)
	older.ScopeJSON = []byte(fmt.Sprintf(`{"scope_schema_version":1,"building_ids":[%d]}`, building.ID))
	_, err = store.InsertEventWithTargets(ctx, older, nil)
	require.NoError(t, err)
	younger := curtailmentStoreClosedLoopFullFleetEvent(
		orgID, user.DatabaseID, uuid.New(), models.ScopeTypeMixed, 0, "younger-group-admission",
	)
	younger.ScopeJSON = []byte(fmt.Sprintf(`{"scope_schema_version":1,"group_ids":[%d]}`, group.Id))
	youngerEvent, err := store.InsertEventWithTargets(ctx, younger, nil)
	require.NoError(t, err)

	tx, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	q := sqlc.New(tx)
	_, err = q.LockBuildingForWrite(ctx, sqlc.LockBuildingForWriteParams{ID: building.ID, OrgID: orgID})
	require.NoError(t, err)
	_, err = q.LockDevicesForReassign(ctx, sqlc.LockDevicesForReassignParams{
		OrgID: orgID, DeviceIdentifiers: []string{device.ID},
	})
	require.NoError(t, err)
	_, err = q.AssignDevicesToBuilding(ctx, sqlc.AssignDevicesToBuildingParams{
		OrgID: orgID, TargetBuildingID: sql.NullInt64{Int64: building.ID, Valid: true}, DeviceIdentifiers: []string{device.ID},
	})
	require.NoError(t, err)

	claimDone := make(chan struct {
		targets []*models.Target
		err     error
	}, 1)
	go func() {
		targets, claimErr := store.ClaimClosedLoopFullFleetTargets(
			ctx,
			youngerEvent.ID,
			orgID,
			0,
			[]models.InsertTargetParams{
				curtailmentStoreTestTarget(device.ID, models.TargetStatePending, models.DesiredStateCurtailed),
			},
		)
		claimDone <- struct {
			targets []*models.Target
			err     error
		}{targets: targets, err: claimErr}
	}()
	select {
	case result := <-claimDone:
		require.NoError(t, result.err)
		t.Fatal("claim completed before the in-flight topology move committed")
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, tx.Commit())
	result := <-claimDone
	require.NoError(t, result.err)
	assert.Empty(t, result.targets, "the committed move must make the older targetless watcher authoritative")
}

func TestSQLCurtailmentStore_BulkReadinessWaitsForTargetlessTopologyReservation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	buildingStore := sqlstores.NewSQLBuildingStore(database)
	collectionStore := sqlstores.NewSQLCollectionStore(database)
	orgID := user.OrganizationID

	building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID: orgID,
		Name:  "Readiness reservation building",
	})
	require.NoError(t, err)
	group, err := collectionStore.CreateCollection(
		ctx,
		orgID,
		collectionpb.CollectionType_COLLECTION_TYPE_GROUP,
		"Readiness policy group",
		"",
	)
	require.NoError(t, err)
	device := testContext.DatabaseService.CreateDevice(orgID, "proto")
	_, err = collectionStore.AddDevicesToCollection(ctx, orgID, group.Id, []string{device.ID})
	require.NoError(t, err)

	older := curtailmentStoreClosedLoopFullFleetEvent(
		orgID, user.DatabaseID, uuid.New(), models.ScopeTypeMixed, 0, "older-readiness-reservation",
	)
	older.ScopeJSON = []byte(fmt.Sprintf(`{"scope_schema_version":1,"building_ids":[%d]}`, building.ID))
	_, err = store.InsertEventWithTargets(ctx, older, nil)
	require.NoError(t, err)
	policy := curtailmentStoreClosedLoopFullFleetEvent(
		orgID, user.DatabaseID, uuid.New(), models.ScopeTypeMixed, 0, "younger-readiness-policy",
	)
	policy.ScopeJSON = []byte(fmt.Sprintf(`{"scope_schema_version":1,"group_ids":[%d]}`, group.Id))
	policy.ForceIncludeAllPairedMiners = true
	policyEvent, err := store.InsertEventWithTargets(ctx, policy, []models.InsertTargetParams{
		curtailmentStoreTestTarget(device.ID, models.TargetStateRestoreFailed, models.DesiredStateActive),
	})
	require.NoError(t, err)

	tx, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	q := sqlc.New(tx)
	_, err = q.LockBuildingForWrite(ctx, sqlc.LockBuildingForWriteParams{ID: building.ID, OrgID: orgID})
	require.NoError(t, err)
	_, err = q.LockDevicesForReassign(ctx, sqlc.LockDevicesForReassignParams{
		OrgID: orgID, DeviceIdentifiers: []string{device.ID},
	})
	require.NoError(t, err)
	_, err = q.AssignDevicesToBuilding(ctx, sqlc.AssignDevicesToBuildingParams{
		OrgID: orgID, TargetBuildingID: sql.NullInt64{Int64: building.ID, Valid: true}, DeviceIdentifiers: []string{device.ID},
	})
	require.NoError(t, err)

	refreshDone := make(chan struct {
		applied []string
		err     error
	}, 1)
	go func() {
		applied, refreshErr := store.BulkRefreshAllPairedTargetReadiness(
			ctx,
			policyEvent.ID,
			orgID,
			models.EventStateActive,
			[]interfaces.AllPairedReadinessUpdate{{
				DeviceIdentifier:     device.ID,
				ExpectedState:        models.TargetStateRestoreFailed,
				ExpectedDesiredState: models.DesiredStateActive,
				State:                models.TargetStatePending,
			}},
		)
		refreshDone <- struct {
			applied []string
			err     error
		}{applied: applied, err: refreshErr}
	}()
	select {
	case result := <-refreshDone:
		require.NoError(t, result.err)
		t.Fatal("readiness refresh completed before the in-flight topology move committed")
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, tx.Commit())
	result := <-refreshDone
	require.NoError(t, result.err)
	assert.Empty(t, result.applied)
	targets, err := store.ListTargetsByEvent(ctx, orgID, policy.EventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, models.TargetStateRestoreFailed, targets[0].State)
}

func TestSQLCurtailmentStore_BulkReadinessDoesNotRequeueTopologyRestoreOwnedElsewhere(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	database := testContext.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(database)
	buildingStore := sqlstores.NewSQLBuildingStore(database)
	siteStore := sqlstores.NewSQLSiteStore(database)
	orgID := user.OrganizationID

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{
		OrgID: orgID,
		Name:  "Topology restore owner site",
	})
	require.NoError(t, err)
	building, err := buildingStore.CreateBuilding(ctx, buildingsmodels.CreateParams{
		OrgID:  orgID,
		SiteID: &site.ID,
		Name:   "Topology restore owner building",
	})
	require.NoError(t, err)
	device := testContext.DatabaseService.CreateDevice(orgID, "proto")
	deviceIDs := []string{device.ID}
	_, err = siteStore.AssignDevicesToSite(ctx, orgID, &site.ID, deviceIDs)
	require.NoError(t, err)
	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, &building.ID, deviceIDs)
	require.NoError(t, err)

	policy := curtailmentStoreClosedLoopFullFleetEvent(
		orgID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeMixed,
		0,
		"topology-restore-owner-policy",
	)
	policy.ScopeJSON = []byte(fmt.Sprintf(`{"scope_schema_version":1,"building_ids":[%d]}`, building.ID))
	policy.ForceIncludeAllPairedMiners = true
	policyTarget := curtailmentStoreTestTarget(device.ID, models.TargetStateRestoreFailed, models.DesiredStateActive)
	policyEvent, err := store.InsertEventWithTargets(ctx, policy, []models.InsertTargetParams{policyTarget})
	require.NoError(t, err)

	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, nil, deviceIDs)
	require.NoError(t, err)
	competitorUUID := uuid.New()
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(orgID, user.DatabaseID, competitorUUID, models.EventStateActive, "topology-restore-owner-competitor"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget(device.ID, models.TargetStateConfirmed, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err, "a departed restore-failed target no longer reserves the miner")

	_, err = buildingStore.AssignDevicesToBuilding(ctx, orgID, &building.ID, deviceIDs)
	require.NoError(t, err)
	applied, err := store.BulkRefreshAllPairedTargetReadiness(
		ctx,
		policyEvent.ID,
		orgID,
		models.EventStateActive,
		[]interfaces.AllPairedReadinessUpdate{{
			DeviceIdentifier:     device.ID,
			ExpectedState:        models.TargetStateRestoreFailed,
			ExpectedDesiredState: models.DesiredStateActive,
			State:                models.TargetStatePending,
		}},
	)
	require.NoError(t, err)
	assert.Empty(t, applied, "restore requeue must skip a miner now owned by another non-terminal event")

	policyTargets, err := store.ListTargetsByEvent(ctx, orgID, policy.EventUUID)
	require.NoError(t, err)
	require.Len(t, policyTargets, 1)
	assert.Equal(t, models.TargetStateRestoreFailed, policyTargets[0].State)
	competitorTargets, err := store.ListTargetsByEvent(ctx, orgID, competitorUUID)
	require.NoError(t, err)
	require.Len(t, competitorTargets, 1)
	assert.Equal(t, models.TargetStateConfirmed, competitorTargets[0].State)
}
