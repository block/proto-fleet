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

func TestDirectPartnerReplacementKeepsInventoryAfterExplicitUnpair(t *testing.T) {
	for _, bFirst := range []bool{false, true} {
		name := "a first"
		if bFirst {
			name = "b first"
		}
		t.Run(name, func(t *testing.T) { testDirectPartnerReplacement(t, bFirst) })
	}
}

func testDirectPartnerReplacement(t *testing.T, bFirst bool) {
	t.Helper()
	db, store, org := ct1HAStore(t)
	first, second := ct1HADiscovery(org, "a", "10.0.0.11"), ct1HADiscovery(org, "b", "10.0.0.12")
	if bFirst {
		first, second = second, first
	}
	saveCT1HA(t, store, "container", first)
	before := saveCT1HA(t, store, "second-scan", second)
	pairCT1HA(t, db, before)
	fallback := ct1HADiscovery(org, "b", "10.0.0.12")
	fallback.Endpoints = nil
	_, err := store.SaveDirect(t.Context(), discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: "keyless-peer-scan"}, fallback)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "a keyless peer scan must not create a duplicate")
	newGroup := ct1HADiscovery(org, "a", "10.0.0.11")
	newGroup.Endpoints[0].DeviceKey, newGroup.Endpoints[0].MemberIds = "replacement-pair", []string{"a", "c"}
	_, err = store.SaveDirect(t.Context(), discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: "new-scan"}, newGroup)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND deleted_at IS NULL`, org).Scan(&count))
	require.Equal(t, 1, count, "a paired group change must not create a duplicate")
	setCT1HAPairing(t, db, before, sqlc.PairingStatusEnumUNPAIRED)
	_, err = store.SaveDirect(t.Context(), discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: "keyless-peer-scan"}, fallback)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	changed := saveCT1HA(t, store, "new-scan", newGroup)
	require.Equal(t, before.DeviceIdentifier, changed.DeviceIdentifier)
	require.Len(t, changed.Endpoints, 1)
	require.Equal(t, "a", changed.Endpoints[0].Id)
	require.Equal(t, "replacement-pair", changed.Endpoints[0].DeviceKey)
	require.Equal(t, "a", changed.SerialNumber, "a removed peer's serial must not survive replacement")
	peer := ct1HADiscovery(org, "c", "10.0.0.13")
	peer.Endpoints[0].DeviceKey, peer.Endpoints[0].MemberIds = "replacement-pair", []string{"a", "c"}
	changed = saveCT1HA(t, store, "scan-c", peer)
	require.Len(t, changed.Endpoints, 2)
	require.Equal(t, "c", changed.Endpoints[1].Id)
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND deleted_at IS NULL`, org).Scan(&count))
	require.Equal(t, 1, count)
	// Key replacement needs the saved ID and raw transport, even while unpaired.
	newGroup.Endpoints[0].DeviceKey, newGroup.Endpoints[0].RpcTls = "unproven-pair", true
	newGroup.RpcTls = true
	_, err = store.SaveDirect(t.Context(), before.GetDeviceOrgIdentifier(), newGroup)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestNodePartnerReplacementKeepsInventoryAfterExplicitUnpair(t *testing.T) {
	db, discovered, org := ct1HAStore(t)
	ctx := t.Context()
	var node int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status)
        VALUES ($1,'replacement-node',decode(repeat('11',32),'hex'),decode(repeat('22',32),'hex'),'CONFIRMED') RETURNING id`, org).Scan(&node))
	store := sqlstores.NewSQLFleetNodePairingStore(db)
	report := func(id, host, key string, members []string) nodepairing.DiscoveredDeviceReport {
		d := ct1HADiscovery(org, id, host)
		d.Endpoints[0].DeviceKey, d.Endpoints[0].MemberIds = key, members
		identifier, err := deviceendpoints.NodeIdentifier(node, d.DriverName, d.Endpoints)
		require.NoError(t, err)
		return nodepairing.DiscoveredDeviceReport{DeviceIdentifier: identifier, IPAddress: host, Port: d.Port, URLScheme: d.UrlScheme, RPCPort: d.RpcPort, RPCTLS: d.RpcTls,
			DriverName: d.DriverName, Model: d.Model, Manufacturer: d.Manufacturer, DeviceClass: d.DeviceClass, Endpoints: d.Endpoints}
	}
	oldMembers := []string{"a", "b"}
	before, stored, err := store.UpsertDiscoveredDeviceFromFleetNode(ctx, org, node, report("a", "10.0.0.11", "old-pair", oldMembers))
	require.NoError(t, err)
	require.True(t, stored)
	_, stored, err = store.UpsertDiscoveredDeviceFromFleetNode(ctx, org, node, report("b", "10.0.0.12", "old-pair", oldMembers))
	require.NoError(t, err)
	require.True(t, stored)
	_, err = db.ExecContext(ctx, `UPDATE discovered_device SET serial_number='b' WHERE org_id=$1 AND device_identifier=$2`, org, before.DeviceIdentifier)
	require.NoError(t, err)
	d, err := discovered.GetDevice(ctx, discovery.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: before.DeviceIdentifier})
	require.NoError(t, err)
	pairCT1HA(t, db, d)
	device, err := sqlc.New(db).GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{OrgID: org, DeviceIdentifier: before.DeviceIdentifier})
	require.NoError(t, err)
	_, err = store.PairDeviceToFleetNode(ctx, node, device.ID, org, nil)
	require.NoError(t, err)
	fallback := report("b", "10.0.0.12", "old-pair", oldMembers)
	fallback.DeviceIdentifier, fallback.Endpoints = "keyless-peer-scan", nil
	_, stored, err = store.UpsertDiscoveredDeviceFromFleetNode(ctx, org, node, fallback)
	require.NoError(t, err)
	require.False(t, stored, "a keyless peer scan must not create a duplicate")
	newMembers := []string{"a", "c"}
	changed := report("a", "10.0.0.11", "new-pair", newMembers)
	_, stored, err = store.UpsertDiscoveredDeviceFromFleetNode(ctx, org, node, changed)
	require.NoError(t, err)
	require.False(t, stored)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND deleted_at IS NULL`, org).Scan(&count))
	require.Equal(t, 1, count)
	_, err = store.UnpairDevice(ctx, device.ID, org)
	require.NoError(t, err)
	setCT1HAPairing(t, db, d, sqlc.PairingStatusEnumUNPAIRED)
	_, stored, err = store.UpsertDiscoveredDeviceFromFleetNode(ctx, org, node, fallback)
	require.NoError(t, err)
	require.False(t, stored)
	saved, stored, err := store.UpsertDiscoveredDeviceFromFleetNode(ctx, org, node, changed)
	require.NoError(t, err)
	require.True(t, stored)
	require.Equal(t, before.DeviceIdentifier, saved.DeviceIdentifier)
	require.Len(t, saved.Endpoints, 1)
	require.Equal(t, "a", saved.Endpoints[0].Id)
	updated, err := discovered.GetDevice(ctx, d.GetDeviceOrgIdentifier())
	require.NoError(t, err)
	require.Empty(t, updated.SerialNumber, "node reports cannot retain a removed peer's serial")
	saved, stored, err = store.UpsertDiscoveredDeviceFromFleetNode(ctx, org, node, report("c", "10.0.0.13", "new-pair", newMembers))
	require.NoError(t, err)
	require.True(t, stored)
	require.Equal(t, before.DeviceIdentifier, saved.DeviceIdentifier)
	require.Len(t, saved.Endpoints, 2)
	require.Equal(t, "c", saved.Endpoints[1].Id)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND deleted_at IS NULL`, org).Scan(&count))
	require.Equal(t, 1, count)
}

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

func TestRigFastPathCannotReclassifyPairedLegacyContainer(t *testing.T) {
	db, store, org := ct1HAStore(t)
	d := ct1HADiscovery(org, "a", "10.0.0.11")
	d.Endpoints, d.RpcPort = nil, 0
	before := saveCT1HA(t, store, "container", d)
	pairCT1HA(t, db, before)
	d.DeviceClass = commonpb.DeviceClass_DEVICE_CLASS_RIG
	_, err := store.SaveDirect(t.Context(), before.GetDeviceOrgIdentifier(), d)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	after, err := store.GetDevice(t.Context(), before.GetDeviceOrgIdentifier())
	require.NoError(t, err)
	require.Equal(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, after.DeviceClass)
	d.IpAddress = "10.0.0.99"
	_, err = store.SaveDirect(t.Context(), before.GetDeviceOrgIdentifier(), d)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
}

func TestDiscoveryCountValidatesEndpointPagesWithoutLoadingPlainRigs(t *testing.T) {
	db, store, org := ct1HAStore(t)
	valid, err := deviceendpoints.Encode(ct1HADiscovery(org, "a", "10.0.0.11").Endpoints)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO discovered_device (org_id, device_identifier, driver_name, ip_address, port, url_scheme, is_active, endpoints)
        SELECT $1, 'count-' || n, 'virtual', '10.0.0.11', '80', 'http', TRUE,
            CASE WHEN n%3=0 THEN '{"invalid":{}}'::jsonb WHEN n%3=1 THEN '{}'::jsonb ELSE $2::jsonb END
        FROM generate_series(1,1002) n`, org, valid)
	require.NoError(t, err)
	count, err := store.CountActiveUnpairedDevices(t.Context(), org)
	require.NoError(t, err)
	require.EqualValues(t, 668, count)
}
