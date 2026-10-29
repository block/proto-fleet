package pairing_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
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
	for _, ranges := range [][]string{nil, {}, {"10.0.1.0/24"}} {
		if ranges != nil {
			config = &gatewaypb.DiscoveryConfig{Ranges: ranges}
		}
		err := svc.PairDevice(t.Context(), node, device, org, nil)
		require.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
	}
	config = &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}}
	require.NoError(t, svc.PairDevice(t.Context(), node, device, org, nil))
	config = &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.1.0/24"}}
	require.NoError(t, svc.PairDevice(t.Context(), node, device, org, nil), "existing binding remains repairable after repartitioning")
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

func TestRetiredDiscoveryCanMoveBetweenActiveFleetNodes(t *testing.T) {
	for _, kind := range []string{"stable rig", "synthetic rig", "endpoint group"} {
		t.Run(kind, func(t *testing.T) {
			db, org, svc, enrollment := setupPairingTest(t)
			original := createFleetNode(t, enrollment, org, "partition-original")
			replacement := createFleetNode(t, enrollment, org, "partition-replacement")
			report := fleetnodepairing.DiscoveredDeviceReport{DeviceIdentifier: "mac:partition-rig", IPAddress: "10.0.0.11", Port: "80", URLScheme: "http", DriverName: "virtual", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG}
			if kind == "synthetic rig" {
				report.DeviceIdentifier = fmt.Sprintf("auto:%d:virtual:10.0.0.11:80", original)
			} else if kind == "endpoint group" {
				report = endpointGroupReport(t, original, "controller-a", "10.0.0.11")
			}
			accepted, rejected, err := svc.UpsertDiscoveredDevices(t.Context(), original, org, []fleetnodepairing.DiscoveredDeviceReport{report})
			require.NoError(t, err)
			require.Zero(t, rejected)
			require.Len(t, accepted, 1)
			identifier := accepted[0].DeviceIdentifier
			require.NoError(t, svc.RetireOutOfScopeDiscoveries(t.Context(), original, org, &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.1.0/24"}}))
			var active bool
			require.NoError(t, db.QueryRow(`SELECT is_active FROM discovered_device WHERE org_id=$1 AND device_identifier=$2`, org, identifier).Scan(&active))
			require.False(t, active)
			if kind == "synthetic rig" {
				report.DeviceIdentifier = fmt.Sprintf("auto:%d:virtual:10.0.0.11:80", replacement)
			} else if kind == "endpoint group" {
				report = endpointGroupReport(t, replacement, "controller-a", "10.0.0.11")
			}
			accepted, rejected, err = svc.UpsertDiscoveredDevices(t.Context(), replacement, org, []fleetnodepairing.DiscoveredDeviceReport{report})
			require.NoError(t, err)
			require.Zero(t, rejected)
			require.Len(t, accepted, 1)
			require.Equal(t, identifier, accepted[0].DeviceIdentifier)
			var owner, count int64
			require.NoError(t, db.QueryRow(`SELECT discovered_by_fleet_node_id, is_active FROM discovered_device WHERE org_id=$1 AND device_identifier=$2`, org, identifier).Scan(&owner, &active))
			require.Equal(t, replacement, owner)
			require.True(t, active)
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM discovered_device WHERE org_id=$1 AND deleted_at IS NULL`, org).Scan(&count))
			require.EqualValues(t, 1, count)
		})
	}
}

func TestRetirementPreservesLiveOwnersAcrossDiscoveryLinks(t *testing.T) {
	for _, kind := range []string{"paired linked", "paired identifier", "bound identifier"} {
		t.Run(kind, func(t *testing.T) {
			db, org, svc, enrollment := setupPairingTest(t)
			node := createFleetNode(t, enrollment, org, "partition-owner")
			replacement := createFleetNode(t, enrollment, org, "partition-other")
			report := fleetnodepairing.DiscoveredDeviceReport{DeviceIdentifier: "mac:protected", IPAddress: "10.0.0.11", Port: "80", URLScheme: "http", DriverName: "virtual"}
			_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{report})
			require.NoError(t, err)
			device := insertDevice(t, db, org)
			if kind == "paired linked" {
				_, err = db.Exec(`UPDATE device SET discovered_device_id=(SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2) WHERE id=$3`, org, report.DeviceIdentifier, device)
			} else {
				_, err = db.Exec(`UPDATE device SET device_identifier=$1 WHERE id=$2`, report.DeviceIdentifier, device)
			}
			require.NoError(t, err)
			if kind == "bound identifier" {
				_, err = db.Exec(`INSERT INTO fleet_node_device (org_id,fleet_node_id,device_id) VALUES ($1,$2,$3)`, org, node, device)
			} else {
				_, err = db.Exec(`INSERT INTO device_pairing (device_id,pairing_status) VALUES ($1,'PAIRED')`, device)
			}
			require.NoError(t, err)
			require.NoError(t, svc.RetireOutOfScopeDiscoveries(t.Context(), node, org, &gatewaypb.DiscoveryConfig{}))
			var active bool
			require.NoError(t, db.QueryRow(`SELECT is_active FROM discovered_device WHERE org_id=$1 AND device_identifier=$2`, org, report.DeviceIdentifier).Scan(&active))
			require.True(t, active)
			// Even a pre-existing inactive row must not transfer paired inventory.
			_, err = db.Exec(`UPDATE discovered_device SET is_active=FALSE WHERE org_id=$1 AND device_identifier=$2`, org, report.DeviceIdentifier)
			require.NoError(t, err)
			_, rejected, err := svc.UpsertDiscoveredDevices(t.Context(), replacement, org, []fleetnodepairing.DiscoveredDeviceReport{report})
			require.NoError(t, err)
			require.EqualValues(t, 1, rejected)
		})
	}
}

func TestRetirementUsesInclusiveIntervalsAndEveryEndpoint(t *testing.T) {
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "partition-retirement")
	for _, address := range []string{"10.0.0.1", "10.0.0.9", "10.0.0.10", "10.0.1.1"} {
		report := fleetnodepairing.DiscoveredDeviceReport{DeviceIdentifier: address, IPAddress: address, Port: "80", URLScheme: "http", DriverName: "virtual"}
		_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{report})
		require.NoError(t, err)
	}
	a := endpointGroupReport(t, node, "controller-a", "10.0.0.2")
	b := endpointGroupReport(t, node, "controller-b", "10.0.1.20")
	_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{a, b})
	require.NoError(t, err)
	require.NoError(t, svc.RetireOutOfScopeDiscoveries(t.Context(), node, org, &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1-10.0.0.9", "10.0.1.1"}}))
	for identifier, expectedActive := range map[string]bool{"10.0.0.1": true, "10.0.0.9": true, "10.0.0.10": false, "10.0.1.1": true, a.DeviceIdentifier: false} {
		var active bool
		require.NoError(t, db.QueryRow(`SELECT is_active FROM discovered_device WHERE org_id=$1 AND device_identifier=$2`, org, identifier).Scan(&active))
		require.Equal(t, expectedActive, active, identifier)
	}
}

func TestRetirementPreservesBoundLegacyHostnameEndpoints(t *testing.T) {
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "partition-legacy-hostname")
	report := endpointGroupReport(t, node, "controller-a", "10.0.0.11")
	_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{report})
	require.NoError(t, err)
	var device int64
	require.NoError(t, db.QueryRow(`INSERT INTO device (org_id, device_identifier, discovered_device_id, mac_address)
		SELECT org_id, device_identifier, id, '' FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 RETURNING id`, org, report.DeviceIdentifier).Scan(&device))
	require.NoError(t, svc.PairDevice(t.Context(), node, device, org, nil))
	_, err = db.Exec(`UPDATE discovered_device SET endpoints=jsonb_set(endpoints, ARRAY[$3,'Host'], to_jsonb($4::text))
		WHERE org_id=$1 AND device_identifier=$2`, org, report.DeviceIdentifier, report.Endpoints[0].Id, "controller.lan")
	require.NoError(t, err)

	require.NoError(t, svc.RetireOutOfScopeDiscoveries(t.Context(), node, org, &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}}))
	var active bool
	require.NoError(t, db.QueryRow(`SELECT is_active FROM discovered_device WHERE org_id=$1 AND device_identifier=$2`, org, report.DeviceIdentifier).Scan(&active))
	require.True(t, active)
	require.True(t, deviceBoundToNode(t, db, org, node, report.DeviceIdentifier))
}

func TestRetirementTreatsUnpairedLegacyHostnameEndpointsAsOutsideScope(t *testing.T) {
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "partition-unpaired-hostname")
	report := endpointGroupReport(t, node, "controller-a", "10.0.0.11")
	_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{report})
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE discovered_device SET endpoints=jsonb_set(endpoints, ARRAY[$3,'Host'], to_jsonb($4::text))
		WHERE org_id=$1 AND device_identifier=$2`, org, report.DeviceIdentifier, report.Endpoints[0].Id, "controller.lan")
	require.NoError(t, err)

	require.NoError(t, svc.RetireOutOfScopeDiscoveries(t.Context(), node, org, &gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.0/24"}}))
	var active bool
	require.NoError(t, db.QueryRow(`SELECT is_active FROM discovered_device WHERE org_id=$1 AND device_identifier=$2`, org, report.DeviceIdentifier).Scan(&active))
	require.False(t, active, "a DNS endpoint has no literal IPv4 address inside the configured partition")
}

func TestPairResultMayFinishAfterDiscoveryRangesChange(t *testing.T) {
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "partition-in-flight")
	report := fleetnodepairing.DiscoveredDeviceReport{DeviceIdentifier: "mac:in-flight", IPAddress: "10.0.0.11", Port: "80", URLScheme: "http", DriverName: "virtual"}
	_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{report})
	require.NoError(t, err)
	meta := pairPersistMeta(t, db, node, org, nil, report.DeviceIdentifier)
	svc.WithDiscoveryConfig(func(int64) *gatewaypb.DiscoveryConfig { return &gatewaypb.DiscoveryConfig{} })
	require.NoError(t, svc.RetireOutOfScopeDiscoveries(t.Context(), node, org, &gatewaypb.DiscoveryConfig{}))
	status, err := svc.PersistFleetNodePairResult(t.Context(), meta, &gatewaypb.FleetNodePairResult{DeviceIdentifier: report.DeviceIdentifier, Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED})
	require.NoError(t, err)
	require.Equal(t, fleetnodepairing.StatusPaired, status)
	require.True(t, deviceBoundToNode(t, db, org, node, report.DeviceIdentifier))
	assertPairedDiscoveryVisible(t, db, org, report.DeviceIdentifier)
}

func TestPairOnNodeRepairsExistingBindingOutsideDiscoveryRanges(t *testing.T) {
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
	case cmd := <-stream.Outgoing:
		stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.CommandId, Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
	case err := <-done:
		t.Fatalf("repair rejected before dispatch: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("repair command did not dispatch")
	}
	require.NoError(t, <-done)
}

func assertPairedDiscoveryVisible(t *testing.T, database *sql.DB, org int64, identifier string) {
	t.Helper()
	var active bool
	require.NoError(t, database.QueryRow(`SELECT is_active FROM discovered_device WHERE org_id=$1 AND device_identifier=$2`, org, identifier).Scan(&active))
	require.True(t, active)
	rows, _, total, err := sqlstores.NewSQLDeviceStore(database).ListMinerStateSnapshots(t.Context(), org, "", 100, nil, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	require.Equal(t, identifier, rows[0].DeviceIdentifier)
}

func TestRetirementRechecksOwnershipAfterWaitingForPairResult(t *testing.T) {
	database, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "partition-persist-lock")
	report := fleetnodepairing.DiscoveredDeviceReport{DeviceIdentifier: "mac:persist-lock", IPAddress: "10.0.0.11", Port: "80", URLScheme: "http", DriverName: "virtual"}
	_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{report})
	require.NoError(t, err)
	meta := pairPersistMeta(t, database, node, org, nil, report.DeviceIdentifier)
	locked := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	paired := make(chan error, 1)
	go func() {
		paired <- sqlstores.NewSQLTransactor(database).RunInTx(t.Context(), func(ctx context.Context) error {
			_, err := svc.PersistFleetNodePairResult(ctx, meta, &gatewaypb.FleetNodePairResult{DeviceIdentifier: report.DeviceIdentifier, Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED})
			if err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-paired:
		t.Fatalf("pairing failed before locking: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("pairing did not lock inventory")
	}
	retired := make(chan error, 1)
	go func() {
		retired <- svc.RetireOutOfScopeDiscoveries(t.Context(), node, org, &gatewaypb.DiscoveryConfig{})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := database.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%discovered_device%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "retirement must wait on the pairing transaction")
	release <- struct{}{}
	require.NoError(t, <-paired)
	require.NoError(t, <-retired)
	assertPairedDiscoveryVisible(t, database, org, report.DeviceIdentifier)
}
