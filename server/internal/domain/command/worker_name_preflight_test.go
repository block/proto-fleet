package command

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/generated/sqlc"
	commandMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	minerDomain "github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// Run admission through the real command service, database queue, PluginMiner,
// and SDK gRPC bridge. A real RemoteFleetNodeMiner covers the absent-interface
// case separately from a driver that implements the RPC but returns unsupported.
func TestWorkerNamePreflightBeforeBatchCreation(t *testing.T) {
	password, empty := "private-stored-password", ""
	valid := []sdk.StoredMiningPool{
		{Priority: 0, URL: "stratum+tcp://pool.example:3333", Username: "wallet.old", Password: &password},
		{Priority: 1, Password: &empty},
	}
	for _, tc := range []struct {
		name       string
		remote     bool
		mixed      bool
		pools      []sdk.StoredMiningPool
		readErr    error
		code       connect.Code
		invalidate bool
	}{
		{name: "Fleet Node lacks reader", remote: true, code: connect.CodeFailedPrecondition},
		{name: "driver returns unsupported", readErr: status.Error(codes.Unimplemented, password), code: connect.CodeFailedPrecondition},
		{name: "unknown password", pools: []sdk.StoredMiningPool{{Priority: 0}}, code: connect.CodeFailedPrecondition},
		{name: "extra slot", pools: []sdk.StoredMiningPool{{Priority: 0}, {Priority: 1}, {Priority: 2}, {Priority: 3}}, code: connect.CodeFailedPrecondition},
		{name: "transient read", readErr: status.Error(codes.Unavailable, password), code: connect.CodeUnavailable},
		{name: "authentication rejected", readErr: status.Error(codes.Unauthenticated, password), code: connect.CodeUnauthenticated},
		{name: "mixed supported and unsupported", pools: valid, mixed: true, code: connect.CodeFailedPrecondition},
		{name: "verified passwords", pools: valid},
		{name: "verified empty storage", pools: []sdk.StoredMiningPool{}},
		{name: "readback changes after admission", pools: valid, invalidate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execution, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "antminer", false)
			_, err := conn.ExecContext(t.Context(), `INSERT INTO "user" (id, user_id, username, password_hash)
    VALUES (42, 'user-1', 'test-user', 'unused')`)
			require.NoError(t, err)
			run := newExecutionRun(t.Context())
			t.Cleanup(run.cancelAdmission)
			t.Cleanup(run.cancelWork)
			execution.run = run
			ctrl := gomock.NewController(t)
			getter := commandMocks.NewMockCachedMinerGetter(ctrl)
			var miner interfaces.Miner
			var device *workerNamePreflightDevice
			if tc.remote {
				miner = &minerDomain.RemoteFleetNodeMiner{}
			} else {
				device = &workerNamePreflightDevice{id: identifier, pools: tc.pools, readErr: tc.readErr}
				miner = newWorkerNamePreflightPluginMiner(t, identifier, device)
			}
			expectedRead := getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(miner, nil)
			desired := map[string]string{identifier: "new-worker"}
			if tc.mixed {
				expectedRead.MaxTimes(1) // The unsupported miner may cancel this read first.
				q := sqlc.New(conn)
				remoteIdentifier := "remote-" + identifier
				discovered, err := q.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
					OrgID: 1, DeviceIdentifier: "discovered-" + remoteIdentifier, DriverName: "antminer",
					IpAddress: "192.0.2.11", Port: "80", UrlScheme: "http", IsActive: true,
				})
				require.NoError(t, err)
				remoteID, err := q.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
					OrgID: 1, DiscoveredDeviceID: discovered, DeviceIdentifier: remoteIdentifier, MacAddress: "00:11:22:33:44:56",
				})
				require.NoError(t, err)
				desired[remoteIdentifier] = "new-worker"
				getter.EXPECT().GetMinerForCommand(gomock.Any(), remoteID).Return(&minerDomain.RemoteFleetNodeMiner{}, nil)
			}
			execution.minerService = getter
			finalizers := 0
			service := &Service{
				config: &Config{}, conn: conn, executionService: execution,
				messageQueue:                     queue.NewDatabaseMessageQueue(&queue.Config{}, conn),
				startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) { finalizers++ },
			}
			batch, err := service.ReapplyCurrentPoolsWithWorkerNames(manualSessionCtx(1), desired)
			if tc.code != 0 {
				var fleetErr fleeterror.FleetError
				require.ErrorAs(t, err, &fleetErr)
				require.Equal(t, tc.code, fleetErr.GRPCCode)
				require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), password)
				require.Empty(t, batch)
				assertCommandDispatchRows(t, conn, 0)
				require.Zero(t, finalizers)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, batch)
				assertCommandDispatchRows(t, conn, 1)
				require.Equal(t, 1, finalizers)
				var body []byte
				require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT payload FROM queue_message WHERE command_batch_log_uuid = $1`, batch).Scan(&body))
				require.NotContains(t, string(body), password, "preflight must not persist readback credentials")
				if tc.invalidate {
					device.mu.Lock()
					device.pools = []sdk.StoredMiningPool{{Priority: 0}}
					device.mu.Unlock()
					getter.EXPECT().GetMinerForCommand(gomock.Any(), deviceID).Return(miner, nil)
					_, _, err = execution.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, queue.Message{
						DeviceID: deviceID, BatchLogUUID: batch, CommandType: commandtype.UpdateMiningPools, Payload: json.RawMessage(body),
					})
					require.True(t, fleeterror.IsFailedPreconditionError(err))
					require.Equal(t, int32(2), device.reads.Load(), "execution must obtain fresh evidence")
				}
			}
			var workerName string
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT COALESCE(worker_name, '') FROM device WHERE id = $1`, deviceID).Scan(&workerName))
			require.Empty(t, workerName, "preflight rejection or admission alone must not persist a worker name")
			if device != nil {
				require.Zero(t, device.runtimeReads.Load(), "ordinary telemetry must not substitute for stored credentials")
				require.Zero(t, device.writes.Load(), "preflight must never write to the device")
			}
		})
	}
}

type workerNamePreflightDevice struct {
	sdk.Device
	id           string
	mu           sync.Mutex
	pools        []sdk.StoredMiningPool
	readErr      error
	reads        atomic.Int32
	runtimeReads atomic.Int32
	writes       atomic.Int32
}

func (d *workerNamePreflightDevice) ID() string { return d.id }
func (d *workerNamePreflightDevice) GetStoredMiningPools(context.Context) ([]sdk.StoredMiningPool, error) {
	d.reads.Add(1)
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pools, d.readErr
}
func (d *workerNamePreflightDevice) GetMiningPools(context.Context) ([]sdk.ConfiguredPool, error) {
	d.runtimeReads.Add(1)
	return nil, fmt.Errorf("unexpected runtime read")
}
func (d *workerNamePreflightDevice) UpdateMiningPools(context.Context, []sdk.MiningPoolConfig) error {
	d.writes.Add(1)
	return fmt.Errorf("unexpected device write")
}

type workerNamePreflightDriver struct {
	sdk.Driver
	device sdk.Device
}

func (d workerNamePreflightDriver) NewDevice(context.Context, string, sdk.DeviceInfo, sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	return sdk.NewDeviceResult{Device: d.device}, nil
}

func newWorkerNamePreflightPluginMiner(t *testing.T, identifier string, device sdk.Device) *plugins.PluginMiner {
	t.Helper()
	bridge := &sdk.DriverPlugin{Impl: workerNamePreflightDriver{device: device}}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	require.NoError(t, bridge.GRPCServer(nil, server))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); require.NoError(t, listener.Close()); require.NoError(t, <-done) })
	conn, err := grpc.NewClient("passthrough:///worker-name-preflight", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	proxy, err := bridge.GRPCClient(t.Context(), nil, conn)
	require.NoError(t, err)
	driver, ok := proxy.(sdk.Driver)
	require.True(t, ok)
	created, err := driver.NewDevice(t.Context(), identifier, sdk.DeviceInfo{}, sdk.SecretBundle{})
	require.NoError(t, err)
	return plugins.NewPluginMiner(1, 0, models.DeviceIdentifier(identifier), "antminer", nil, "", networking.ConnectionInfo{}, created.Device, sdk.DeviceInfo{}, nil)
}
