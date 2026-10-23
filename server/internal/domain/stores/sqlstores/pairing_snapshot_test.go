package sqlstores_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
)

func TestPairingSnapshotRejectsChangedInventory(t *testing.T) {
	for _, change := range []string{"group", "route", "browser route", "owner", "legacy upgrade"} {
		t.Run(change, func(t *testing.T) {
			db, store, org := ct1HAStore(t)
			initial := ct1HADiscovery(org, "a", "10.0.0.11")
			if change == "legacy upgrade" {
				initial.Endpoints = nil
			}
			before := saveCT1HA(t, store, "container", initial)
			incoming := ct1HADiscovery(org, "a", "10.0.0.11")
			switch change {
			case "group":
				incoming.Endpoints[0].DeviceKey = "a-c"
				incoming.Endpoints[0].MemberIds = []string{"a", "c"}
			case "route":
				incoming.IpAddress, incoming.Endpoints[0].Host = "10.0.0.99", "10.0.0.99"
			case "browser route":
				_, err := db.ExecContext(t.Context(), `UPDATE discovered_device SET port='8443' WHERE org_id=$1`, org)
				require.NoError(t, err)
			case "owner":
				var node int64
				require.NoError(t, db.QueryRowContext(t.Context(), `INSERT INTO fleet_node (org_id, name, identity_pubkey, encryption_pubkey, enrollment_status) VALUES ($1,'pair-race',decode(repeat('11',32),'hex'),decode(repeat('22',32),'hex'),'CONFIRMED') RETURNING id`, org).Scan(&node))
				_, err := db.ExecContext(t.Context(), `UPDATE discovered_device SET discovered_by_fleet_node_id = $2 WHERE org_id=$1`, org, node)
				require.NoError(t, err)
			}
			if change != "owner" && change != "browser route" {
				saveCT1HA(t, store, "scan", incoming)
			}
			err := sqlstores.NewSQLTransactor(db).RunInTx(t.Context(), func(ctx context.Context) error {
				return store.LockPairingSnapshot(ctx, before.GetDeviceOrgIdentifier(), &before.Device, nil)
			})
			require.True(t, fleeterror.IsFailedPreconditionError(err), "stale %s snapshot accepted: %v", change, err)
		})
	}
}

func TestPairingSnapshotLocksThroughPairingCommit(t *testing.T) {
	db, store, org := ct1HAStore(t)
	before := saveCT1HA(t, store, "container", ct1HADiscovery(org, "a", "10.0.0.11"))
	devices := sqlstores.NewSQLDeviceStore(db)
	locked, release := make(chan struct{}), make(chan struct{})
	unlock := sync.OnceFunc(func() { close(release) })
	defer unlock()
	pairDone := make(chan error, 1)
	go func() {
		pairDone <- sqlstores.NewSQLTransactor(db).RunInTx(t.Context(), func(ctx context.Context) error {
			if err := store.LockPairingSnapshot(ctx, before.GetDeviceOrgIdentifier(), &before.Device, nil); err != nil {
				return err
			}
			close(locked)
			<-release
			if err := devices.InsertDevice(ctx, &before.Device, org, before.DeviceIdentifier); err != nil {
				return err
			}
			return devices.UpsertDevicePairing(ctx, &before.Device, org, "PAIRED")
		})
	}()
	select {
	case <-locked:
	case err := <-pairDone:
		t.Fatalf("pairing failed before locking: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("pairing did not lock")
	}
	scanDone := make(chan error, 1)
	go func() {
		_, err := store.SaveDirect(t.Context(), before.GetDeviceOrgIdentifier(), ct1HADiscovery(org, "a", "10.0.0.99"))
		scanDone <- err
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := db.QueryRowContext(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockDiscoveredDeviceForEndpointDiscovery%')`).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	unlock()
	require.NoError(t, <-pairDone)
	require.True(t, fleeterror.IsFailedPreconditionError(<-scanDone))
}

func TestPairingSnapshotRechecksAfterDiscoveryCommit(t *testing.T) {
	db, store, org := ct1HAStore(t)
	before := saveCT1HA(t, store, "container", ct1HADiscovery(org, "a", "10.0.0.11"))
	locked, release := make(chan struct{}), make(chan struct{})
	unlock := sync.OnceFunc(func() { close(release) })
	defer unlock()
	scanDone := make(chan error, 1)
	go func() {
		scanDone <- sqlstores.NewSQLTransactor(db).RunInTx(t.Context(), func(ctx context.Context) error {
			incoming := ct1HADiscovery(org, "a", "10.0.0.11")
			incoming.Endpoints[0].DeviceKey = "a-c"
			incoming.Endpoints[0].MemberIds = []string{"a", "c"}
			if _, err := store.SaveDirect(ctx, before.GetDeviceOrgIdentifier(), incoming); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-scanDone:
		t.Fatalf("discovery failed before locking: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("discovery did not lock")
	}
	pairDone := make(chan error, 1)
	go func() {
		pairDone <- sqlstores.NewSQLTransactor(db).RunInTx(t.Context(), func(ctx context.Context) error {
			return store.LockPairingSnapshot(ctx, before.GetDeviceOrgIdentifier(), &before.Device, nil)
		})
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := db.QueryRowContext(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockDiscoveredDeviceForEndpointDiscovery%')`).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	unlock()
	require.NoError(t, <-scanDone)
	require.True(t, fleeterror.IsFailedPreconditionError(<-pairDone))
}
