package pairing_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/stretchr/testify/require"
)

func TestEndpointDiscoveryGroupsByPluginKeyAndNetworkOwner(t *testing.T) {
	for _, partnerFirst := range []bool{false, true} {
		name := "legacy endpoint first"
		if partnerFirst {
			name = "partner endpoint first"
		}
		t.Run(name, func(t *testing.T) {
			testEndpointDiscoveryGroupsByPluginKeyAndNetworkOwner(t, partnerFirst)
		})
	}
}

func endpointGroupReport(t *testing.T, node int64, endpointID, host string) fleetnodepairing.DiscoveredDeviceReport {
	t.Helper()
	endpoints := []*commonpb.DeviceEndpoint{{Id: endpointID, DeviceKey: "ct1-pair", MemberIds: []string{"controller-a", "controller-b"}, Host: host, Port: 8080, UrlScheme: "http", RpcPort: 9095}}
	identifier, err := deviceendpoints.NodeIdentifier(node, "proto", endpoints)
	require.NoError(t, err)
	return fleetnodepairing.DiscoveredDeviceReport{
		DeviceIdentifier: identifier, IPAddress: host, Port: "8080", URLScheme: "http", RPCPort: 9095,
		DriverName: "proto", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
		Model: "CT1", Manufacturer: "Proto", Endpoints: endpoints,
	}
}

func TestEndpointDiscoveryReclaimsRevokedNodeInventory(t *testing.T) {
	ctx := t.Context()
	db, org, svc, enrollment := setupPairingTest(t)
	original := createFleetNode(t, enrollment, org, "original-node")
	replacement := createFleetNode(t, enrollment, org, "replacement-node")
	a := endpointGroupReport(t, original, "controller-a", "10.0.0.11")
	b := endpointGroupReport(t, original, "controller-b", "10.0.0.12")
	_, _, err := svc.UpsertDiscoveredDevices(ctx, original, org, []fleetnodepairing.DiscoveredDeviceReport{a, b})
	require.NoError(t, err)
	result := &gatewaypb.FleetNodePairResult{
		DeviceIdentifier: a.DeviceIdentifier, Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED,
		SerialNumber: "controller-a", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
		Model: "CT1", Manufacturer: "Proto",
	}
	status, err := svc.PersistFleetNodePairResult(ctx, original, org, result, nil)
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, status)
	deviceID := deviceIDByIdentifier(t, db, org, a.DeviceIdentifier)
	var discoveredID, siteID, tankID int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT discovered_device_id FROM device WHERE id=$1`, deviceID).Scan(&discoveredID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO site (org_id, name, slug) VALUES ($1, 'Mine', 'mine') RETURNING id`, org).Scan(&siteID))
	_, err = db.ExecContext(ctx, `UPDATE device SET custom_name='North container', site_id=$2 WHERE id=$1`, deviceID, siteID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO container_inventory (device_id, org_id, observed_at) VALUES ($1, $2, now())`, deviceID, org)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO container_tank (device_id, position, first_seen_at, last_seen_at) VALUES ($1, 1, now(), now()) RETURNING id`, deviceID).Scan(&tankID))
	require.NoError(t, enrollment.RevokeFleetNode(ctx, original, org))

	// The replacement scans the partner first and has a different node-scoped ID.
	report := endpointGroupReport(t, replacement, "controller-b", "10.0.0.22")
	require.NotEqual(t, a.DeviceIdentifier, report.DeviceIdentifier)
	accepted, rejected, err := svc.UpsertDiscoveredDevices(ctx, replacement, org, []fleetnodepairing.DiscoveredDeviceReport{report})
	require.NoError(t, err)
	require.Zero(t, rejected)
	require.Len(t, accepted, 1)
	require.Equal(t, a.DeviceIdentifier, accepted[0].DeviceIdentifier)
	require.Len(t, accepted[0].Endpoints, 2)
	require.Equal(t, "10.0.0.11", accepted[0].Endpoints[0].Host)
	require.Equal(t, "10.0.0.22", accepted[0].Endpoints[1].Host)
	status, err = svc.PersistFleetNodePairResult(ctx, replacement, org, result, nil)
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, status)
	require.True(t, deviceBoundToNode(t, db, org, replacement, a.DeviceIdentifier))
	require.Equal(t, deviceID, deviceIDByIdentifier(t, db, org, a.DeviceIdentifier))

	var savedDiscoveredID, savedSiteID, savedTankID, owner int64
	var name, serial string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT d.discovered_device_id, d.site_id, d.custom_name, d.serial_number, dd.discovered_by_fleet_node_id, ct.id
		FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
		JOIN container_tank ct ON ct.device_id=d.id WHERE d.id=$1`, deviceID).
		Scan(&savedDiscoveredID, &savedSiteID, &name, &serial, &owner, &savedTankID))
	require.Equal(t, discoveredID, savedDiscoveredID)
	require.Equal(t, siteID, savedSiteID)
	require.Equal(t, "North container", name)
	require.Equal(t, "controller-a", serial)
	require.Equal(t, replacement, owner)
	require.Equal(t, tankID, savedTankID)
	var deviceCount, discoveryCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM device WHERE org_id=$1 AND deleted_at IS NULL`, org).Scan(&deviceCount))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND deleted_at IS NULL`, org).Scan(&discoveryCount))
	require.Equal(t, 1, deviceCount)
	require.Equal(t, 1, discoveryCount)
}

func TestEndpointDiscoveryRejectsAmbiguousRevokedNodes(t *testing.T) {
	ctx := t.Context()
	db, org, svc, enrollment := setupPairingTest(t)
	var nodes []int64
	for _, name := range []string{"first-node", "second-node"} {
		node := createFleetNode(t, enrollment, org, name)
		nodes = append(nodes, node)
		report := endpointGroupReport(t, node, "controller-a", "10.0.0.11")
		accepted, rejected, err := svc.UpsertDiscoveredDevices(ctx, node, org, []fleetnodepairing.DiscoveredDeviceReport{report})
		require.NoError(t, err)
		require.Zero(t, rejected)
		require.Len(t, accepted, 1)
		require.Equal(t, report.DeviceIdentifier, accepted[0].DeviceIdentifier)
		// Keep both owners live until both distinct inventories exist.
	}
	require.NoError(t, enrollment.RevokeFleetNode(ctx, nodes[0], org))
	liveReport := endpointGroupReport(t, nodes[1], "controller-a", "10.0.0.21")
	accepted, rejected, err := svc.UpsertDiscoveredDevices(ctx, nodes[1], org, []fleetnodepairing.DiscoveredDeviceReport{liveReport})
	require.NoError(t, err)
	require.Zero(t, rejected)
	require.Len(t, accepted, 1)
	require.Equal(t, liveReport.DeviceIdentifier, accepted[0].DeviceIdentifier)
	var liveID, revokedHost string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT device_identifier FROM discovered_device WHERE org_id=$1 AND discovered_by_fleet_node_id=$2`, org, nodes[1]).Scan(&liveID))
	require.Equal(t, liveReport.DeviceIdentifier, liveID)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT ip_address FROM discovered_device WHERE org_id=$1 AND discovered_by_fleet_node_id=$2`, org, nodes[0]).Scan(&revokedHost))
	require.Equal(t, "10.0.0.11", revokedHost)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND deleted_at IS NULL`, org).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, enrollment.RevokeFleetNode(ctx, nodes[1], org))
	replacement := createFleetNode(t, enrollment, org, "replacement-node")
	report := endpointGroupReport(t, replacement, "controller-a", "10.0.0.11")
	_, _, err = svc.UpsertDiscoveredDevices(ctx, replacement, org, []fleetnodepairing.DiscoveredDeviceReport{report})
	require.Error(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND deleted_at IS NULL`, org).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND discovered_by_fleet_node_id=$2`, org, replacement).Scan(&count))
	require.Zero(t, count)
}

func testEndpointDiscoveryGroupsByPluginKeyAndNetworkOwner(t *testing.T, partnerFirst bool) {
	t.Helper()
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "node-a")
	other := createFleetNode(t, enrollment, org, "node-b")
	report := func(id, host string) fleetnodepairing.DiscoveredDeviceReport {
		return fleetnodepairing.DiscoveredDeviceReport{DeviceIdentifier: id, IPAddress: host, Port: "8080", URLScheme: "http", RPCPort: 0, DriverName: "virtual", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG, Endpoints: []*commonpb.DeviceEndpoint{{Id: id, DeviceKey: "opaque-key", MemberIds: []string{"a", "b"}, Host: host, Port: 8080, UrlScheme: "http", RpcPort: 0}}}
	}
	a, b := report("a", "10.0.0.11"), report("b", "10.0.0.12")
	// An existing single-address discovery must retain its identifier on upgrade.
	legacy := a
	legacy.Endpoints = nil
	legacy.Port = "8181"
	legacy.URLScheme = "https"
	_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{legacy})
	require.NoError(t, err)
	if partnerFirst {
		// A partner-first upgrade needs the identity learned during pairing.
		_, err = db.ExecContext(t.Context(), `UPDATE discovered_device SET serial_number = 'a' WHERE org_id = $1 AND device_identifier = $2`, org, legacy.DeviceIdentifier)
		require.NoError(t, err)
	}
	// The generic store supports endpoints for other plugins and device classes.
	reports := []fleetnodepairing.DiscoveredDeviceReport{a, b}
	if partnerFirst {
		reports[0], reports[1] = reports[1], reports[0]
	}
	accepted, rejected, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, reports)
	require.NoError(t, err)
	require.Zero(t, rejected)
	require.Len(t, accepted, 2)
	require.Equal(t, "a", accepted[0].DeviceIdentifier)
	require.Equal(t, accepted[0].DeviceIdentifier, accepted[1].DeviceIdentifier)
	require.Len(t, accepted[1].Endpoints, 2)
	listed, _, err := svc.ListDiscoveredDevicesForFleetNode(t.Context(), org, &node, nil, nil)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "a", listed[0].DeviceIdentifier)
	require.Len(t, listed[0].Endpoints, 2)
	require.Equal(t, "a", listed[0].Endpoints[0].Id)
	require.Equal(t, "10.0.0.11", listed[0].Endpoints[0].Host)
	require.EqualValues(t, 8181, listed[0].Endpoints[0].Port)
	require.Equal(t, "https", listed[0].Endpoints[0].UrlScheme)
	require.Equal(t, "b", listed[0].Endpoints[1].Id)
	require.Equal(t, "10.0.0.12", listed[0].Endpoints[1].Host)
	require.EqualValues(t, 8080, listed[0].Endpoints[1].Port)
	require.Equal(t, "10.0.0.11", listed[0].IPAddress)
	require.Equal(t, "8181", listed[0].Port)
	require.Equal(t, "https", listed[0].URLScheme)
	// Equal plugin keys on another LAN are distinct devices.
	elsewhere, _, err := svc.UpsertDiscoveredDevices(t.Context(), other, org, []fleetnodepairing.DiscoveredDeviceReport{a, b})
	require.NoError(t, err)
	require.NotEqual(t, accepted[0].DeviceIdentifier, elsewhere[0].DeviceIdentifier)
	// Unpaired node discoveries can have no saved serial number.
	_, err = db.ExecContext(t.Context(), `UPDATE discovered_device SET serial_number = '' WHERE org_id = $1 AND device_identifier = $2`, org, legacy.DeviceIdentifier)
	require.NoError(t, err)
	// A rescan moves only the scanned endpoint and retains its partner.
	moved := report("a", "10.0.0.21")
	again, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{moved})
	require.NoError(t, err)
	require.Len(t, again[0].Endpoints, 2)
	require.Equal(t, "a", again[0].DeviceIdentifier)
	require.Equal(t, "10.0.0.21", again[0].IPAddress)
	require.Equal(t, "8181", again[0].Port)
	require.Equal(t, "https", again[0].URLScheme)
	require.Equal(t, "10.0.0.21", again[0].Endpoints[0].Host)
	require.EqualValues(t, 8181, again[0].Endpoints[0].Port)
	require.Equal(t, "https", again[0].Endpoints[0].UrlScheme)
	require.Equal(t, "10.0.0.12", again[0].Endpoints[1].Host)
	partnerMoved := report("b", "10.0.0.22")
	again, _, err = svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{partnerMoved})
	require.NoError(t, err)
	require.Equal(t, "10.0.0.21", again[0].IPAddress)
	require.Equal(t, "8181", again[0].Port)
	require.Equal(t, "https", again[0].URLScheme)
	require.Equal(t, "10.0.0.22", again[0].Endpoints[1].Host)
	// A claimed second address must not bypass scan admission.
	invalid := a
	invalid.Endpoints = []*commonpb.DeviceEndpoint{a.Endpoints[0], b.Endpoints[0]}
	_, _, err = svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{invalid})
	require.Error(t, err)
}
