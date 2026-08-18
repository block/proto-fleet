package sqlstores_test

import (
	"testing"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	collectiondomain "github.com/block/proto-fleet/server/internal/domain/collection"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestReleaseSet(
	t *testing.T,
	store *sqlstores.SQLCollectionStore,
	orgID int64,
	suffix string,
) *collectionpb.FirmwareReleaseSet {
	t.Helper()
	releaseSet, err := store.CreateFirmwareReleaseSet(t.Context(), orgID)
	require.NoError(t, err)
	require.NoError(t, store.CreateFirmwareReleaseTarget(
		t.Context(),
		orgID,
		releaseSet.Id,
		&collectionpb.FirmwareReleaseTarget{
			FirmwareFileId:     "firmware-" + suffix,
			TargetManufacturer: "Proto",
			TargetModel:        "Rig-" + suffix,
			FirmwareVersion:    "2.0.0",
			Sha256:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	))
	return releaseSet
}

func createTestChannel(
	t *testing.T,
	store *sqlstores.SQLCollectionStore,
	orgID, releaseSetID int64,
	label string,
) *collectionpb.DeviceCollection {
	t.Helper()
	channel, err := store.CreateCollection(
		t.Context(),
		orgID,
		collectionpb.CollectionType_COLLECTION_TYPE_CHANNEL,
		label,
		"",
	)
	require.NoError(t, err)
	require.NoError(t, store.CreateChannelExtension(t.Context(), interfaces.CreateChannelExtensionParams{
		OrgID:        orgID,
		CollectionID: channel.Id,
		ReleaseSetID: releaseSetID,
	}))
	return channel
}

func TestChannelStore_RackAndChannelExclusivityCoexist(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 1)
	store := newCollectionStore(db)
	ctx := t.Context()

	rack, err := store.CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack", "")
	require.NoError(t, err)
	require.NoError(t, store.CreateRackExtension(ctx, interfaces.CreateRackExtensionParams{
		OrgID:        orgID,
		CollectionID: rack.Id,
		Rows:         1,
		Columns:      1,
		OrderIndex:   int32(collectionpb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT),
		CoolingType:  int32(collectionpb.RackCoolingType_RACK_COOLING_TYPE_AIR),
	}))
	_, err = store.AddDevicesToCollection(ctx, orgID, rack.Id, deviceIDs)
	require.NoError(t, err)

	releaseSet := createTestReleaseSet(t, store, orgID, "stable")
	channel := createTestChannel(t, store, orgID, releaseSet.Id, "Stable")
	_, err = store.AddDevicesToCollection(ctx, orgID, channel.Id, deviceIDs)
	require.NoError(t, err)

	secondReleaseSet := createTestReleaseSet(t, store, orgID, "canary")
	secondChannel := createTestChannel(t, store, orgID, secondReleaseSet.Id, "Canary")
	_, err = store.AddDevicesToCollection(ctx, orgID, secondChannel.Id, deviceIDs)
	require.Error(t, err)

	racks, err := store.GetDeviceCollections(
		ctx,
		orgID,
		deviceIDs[0],
		collectionpb.CollectionType_COLLECTION_TYPE_RACK,
	)
	require.NoError(t, err)
	require.Len(t, racks, 1)
	channels, err := store.GetDeviceCollections(
		ctx,
		orgID,
		deviceIDs[0],
		collectionpb.CollectionType_COLLECTION_TYPE_CHANNEL,
	)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, channel.Id, channels[0].Id)
}

func TestChannelStore_ReleaseSnapshotsAreImmutableAndShareable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	releaseSet := createTestReleaseSet(t, store, orgID, "shared")
	first := createTestChannel(t, store, orgID, releaseSet.Id, "Stable")
	second := createTestChannel(t, store, orgID, releaseSet.Id, "Canary")

	firstInfo, err := store.GetChannelInfo(t.Context(), first.Id, orgID)
	require.NoError(t, err)
	secondInfo, err := store.GetChannelInfo(t.Context(), second.Id, orgID)
	require.NoError(t, err)
	assert.Equal(t, releaseSet.Id, firstInfo.ReleaseSetId)
	assert.Equal(t, releaseSet.Id, secondInfo.ReleaseSetId)
	require.Len(t, firstInfo.ReleaseTargets, 1)
	assert.Equal(t, "firmware-shared", firstInfo.ReleaseTargets[0].FirmwareFileId)
	referenced, err := store.FirmwareArtifactReferenced(t.Context(), "firmware-shared")
	require.NoError(t, err)
	assert.True(t, referenced)
	anyReferenced, err := store.AnyFirmwareArtifactReferenced(t.Context())
	require.NoError(t, err)
	assert.True(t, anyReferenced)

	_, err = db.ExecContext(
		t.Context(),
		"UPDATE firmware_release_target SET firmware_version = 'changed' WHERE release_set_id = $1",
		releaseSet.Id,
	)
	require.Error(t, err)
	_, err = db.ExecContext(
		t.Context(),
		"DELETE FROM firmware_release_target WHERE release_set_id = $1",
		releaseSet.Id,
	)
	require.Error(t, err)
	_, err = db.ExecContext(
		t.Context(),
		"DELETE FROM firmware_release_set WHERE id = $1",
		releaseSet.Id,
	)
	require.Error(t, err)
}

func TestChannelService_AssignmentFailureRollsBackMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIDs := setupCollectionTestData(t, 1)
	store := newCollectionStore(db)
	releaseSet := createTestReleaseSet(t, store, orgID, "rollback")
	source := createTestChannel(t, store, orgID, releaseSet.Id, "Source")
	target := createTestChannel(t, store, orgID, releaseSet.Id, "Target")
	_, err := store.AddDevicesToCollection(t.Context(), orgID, source.Id, deviceIDs)
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(), `
		CREATE FUNCTION fail_target_channel_membership()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			IF EXISTS (
				SELECT 1
				FROM device_set
				WHERE id = NEW.device_set_id
				  AND label = 'Target'
			) THEN
				RAISE EXCEPTION 'injected membership failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER fail_target_channel_membership
		BEFORE INSERT ON device_set_membership
		FOR EACH ROW
		EXECUTE FUNCTION fail_target_channel_membership();
	`)
	require.NoError(t, err)

	service := collectiondomain.NewService(
		store,
		nil,
		nil,
		nil,
		sqlstores.NewSQLTransactor(db),
		nil,
		nil,
		nil,
		nil,
	)
	_, err = service.AssignDevicesToChannel(t.Context(), collectiondomain.AssignDevicesToChannelParams{
		OrgID:             orgID,
		TargetChannelID:   &target.Id,
		DeviceIdentifiers: deviceIDs,
	})
	require.Error(t, err)

	channels, err := store.GetDeviceCollections(
		t.Context(),
		orgID,
		deviceIDs[0],
		collectionpb.CollectionType_COLLECTION_TYPE_CHANNEL,
	)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, source.Id, channels[0].Id)
}

func TestChannelService_CreateFailureLeavesNoPartialChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, _ := setupCollectionTestData(t, 0)
	store := newCollectionStore(db)
	releaseSet := createTestReleaseSet(t, store, orgID, "create-rollback")
	_, err := db.ExecContext(t.Context(), `
		CREATE FUNCTION fail_broken_channel_extension()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			IF EXISTS (
				SELECT 1
				FROM device_set
				WHERE id = NEW.device_set_id
				  AND label = 'Broken'
			) THEN
				RAISE EXCEPTION 'injected channel extension failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER fail_broken_channel_extension
		BEFORE INSERT ON device_set_channel
		FOR EACH ROW
		EXECUTE FUNCTION fail_broken_channel_extension();
	`)
	require.NoError(t, err)

	service := collectiondomain.NewService(
		store,
		nil,
		nil,
		nil,
		sqlstores.NewSQLTransactor(db),
		nil,
		nil,
		nil,
		nil,
	)
	ctx := testutil.MockAuthContextForTesting(t.Context(), 1, orgID)
	response, err := service.CreateCollection(ctx, &collectionpb.CreateCollectionRequest{
		Type:  collectionpb.CollectionType_COLLECTION_TYPE_CHANNEL,
		Label: "Broken",
		TypeDetails: &collectionpb.CreateCollectionRequest_ChannelInfo{
			ChannelInfo: &collectionpb.ChannelInfo{ReleaseSetId: releaseSet.Id},
		},
	})
	require.Error(t, err)
	assert.Nil(t, response)

	var channelCount int
	require.NoError(t, db.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM device_set WHERE org_id = $1 AND label = 'Broken'",
		orgID,
	).Scan(&channelCount))
	assert.Zero(t, channelCount)
}

func TestChannelService_AssignmentIsOrganizationScopedAndAtomic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	userA := testContext.DatabaseService.CreateSuperAdminUser()
	userB := testContext.DatabaseService.CreateSuperAdminUser2()
	deviceA := testContext.DatabaseService.CreateDevice(userA.OrganizationID, "proto")
	deviceB := testContext.DatabaseService.CreateDevice(userB.OrganizationID, "proto")
	db := testContext.DatabaseService.DB
	store := newCollectionStore(db)

	releaseA := createTestReleaseSet(t, store, userA.OrganizationID, "org-a")
	sourceA := createTestChannel(t, store, userA.OrganizationID, releaseA.Id, "Source A")
	targetA := createTestChannel(t, store, userA.OrganizationID, releaseA.Id, "Target A")
	_, err := store.AddDevicesToCollection(t.Context(), userA.OrganizationID, sourceA.Id, []string{deviceA.ID})
	require.NoError(t, err)

	releaseB := createTestReleaseSet(t, store, userB.OrganizationID, "org-b")
	sourceB := createTestChannel(t, store, userB.OrganizationID, releaseB.Id, "Source B")
	_, err = store.AddDevicesToCollection(t.Context(), userB.OrganizationID, sourceB.Id, []string{deviceB.ID})
	require.NoError(t, err)

	service := collectiondomain.NewService(
		store,
		nil,
		nil,
		nil,
		sqlstores.NewSQLTransactor(db),
		nil,
		nil,
		nil,
		nil,
	)
	_, err = service.AssignDevicesToChannel(t.Context(), collectiondomain.AssignDevicesToChannelParams{
		OrgID:             userA.OrganizationID,
		TargetChannelID:   &targetA.Id,
		DeviceIdentifiers: []string{deviceA.ID, deviceB.ID},
	})
	require.Error(t, err)

	channelA, err := store.GetDeviceCollections(
		t.Context(),
		userA.OrganizationID,
		deviceA.ID,
		collectionpb.CollectionType_COLLECTION_TYPE_CHANNEL,
	)
	require.NoError(t, err)
	require.Len(t, channelA, 1)
	assert.Equal(t, sourceA.Id, channelA[0].Id)
	channelB, err := store.GetDeviceCollections(
		t.Context(),
		userB.OrganizationID,
		deviceB.ID,
		collectionpb.CollectionType_COLLECTION_TYPE_CHANNEL,
	)
	require.NoError(t, err)
	require.Len(t, channelB, 1)
	assert.Equal(t, sourceB.Id, channelB[0].Id)
}
