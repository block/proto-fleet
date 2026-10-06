package fleetmanagement_test

import (
	"context"
	"testing"
	"time"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/pairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetryv2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestLookupMinerByInternalIdentifier(t *testing.T) {
	testContext := testutil.InitializeDBServiceInfrastructure(t)
	admin := testContext.DatabaseService.CreateSuperAdminUser()
	otherAdmin := testContext.DatabaseService.CreateSuperAdminUser2()
	created := testContext.DatabaseService.CreateAndAssignDevices(1, admin.OrganizationID)[0]
	store := testContext.ServiceProvider.DeviceStore
	device, err := store.GetDeviceByDeviceIdentifier(t.Context(), created.ID, admin.OrganizationID)
	require.NoError(t, err)
	// Reconciliation preserves independent device and discovery identifiers.
	_, err = testContext.ServiceProvider.DB.ExecContext(t.Context(),
		"UPDATE discovered_device SET device_identifier = $1 WHERE device_identifier = $2 AND org_id = $3",
		"independent-discovery-row", created.ID, admin.OrganizationID)
	require.NoError(t, err)
	collections := sqlstores.NewSQLCollectionStore(testContext.ServiceProvider.DB)
	group, err := collections.CreateCollection(t.Context(), admin.OrganizationID, collectionpb.CollectionType_COLLECTION_TYPE_GROUP, "Recovery group", "")
	require.NoError(t, err)
	_, err = collections.AddDevicesToCollection(t.Context(), admin.OrganizationID, group.Id, []string{created.ID})
	require.NoError(t, err)
	telemetry := &identifierLookupTelemetry{TelemetryCollector: fleetmanagement.NewMockTelemetryCollector(), t: t, deviceID: created.ID}
	service := fleetmanagement.NewService(store, nil, telemetry, nil, testContext.ServiceProvider.PluginService, nil, nil, collections, nil, nil, nil)
	ctx := testutil.MockAuthContextForTesting(t.Context(), admin.DatabaseID, admin.OrganizationID)
	request := &pb.LookupMinerByIdentifierRequest{
		Identifier: created.ID, IdentifierType: pb.MinerIdentifierType_MINER_IDENTIFIER_TYPE_DEVICE_IDENTIFIER,
	}
	for _, status := range []string{pairing.StatusPaired, pairing.StatusDefaultPassword, pairing.StatusAuthenticationNeeded} {
		t.Run(status, func(t *testing.T) {
			require.NoError(t, store.UpsertDevicePairing(t.Context(), device, admin.OrganizationID, status))
			result, err := service.LookupMinerByIdentifier(ctx, request)
			require.NoError(t, err)
			require.Equal(t, created.ID, result.GetSnapshot().GetDeviceIdentifier())
			if status != pairing.StatusAuthenticationNeeded {
				require.Equal(t, device.MacAddress, result.GetSnapshot().GetMacAddress())
				require.Len(t, result.Snapshot.Hashrate, 1)
				require.InDelta(t, 100, result.Snapshot.Hashrate[0].Value, 0.001)
				require.Len(t, result.Snapshot.Placement.Groups, 1)
				require.Equal(t, group.Id, result.Snapshot.Placement.Groups[0].Id)
			}
		})
	}

	for _, query := range []string{created.ID, "independent-discovery-row"} {
		t.Run("search "+query, func(t *testing.T) {
			require.NoError(t, store.UpsertDevicePairing(t.Context(), device, admin.OrganizationID, pairing.StatusPaired))
			result, err := service.ListMinerStateSnapshots(ctx, &pb.ListMinerStateSnapshotsRequest{Filter: &pb.MinerListFilter{SearchQuery: query}, PageSize: 10})
			require.NoError(t, err)
			require.Len(t, result.Miners, 1)
			require.Equal(t, created.ID, result.Miners[0].DeviceIdentifier)
			require.Equal(t, int32(1), result.TotalMiners)
		})
	}
	otherCtx := testutil.MockAuthContextForTesting(t.Context(), otherAdmin.DatabaseID, otherAdmin.OrganizationID)
	_, err = service.LookupMinerByIdentifier(otherCtx, request)
	require.True(t, fleeterror.IsNotFoundError(err), "internal IDs must not escape organization scope")
	require.NoError(t, store.UpsertDevicePairing(t.Context(), device, admin.OrganizationID, pairing.StatusUnpaired))
	_, err = service.LookupMinerByIdentifier(ctx, request)
	require.True(t, fleeterror.IsNotFoundError(err), "an unpaired miner cannot resolve as a linked paired miner")
}

type identifierLookupTelemetry struct {
	fleetmanagement.TelemetryCollector
	t        *testing.T
	deviceID string
}

func (c *identifierLookupTelemetry) GetLatestDeviceMetrics(_ context.Context, ids []minermodels.DeviceIdentifier) (map[minermodels.DeviceIdentifier]telemetryv2.DeviceMetrics, error) {
	require.Equal(c.t, []minermodels.DeviceIdentifier{minermodels.DeviceIdentifier(c.deviceID)}, ids)
	return map[minermodels.DeviceIdentifier]telemetryv2.DeviceMetrics{minermodels.DeviceIdentifier(c.deviceID): {
		DeviceIdentifier: c.deviceID, Timestamp: time.Now(), HashrateHS: &telemetryv2.MetricValue{Value: 100e12},
	}}, nil
}
