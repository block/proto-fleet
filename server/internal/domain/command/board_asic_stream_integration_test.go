package command_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type boardStreamCapabilities struct{ supported bool }

func (p boardStreamCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityBoardASICStream: p.supported}
}

// boardStreamMiner streams frames then returns err. With hold set it instead
// keeps the stream open, as a live controller does, until its context ends.
type boardStreamMiner struct {
	interfaces.Miner
	frames []sdk.BoardASICFrame
	err    error
	hold   bool
	boards []sdk.BoardIdentifyTarget
}

func (m *boardStreamMiner) StreamBoardASICReadings(ctx context.Context, board sdk.BoardIdentifyTarget, send func(sdk.BoardASICFrame) error) error {
	m.boards = append(m.boards, board)
	for _, frame := range m.frames {
		if err := send(frame); err != nil {
			return err
		}
	}
	if m.hold {
		<-ctx.Done()
		return status.FromContextError(ctx.Err()).Err() //nolint:wrapcheck // stands in for the plugin's gRPC status
	}
	return m.err
}

// fleetNodeMiner stands in for a Fleet Node routed miner, which cannot stream.
type fleetNodeMiner struct{ interfaces.Miner }

type boardStreamGetter struct {
	command.CachedMinerGetter
	miner interfaces.Miner
}

func (g boardStreamGetter) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return g.miner, nil
}

func newBoardStreamService(t *testing.T, conn *sql.DB, supported bool, miner interfaces.Miner) *command.Service {
	t.Helper()
	config := &command.Config{MaxWorkers: 1, WorkerExecutionTimeout: time.Second}
	messageQueue := queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)
	execution := command.NewExecutionService(config, conn, messageQueue, nil, nil, boardStreamGetter{miner: miner}, nil, nil, nil)
	svc := command.NewService(config, conn, execution, messageQueue, command.NewStatusService(conn, messageQueue), nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetPluginCapabilitiesProvider(boardStreamCapabilities{supported})
	return svc
}

func collectBoardStream(ctx context.Context, svc *command.Service, target command.BoardIdentifyTarget, authorize func(context.Context, *int64) error) ([]*containerpb.StreamBoardASICReadingsResponse, error) {
	var out []*containerpb.StreamBoardASICReadingsResponse
	err := svc.StreamBoardASICReadings(ctx, target, authorize, func(r *containerpb.StreamBoardASICReadingsResponse) error {
		out = append(out, r)
		return nil
	})
	return out, err
}

func TestBoardASICStreamRelaysTheConfirmedBoardWithItsLayout(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	_, _, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
	_, boards := seedIdentifyModule(t, conn, user.OrganizationID, tanks[0], 2)
	observed := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	quarantined := true
	miner := &boardStreamMiner{frames: []sdk.BoardASICFrame{
		{ObservedAt: observed, Layout: &sdk.BoardASICLayout{ASICsPerStack: 2, Positions: []sdk.BoardASICPosition{{Index: 0}, {Index: 1, StackIndex: 1, X: 1}}}, ASICs: []sdk.BoardASICReading{{Index: 0, TemperatureC: 72.5, ThermalQuarantined: &quarantined}, {Index: 1, TemperatureC: 70}}},
		{ObservedAt: observed.Add(time.Second), ASICs: []sdk.BoardASICReading{{Index: 0, TemperatureC: 73}, {Index: 1, TemperatureC: 71}, {Index: 2}}},
	}}
	svc := newBoardStreamService(t, conn, true, miner)
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	var site *int64
	authorize := func(_ context.Context, s *int64) error {
		site = s
		return nil
	}

	frames, err := collectBoardStream(ctx, svc, command.BoardIdentifyTarget{HashboardID: boards[1].hashboard, InstallationID: boards[1].installation}, authorize)
	require.True(t, fleeterror.IsUnavailableError(err), "a controller that ends the stream itself is unavailable, so the viewer backs off: %v", err)
	require.Nil(t, site, "authorization runs at the board's current site")
	require.Equal(t, []sdk.BoardIdentifyTarget{{Tank: 1, Module: 2, Slot: 2, ModuleReportedID: fmt.Sprintf("module-%d-2", tanks[0]), MACAddress: fmt.Sprintf("aa:bb:%02x:02:02:01", tanks[0]%256)}}, miner.boards)
	require.Len(t, frames, 2)
	for _, frame := range frames {
		require.Equal(t, boards[1].hashboard, frame.HashboardId)
		require.Equal(t, int32(2), frame.AsicsPerStack)
		require.Equal(t, int32(1), frame.Asics[1].GetPosition().GetX(), "the last layout is joined onto every frame")
	}
	require.Equal(t, observed, frames[0].ObservedAt.AsTime())
	require.True(t, frames[0].Asics[0].GetThermalQuarantined())
	require.Nil(t, frames[1].Asics[0].ThermalQuarantined)
	require.Nil(t, frames[1].Asics[2].Position, "an ASIC the layout does not map has no position")
}

func TestBoardASICStreamRefusesBeforeOpeningTheController(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	_, _, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
	_, boards := seedIdentifyModule(t, conn, user.OrganizationID, tanks[0], 2)
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	allow := func(context.Context, *int64) error { return nil }
	target := command.BoardIdentifyTarget{HashboardID: boards[0].hashboard, InstallationID: boards[0].installation}

	miner := &boardStreamMiner{}
	svc := newBoardStreamService(t, conn, true, miner)
	_, err := collectBoardStream(ctx, svc, target, func(context.Context, *int64) error { return fleeterror.NewForbiddenError("denied") })
	require.True(t, fleeterror.IsForbiddenError(err))

	_, err = collectBoardStream(ctx, svc, command.BoardIdentifyTarget{HashboardID: boards[0].hashboard, InstallationID: boards[1].installation}, allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "a moved or replaced board is not shown under the old view: %v", err)

	foreign := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID+100)
	_, err = collectBoardStream(foreign, svc, target, allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err))

	_, err = collectBoardStream(ctx, svc, command.BoardIdentifyTarget{HashboardID: boards[0].hashboard}, allow)
	require.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = collectBoardStream(ctx, newBoardStreamService(t, conn, false, miner), target, allow)
	require.True(t, fleeterror.IsUnimplementedError(err))
	require.Empty(t, miner.boards, "refused streams never reach the controller")

	_, err = collectBoardStream(ctx, newBoardStreamService(t, conn, true, fleetNodeMiner{}), target, allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.ErrorContains(t, err, "Fleet Node")
}

func TestBoardASICStreamMapsControllerOutcomes(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	_, _, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
	_, boards := seedIdentifyModule(t, conn, user.OrganizationID, tanks[0], 2)
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	allow := func(context.Context, *int64) error { return nil }
	target := command.BoardIdentifyTarget{HashboardID: boards[0].hashboard, InstallationID: boards[0].installation}

	for _, tc := range []struct {
		name  string
		err   error
		check func(error) bool
	}{
		{"board moved on the controller", status.Error(codes.FailedPrecondition, "slot 1 board was replaced"), fleeterror.IsFailedPreconditionError},
		{"controller unavailable", status.Error(codes.Unavailable, "board offline"), fleeterror.IsUnavailableError},
		{"controller cannot stream", status.Error(codes.Unimplemented, "unknown method"), fleeterror.IsUnimplementedError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := collectBoardStream(ctx, newBoardStreamService(t, conn, true, &boardStreamMiner{err: tc.err}), target, allow)
			require.True(t, tc.check(err), "%v", err)
		})
	}

	_, err := collectBoardStream(ctx, newBoardStreamService(t, conn, true, &boardStreamMiner{err: status.Error(codes.Unavailable, "dial tcp 10.1.2.3:9090: connection refused")}), target, allow)
	require.True(t, fleeterror.IsUnavailableError(err))
	require.NotContains(t, err.Error(), "10.1.2.3", "transport details stay in the server log")
	_, err = collectBoardStream(ctx, newBoardStreamService(t, conn, true, &boardStreamMiner{err: status.Error(codes.DataLoss, `controller streamed readings for "aa:00:00:00:00:09"`)}), target, allow)
	require.True(t, fleeterror.IsUnavailableError(err))
	require.NotContains(t, err.Error(), "aa:00", "another board's identity stays in the server log")

	closed := errors.New("client went away")
	svc := newBoardStreamService(t, conn, true, &boardStreamMiner{frames: []sdk.BoardASICFrame{{}}})
	err = svc.StreamBoardASICReadings(ctx, target, allow, func(*containerpb.StreamBoardASICReadingsResponse) error { return closed })
	require.ErrorIs(t, err, closed)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = collectBoardStream(cancelled, newBoardStreamService(t, conn, true, &boardStreamMiner{err: status.Error(codes.Canceled, "context canceled")}), target, allow)
	require.Error(t, err, "a cancelled caller never sees a clean end")
}

func TestBoardASICStreamEndsAnOpenStream(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	_, _, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
	_, boards := seedIdentifyModule(t, conn, user.OrganizationID, tanks[0], 2)
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	allow := func(context.Context, *int64) error { return nil }
	target := command.BoardIdentifyTarget{HashboardID: boards[0].hashboard, InstallationID: boards[0].installation}
	open := func() *command.Service {
		return newBoardStreamService(t, conn, true, &boardStreamMiner{frames: []sdk.BoardASICFrame{{}}, hold: true})
	}

	t.Run("the lifetime cap ends it cleanly so the viewer reopens and is re-checked", func(t *testing.T) {
		command.SetBoardASICStreamTimingForTest(t, 50*time.Millisecond, time.Minute)
		frames, err := collectBoardStream(ctx, open(), target, allow)
		require.NoError(t, err)
		require.Len(t, frames, 1)
	})

	t.Run("a controller that goes silent is unavailable", func(t *testing.T) {
		command.SetBoardASICStreamTimingForTest(t, time.Minute, 50*time.Millisecond)
		frames, err := collectBoardStream(ctx, open(), target, allow)
		require.True(t, fleeterror.IsUnavailableError(err), "%v", err)
		require.Len(t, frames, 1)
	})

	t.Run("a viewer that leaves mid-stream is cancelled", func(t *testing.T) {
		leaving, leave := context.WithCancel(ctx)
		defer leave()
		err := open().StreamBoardASICReadings(leaving, target, allow, func(*containerpb.StreamBoardASICReadingsResponse) error {
			leave()
			return nil
		})
		require.Error(t, err)
		require.False(t, fleeterror.IsUnavailableError(err), "leaving is not a controller failure: %v", err)
	})
}
