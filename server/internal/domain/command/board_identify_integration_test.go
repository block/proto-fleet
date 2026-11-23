package command_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type boardIdentifyCapabilities struct{ supported bool }

func (p boardIdentifyCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityBoardIdentify: p.supported}
}

type seededBoard struct{ hashboard, installation int64 }

func seedIdentifyBoard(t *testing.T, conn *sql.DB, orgID, module int64, slot int, mac string) seededBoard {
	t.Helper()
	var b seededBoard
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, orgID).Scan(&b.hashboard))
	_, err := conn.ExecContext(t.Context(), `INSERT INTO hashboard_identity(org_id,kind,value,hashboard_id) VALUES($1,'mac',$2,$3),($1,'serial',$4,$3)`, orgID, mac, b.hashboard, "SN-"+mac)
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,$3,now(),now()) RETURNING id`, b.hashboard, module, slot).Scan(&b.installation))
	_, err = conn.ExecContext(t.Context(), `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,$3,'occupied',now())`, module, b.hashboard, slot)
	require.NoError(t, err)
	return b
}

func seedIdentifyModule(t *testing.T, conn *sql.DB, orgID, tank int64, position int) (int64, []seededBoard) {
	t.Helper()
	module := seedCommandModule(t, conn, tank, position, fmt.Sprintf("module-%d-%d", tank, position))
	boards := []seededBoard{}
	for slot := 1; slot <= 3; slot++ {
		boards = append(boards, seedIdentifyBoard(t, conn, orgID, module, slot, fmt.Sprintf("aa:bb:%02x:%02x:%02x:01", tank%256, position, slot)))
	}
	return module, boards
}

func identifyTargets(boards ...seededBoard) []command.BoardIdentifyTarget {
	out := []command.BoardIdentifyTarget{}
	for _, b := range boards {
		out = append(out, command.BoardIdentifyTarget{HashboardID: b.hashboard, InstallationID: b.installation})
	}
	return out
}

func newBoardIdentifyService(t *testing.T, conn *sql.DB, supported bool) *command.Service {
	t.Helper()
	svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
	svc.SetPluginCapabilitiesProvider(boardIdentifyCapabilities{supported})
	return svc
}

func countQueuedBoardIdentify(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message WHERE command_type='SetBoardIdentify'`).Scan(&n))
	return n
}

func TestBoardIdentifyExpandsModuleAndQueuesOneCommandPerConfirmedBoard(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	identifier, _, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
	module, boards := seedIdentifyModule(t, conn, user.OrganizationID, tanks[0], 2)
	svc := newBoardIdentifyService(t, conn, true)
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	allow := func(context.Context, *int64) error { return nil }

	options, err := svc.GetBoardIdentifyOptions(ctx, []int64{module}, nil, allow)
	require.NoError(t, err)
	require.True(t, options.CanIdentify, options.UnavailableReason)
	require.Len(t, options.Boards, 3)
	for i, board := range options.Boards {
		require.Equal(t, boards[i].hashboard, board.HashboardId)
		require.Equal(t, boards[i].installation, board.InstallationId)
		require.Equal(t, identifier, board.DeviceIdentifier)
		require.Equal(t, int32(1), board.Tank)
		require.Equal(t, int32(2), board.Module)
		require.Equal(t, []int32{1, 2, 3}[i], board.Slot)
		require.NotEmpty(t, board.SerialNumber)
	}

	denied := func(context.Context, *int64) error { return fleeterror.NewForbiddenError("denied") }
	_, err = svc.GetBoardIdentifyOptions(ctx, []int64{module}, nil, denied)
	require.True(t, fleeterror.IsForbiddenError(err))
	_, err = svc.SetBoardIdentify(ctx, identifyTargets(boards...), sdk.BoardIdentifyBlink, denied)
	require.True(t, fleeterror.IsForbiddenError(err))
	require.Zero(t, countQueuedBoardIdentify(t, conn))

	foreign := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID+100)
	_, err = svc.GetBoardIdentifyOptions(foreign, []int64{module}, nil, allow)
	require.True(t, fleeterror.IsNotFoundError(err))

	result, err := svc.SetBoardIdentify(ctx, identifyTargets(boards[0], boards[2]), sdk.BoardIdentifyBlink, allow)
	require.NoError(t, err)
	require.Len(t, result.Commands, 2)
	require.NotEqual(t, result.Commands[0].BatchIdentifier, result.Commands[1].BatchIdentifier, "each board reports its own outcome")
	require.Equal(t, []int64{boards[0].hashboard, boards[2].hashboard}, []int64{result.Commands[0].HashboardId, result.Commands[1].HashboardId})
	require.Equal(t, 2, countQueuedBoardIdentify(t, conn))

	var payload string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT payload::text FROM queue_message WHERE command_type='SetBoardIdentify' ORDER BY id LIMIT 1`).Scan(&payload))
	require.Contains(t, payload, `"mac_address": "aa:bb:`)
	require.Contains(t, payload, `"mode": "blink"`)
	require.Contains(t, payload, fmt.Sprintf(`"installation_id": %d`, boards[0].installation))

	options, err = svc.GetBoardIdentifyOptions(ctx, []int64{module}, nil, allow)
	require.NoError(t, err)
	require.False(t, options.CanIdentify)
	require.Contains(t, options.UnavailableReason, "already pending")
	_, err = svc.SetBoardIdentify(ctx, identifyTargets(boards[1]), sdk.BoardIdentifyOff, allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.Equal(t, 2, countQueuedBoardIdentify(t, conn))
}

func TestBoardIdentifyRejectsChangedOrUnreadyBoardsWithoutQueueing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{"board reinstalled after confirmation", `WITH removed AS (UPDATE hashboard_installation SET removed_at=now() WHERE id=$2 RETURNING module_id,position)
			INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) SELECT $1,module_id,position,now(),now() FROM removed`},
		{"replacement board in the slot", `UPDATE hashboard_slot SET hashboard_id=(SELECT id FROM hashboard WHERE id<>$1 ORDER BY id DESC LIMIT 1) WHERE hashboard_id=$1 AND $2>0`},
		{"slot emptied", `UPDATE hashboard_slot SET occupancy='empty' WHERE hashboard_id=$1 AND $2>0`},
		{"slot reports unavailable", `UPDATE hashboard_slot SET unavailable_reason='controller offline' WHERE hashboard_id=$1 AND $2>0`},
		{"stale slot observation", `UPDATE hashboard_slot SET observed_at=now()-interval '3 minutes' WHERE hashboard_id=$1 AND $2>0`},
		{"module identity withdrawn", `UPDATE container_module SET identity_confirmed=false WHERE id=(SELECT module_id FROM hashboard_installation WHERE id=$2) AND $1>0`},
		{"board has two MACs", `INSERT INTO hashboard_identity(org_id,kind,value,hashboard_id) SELECT org_id,'mac','ff:ff:ff:ff:ff:ff',id FROM hashboard WHERE id=$1 AND $2>0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, dbs, user := setupRetentionTest(t)
			_, _, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
			_, boards := seedIdentifyModule(t, conn, user.OrganizationID, tanks[0], 1)
			svc := newBoardIdentifyService(t, conn, true)
			ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
			allow := func(context.Context, *int64) error { return nil }
			_, err := conn.ExecContext(t.Context(), tc.query, boards[0].hashboard, boards[0].installation)
			require.NoError(t, err)
			_, err = svc.SetBoardIdentify(ctx, identifyTargets(boards...), sdk.BoardIdentifyBlink, allow)
			require.True(t, fleeterror.IsFailedPreconditionError(err), err)
			require.Zero(t, countQueuedBoardIdentify(t, conn))
			require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
		})
	}
}

func TestBoardIdentifyAvailabilityRequiresCapabilityAndProtocolV10(t *testing.T) {
	for _, tc := range []struct {
		name      string
		supported bool
		version   gatewaypb.CommandProtocolVersion
		reason    string
	}{
		{name: "local supported", supported: true},
		{name: "local unsupported", reason: "not supported"},
		{name: "remote V9 requires upgrade", supported: true, version: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V9, reason: "protocol version 10"},
		{name: "remote V10 supported", supported: true, version: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, dbs, user := setupRetentionTest(t)
			_, deviceID, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
			module, boards := seedIdentifyModule(t, conn, user.OrganizationID, tanks[0], 1)
			var protocolResults []bool
			registry := control.NewRegistry().WithProtocolObserver(func(compatible bool) {
				protocolResults = append(protocolResults, compatible)
			})
			if tc.version != 0 {
				stream, err := registry.RegisterAuthenticated(bindTankControllerToFleetNode(t, conn, user.OrganizationID, deviceID), "session", tc.version)
				require.NoError(t, err)
				defer stream.Unregister()
			}
			svc := newBoardIdentifyService(t, conn, tc.supported)
			svc.SetCommandProtocolChecker(registry)
			ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
			allow := func(context.Context, *int64) error { return nil }
			options, err := svc.GetBoardIdentifyOptions(ctx, []int64{module}, nil, allow)
			require.NoError(t, err)
			require.Equal(t, tc.reason == "", options.CanIdentify)
			require.Contains(t, options.UnavailableReason, tc.reason)
			require.Empty(t, protocolResults, "display checks must not emit protocol metrics")
			_, err = svc.SetBoardIdentify(ctx, identifyTargets(boards[0]), sdk.BoardIdentifyOff, allow)
			if tc.version == gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V9 {
				require.Equal(t, []bool{false}, protocolResults)
			} else {
				require.Empty(t, protocolResults, "the dispatch check records compatible outcomes")
			}
			if tc.reason == "" {
				require.NoError(t, err)
				require.Equal(t, 1, countQueuedBoardIdentify(t, conn))
				return
			}
			require.True(t, fleeterror.IsFailedPreconditionError(err))
			require.Zero(t, countQueuedBoardIdentify(t, conn))
		})
	}
}

func TestBoardIdentifyRequestValidation(t *testing.T) {
	conn, _, user := setupRetentionTest(t)
	svc := newBoardIdentifyService(t, conn, true)
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	allow := func(context.Context, *int64) error { return nil }
	for _, tc := range []struct {
		name    string
		targets []command.BoardIdentifyTarget
		mode    sdk.BoardIdentifyMode
	}{
		{"no boards", nil, sdk.BoardIdentifyBlink},
		{"invalid mode", []command.BoardIdentifyTarget{{HashboardID: 1, InstallationID: 1}}, "strobe"},
		{"missing installation", []command.BoardIdentifyTarget{{HashboardID: 1}}, sdk.BoardIdentifyBlink},
		{"duplicate board", []command.BoardIdentifyTarget{{HashboardID: 1, InstallationID: 1}, {HashboardID: 1, InstallationID: 2}}, sdk.BoardIdentifyBlink},
	} {
		_, err := svc.SetBoardIdentify(ctx, tc.targets, tc.mode, allow)
		require.True(t, fleeterror.IsInvalidArgumentError(err), tc.name)
	}
	_, err := svc.GetBoardIdentifyOptions(ctx, nil, nil, allow)
	require.True(t, fleeterror.IsInvalidArgumentError(err))
	_, err = svc.SetBoardIdentify(ctx, []command.BoardIdentifyTarget{{HashboardID: 1, InstallationID: 1}}, sdk.BoardIdentifyBlink, nil)
	require.True(t, fleeterror.IsForbiddenError(err))
}
