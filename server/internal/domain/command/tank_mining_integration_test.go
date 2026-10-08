package command_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
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
