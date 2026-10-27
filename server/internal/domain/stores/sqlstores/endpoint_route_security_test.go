package sqlstores_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestPairedEndpointDiscoveryRejectsSpoofedRoutes(t *testing.T) {
	for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumPAIRED, sqlc.PairingStatusEnumDEFAULTPASSWORD, sqlc.PairingStatusEnumAUTHENTICATIONNEEDED} {
		t.Run(string(status), func(t *testing.T) {
			db, store, org := ct1HAStore(t)
			saveCT1HA(t, store, "container", ct1HADiscovery(org, "a", "10.0.0.11"))
			before := saveCT1HA(t, store, "scan-b", ct1HADiscovery(org, "b", "10.0.0.12"))
			pairCT1HA(t, db, before)
			setCT1HAPairing(t, db, before, status)
			for _, attack := range []struct {
				name   string
				change func(*discovery.DiscoveredDevice)
			}{
				{"host", func(d *discovery.DiscoveredDevice) { d.IpAddress, d.Endpoints[0].Host = "10.0.0.99", "10.0.0.99" }},
				{"rpc port", func(d *discovery.DiscoveredDevice) { d.RpcPort, d.Endpoints[0].RpcPort = 9443, 9443 }},
				{"rpc tls", func(d *discovery.DiscoveredDevice) { d.RpcTls, d.Endpoints[0].RpcTls = true, true }},
				{"new identity", func(d *discovery.DiscoveredDevice) {
					d.Endpoints[0].Id = "c"
				}},
				{"peer address swap", func(d *discovery.DiscoveredDevice) { d.IpAddress, d.Endpoints[0].Host = "10.0.0.12", "10.0.0.12" }},
				{"omitted endpoints", func(d *discovery.DiscoveredDevice) { d.Endpoints = nil }},
				{"changed members", func(d *discovery.DiscoveredDevice) { d.Endpoints[0].MemberIds = []string{"a", "c"} }},
			} {
				t.Run(attack.name, func(t *testing.T) {
					incoming := ct1HADiscovery(org, "a", "10.0.0.11")
					attack.change(incoming)
					_, err := store.SaveDirect(t.Context(), before.GetDeviceOrgIdentifier(), incoming)
					require.True(t, fleeterror.IsFailedPreconditionError(err), "a spoofed route must be rejected, got %v", err)
					after, err := store.GetDevice(t.Context(), before.GetDeviceOrgIdentifier())
					require.NoError(t, err)
					require.True(t, proto.Equal(&before.Device, &after.Device), "a rejected scan must not change paired routes or metadata")
				})
			}
			// Discovery defaults cannot overwrite saved web settings.
			incoming := ct1HADiscovery(org, "a", "10.0.0.11")
			incoming.Endpoints[0].Port, incoming.Endpoints[0].UrlScheme = 443, "https"
			saved := saveCT1HA(t, store, before.DeviceIdentifier, incoming)
			require.True(t, proto.Equal(&before.Device, &saved.Device))
		})
	}
}

func TestEndpointDiscoveryRechecksPairingAfterDeviceLock(t *testing.T) {
	t.Run("existing device lock", func(t *testing.T) { testEndpointDiscoveryWaitsForPairing(t, false) })
	t.Run("first pairing foreign key lock", func(t *testing.T) { testEndpointDiscoveryWaitsForPairing(t, true) })
}

func testEndpointDiscoveryWaitsForPairing(t *testing.T, firstPairing bool) {
	t.Helper()
	db, store, org := ct1HAStore(t)
	before := saveCT1HA(t, store, "container", ct1HADiscovery(org, "a", "10.0.0.11"))
	devices := sqlstores.NewSQLDeviceStore(db)
	waitQuery := "LockEndpointDiscoveryDevices"
	if !firstPairing {
		require.NoError(t, devices.InsertDevice(t.Context(), &before.Device, org, before.DeviceIdentifier))
	} else {
		waitQuery = "LockDiscoveredDeviceForEndpointDiscovery"
	}
	locked, release := make(chan struct{}), make(chan struct{})
	releasePairing := sync.OnceFunc(func() { close(release) })
	defer releasePairing()
	pairDone := make(chan error, 1)
	go func() {
		pairDone <- sqlstores.NewSQLTransactor(db).RunInTx(t.Context(), func(ctx context.Context) error {
			if firstPairing {
				if err := devices.InsertDevice(ctx, &before.Device, org, before.DeviceIdentifier); err != nil {
					return err
				}
			}
			if err := devices.UpsertDevicePairing(ctx, &before.Device, org, "PAIRED"); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-pairDone:
		t.Fatalf("pairing exited before taking the lock: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("pairing did not acquire the lock")
	}
	discoveryDone := make(chan error, 1)
	go func() {
		_, err := store.SaveDirect(t.Context(), before.GetDeviceOrgIdentifier(), ct1HADiscovery(org, "a", "10.0.0.99"))
		discoveryDone <- err
	}()
	// Confirm that the scan is waiting on the pairing transaction, not merely
	// that the goroutine has not yet run.
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%' || $1 || '%')`, waitQuery).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	releasePairing()
	require.NoError(t, <-pairDone)
	require.True(t, fleeterror.IsFailedPreconditionError(<-discoveryDone))
	after, err := store.GetDevice(t.Context(), before.GetDeviceOrgIdentifier())
	require.NoError(t, err)
	require.True(t, proto.Equal(&before.Device, &after.Device))
}
