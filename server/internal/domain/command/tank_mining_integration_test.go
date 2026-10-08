package command_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type tankCapabilities struct{ supported bool }

func (p tankCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityTankMining: p.supported}
}
func seedTankController(t *testing.T, conn *sql.DB, dbs *testutil.DatabaseService, orgID int64) (string, int64, []int64) {
	t.Helper()
	device := dbs.CreateDevice(orgID, "proto")
	_, err := conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2,model='CT1' WHERE device_identifier=$1`, device.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, device.DatabaseID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_inventory(device_id,observed_at) VALUES($1,now())`, device.DatabaseID)
	require.NoError(t, err)
	tanks := []int64{}
	for _, position := range []int{1, 2, 3} {
		var id int64
		require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,$2,now(),now()) RETURNING id`, device.DatabaseID, position).Scan(&id))
		tanks = append(tanks, id)
	}
	return device.ID, device.DatabaseID, tanks
}
func TestTankMiningAdmissionGroupsExactSelectionAndRejectsStaleForeignAndDenied(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	identifier, deviceID, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
	_, otherDevice, other := seedTankController(t, conn, dbs, user.OrganizationID)
	svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
	svc.SetPluginCapabilitiesProvider(tankCapabilities{true})
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	allow := func(context.Context, *int64, bool) error { return nil }
	denied := func(context.Context, *int64, bool) error { return fleeterror.NewForbiddenError("denied") }
	_, err := svc.SetTankMining(ctx, []int64{tanks[0]}, new(false), denied)
	require.Error(t, err)
	require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
	foreign := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID+100)
	_, err = svc.SetTankMining(foreign, []int64{tanks[0]}, new(false), allow)
	require.True(t, fleeterror.IsNotFoundError(err))
	_, err = conn.ExecContext(t.Context(), `UPDATE container_tank SET last_seen_at=now()-interval '3 minutes' WHERE id=$1`, tanks[1])
	require.NoError(t, err)
	_, err = svc.SetTankMining(ctx, []int64{tanks[0], tanks[1]}, new(false), allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
	svc.SetPluginCapabilitiesProvider(tankCapabilities{false})
	options, err := svc.GetTankMiningOptions(ctx, []int64{tanks[0]}, allow)
	require.NoError(t, err)
	require.False(t, options.CanStart)
	require.NotEmpty(t, options.UnavailableReason)
	_, err = svc.SetTankMining(ctx, []int64{tanks[0]}, new(true), allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	svc.SetPluginCapabilitiesProvider(tankCapabilities{true})
	result, err := svc.SetTankMining(ctx, []int64{tanks[2], other[0], tanks[0], tanks[0]}, new(false), allow)
	require.NoError(t, err)
	require.Len(t, result.Commands, 2)
	require.Equal(t, identifier, result.Commands[0].DeviceIdentifier)
	require.Equal(t, []int64{tanks[0], tanks[2]}, result.Commands[0].TankIds)
	rows, err := conn.QueryContext(t.Context(), `SELECT device_id,payload FROM queue_message WHERE command_type='SetTankMining' ORDER BY device_id`)
	require.NoError(t, err)
	defer rows.Close()
	var seen int
	for rows.Next() {
		var id int64
		var raw []byte
		require.NoError(t, rows.Scan(&id, &raw))
		var payload struct {
			Positions []int32
			Mining    *bool
		}
		require.NoError(t, json.Unmarshal(raw, &payload))
		require.False(t, *payload.Mining)
		if id == deviceID {
			require.Equal(t, []int32{1, 3}, payload.Positions)
		} else {
			require.Equal(t, otherDevice, id)
			require.Equal(t, []int32{1}, payload.Positions)
		}
		seen++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 2, seen)
	_, err = svc.SetTankMining(ctx, []int64{tanks[0]}, new(true), allow)
	require.ErrorContains(t, err, "already pending")
	results, err := svc.GetContainerControlResult(ctx, identifier, result.Commands[0].BatchIdentifier, func(context.Context, *int64) error { return nil })
	require.NoError(t, err)
	require.Equal(t, int32(1), results.TotalCount)
}

func TestTankMiningOptionsExplainMixedSitePermissions(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	_, northDevice, northTanks := seedTankController(t, conn, dbs, user.OrganizationID)
	_, southDevice, southTanks := seedTankController(t, conn, dbs, user.OrganizationID)
	q := sqlc.New(conn)
	north, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: user.OrganizationID, Name: "North", Slug: "north"})
	require.NoError(t, err)
	south, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: user.OrganizationID, Name: "South", Slug: "south"})
	require.NoError(t, err)
	for _, target := range []struct{ device, site int64 }{{northDevice, north.ID}, {southDevice, south.ID}} {
		_, err := conn.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, target.site, target.device)
		require.NoError(t, err)
	}
	svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
	svc.SetPluginCapabilitiesProvider(tankCapabilities{true})
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	authorize := func(_ context.Context, site *int64, mining bool) error {
		if site != nil && ((*site == north.ID && mining) || (*site == south.ID && !mining)) {
			return nil
		}
		return fleeterror.NewForbiddenError("action denied at this site")
	}
	for _, selection := range []struct {
		ids   []int64
		start bool
		stop  bool
	}{
		{[]int64{northTanks[0]}, true, false},
		{[]int64{southTanks[0]}, false, true},
		{[]int64{northTanks[0], southTanks[0]}, false, false},
	} {
		options, err := svc.GetTankMiningOptions(ctx, selection.ids, authorize)
		require.NoError(t, err)
		require.Equal(t, selection.start, options.CanStart)
		require.Equal(t, selection.stop, options.CanStop)
		if selection.start || selection.stop {
			require.Empty(t, options.UnavailableReason)
		} else {
			require.Equal(t, "No mining action is authorized for every selected tank.", options.UnavailableReason)
		}
	}
}

func bindTankControllerToFleetNode(t *testing.T, conn *sql.DB, orgID, deviceID int64) int64 {
	t.Helper()
	var nodeID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO fleet_node
		(org_id, name, identity_pubkey, encryption_pubkey, enrollment_status) VALUES ($1,$2,$3,$4,'CONFIRMED') RETURNING id`,
		orgID, fmt.Sprintf("tank-node-%d", deviceID), []byte(fmt.Sprintf("identity-%d", deviceID)), []byte("01234567890123456789012345678901")).Scan(&nodeID))
	_, err := conn.ExecContext(t.Context(), `INSERT INTO fleet_node_device (fleet_node_id,device_id,org_id) VALUES($1,$2,$3)`, nodeID, deviceID, orgID)
	require.NoError(t, err)
	return nodeID
}

func TestTankMiningProtocolAvailabilityChecksExactSelectedRoutes(t *testing.T) {
	for _, target := range []struct {
		name    string
		remote  bool
		version gatewaypb.CommandProtocolVersion
		allowed bool
	}{
		{name: "V2 remote requires upgrade", remote: true, version: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V2},
		{name: "V3 remote supported", remote: true, version: gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V3, allowed: true},
		{name: "local supported", allowed: true},
	} {
		t.Run(target.name, func(t *testing.T) {
			conn, dbs, user := setupRetentionTest(t)
			_, deviceID, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
			_, _, localTanks := seedTankController(t, conn, dbs, user.OrganizationID)
			_, unselectedDevice, _ := seedTankController(t, conn, dbs, user.OrganizationID)
			registry := control.NewRegistry()
			unselectedNode := bindTankControllerToFleetNode(t, conn, user.OrganizationID, unselectedDevice)
			unselectedStream, err := registry.RegisterAuthenticated(unselectedNode, "session", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V2)
			require.NoError(t, err)
			defer unselectedStream.Unregister()
			if target.remote {
				nodeID := bindTankControllerToFleetNode(t, conn, user.OrganizationID, deviceID)
				stream, err := registry.RegisterAuthenticated(nodeID, "session", target.version)
				require.NoError(t, err)
				defer stream.Unregister()
			}
			svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
			svc.SetPluginCapabilitiesProvider(tankCapabilities{true})
			svc.SetCommandProtocolChecker(registry)
			ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
			allow := func(context.Context, *int64, bool) error { return nil }
			selection := []int64{tanks[0], tanks[2], localTanks[0]}
			options, err := svc.GetTankMiningOptions(ctx, selection, allow)
			require.NoError(t, err)
			require.Equal(t, target.allowed, options.CanStart)
			require.Equal(t, target.allowed, options.CanStop)
			result, err := svc.SetTankMining(ctx, selection, new(false), allow)
			var queued int
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message WHERE command_type='SetTankMining'`).Scan(&queued))
			if target.allowed {
				require.NoError(t, err)
				require.Empty(t, options.UnavailableReason)
				require.Len(t, result.Commands, 2)
				require.Equal(t, 2, queued)
			} else {
				require.True(t, fleeterror.IsFailedPreconditionError(err))
				require.ErrorContains(t, err, "upgrade Fleet Node before retrying")
				require.Equal(t, "tank mining requires Fleet Node command protocol version 3; upgrade Fleet Node before retrying", options.UnavailableReason)
				require.Nil(t, result)
				require.Zero(t, queued, "one incompatible controller must reject the complete selection before enqueue")
				require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
			}
		})
	}
}

func TestTankMiningAdmissionRechecksProtocolAfterOptions(t *testing.T) {
	conn, dbs, user := setupRetentionTest(t)
	_, deviceID, tanks := seedTankController(t, conn, dbs, user.OrganizationID)
	registry := control.NewRegistry()
	nodeID := bindTankControllerToFleetNode(t, conn, user.OrganizationID, deviceID)
	stream, err := registry.RegisterAuthenticated(nodeID, "session", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V3)
	require.NoError(t, err)
	defer stream.Unregister()
	svc := newDispatchIntegrationTestService(t, conn, failingEnqueueQueue{delegate: queue.NewDatabaseMessageQueue(&queue.Config{DequeLimit: 10, MaxFailureRetries: 1}, conn)})
	svc.SetPluginCapabilitiesProvider(tankCapabilities{true})
	svc.SetCommandProtocolChecker(registry)
	ctx := testutil.MockAuthContextForTesting(t.Context(), user.DatabaseID, user.OrganizationID)
	allow := func(context.Context, *int64, bool) error { return nil }
	options, err := svc.GetTankMiningOptions(ctx, tanks[:1], allow)
	require.NoError(t, err)
	require.True(t, options.CanStart)
	legacy, err := registry.RegisterAuthenticated(nodeID, "session", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V2)
	require.NoError(t, err)
	defer legacy.Unregister()
	_, err = svc.SetTankMining(ctx, tanks[:1], new(true), allow)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.ErrorContains(t, err, "upgrade Fleet Node before retrying")
	require.Zero(t, countCommandBatches(t, conn, user.OrganizationID))
}
