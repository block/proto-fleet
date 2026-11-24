package pairing_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func pairingSnapshot(t *testing.T, db *sql.DB, orgID int64, identifier string) *pairingpb.FleetNodePairTarget {
	t.Helper()
	d, err := sqlstores.NewSQLDiscoveredDeviceStore(db).GetDevice(t.Context(), discovery.DeviceOrgIdentifier{OrgID: orgID, DeviceIdentifier: identifier})
	if fleeterror.IsNotFoundError(err) {
		return &pairingpb.FleetNodePairTarget{DeviceIdentifier: identifier}
	}
	require.NoError(t, err)
	return &pairingpb.FleetNodePairTarget{
		DeviceIdentifier: d.DeviceIdentifier, DriverName: d.DriverName,
		IpAddress: d.IpAddress, Port: d.Port, UrlScheme: d.UrlScheme,
		RpcPort: d.RpcPort, RpcTls: d.RpcTls, Endpoints: d.Endpoints,
		DeviceClass: d.DeviceClass, Manufacturer: d.Manufacturer,
	}
}

func pairPersistMeta(t *testing.T, db *sql.DB, nodeID, orgID int64, assignedBy *int64, identifier string) control.PairPersistMeta {
	t.Helper()
	return control.PairPersistMeta{
		FleetNodeID: nodeID,
		OrgID:       orgID,
		AssignedBy:  assignedBy,
		Snapshots:   map[string]*pairingpb.FleetNodePairTarget{identifier: pairingSnapshot(t, db, orgID, identifier)},
	}
}

type pausedPairSnapshotStore struct {
	*sqlstores.SQLDiscoveredDeviceStore
	started, resume chan struct{}
	once            sync.Once
}

func (s *pausedPairSnapshotStore) LockPairingSnapshot(ctx context.Context, doi discovery.DeviceOrgIdentifier, expected *pairingpb.Device, owner *int64) error {
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.resume:
	case <-ctx.Done():
		return fmt.Errorf("wait for pairing snapshot: %w", ctx.Err())
	}
	return s.SQLDiscoveredDeviceStore.LockPairingSnapshot(ctx, doi, expected, owner)
}

func TestPairResultRejectsDiscoveryChangesAfterDispatch(t *testing.T) {
	for _, change := range []string{"partner route", "browser route"} {
		t.Run(change, func(t *testing.T) {
			db, org, svc, enrollment := setupPairingTest(t)
			node := createFleetNode(t, enrollment, org, "pair-snapshot-node")
			a, b := endpointGroupReport(t, node, "controller-a", "10.0.0.11"), endpointGroupReport(t, node, "controller-b", "10.0.0.12")
			_, _, err := svc.UpsertDiscoveredDevices(t.Context(), node, org, []fleetnodepairing.DiscoveredDeviceReport{a, b})
			require.NoError(t, err)
			targets, err := svc.ResolvePairTargets(t.Context(), node, org, []string{a.DeviceIdentifier}, false, nil)
			require.NoError(t, err)
			require.Len(t, targets, 1)
			store := &pausedPairSnapshotStore{SQLDiscoveredDeviceStore: sqlstores.NewSQLDiscoveredDeviceStore(db), started: make(chan struct{}), resume: make(chan struct{})}
			release := sync.OnceFunc(func() { close(store.resume) })
			defer release()
			svc.WithProvisioning(sqlstores.NewSQLDeviceStore(db), store, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			completed := make(chan error, 1)
			go func() {
				result := &gatewaypb.FleetNodePairResult{
					DeviceIdentifier: a.DeviceIdentifier, Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED,
					SerialNumber: "controller-b", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
					EncryptedCredentials: &gatewaypb.EncryptedCredentials{Username: []byte("stale-user"), Password: []byte("stale-password")},
				}
				meta := pairPersistMeta(t, db, node, org, nil, a.DeviceIdentifier)
				meta.Snapshots[a.DeviceIdentifier] = targets[0]
				_, err := svc.PersistFleetNodePairResult(ctx, meta, result)
				completed <- err
			}()
			select {
			case <-store.started:
			case err := <-completed:
				t.Fatalf("pair result ended before snapshot validation: %v", err)
			case <-ctx.Done():
				t.Fatal("pair result did not reach snapshot validation")
			}

			changed := a
			switch change {
			case "partner route":
				changed = b
				changed.IPAddress = "10.0.0.22"
				changed.Endpoints[0].Host = changed.IPAddress
			case "browser route":
				changed.RPCPort = 9195
				changed.Endpoints[0].RpcPort = changed.RPCPort
			}
			accepted, rejected, err := svc.UpsertDiscoveredDevices(ctx, node, org, []fleetnodepairing.DiscoveredDeviceReport{changed})
			require.NoError(t, err)
			require.Zero(t, rejected)
			require.Len(t, accepted, 1)
			require.Equal(t, a.DeviceIdentifier, accepted[0].DeviceIdentifier)
			release()
			err = <-completed
			require.True(t, fleeterror.IsFailedPreconditionError(err), "a changed discovery snapshot must reject the stale result, got %v", err)
			var devices int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM device WHERE org_id=$1 AND device_identifier=$2`, org, a.DeviceIdentifier).Scan(&devices))
			require.Zero(t, devices, "a stale pairing result must not create or bind inventory")
			var bindings, credentials, pairings int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM fleet_node_device WHERE fleet_node_id=$1`, node).Scan(&bindings))
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM miner_credentials`).Scan(&credentials))
			require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM device_pairing`).Scan(&pairings))
			require.Zero(t, bindings, "a stale result must not bind a device")
			require.Zero(t, credentials, "a stale result must not save credentials")
			require.Zero(t, pairings, "a stale result must not save pairing status")
			var serial string
			require.NoError(t, db.QueryRowContext(ctx, `SELECT COALESCE(serial_number, '') FROM discovered_device WHERE org_id=$1 AND device_identifier=$2`, org, a.DeviceIdentifier).Scan(&serial))
			require.Empty(t, serial, "a stale identity must not overwrite discovery")
		})
	}
}

func TestPairResultRequiresDispatchSnapshot(t *testing.T) {
	db, org, svc, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "missing-snapshot-node")
	upsertNodeDiscovered(t, svc, org, node, "missing-snapshot")
	_, err := svc.PersistFleetNodePairResult(t.Context(), control.PairPersistMeta{
		FleetNodeID: node,
		OrgID:       org,
	}, pairResult("missing-snapshot", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED))
	require.True(t, fleeterror.IsFailedPreconditionError(err), "a missing snapshot must stop persistence, got %v", err)
	var devices int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM device WHERE org_id=$1 AND device_identifier=$2`, org, "missing-snapshot").Scan(&devices))
	require.Zero(t, devices)
}
