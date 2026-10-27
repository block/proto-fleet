package pairing_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

type noRetryDiscoveryTransactor struct{ *sqlstores.SQLTransactor }

func (tx noRetryDiscoveryTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return tx.RunInTxNoRetry(ctx, fn)
}

type pausedDiscoveryStore struct {
	*sqlstores.SQLFleetNodePairingStore
	first, resume chan struct{}
	once          sync.Once
}

func (s *pausedDiscoveryStore) UpsertDiscoveredDeviceFromFleetNode(ctx context.Context, orgID, nodeID int64, report fleetnodepairing.DiscoveredDeviceReport) (fleetnodepairing.DiscoveredDeviceReport, bool, error) {
	saved, stored, err := s.SQLFleetNodePairingStore.UpsertDiscoveredDeviceFromFleetNode(ctx, orgID, nodeID, report)
	if err == nil && stored {
		s.once.Do(func() {
			close(s.first)
			<-s.resume
		})
	}
	return saved, stored, err
}

func TestConcurrentDiscoveryBatchesKeepOneLockOrder(t *testing.T) {
	for _, mixedLegacy := range []bool{false, true} {
		name := "reversed groups"
		if mixedLegacy {
			name = "mixed legacy and group"
		}
		t.Run(name, func(t *testing.T) {
			db, org, _, enrollment := setupPairingTest(t)
			node := createFleetNode(t, enrollment, org, "batch-node")
			a, b := endpointGroupReport(t, node, "controller-a", "10.0.0.11"), endpointGroupReport(t, node, "controller-b", "10.0.0.12")
			a.Endpoints[0].DeviceKey = "group-a"
			b.Endpoints[0].DeviceKey = "group-b"
			if mixedLegacy {
				a.DeviceIdentifier, a.Endpoints = "legacy-a", nil
			}
			store := &pausedDiscoveryStore{SQLFleetNodePairingStore: sqlstores.NewSQLFleetNodePairingStore(db), first: make(chan struct{}), resume: make(chan struct{})}
			release := sync.OnceFunc(func() { close(store.resume) })
			defer release()
			svc := fleetnodepairing.NewService(store, nil, noRetryDiscoveryTransactor{sqlstores.NewSQLTransactor(db)})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			errors := make(chan error, 2)
			go func() {
				_, _, err := svc.UpsertDiscoveredDevices(ctx, node, org, []fleetnodepairing.DiscoveredDeviceReport{a, b})
				errors <- err
			}()
			select {
			case <-store.first:
			case err := <-errors:
				t.Fatalf("first batch ended before pausing: %v", err)
			case <-ctx.Done():
				t.Fatal("first batch did not reach its first write")
			}
			go func() {
				accepted, _, err := svc.UpsertDiscoveredDevices(ctx, node, org, []fleetnodepairing.DiscoveredDeviceReport{b, a})
				if err == nil && (len(accepted) != 2 || accepted[0].IPAddress != b.IPAddress || accepted[1].IPAddress != a.IPAddress) {
					err = fmt.Errorf("accepted reports did not retain the input order: %v", accepted)
				}
				errors <- err
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockEndpointDiscovery%')`).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond, "the second batch must wait before taking group or row locks")
			release()
			require.NoError(t, <-errors)
			require.NoError(t, <-errors, "reversed batches must succeed without a transaction retry")
		})
	}
}

func TestDirectDiscoveryWaitsForNodeDiscoveryDatabasePhase(t *testing.T) {
	db, org, _, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, org, "mixed-discovery-node")
	store := &pausedDiscoveryStore{SQLFleetNodePairingStore: sqlstores.NewSQLFleetNodePairingStore(db), first: make(chan struct{}), resume: make(chan struct{})}
	release := sync.OnceFunc(func() { close(store.resume) })
	defer release()
	svc := fleetnodepairing.NewService(store, nil, noRetryDiscoveryTransactor{sqlstores.NewSQLTransactor(db)})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	results := make(chan error, 2)
	nodeReport := endpointGroupReport(t, node, "controller-a", "10.0.0.11")
	directReport := endpointGroupReport(t, node, "controller-b", "10.0.0.12")
	directReport.Endpoints[0].DeviceKey = "unrelated-direct-group"
	go func() {
		_, _, err := svc.UpsertDiscoveredDevices(ctx, node, org, []fleetnodepairing.DiscoveredDeviceReport{nodeReport})
		results <- err
	}()
	select {
	case <-store.first:
	case err := <-results:
		t.Fatalf("node batch ended before pausing: %v", err)
	case <-ctx.Done():
		t.Fatal("node batch did not reach its first write")
	}
	go func() {
		report := directReport
		d := &discovery.DiscoveredDevice{OrgID: org, Device: pb.Device{
			DeviceIdentifier: "direct-device", DriverName: report.DriverName, DeviceClass: report.DeviceClass,
			Model: report.Model, Manufacturer: report.Manufacturer, IpAddress: report.IPAddress, Port: report.Port, UrlScheme: report.URLScheme,
			RpcPort: report.RPCPort, RpcTls: report.RPCTLS, Endpoints: report.Endpoints,
		}}
		_, err := sqlstores.NewSQLDiscoveredDeviceStore(db).SaveDirect(ctx, d.GetDeviceOrgIdentifier(), d)
		results <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockEndpointDiscovery%')`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "direct discovery must use the same outer lock before group locks")
	release()
	require.NoError(t, <-results)
	require.NoError(t, <-results)
}
