package sqlstores_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	nodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func TestDirectUnpairRebuildsControllerRoutesAfterLastOwner(t *testing.T) {
	db, discoveryStore, org := ct1HAStore(t)
	saveCT1HA(t, discoveryStore, "scan-a", ct1HADiscovery(org, "a", "10.0.0.11"))
	saved := saveCT1HA(t, discoveryStore, "scan-b", ct1HADiscovery(org, "b", "10.0.0.12"))
	pairCT1HA(t, db, saved)
	q := sqlc.New(db)
	discoveredID, err := discoveryStore.GetDatabaseID(t.Context(), saved.GetDeviceOrgIdentifier())
	require.NoError(t, err)
	sharedID, err := q.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID: org, DiscoveredDeviceID: discoveredID, DeviceIdentifier: "other-owner",
	})
	require.NoError(t, err)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: sharedID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	count, err := deviceStore.SoftDeleteDevices(t.Context(), []string{saved.DeviceIdentifier}, org)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	retained, err := discoveryStore.GetDevice(t.Context(), saved.GetDeviceOrgIdentifier())
	require.NoError(t, err)
	require.Len(t, retained.Endpoints, 2, "the remaining paired owner keeps both routes")

	count, err = deviceStore.SoftDeleteDevices(t.Context(), []string{"other-owner"}, org)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	requireEmptyControllerDiscovery(t, db, discoveredID)

	saveCT1HA(t, discoveryStore, "scan-a", ct1HADiscovery(org, "a", "10.0.0.11"))
	rebuilt := saveCT1HA(t, discoveryStore, "scan-c", ct1HADiscovery(org, "c", "10.0.0.13"))
	require.Len(t, rebuilt.Endpoints, 2)
	require.Equal(t, "a", rebuilt.Endpoints[0].Id)
	require.Equal(t, "c", rebuilt.Endpoints[1].Id, "the replaced B controller must not survive the new scan")
}

func TestNodeUnpairRebuildsControllerRoutesAfterLastPairing(t *testing.T) {
	db, discoveryStore, org := ct1HAStore(t)
	var node int64
	require.NoError(t, db.QueryRowContext(t.Context(), `INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
        VALUES ($1,'reset-node',decode(repeat('11',32),'hex'),decode(repeat('22',32),'hex'),'CONFIRMED') RETURNING id`, org).Scan(&node))
	nodeStore := sqlstores.NewSQLFleetNodePairingStore(db)
	report := func(id, host string) nodepairing.DiscoveredDeviceReport {
		t.Helper()
		d := ct1HADiscovery(org, id, host)
		identifier, err := deviceendpoints.NodeIdentifier(node, d.DriverName, d.Endpoints)
		require.NoError(t, err)
		saved, stored, err := nodeStore.UpsertDiscoveredDeviceFromFleetNode(t.Context(), org, node, nodepairing.DiscoveredDeviceReport{
			DeviceIdentifier: identifier, IPAddress: host, Port: d.Port, URLScheme: d.UrlScheme,
			RPCPort: d.RpcPort, RPCTLS: d.RpcTls, DriverName: d.DriverName,
			Model: d.Model, Manufacturer: d.Manufacturer, DeviceClass: d.DeviceClass, Endpoints: d.Endpoints,
		})
		require.NoError(t, err)
		require.True(t, stored)
		return saved
	}
	report("a", "10.0.0.11")
	saved := report("b", "10.0.0.12")
	d, err := discoveryStore.GetDevice(t.Context(), discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: saved.DeviceIdentifier})
	require.NoError(t, err)
	pairCT1HA(t, db, d)
	q := sqlc.New(db)
	device, err := q.GetDeviceByDeviceIdentifier(t.Context(), sqlc.GetDeviceByDeviceIdentifierParams{OrgID: org, DeviceIdentifier: d.DeviceIdentifier})
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE discovered_device SET serial_number='b', mac_address='AA:BB:CC:DD:EE:0B' WHERE id=$1`, device.DiscoveredDeviceID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `UPDATE device SET serial_number='b', mac_address='AA:BB:CC:DD:EE:0B' WHERE id=$1`, device.ID)
	require.NoError(t, err)
	_, err = nodeStore.PairDeviceToFleetNode(t.Context(), node, device.ID, org, nil)
	require.NoError(t, err)
	count, err := nodeStore.UnpairDevice(t.Context(), device.ID, org)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	retained, err := discoveryStore.GetDevice(t.Context(), d.GetDeviceOrgIdentifier())
	require.NoError(t, err)
	require.Len(t, retained.Endpoints, 2, "a remaining cloud pairing keeps both routes")
	require.Equal(t, "b", retained.SerialNumber)

	_, err = nodeStore.PairDeviceToFleetNode(t.Context(), node, device.ID, org, nil)
	require.NoError(t, err)
	setCT1HAPairing(t, db, d, sqlc.PairingStatusEnumUNPAIRED)
	count, err = nodeStore.UnpairDevice(t.Context(), device.ID, org)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	requireEmptyControllerDiscovery(t, db, device.DiscoveredDeviceID)
	device, err = q.GetDeviceByID(t.Context(), sqlc.GetDeviceByIDParams{ID: device.ID, OrgID: org})
	require.NoError(t, err)
	require.False(t, device.SerialNumber.Valid, "cleared serials must not conflict in the unique index")
	require.Empty(t, device.MacAddress)

	report("a", "10.0.0.11")
	rebuilt := report("c", "10.0.0.13")
	require.Len(t, rebuilt.Endpoints, 2)
	require.Equal(t, "a", rebuilt.Endpoints[0].Id)
	require.Equal(t, "c", rebuilt.Endpoints[1].Id)
}

func requireEmptyControllerDiscovery(t *testing.T, db *sql.DB, discoveredID int64) {
	t.Helper()
	var endpoints, key, serial, mac string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT endpoints::text, endpoint_key, serial_number, mac_address FROM discovered_device WHERE id=$1`, discoveredID).Scan(&endpoints, &key, &serial, &mac))
	require.Equal(t, "{}", endpoints)
	require.Empty(t, key)
	require.Empty(t, serial)
	require.Empty(t, mac)
}

func TestConcurrentNodeUnpairClearsLastOwnerRoutes(t *testing.T) {
	db, discoveryStore, org := ct1HAStore(t)
	saved := saveCT1HA(t, discoveryStore, "scan-a", ct1HADiscovery(org, "a", "10.0.0.11"))
	discoveredID, err := discoveryStore.GetDatabaseID(t.Context(), saved.GetDeviceOrgIdentifier())
	require.NoError(t, err)
	var node int64
	require.NoError(t, db.QueryRowContext(t.Context(), `INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
        VALUES ($1,'concurrent-unpair',decode(repeat('11',32),'hex'),decode(repeat('22',32),'hex'),'CONFIRMED') RETURNING id`, org).Scan(&node))
	q := sqlc.New(db)
	store := sqlstores.NewSQLFleetNodePairingStore(db)
	ids := make([]int64, 0, 2)
	for _, identifier := range []string{"owner-a", "owner-b"} {
		id, err := q.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
			OrgID: org, DiscoveredDeviceID: discoveredID, DeviceIdentifier: identifier,
			SerialNumber: sql.NullString{String: identifier, Valid: true},
		})
		require.NoError(t, err)
		_, err = store.PairDeviceToFleetNode(t.Context(), node, id, org, nil)
		require.NoError(t, err)
		ids = append(ids, id)
	}
	removed, release := make(chan struct{}), make(chan struct{})
	unlock := sync.OnceFunc(func() { close(release) })
	defer unlock()
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() {
		firstDone <- sqlstores.NewSQLTransactor(db).RunInTx(t.Context(), func(ctx context.Context) error {
			if _, err := store.UnpairDevice(ctx, ids[0], org); err != nil {
				return err
			}
			close(removed)
			<-release
			return nil
		})
	}()
	select {
	case <-removed:
	case err := <-firstDone:
		t.Fatalf("first unpair failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("first unpair did not finish its write")
	}
	go func() {
		_, err := store.UnpairDevice(t.Context(), ids[1], org)
		secondDone <- err
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := db.QueryRowContext(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockEndpointDiscoveryDevices%')`).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	unlock()
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	requireEmptyControllerDiscovery(t, db, discoveredID)
}
