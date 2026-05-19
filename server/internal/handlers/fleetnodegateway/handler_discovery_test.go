package fleetnodegateway_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeauth"
)

func TestReportDiscoveredDevices_PersistsAttribution(t *testing.T) {
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
	var attributed sql.NullInt64
	require.NoError(t, db.QueryRow(`SELECT discovered_by_fleet_node_id FROM discovered_device WHERE device_identifier = 'discovered-1' AND org_id = 1`).Scan(&attributed))
	require.True(t, attributed.Valid, "discovered_by_fleet_node_id must be set")
	assert.Equal(t, fleetNodeID, attributed.Int64)
	var rowCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM discovered_device WHERE org_id = 1 AND device_identifier IN ('discovered-1','discovered-2')`).Scan(&rowCount))
	assert.Equal(t, 2, rowCount)
}

func TestReportDiscoveredDevices_PublishesBatchToInFlightCommand(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	subject := &fleetnodeauth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-correlation"}

	stream, err := h.registry.Register(h.fleetNodeID)
	require.NoError(t, err)
	defer stream.Unregister()
	events, cleanup, err := h.registry.Send(context.Background(), h.fleetNodeID, &pb.ControlCommand{CommandId: "operator-cmd"})
	require.NoError(t, err)
	defer cleanup()
	<-stream.Outgoing

	ctx := authn.SetInfo(context.Background(), subject)

	// Act
	resp, err := h.handler.ReportDiscoveredDevices(ctx, connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
		CommandId: "operator-cmd",
		Devices: []*pb.DiscoveredDeviceReport{
			{DeviceIdentifier: "corr-1", IpAddress: "10.0.0.50", Port: "4028", UrlScheme: "http", DriverName: "virtual"},
		},
	}))

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Msg.GetAcceptedCount())
	select {
	case ev, ok := <-events:
		require.True(t, ok)
		require.NotNil(t, ev.Batch)
		require.Len(t, ev.Batch.GetDevices(), 1)
		assert.Equal(t, "corr-1", ev.Batch.GetDevices()[0].GetDeviceIdentifier())
	case <-time.After(time.Second):
		t.Fatal("expected batch on events channel")
	}
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
