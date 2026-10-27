package sqlstores_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
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
