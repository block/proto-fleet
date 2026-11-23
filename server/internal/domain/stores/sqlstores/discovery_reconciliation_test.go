package sqlstores_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	nodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func TestPlainRigDiscoveryDoesNotTakeEndpointOrganizationLock(t *testing.T) {
	db, store, org := ct1HAStore(t)
	d := &discovery.DiscoveredDevice{OrgID: org, Device: pb.Device{DeviceIdentifier: "plain-rig", DriverName: "virtual", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG, IpAddress: "10.0.0.11", Port: "80", UrlScheme: "http"}}
	_, err := store.SaveDirect(t.Context(), d.GetDeviceOrgIdentifier(), d)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	locked, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	defer release()
	done := make(chan error, 1)
	tx := sqlstores.NewSQLTransactor(db)
	go func() {
		done <- tx.RunInTxNoRetry(ctx, func(ctx context.Context) error {
			if err := tx.GetQueries(ctx).LockEndpointDiscovery(ctx, fmt.Sprintf("endpoint-discovery:%d", org)); err != nil {
				return err
			}
			close(locked)
			<-resume
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-done:
		t.Fatalf("lock holder exited early: %v", err)
	case <-ctx.Done():
		t.Fatal("lock holder did not start")
	}
	d.IpAddress = "10.0.0.21"
	result := make(chan error, 1)
	go func() { _, err := store.SaveDirect(ctx, d.GetDeviceOrgIdentifier(), d); result <- err }()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("plain rig discovery waited for an unrelated endpoint lock")
	}
	release()
	require.NoError(t, <-done)
}

func TestEndpointDiscoveryRejectsClusterReplacementAndDowngrade(t *testing.T) {
	for _, paired := range []bool{false, true} {
		t.Run(fmt.Sprintf("paired=%t", paired), func(t *testing.T) {
			db, store, org := ct1HAStore(t)
			before := saveCT1HA(t, store, "container", ct1HADiscovery(org, "a", "10.0.0.11"))
			if paired {
				pairCT1HA(t, db, before)
			}
			incoming := ct1HADiscovery(org, "a", "10.0.0.11")
			incoming.Endpoints[0].DeviceKey = "another-cluster"
			_, err := store.SaveDirect(t.Context(), discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: "new-scan"}, incoming)
			require.True(t, fleeterror.IsFailedPreconditionError(err))
			incoming.Endpoints = nil
			_, err = store.SaveDirect(t.Context(), discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: "new-scan"}, incoming)
			require.True(t, fleeterror.IsFailedPreconditionError(err))
			after, err := store.GetDevice(t.Context(), before.GetDeviceOrgIdentifier())
			require.NoError(t, err)
			require.Equal(t, before.Endpoints, after.Endpoints)
			count, err := store.CountActiveUnpairedDevices(t.Context(), org)
			require.NoError(t, err)
			if paired {
				require.Zero(t, count)
			} else {
				require.EqualValues(t, 1, count)
			}
		})
	}
}

func TestNodeEndpointDiscoveryRejectsClusterReplacement(t *testing.T) {
	db, _, org := ct1HAStore(t)
	var node int64
	require.NoError(t, db.QueryRowContext(t.Context(), `INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
        VALUES ($1,'node',decode(repeat('11',32),'hex'),decode(repeat('22',32),'hex'),'CONFIRMED') RETURNING id`, org).Scan(&node))
	store := sqlstores.NewSQLFleetNodePairingStore(db)
	device := ct1HADiscovery(org, "a", "10.0.0.11")
	report := nodepairing.DiscoveredDeviceReport{DeviceIdentifier: "container", IPAddress: device.IpAddress, Port: device.Port,
		URLScheme: device.UrlScheme, RPCPort: device.RpcPort, DriverName: device.DriverName, DeviceClass: device.DeviceClass, Endpoints: device.Endpoints}
	before, stored, err := store.UpsertDiscoveredDeviceFromFleetNode(t.Context(), org, node, report)
	require.NoError(t, err)
	require.True(t, stored)
	report.Endpoints[0].DeviceKey = "another-cluster"
	_, stored, err = store.UpsertDiscoveredDeviceFromFleetNode(t.Context(), org, node, report)
	require.NoError(t, err)
	require.False(t, stored)
	row, err := sqlstores.NewSQLDiscoveredDeviceStore(db).GetDevice(t.Context(), discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: before.DeviceIdentifier})
	require.NoError(t, err)
	require.Equal(t, "opaque-pair", row.Endpoints[0].DeviceKey)
}

func TestNodeRediscoveryKeepsFirmwareVersionOnlyForTrustedIdentity(t *testing.T) {
	rig := nodepairing.DiscoveredDeviceReport{IPAddress: "10.0.0.11", Port: "8080", URLScheme: "http",
		DriverName: "proto", Model: "Rig", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_RIG}
	container := nodepairing.DiscoveredDeviceReport{IPAddress: "10.0.0.11", Port: "8080", URLScheme: "http",
		DriverName: "proto", Model: "CT1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER}
	controllerGroup := container
	controllerGroup.Endpoints = []*commonpb.DeviceEndpoint{{Id: "a", DeviceKey: "opaque-pair", Host: "10.0.0.11", Port: 8080, UrlScheme: "http"}}
	const mac, serial, ct1, auto = "mac:AA:BB:CC:DD:EE:01", "serial:SN-1", "container:0123456789abcdef", "auto:0123456789abcdef"
	for _, tc := range []struct {
		name                   string
		identifier             string
		first, second          nodepairing.DiscoveredDeviceReport
		pairing                string
		stored, reported, want string
	}{
		{name: "no version, MAC identity of a node-owned device", identifier: mac, first: rig, second: rig, pairing: "PAIRED", stored: "1.2.3", want: "1.2.3"},
		{name: "no version, serial identity of an unpaired device", identifier: serial, first: rig, second: rig, stored: "1.2.3", want: "1.2.3"},
		{name: "no version, container of a node-owned device", identifier: ct1, first: container, second: container, pairing: "PAIRED", stored: "1.2.3", want: "1.2.3"},
		{name: "no version, container without a device", identifier: ct1, first: container, second: container, stored: "1.2.3"},
		{name: "no version, address identity of a node-owned device", identifier: auto, first: rig, second: rig, pairing: "PAIRED", stored: "1.2.3"},
		{name: "no version, controller group at the address of a MAC identity", identifier: mac, first: rig, second: controllerGroup, stored: "1.2.3"},
		{name: "old version, MAC identity of a node-owned device", identifier: mac, first: rig, second: rig, pairing: "PAIRED", stored: "2.0.0", reported: "1.0.0", want: "2.0.0"},
		{name: "old version, serial identity of a default-password device", identifier: serial, first: rig, second: rig, pairing: "DEFAULT_PASSWORD", stored: "2.0.0", reported: "1.0.0", want: "2.0.0"},
		{name: "old version, container of a node-owned device", identifier: ct1, first: container, second: container, pairing: "PAIRED", stored: "2.0.0", reported: "1.0.0", want: "2.0.0"},
		{name: "version, MAC identity of a node-owned device without a stored version", identifier: mac, first: rig, second: rig, pairing: "PAIRED", reported: "1.0.0", want: "1.0.0"},
		{name: "version, MAC identity without a device", identifier: mac, first: rig, second: rig, stored: "2.0.0", reported: "1.0.0", want: "1.0.0"},
		{name: "version, MAC identity of an unpaired device", identifier: mac, first: rig, second: rig, pairing: "UNPAIRED", stored: "2.0.0", reported: "1.0.0", want: "1.0.0"},
		{name: "version, address identity of a node-owned device", identifier: auto, first: rig, second: rig, pairing: "PAIRED", stored: "2.0.0", reported: "1.0.0", want: "1.0.0"},
		{name: "version, controller group at the address of a MAC identity", identifier: mac, first: rig, second: controllerGroup, stored: "2.0.0", reported: "1.0.0", want: "1.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, discoveryStore, org := ct1HAStore(t)
			var node int64
			require.NoError(t, db.QueryRowContext(t.Context(), `INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
        VALUES ($1,'node',decode(repeat('11',32),'hex'),decode(repeat('22',32),'hex'),'CONFIRMED') RETURNING id`, org).Scan(&node))
			store := sqlstores.NewSQLFleetNodePairingStore(db)
			first := tc.first
			first.DeviceIdentifier, first.FirmwareVersion = tc.identifier, tc.stored
			_, stored, err := store.UpsertDiscoveredDeviceFromFleetNode(t.Context(), org, node, first)
			require.NoError(t, err)
			require.True(t, stored)
			doi := discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: tc.identifier}
			if tc.pairing != "" {
				d, err := discoveryStore.GetDevice(t.Context(), doi)
				require.NoError(t, err)
				deviceStore := sqlstores.NewSQLDeviceStore(db)
				require.NoError(t, deviceStore.InsertDevice(t.Context(), &d.Device, org, tc.identifier))
				require.NoError(t, deviceStore.UpsertDevicePairing(t.Context(), &d.Device, org, tc.pairing))
				if tc.pairing != "UNPAIRED" {
					device, err := sqlc.New(db).GetDeviceByDeviceIdentifier(t.Context(), sqlc.GetDeviceByDeviceIdentifierParams{OrgID: org, DeviceIdentifier: tc.identifier})
					require.NoError(t, err)
					_, err = store.PairDeviceToFleetNode(t.Context(), node, device.ID, org, nil)
					require.NoError(t, err)
				}
			}

			second := tc.second
			second.DeviceIdentifier, second.FirmwareVersion = tc.identifier, tc.reported
			if len(second.Endpoints) > 0 {
				second.DeviceIdentifier, err = deviceendpoints.NodeIdentifier(node, second.DriverName, second.Endpoints)
				require.NoError(t, err)
			}
			saved, stored, err := store.UpsertDiscoveredDeviceFromFleetNode(t.Context(), org, node, second)
			require.NoError(t, err)
			require.True(t, stored)
			require.Equal(t, tc.identifier, saved.DeviceIdentifier)
			row, err := discoveryStore.GetDevice(t.Context(), doi)
			require.NoError(t, err)
			require.Equal(t, tc.want, row.FirmwareVersion)
		})
	}
}

func TestNodeEndpointDiscoveryKeepsInactiveLiveNodeInventorySeparate(t *testing.T) {
	for _, group := range []string{"opaque-pair", "another-cluster"} {
		t.Run(group, func(t *testing.T) {
			db, _, org := ct1HAStore(t)
			var nodes [2]int64
			for i := range nodes {
				require.NoError(t, db.QueryRowContext(t.Context(), `INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
        VALUES ($1,$2,decode(repeat($3,32),'hex'),decode(repeat('22',32),'hex'),'CONFIRMED') RETURNING id`,
					org, fmt.Sprintf("node-%d", i), fmt.Sprintf("%02x", i+1)).Scan(&nodes[i]))
			}
			store := sqlstores.NewSQLFleetNodePairingStore(db)
			device := ct1HADiscovery(org, "a", "10.0.0.11")
			report := nodepairing.DiscoveredDeviceReport{DeviceIdentifier: "container", IPAddress: device.IpAddress, Port: device.Port,
				URLScheme: device.UrlScheme, RPCPort: device.RpcPort, DriverName: device.DriverName, DeviceClass: device.DeviceClass, Endpoints: device.Endpoints}
			first, stored, err := store.UpsertDiscoveredDeviceFromFleetNode(t.Context(), org, nodes[0], report)
			require.NoError(t, err)
			require.True(t, stored)
			_, err = db.ExecContext(t.Context(), `UPDATE discovered_device SET is_active = FALSE WHERE org_id = $1 AND device_identifier = $2`, org, first.DeviceIdentifier)
			require.NoError(t, err)
			q := sqlc.New(db)
			firstParams := sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{OrgID: org, DeviceIdentifier: first.DeviceIdentifier}
			before, err := q.GetDiscoveredDeviceByDeviceIdentifier(t.Context(), firstParams)
			require.NoError(t, err)
			require.False(t, before.IsActive)

			// Both nodes remain live, so even an identical group at this socket
			// belongs to the reporting node's own inventory namespace.
			report.Endpoints[0].DeviceKey = group
			second, stored, err := store.UpsertDiscoveredDeviceFromFleetNode(t.Context(), org, nodes[1], report)
			require.NoError(t, err)
			require.True(t, stored)
			expectedID, err := deviceendpoints.NodeIdentifier(nodes[1], report.DriverName, report.Endpoints)
			require.NoError(t, err)
			require.Equal(t, expectedID, second.DeviceIdentifier)
			require.NotEqual(t, first.DeviceIdentifier, second.DeviceIdentifier)
			incoming, err := q.GetDiscoveredDeviceByDeviceIdentifier(t.Context(), sqlc.GetDiscoveredDeviceByDeviceIdentifierParams{OrgID: org, DeviceIdentifier: second.DeviceIdentifier})
			require.NoError(t, err)
			require.True(t, incoming.IsActive)
			require.Equal(t, nodes[1], incoming.DiscoveredByFleetNodeID.Int64)
			require.Equal(t, device.IpAddress, incoming.IpAddress)
			require.Equal(t, device.RpcPort, incoming.RpcPort)
			endpoints, err := deviceendpoints.Decode(incoming.Endpoints)
			require.NoError(t, err)
			require.Len(t, endpoints, 1)
			require.Equal(t, group, endpoints[0].DeviceKey)
			after, err := q.GetDiscoveredDeviceByDeviceIdentifier(t.Context(), firstParams)
			require.NoError(t, err)
			require.Equal(t, before, after, "the first node's inactive inventory must remain intact")
		})
	}
}
