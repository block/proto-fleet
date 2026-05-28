package fleetnodegateway_test

import (
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeauth"
)

func TestReportDiscoveredDevices_PersistsRows(t *testing.T) {
	// Arrange
	handler, db, fleetNodeID := newHeartbeatHandler(t)
	ctx := authn.SetInfo(t.Context(), &fleetnodeauth.Subject{
		FleetNodeID: fleetNodeID,
		OrgID:       1,
		Name:        "agent-discovery",
	})
	req := connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
		Devices: []*pb.DiscoveredDeviceReport{
			{
				DeviceIdentifier: "discovered-1",
				IpAddress:        "192.168.1.10",
				Port:             "80",
				UrlScheme:        "http",
				DriverName:       "virtual",
				Model:            "S19",
				Manufacturer:     "Acme",
				FirmwareVersion:  "1.2.3",
			},
			{
				DeviceIdentifier: "discovered-2",
				IpAddress:        "192.168.1.11",
				Port:             "443",
				UrlScheme:        "https",
				DriverName:       "virtual",
			},
		},
	})

	// Act
	resp, err := handler.ReportDiscoveredDevices(ctx, req)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.Msg.GetAcceptedCount())
	var rowCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM discovered_device WHERE org_id = 1 AND device_identifier IN ('discovered-1','discovered-2')`).Scan(&rowCount))
	assert.Equal(t, 2, rowCount)
}

func TestReportDiscoveredDevices_RejectsMissingSubject(t *testing.T) {
	// Arrange
	handler, _, _ := newHeartbeatHandler(t)
	req := connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
		Devices: []*pb.DiscoveredDeviceReport{
			{DeviceIdentifier: "x", IpAddress: "10.0.0.1", Port: "80", UrlScheme: "http", DriverName: "virtual"},
		},
	})

	// Act
	_, err := handler.ReportDiscoveredDevices(t.Context(), req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fleet node subject")
}
