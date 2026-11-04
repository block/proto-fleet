package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/authn"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestContainerMiningReadBoundsAuthorizationAndPreservesCallerDeadline(t *testing.T) {
	for _, action := range []string{"status", "power_target"} {
		for _, callerTimeout := range []time.Duration{0, time.Second} {
			t.Run(action+"/"+callerTimeout.String(), func(t *testing.T) {
				f := setupContainerMiningSiteTest(t)
				ctx := authn.SetInfo(t.Context(), f.session)
				if callerTimeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, callerTimeout)
					defer cancel()
				}
				before := time.Now()
				denied := errors.New("stop before controller I/O")
				var deadline time.Time
				var bounded bool
				_, err := f.svc.ReadContainerMining(ctx, f.identifier, sdk.ContainerMiningRequest{Action: action}, func(ctx context.Context, _ *int64, _ sdk.ContainerMiningRequest) error {
					deadline, bounded = ctx.Deadline()
					return denied
				})
				require.ErrorIs(t, err, denied)
				require.True(t, bounded, "server must bound the read before authorization and controller access")
				if callerTimeout > 0 {
					parentDeadline, _ := ctx.Deadline()
					require.Equal(t, parentDeadline, deadline)
				} else {
					require.WithinDuration(t, before.Add(runtimepolicy.ContainerMiningReadTimeout), deadline, time.Second)
				}
				require.Zero(t, f.miner.calls)
			})
		}
	}
}

func TestContainerMiningReadMissingAckReleasesLocksAndCapacity(t *testing.T) {
	f := setupContainerMiningSiteTest(t)
	// One dispatch connection and one connection for completion/competing work.
	f.conn.SetMaxOpenConns(2)
	capacity, err := db.DispatchCapacity(f.conn)
	require.NoError(t, err)
	require.Equal(t, 1, capacity)
	registry := control.NewRegistry()
	stream, err := registry.RegisterAuthenticated(7, "connected-node", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8)
	require.NoError(t, err)
	defer stream.Unregister()
	gate := remotenode.NewPerNodeLimiter(1)
	miner, err := remotenode.New(remotenode.Config{
		Sender: registry, Gate: gate, FleetNodeID: 7, OrgID: f.orgID,
		DeviceIdentifier: f.identifier, DriverName: "proto", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
		IPAddress: "192.0.2.10", Port: "8080", URLScheme: "http",
	})
	require.NoError(t, err)
	f.execution.minerService = containerMiningSiteGetter{miner: miner}
	ctx, cancel := context.WithCancel(authn.SetInfo(t.Context(), f.session))
	defer cancel()
	_, hasDeadline := ctx.Deadline()
	require.False(t, hasDeadline, "the caller deliberately supplies no deadline")
	finished := make(chan error, 1)
	readDone := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-readDone:
		case <-time.After(5 * time.Second):
			t.Error("read did not release its resources after cancellation")
		}
	})
	go func() {
		defer close(readDone)
		_, err := f.svc.ReadContainerMining(ctx, f.identifier, sdk.ContainerMiningRequest{Action: "status"}, func(context.Context, *int64, sdk.ContainerMiningRequest) error { return nil })
		finished <- err
	}()
	select {
	case <-stream.Outgoing: // The node stays registered but never replies.
	case err := <-finished:
		t.Fatalf("read returned before reaching node: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("read did not reach node")
	}
	// The read must hold placement stable until its remote wait returns.
	lockCtx, stopLock := context.WithTimeout(t.Context(), 100*time.Millisecond)
	_, lockErr := f.q.AssignContainersToSite(lockCtx, sqlc.AssignContainersToSiteParams{OrgID: f.orgID, Identifiers: []string{f.identifier}})
	stopLock()
	require.Error(t, lockErr)
	require.ErrorIs(t, lockCtx.Err(), context.DeadlineExceeded)
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(runtimepolicy.ContainerMiningReadTimeout + 5*time.Second):
		cancel()
		<-finished
		t.Fatal("connected node retained read beyond the server deadline")
	}
	require.True(t, registry.SupportsMinerWeb(7), "timeout must not depend on disconnecting the node")
	require.Zero(t, f.conn.Stats().InUse, "read must return its database connection")
	checkCtx, stopCheck := context.WithTimeout(t.Context(), time.Second)
	defer stopCheck()
	rows, err := f.q.AssignContainersToSite(checkCtx, sqlc.AssignContainersToSiteParams{OrgID: f.orgID, Identifiers: []string{f.identifier}})
	require.NoError(t, err, "binding locks must be released")
	require.EqualValues(t, 1, rows)
	release, err := gate.Acquire(checkCtx, 7)
	require.NoError(t, err, "node command capacity must be released")
	release()
	require.NoError(t, db.WithDispatchTransaction(checkCtx, f.conn, func(sqlc.Querier) error { return nil }), "the sole dispatch permit must be reusable")
	_, err = f.enqueueReadAfterTimeout(checkCtx, stream)
	require.NoError(t, err, "subsequent reads must still succeed")
	writes := devicewrite.NewService(f.conn)
	epoch, err := writes.Reserve(checkCtx, f.orgID, f.deviceID)
	require.NoError(t, err)
	drained, err := writes.Drained(checkCtx, f.orgID, f.deviceID, epoch)
	require.NoError(t, err)
	require.True(t, drained, "expired read admission must not block ownership transfer")
}

func (f *containerMiningSiteTest) enqueueReadAfterTimeout(ctx context.Context, stream *control.Stream) (sdk.ContainerMiningResult, error) {
	go func() {
		select {
		case cmd := <-stream.Outgoing:
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.CommandId, Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: []byte(`{"status":{"mining":false,"operating_state":"STOPPED"}}`)})
		case <-ctx.Done():
		}
	}()
	return f.svc.ReadContainerMining(authn.SetInfo(ctx, f.session), f.identifier, sdk.ContainerMiningRequest{Action: "status"}, func(context.Context, *int64, sdk.ContainerMiningRequest) error { return nil })
}
