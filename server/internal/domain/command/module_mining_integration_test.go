package command_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type moduleCapabilities struct{ supported bool }

func (p moduleCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityModuleMining: p.supported, sdk.CapabilityTankMining: true}
}

func TestModuleMiningRequiresV5ForRemoteControllers(t *testing.T) {
	for _, version := range []gatewaypb.CommandProtocolVersion{
		gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V4,
		gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V5,
	} {
		t.Run(version.String(), func(t *testing.T) {
			conn, dbs, user := setupRetentionTest(t)
			_, deviceID, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
			moduleID := seedCommandModule(t, conn, tanks[0], 1, "module-a")
			nodeID := bindTankControllerToFleetNode(t, conn, user.OrganizationID, deviceID)
			registry := control.NewRegistry()
			stream, err := registry.RegisterAuthenticated(nodeID, "session", version)
			require.NoError(t, err)
			defer stream.Unregister()
			svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
			svc.SetPluginCapabilitiesProvider(moduleCapabilities{true})
			svc.SetCommandProtocolChecker(registry)
			ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
			allow := func(context.Context, *int64, bool) error { return nil }
			options, err := svc.GetModuleMiningOptions(ctx, []int64{moduleID}, allow)
			require.NoError(t, err)
			r, err := svc.SetModuleMining(ctx, []int64{moduleID}, new(false), allow)
			if version == gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V5 {
				require.NoError(t, err)
				require.True(t, options.CanStart)
				require.True(t, options.CanStop)
				require.Len(t, r.Commands, 1)
			} else {
				require.True(t, fleeterror.IsFailedPreconditionError(err))
				require.False(t, options.CanStart)
				require.False(t, options.CanStop)
				require.Contains(t, options.UnavailableReason, "protocol version 5")
				require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
			}
		})
	}
}
func seedCommandModule(t *testing.T, conn *sql.DB, tank int64, position int, identity string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at,state) VALUES($1,$2,$3,now(),now(),'hashing') RETURNING id`, tank, position, identity).Scan(&id))
	return id
}

func TestModuleMiningOptionsValidateIdentityPerController(t *testing.T) {
	for _, sameController := range []bool{true, false} {
		name := "different controllers"
		if sameController {
			name = "same controller"
		}
		t.Run(name, func(t *testing.T) {
			conn, dbs, user := setupRetentionTest(t)
			_, _, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
			secondTank := tanks[1]
			if !sameController {
				_, _, otherTanks := seedTankController(t, conn, dbs, user.OrganizationID)
				secondTank = otherTanks[0]
			}
			ids := []int64{
				seedCommandModule(t, conn, tanks[0], 1, "shared-identity"),
				seedCommandModule(t, conn, secondTank, 1, "shared-identity"),
			}
			svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
			svc.SetPluginCapabilitiesProvider(moduleCapabilities{true})
			ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
			allow := func(context.Context, *int64, bool) error { return nil }
			options, err := svc.GetModuleMiningOptions(ctx, ids, allow)
			require.NoError(t, err)
			require.Equal(t, !sameController, options.CanStart)
			require.Equal(t, !sameController, options.CanStop)
			if sameController {
				require.Contains(t, options.UnavailableReason, "ambiguous")
				_, err = svc.SetModuleMining(ctx, ids, new(false), allow)
				require.True(t, fleeterror.IsFailedPreconditionError(err))
				require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
			} else {
				require.Empty(t, options.UnavailableReason)
				result, err := svc.SetModuleMining(ctx, ids, new(false), allow)
				require.NoError(t, err)
				require.Len(t, result.Commands, 2)
			}
		})
	}
}

func TestModuleMiningAdmissionGroupsExactScopeAcrossTanksAndControllers(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	identifier, deviceID, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
	_, otherDevice, otherTanks := seedTankController(t, conn, dbs, user.OrganizationID)
	m1 := seedCommandModule(t, conn, tanks[0], 1, "module-a")
	m2 := seedCommandModule(t, conn, tanks[1], 1, "module-b")
	m3 := seedCommandModule(t, conn, otherTanks[0], 1, "module-c")
	_ = seedCommandModule(t, conn, tanks[0], 2, "neighbor")
	svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
	svc.SetPluginCapabilitiesProvider(moduleCapabilities{true})
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	allow := func(context.Context, *int64, bool) error { return nil }
	denied := func(context.Context, *int64, bool) error { return fleeterror.NewForbiddenError("denied") }
	_, err := svc.SetModuleMining(ctx, []int64{m1, m3}, new(false), denied)
	require.Error(t, err)
	require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
	foreign := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID+100)
	_, err = svc.SetModuleMining(foreign, []int64{m1}, new(false), allow)
	require.True(t, fleeterror.IsNotFoundError(err))
	svc.SetPluginCapabilitiesProvider(moduleCapabilities{false})
	options, err := svc.GetModuleMiningOptions(ctx, []int64{m1}, allow)
	require.NoError(t, err)
	require.False(t, options.CanStart)
	_, err = svc.SetModuleMining(ctx, []int64{m1}, new(false), allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	svc.SetPluginCapabilitiesProvider(moduleCapabilities{true})
	_, err = conn.ExecContext(t.Context(), `UPDATE container_module SET last_seen_at=now()-interval '3 minutes' WHERE id=$1`, m2)
	require.NoError(t, err)
	_, err = svc.SetModuleMining(ctx, []int64{m1, m2}, new(false), allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
	_, err = conn.ExecContext(t.Context(), `UPDATE container_module SET last_seen_at=now() WHERE id=$1`, m2)
	require.NoError(t, err)
	r, err := svc.SetModuleMining(ctx, []int64{m3, m2, m1, m1}, new(false), allow)
	require.NoError(t, err)
	require.Len(t, r.Commands, 2)
	require.Equal(t, []int64{m1, m2}, r.Commands[0].ModuleIds)
	rows, err := conn.QueryContext(t.Context(), `SELECT device_id,payload FROM queue_message WHERE command_type='SetModuleMining' ORDER BY device_id`)
	require.NoError(t, err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id int64
		var raw []byte
		require.NoError(t, rows.Scan(&id, &raw))
		var payload sdk.ModuleMiningRequest
		require.NoError(t, json.Unmarshal(raw, &payload))
		require.False(t, *payload.Mining)
		if id == deviceID {
			require.Equal(t, []sdk.ModuleMiningTarget{{Tank: 1, Module: 1, ReportedID: "module-a"}, {Tank: 2, Module: 1, ReportedID: "module-b"}}, payload.Modules)
		} else {
			require.Equal(t, otherDevice, id)
			require.Equal(t, []sdk.ModuleMiningTarget{{Tank: 1, Module: 1, ReportedID: "module-c"}}, payload.Modules)
		}
		count++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 2, count)
	_, err = svc.SetTankMining(ctx, tanks[:1], new(true), allow)
	require.ErrorContains(t, err, "already pending")
	_, err = svc.SetModuleMining(ctx, []int64{m1}, new(true), allow)
	require.ErrorContains(t, err, "already pending")
	result, err := svc.GetContainerControlResult(ctx, identifier, r.Commands[0].BatchIdentifier, func(context.Context, *int64) error { return nil })
	require.NoError(t, err)
	require.EqualValues(t, 1, result.TotalCount)
}

func TestScopedMiningPendingReasonCoversBothScopes(t *testing.T) {
	for _, pendingModule := range []bool{false, true} {
		name := "pending tank"
		if pendingModule {
			name = "pending module"
		}
		t.Run(name, func(t *testing.T) {
			conn, dbs, user := setupRetentionTest(t)
			_, _, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
			modules := []int64{seedCommandModule(t, conn, tanks[0], 2, "module-a")}
			svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
			svc.SetPluginCapabilitiesProvider(moduleCapabilities{true})
			ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
			allow := func(context.Context, *int64, bool) error { return nil }
			if pendingModule {
				_, err := svc.SetModuleMining(ctx, modules, new(false), allow)
				require.NoError(t, err)
			} else {
				_, err := svc.SetTankMining(ctx, tanks[:1], new(false), allow)
				require.NoError(t, err)
			}
			tankOptions, err := svc.GetTankMiningOptions(ctx, tanks[:1], allow)
			require.NoError(t, err)
			require.False(t, tankOptions.CanStart)
			require.False(t, tankOptions.CanStop)
			require.Equal(t, "A tank or module mining command is already pending for this container.", tankOptions.UnavailableReason)
			moduleOptions, err := svc.GetModuleMiningOptions(ctx, modules, allow)
			require.NoError(t, err)
			require.False(t, moduleOptions.CanStart)
			require.False(t, moduleOptions.CanStop)
			require.Equal(t, tankOptions.UnavailableReason, moduleOptions.UnavailableReason)
			_, err = svc.SetTankMining(ctx, tanks[:1], new(true), allow)
			require.ErrorContains(t, err, "a tank or module mining command is already pending for this container")
			_, err = svc.SetModuleMining(ctx, modules, new(true), allow)
			require.ErrorContains(t, err, "a tank or module mining command is already pending for this container")
		})
	}
}
