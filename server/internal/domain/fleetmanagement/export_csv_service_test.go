package fleetmanagement_test

import (
	"encoding/csv"
	"strings"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_ExportMinerListCsv_ShouldExportOnlyPairedMinersAndRespectFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	testUser := testContext.DatabaseService.CreateSuperAdminUser()

	deviceIDs := testContext.DatabaseService.CreateTestMiners(testUser.OrganizationID, 2, "https://172.17.0.1:80")
	deviceStore := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)
	expectedWorkerName := "worker-01"
	require.NoError(t, deviceStore.UpdateWorkerName(t.Context(), minermodels.DeviceIdentifier(deviceIDs[0]), expectedWorkerName))
	require.NoError(t, deviceStore.UpsertDeviceStatus(t.Context(), minermodels.DeviceIdentifier(deviceIDs[0]), minermodels.MinerStatusActive, ""))
	require.NoError(t, deviceStore.UpsertDeviceStatus(t.Context(), minermodels.DeviceIdentifier(deviceIDs[1]), minermodels.MinerStatusNeedsMiningPool, ""))

	discoveredDeviceStore := sqlstores.NewSQLDiscoveredDeviceStore(testContext.ServiceProvider.DB)
	_, err := discoveredDeviceStore.Save(t.Context(), discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: "unpaired-device-1",
		OrgID:            testUser.OrganizationID,
	}, &discoverymodels.DiscoveredDevice{
		Device: pairingpb.Device{
			DeviceIdentifier: "unpaired-device-1",
			Model:            "S19 Pro",
			Manufacturer:     "Bitmain",
			DriverName:       "ANTMINER",
			IpAddress:        "192.168.1.100",
			Port:             "4028",
			UrlScheme:        "http",
		},
		IsActive: true,
		OrgID:    testUser.OrganizationID,
	})
	require.NoError(t, err)

	ctx := testutil.MockAuthContextForTesting(t.Context(), testUser.DatabaseID, testUser.OrganizationID)

	var chunks []*pb.ExportMinerListCsvResponse
	err = testContext.ServiceProvider.FleetManagementService.ExportMinerListCsv(ctx, &pb.ExportMinerListCsvRequest{
		Filter: &pb.MinerListFilter{
			DeviceStatus: []pb.DeviceStatus{pb.DeviceStatus_DEVICE_STATUS_ONLINE},
		},
		TemperatureUnit: pb.CsvTemperatureUnit_CSV_TEMPERATURE_UNIT_CELSIUS,
	}, func(chunk *pb.ExportMinerListCsvResponse) error {
		chunks = append(chunks, chunk)
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Reassemble all chunks into a single CSV
	var allData []byte
	for _, chunk := range chunks {
		allData = append(allData, chunk.CsvData...)
	}

	// Verify UTF-8 BOM is present for Excel compatibility
	require.True(t, len(allData) >= 3 && allData[0] == 0xEF && allData[1] == 0xBB && allData[2] == 0xBF, "CSV should start with UTF-8 BOM")

	// Strip BOM before parsing
	records, err := csv.NewReader(strings.NewReader(string(allData[3:]))).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, []string{
		"Name",
		"Worker Name",
		"Groups",
		"Rack",
		"Model",
		"MAC Address",
		"IP Address",
		"Status",
		"Issues",
		"Hashrate (TH/s)",
		"Efficiency (J/TH)",
		"Power (kW)",
		"Temp (°C)",
		"Firmware",
	}, records[0])
	assert.Equal(t, expectedWorkerName, records[1][1])
	assert.Equal(t, "172.17.0.1", records[1][6])
	assert.Equal(t, "Hashing", records[1][7])
}
