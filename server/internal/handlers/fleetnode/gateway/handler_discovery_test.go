package gateway_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

func TestReportDiscoveredDevices_ContainerRPCScopeAndPersistence(t *testing.T) {
	h := newControlHarness(t)
	ctx := authn.SetInfo(t.Context(), &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1})
	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	scope := func(ip, port string) bool { return ip == "10.0.0.50" && port == "9095" }
	session, err := h.registry.Send(ctx, h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "rpc-scan"}, scope, control.ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing
	resp, err := h.handler.ReportDiscoveredDevices(ctx, connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
		CommandId: "rpc-scan",
		Devices: []*pb.DiscoveredDeviceReport{
			{DeviceIdentifier: "ct1", IpAddress: "10.0.0.50", Port: "8181", UrlScheme: "https", RpcPort: 9095, RpcTls: true, DriverName: "proto", Model: "CT1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER},
			{DeviceIdentifier: "wrong-ip", IpAddress: "10.0.0.51", Port: "8181", RpcPort: 9095, DriverName: "proto", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER},
			{DeviceIdentifier: "wrong-rpc", IpAddress: "10.0.0.50", Port: "9095", RpcPort: 9443, DriverName: "proto", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER},
			{DeviceIdentifier: "rig-rpc", IpAddress: "10.0.0.50", Port: "8181", RpcPort: 9095, DriverName: "proto", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG},
			{DeviceIdentifier: "invalid-rpc", IpAddress: "10.0.0.50", Port: "9095", RpcPort: -1, DriverName: "proto", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, int64(1), resp.Msg.GetAcceptedCount())
	require.Equal(t, int64(4), resp.Msg.GetRejectedCount())
	var web, scheme string
	var rpc int
	var tls bool
	require.NoError(t, h.db.QueryRowContext(ctx, "SELECT port,url_scheme,rpc_port,rpc_tls FROM discovered_device WHERE org_id=1 AND device_identifier='ct1'").Scan(&web, &scheme, &rpc, &tls))
	require.Equal(t, "8181", web)
	require.Equal(t, "https", scheme)
	require.Equal(t, 9095, rpc)
	require.True(t, tls)
	select {
	case event := <-session.Events():
		require.Len(t, event.Batch.GetDevices(), 1)
		require.Equal(t, "ct1", event.Batch.GetDevices()[0].GetDeviceIdentifier())
	case <-time.After(time.Second):
		t.Fatal("expected accepted container report")
	}
}

func TestReportDiscoveredDevices_RejectsMissingCommandID(t *testing.T) {
	// Arrange
	handler, _, fleetNodeID := newHeartbeatHandler(t)
	ctx := authn.SetInfo(t.Context(), &auth.Subject{
		FleetNodeID: fleetNodeID,
		OrgID:       1,
		Name:        "agent-discovery",
	})
	req := connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
		Devices: []*pb.DiscoveredDeviceReport{
			{DeviceIdentifier: "x", IpAddress: "10.0.0.1", Port: "80", UrlScheme: "http", DriverName: "virtual"},
		},
	})

	// Act
	_, err := handler.ReportDiscoveredDevices(ctx, req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command_id")
}

func TestReportDiscoveredDevices_RejectsUnknownCommandID(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	subject := &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-unknown-cmd"}
	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	ctx := authn.SetInfo(context.Background(), subject)

	// Act
	_, err := h.handler.ReportDiscoveredDevices(ctx, connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
		CommandId: "never-sent",
		Devices: []*pb.DiscoveredDeviceReport{
			{DeviceIdentifier: "x", IpAddress: "10.0.0.1", Port: "80", UrlScheme: "http", DriverName: "virtual"},
		},
	}))

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in-flight server-issued command")
}

func TestReportDiscoveredDevices_PublishesBatchToInFlightCommand(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	subject := &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-correlation"}

	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "operator-cmd"}, nil, control.ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
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
	case ev := <-session.Events():
		require.NotNil(t, ev.Batch)
		require.Len(t, ev.Batch.GetDevices(), 1)
		assert.Equal(t, "corr-1", ev.Batch.GetDevices()[0].GetDeviceIdentifier())
	case <-time.After(time.Second):
		t.Fatal("expected batch on events channel")
	}
}

// A partially-accepted report must only forward accepted devices to the
// operator. Rejected rows (e.g. ownership conflicts) must not surface,
// since the operator-facing batch is a trust signal — the DB already
// refused to persist them.
func TestReportDiscoveredDevices_PublishesOnlyAcceptedDevices(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	db := h.db
	subject := &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-partial"}

	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "partial-cmd"}, nil, control.ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing

	// Seed a discovered_device + paired device attributed to a different
	// fleet node A, then have fleet node B (h.fleetNodeID) try to report
	// the same identifier. The upsert's ownership guard rejects B's row;
	// the accepted row alongside it should still flow through.
	var otherNodeID int64
	require.NoError(t, db.QueryRow(`INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
		VALUES (1, 'other-node-for-partial', $1, $2, 'CONFIRMED') RETURNING id`,
		[]byte("partial-pubkey"),
		[]byte("01234567890123456789012345678901")).Scan(&otherNodeID))
	var ddID int64
	require.NoError(t, db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		VALUES (1, 'owned-by-other', '10.0.0.70', '80', 'http', 'virtual', TRUE, $1) RETURNING id`, otherNodeID).Scan(&ddID))
	var devID int64
	require.NoError(t, db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ('owned-by-other', 'aa:bb:cc:00:00:fa', 'sn-other', 1, $1) RETURNING id`, ddID).Scan(&devID))
	_, err = db.Exec(`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`, otherNodeID, devID)
	require.NoError(t, err)

	ctx := authn.SetInfo(context.Background(), subject)

	// Act: report two devices, one of which is owned-by-other (rejected).
	resp, err := h.handler.ReportDiscoveredDevices(ctx, connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
		CommandId: "partial-cmd",
		Devices: []*pb.DiscoveredDeviceReport{
			{DeviceIdentifier: "owned-by-other", IpAddress: "10.0.0.71", Port: "80", UrlScheme: "http", DriverName: "virtual"},
			{DeviceIdentifier: "fresh-1", IpAddress: "10.0.0.72", Port: "80", UrlScheme: "http", DriverName: "virtual"},
		},
	}))

	// Assert: accepted count is 1, published batch contains only fresh-1.
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Msg.GetAcceptedCount())
	select {
	case ev := <-session.Events():
		require.NotNil(t, ev.Batch)
		require.Len(t, ev.Batch.GetDevices(), 1)
		assert.Equal(t, "fresh-1", ev.Batch.GetDevices()[0].GetDeviceIdentifier())
	case <-time.After(time.Second):
		t.Fatal("expected batch on events channel")
	}
}

func TestReportDiscoveredDevices_PublishesPersistedDeviceClass(t *testing.T) {
	h := newControlHarness(t)
	ctx := authn.SetInfo(t.Context(), &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1})
	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	session, err := h.registry.Send(ctx, h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "class-rescan"}, nil, control.ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing

	for _, tc := range []struct {
		name     string
		reported commonpb.DeviceClass
		want     commonpb.DeviceClass
	}{
		{"new unspecified device", commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED, commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED},
		{"learn container class", commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER},
		{"preserve container on rescan", commonpb.DeviceClass_DEVICE_CLASS_UNSPECIFIED, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER},
		{"replace with explicit class", commonpb.DeviceClass_DEVICE_CLASS_RIG, commonpb.DeviceClass_DEVICE_CLASS_RIG},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := h.handler.ReportDiscoveredDevices(ctx, connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
				CommandId: "class-rescan",
				Devices: []*pb.DiscoveredDeviceReport{{
					DeviceIdentifier: "class-device", IpAddress: "10.0.0.50", Port: "80",
					UrlScheme: "http", DriverName: "proto", Model: "CT1", DeviceClass: tc.reported,
				}},
			}))
			require.NoError(t, err)
			require.Equal(t, int64(1), resp.Msg.GetAcceptedCount())
			require.Zero(t, resp.Msg.GetRejectedCount())
			select {
			case ev := <-session.Events():
				require.NotNil(t, ev.Batch)
				require.Len(t, ev.Batch.GetDevices(), 1)
				assert.Equal(t, "class-device", ev.Batch.GetDevices()[0].GetDeviceIdentifier())
				assert.Equal(t, tc.want, ev.Batch.GetDevices()[0].GetDeviceClass())
			case <-time.After(time.Second):
				t.Fatal("expected discovery batch on events channel")
			}
		})
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

// A device whose IP/port is outside the command's requested scan scope must be
// dropped before upsert/publish, so a compromised node can't report devices it
// was never asked to scan.
func TestReportDiscoveredDevices_DropsOutOfScopeDevices(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	subject := &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-scope"}
	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	scope := func(ip, port string) bool { return ip == "10.0.0.50" && port == "4028" }
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "scoped-cmd"}, scope, control.ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing
	ctx := authn.SetInfo(context.Background(), subject)

	// Act: report one in-scope device and one outside the scope.
	resp, err := h.handler.ReportDiscoveredDevices(ctx, connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
		CommandId: "scoped-cmd",
		Devices: []*pb.DiscoveredDeviceReport{
			{DeviceIdentifier: "in-scope", IpAddress: "10.0.0.50", Port: "4028", UrlScheme: "http", DriverName: "virtual"},
			{DeviceIdentifier: "out-of-scope", IpAddress: "10.0.0.99", Port: "4028", UrlScheme: "http", DriverName: "virtual"},
		},
	}))

	// Assert: only the in-scope device is accepted and published.
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Msg.GetAcceptedCount())
	assert.Equal(t, int64(1), resp.Msg.GetRejectedCount())
	select {
	case ev := <-session.Events():
		require.NotNil(t, ev.Batch)
		require.Len(t, ev.Batch.GetDevices(), 1)
		assert.Equal(t, "in-scope", ev.Batch.GetDevices()[0].GetDeviceIdentifier())
	case <-time.After(time.Second):
		t.Fatal("expected batch on events channel")
	}
}

func TestReportDiscoveredDevices_PublishesAuthoritativeBinding(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	ctx := authn.SetInfo(t.Context(), &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1})
	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	session, err := h.registry.Send(ctx, h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "binding-rescan"}, nil, control.ReportKindDiscovery, nil)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing
	report := &pb.ReportDiscoveredDevicesRequest{CommandId: "binding-rescan", Devices: []*pb.DiscoveredDeviceReport{{DeviceIdentifier: "binding-device", IpAddress: "10.0.0.50", Port: "80", UrlScheme: "http", DriverName: "virtual"}}}

	// Act
	_, err = h.handler.ReportDiscoveredDevices(ctx, connect.NewRequest(report))
	require.NoError(t, err)
	first := <-session.Events()

	// Assert
	require.NotNil(t, first.Batch)
	require.False(t, first.Batch.Devices[0].BoundToFleetNode)

	// Arrange
	_, err = h.db.Exec(`WITH inserted AS (
		INSERT INTO device (org_id, device_identifier, discovered_device_id, mac_address)
		SELECT org_id, device_identifier, id, '' FROM discovered_device WHERE device_identifier='binding-device' AND org_id=1 RETURNING id
	) INSERT INTO fleet_node_device (org_id, fleet_node_id, device_id) SELECT 1, $1, id FROM inserted`, h.fleetNodeID)
	require.NoError(t, err)

	// Act
	_, err = h.handler.ReportDiscoveredDevices(ctx, connect.NewRequest(report))
	require.NoError(t, err)

	// Assert
	select {
	case event := <-session.Events():
		require.NotNil(t, event.Batch)
		require.True(t, event.Batch.Devices[0].BoundToFleetNode)
	case <-time.After(time.Second):
		t.Fatal("expected discovery batch with existing binding")
	}
}
