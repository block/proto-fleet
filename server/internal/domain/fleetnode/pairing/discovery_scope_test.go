package pairing_test

import (
	"testing"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func TestDirectBindingUsesLiveDiscoveryRanges(t *testing.T) {
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "partition-binding")
	device := insertDevice(t, db, org)
	var config *gatewaypb.DiscoveryConfig
	svc.WithDiscoveryConfig(func(int64) *gatewaypb.DiscoveryConfig { return config })
	for _, ranges := range [][]string{nil, {"10.0.1.0/24"}} {
		if ranges != nil {
			config = &gatewaypb.DiscoveryConfig{Ranges: ranges}
		}
		err := svc.PairDevice(t.Context(), node, device, org, nil)
		require.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
	}
	config = &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}}
	require.NoError(t, svc.PairDevice(t.Context(), node, device, org, nil))
	config = &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.1.0/24"}}
	require.True(t, fleeterror.IsFailedPreconditionError(svc.PairDevice(t.Context(), node, device, org, nil)))
}

func TestDirectBindingRequiresEveryEndpointInDiscoveryRanges(t *testing.T) {
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "partition-group-binding")
	a := endpointGroupReport(t, node, "controller-a", "10.0.0.11")
	b := endpointGroupReport(t, node, "controller-b", "10.0.1.12")
	_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{a, b})
	require.NoError(t, err)
	var device int64
	require.NoError(t, db.QueryRow(`INSERT INTO device (org_id, device_identifier, discovered_device_id, mac_address)
		SELECT org_id, device_identifier, id, '' FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 RETURNING id`, org, a.DeviceIdentifier).Scan(&device))
	svc.WithDiscoveryConfig(func(int64) *gatewaypb.DiscoveryConfig {
		return &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}}
	})
	require.True(t, fleeterror.IsFailedPreconditionError(svc.PairDevice(t.Context(), node, device, org, nil)))
}

func TestPairOnNodeRejectsExistingBindingOutsideDiscoveryRanges(t *testing.T) {
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "partition-repair")
	report := fleetnodepairing.DiscoveredDeviceReport{DeviceIdentifier: "mac:repair", IPAddress: "10.0.0.11", Port: "80", URLScheme: "http", DriverName: "virtual"}
	_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{report})
	require.NoError(t, err)
	result := &gatewaypb.FleetNodePairResult{DeviceIdentifier: report.DeviceIdentifier, Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED}
	_, err = svc.PersistFleetNodePairResult(t.Context(), pairPersistMeta(t, db, node, org, nil, report.DeviceIdentifier), result)
	require.NoError(t, err)
	require.NoError(t, svc.PairDevice(t.Context(), node, deviceIDByIdentifier(t, db, org, report.DeviceIdentifier), org, nil))
	registry := control.NewRegistry()
	stream := registry.Register(node)
	defer stream.Unregister()
	stream.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.1.0/24"}})
	svc.WithProvisioning(sqlstores.NewSQLDeviceStore(db), sqlstores.NewSQLDiscoveredDeviceStore(db), registry)
	targets, err := svc.ResolvePairTargets(t.Context(), node, org, []string{report.DeviceIdentifier}, false, &pairingpb.Credentials{})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	done := make(chan error, 1)
	go func() {
		done <- svc.PairOnNode(t.Context(), node, targets, nil, org, nil, func([]*gatewaypb.FleetNodePairResult) error { return nil })
	}()
	select {
	case err := <-done:
		require.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
	case <-stream.Outgoing:
		t.Fatal("target outside the configured ranges was dispatched")
	case <-time.After(5 * time.Second):
		t.Fatal("pair command did not return")
	}
}
