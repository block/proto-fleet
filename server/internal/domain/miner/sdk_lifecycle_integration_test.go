package miner_test

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// This is a newly allocated backend for every real SDK NewDevice call, rather
// than a mock returning one device regardless of the requested binding.
type registryIntegrationDevice struct {
	sdk.Device
	id    string
	calls *atomic.Int64
}

func (d *registryIntegrationDevice) ID() string                   { return d.id }
func (d *registryIntegrationDevice) Close(context.Context) error  { return nil }
func (d *registryIntegrationDevice) Reboot(context.Context) error { d.calls.Add(1); return nil }

type registryIntegrationDriver struct {
	sdk.Driver
	name           string
	creates, calls *atomic.Int64
}

func (d registryIntegrationDriver) Handshake(context.Context) (sdk.DriverIdentifier, error) {
	return sdk.DriverIdentifier{DriverName: d.name, APIVersion: "v1"}, nil
}
func (d registryIntegrationDriver) DescribeDriver(ctx context.Context) (sdk.DriverIdentifier, sdk.Capabilities, error) {
	id, e := d.Handshake(ctx)
	return id, sdk.Capabilities{}, e
}
func (d registryIntegrationDriver) NewDevice(_ context.Context, id string, _ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	d.creates.Add(1)
	return sdk.NewDeviceResult{Device: &registryIntegrationDevice{id: id, calls: d.calls}}, nil
}

type registryIntegrationManager struct {
	clients map[string]sdk.Driver
	caps    sdk.Capabilities
}

func (m *registryIntegrationManager) HasPluginForDriverName(name string) bool {
	return m.clients[name] != nil
}
func (m *registryIntegrationManager) GetCapabilitiesForDriverName(string) sdk.Capabilities {
	return m.caps
}
func (m *registryIntegrationManager) GetDriverByDriverName(name string) (sdk.Driver, error) {
	d, ok := m.clients[name]
	if !ok {
		return nil, fmt.Errorf("missing test driver %s", name)
	}
	return d, nil
}
func registryIntegrationClient(t *testing.T, name string, creates, calls *atomic.Int64) sdk.Driver {
	return registryIntegrationClientWithStop(t, name, creates, calls, nil)
}
func registryIntegrationClientWithStop(t *testing.T, name string, creates, calls *atomic.Int64, stop *func()) sdk.Driver {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	plugin := &sdk.DriverPlugin{Impl: registryIntegrationDriver{name: name, creates: creates, calls: calls}}
	require.NoError(t, plugin.GRPCServer(nil, server))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	if stop != nil {
		*stop = server.Stop
	}
	conn, err := grpc.NewClient("passthrough:///registry-integration", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client, err := plugin.GRPCClient(t.Context(), nil, conn)
	require.NoError(t, err)
	driver, ok := client.(sdk.Driver)
	require.True(t, ok, "SDK plugin client must implement Driver")
	return driver
}

func TestFreshCommandFacadesRetainRealSDKBackendAcrossReadCacheExpiry(t *testing.T) {
	if testing.Short() {
		t.Skip("requires real Postgres")
	}
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("sdk-registry-lifetime")
	id := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, id)
	var creates, calls atomic.Int64
	manager := &registryIntegrationManager{clients: map[string]sdk.Driver{"antminer": registryIntegrationClient(t, "antminer", &creates, &calls)}, caps: sdk.Capabilities{sdk.CapabilityCurtailFull: true}}
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, manager)
	miner.SetReadCacheTTL(service, time.Second)
	first, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	fresh, err := service.GetMinerForCommand(t.Context(), id)
	require.NoError(t, err)
	require.NotSame(t, first, fresh)
	require.Equal(t, int64(1), creates.Load())
	service.InvalidateMiner(identifier)
	invalidated, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	require.NotSame(t, first, invalidated)
	require.Equal(t, int64(1), creates.Load())
	cached, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.NoError(t, err)
	require.Same(t, invalidated, cached)
	// Exercise real TTL expiry while retaining the SDK backend and its state.
	expired := invalidated
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		expired, err = service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
		require.NoError(c, err)
		require.NotSame(c, invalidated, expired)
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, int64(1), creates.Load())
	var site int64
	require.NoError(t, conn.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES(1,'Registry Site','registry-site') RETURNING id").Scan(&site))
	_, err = conn.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", site, id)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), "UPDATE discovered_device SET model='New display model',manufacturer='New display vendor' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", id)
	require.NoError(t, err)
	manager.caps = sdk.Capabilities{}
	moved, err := service.GetMinerForCommand(t.Context(), id)
	require.NoError(t, err)
	require.NotSame(t, expired, moved)
	require.Equal(t, site, moved.GetSiteID())
	require.Zero(t, expired.GetSiteID())
	require.Equal(t, int64(1), creates.Load())
	require.Error(t, expired.Reboot(t.Context()), "old captured facade must retain its binding and reject before SDK dispatch")
	require.Zero(t, calls.Load())
	require.Error(t, moved.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}), "current capability removal must be enforced by the new facade")
	require.Zero(t, calls.Load())
	require.NoError(t, moved.Reboot(t.Context()))
	require.Equal(t, int64(1), calls.Load())
}

func TestFreshCommandFacadesFenceChangedBindings(t *testing.T) {
	changes := []string{"route", "credential contents", "credential version", "pairing version", "epoch", "org", "discovery incarnation", "numeric incarnation", "driver", "driver process"}
	for _, change := range changes {
		t.Run(change, func(t *testing.T) {
			conn, crypto, files := setupTestDB(t)
			identifier := models.DeviceIdentifier("sdk-binding-matrix")
			id := createTestDevice(t, conn, string(identifier))
			createTestMinerCredentials(t, conn, crypto, id)
			q := sqlc.New(conn)
			require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), []int64{id}))
			var creates, calls atomic.Int64
			var stopDriver func()
			manager := &registryIntegrationManager{clients: map[string]sdk.Driver{"antminer": registryIntegrationClientWithStop(t, "antminer", &creates, &calls, &stopDriver), "proto": registryIntegrationClient(t, "proto", &creates, &calls)}}
			service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, manager)
			old, err := service.GetMinerForCommand(t.Context(), id)
			require.NoError(t, err)
			require.Equal(t, int64(1), creates.Load())
			exec := func(query string, args ...any) {
				t.Helper()
				_, err := conn.ExecContext(t.Context(), query, args...)
				require.NoError(t, err)
			}
			switch change {
			case "route":
				exec("UPDATE discovered_device SET ip_address='192.0.2.88',port='8080',url_scheme='http' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", id)
			case "credential contents":
				encrypted, err := crypto.Encrypt([]byte("changed-test-password"))
				require.NoError(t, err)
				exec("UPDATE miner_credentials SET password_enc=$1 WHERE device_id=$2", encrypted, id)
			case "credential version":
				exec("UPDATE miner_credentials SET updated_at=updated_at+interval '1 second' WHERE device_id=$1", id)
			case "pairing version":
				exec("UPDATE device_pairing SET paired_at=paired_at+interval '1 second' WHERE device_id=$1", id)
			case "epoch":
				exec("UPDATE device_write_fence SET epoch=epoch+1 WHERE device_id=$1", id)
			case "org":
				exec("INSERT INTO organization(id,org_id,name) VALUES(2,'sdk-other-org','SDK other org')")
				exec("DELETE FROM device_write_fence WHERE device_id=$1", id)
				exec("UPDATE device SET org_id=2 WHERE id=$1", id)
			case "discovery incarnation":
				newDiscovery := createDiscoveredDevice(t, conn, "TestMiner", "TestCorp", "antminer")
				var originalIP string
				require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT ip_address FROM discovered_device WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", id).Scan(&originalIP))
				exec("UPDATE discovered_device SET ip_address='192.0.2.201' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", id)
				exec("UPDATE discovered_device SET ip_address=$1,port='4028',url_scheme='https' WHERE id=$2", originalIP, newDiscovery)
				exec("UPDATE device SET discovered_device_id=$1 WHERE id=$2", newDiscovery, id)
			case "numeric incarnation":
				exec("UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", id)
				id = createTestDevice(t, conn, string(identifier))
				createTestMinerCredentials(t, conn, crypto, id)
			case "driver process":
				stopDriver()
				manager.clients["antminer"] = registryIntegrationClient(t, "antminer", &creates, &calls)
			case "driver":
				exec("UPDATE discovered_device SET driver_name='proto' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", id)
			}
			current, err := service.GetMinerForCommand(t.Context(), id)
			require.NoError(t, err)
			require.NotSame(t, old, current)
			expectedCreates := int64(2)
			if change == "epoch" {
				expectedCreates = 1 // Custody changes fence the facade, not the physical backend.
			}
			require.Equal(t, expectedCreates, creates.Load())
			again, err := service.GetMinerForCommand(t.Context(), id)
			require.NoError(t, err)
			require.NotSame(t, current, again)
			require.Equal(t, expectedCreates, creates.Load())
			require.Error(t, old.Reboot(t.Context()), "stale captured binding must not dispatch through the current backend")
			require.Zero(t, calls.Load())
		})
	}
}

func TestPasswordRecoveryHandlesRetainBackendGeneration(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("password-recovery-generation")
	deviceID := createTestDevice(t, conn, string(identifier))
	_, err := conn.ExecContext(t.Context(), "UPDATE discovered_device SET driver_name='proto' WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", deviceID)
	require.NoError(t, err)
	var creates, calls atomic.Int64
	manager := &registryIntegrationManager{
		clients: map[string]sdk.Driver{"proto": registryIntegrationClient(t, "proto", &creates, &calls)},
		caps:    sdk.Capabilities{sdk.CapabilityBasicAuth: true},
	}
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, manager)
	first, err := service.GetMinerForPasswordUpdate(t.Context(), deviceID, "test-current-password")
	require.NoError(t, err)
	second, err := service.GetMinerForPasswordUpdate(t.Context(), deviceID, "test-current-password")
	require.NoError(t, err)
	require.Equal(t, int64(2), creates.Load(), "each recovery must use a fresh transient backend")
	require.Error(t, first.Reboot(t.Context()), "an earlier recovery handle must not select its replacement")
	require.Zero(t, calls.Load())
	require.NoError(t, second.Reboot(t.Context()))
	require.Equal(t, int64(1), calls.Load())
	_, err = service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
	require.Error(t, err, "transient recovery must not populate ordinary credential resolution")
	var credentialCount int
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT count(*) FROM miner_credentials WHERE device_id=$1", deviceID).Scan(&credentialCount))
	require.Zero(t, credentialCount, "transient credentials must remain unpersisted")
}
