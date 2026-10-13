package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestReportedIdentityCannotFenceAnotherOrganization(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	attacker := database.CreateSuperAdminUser()
	victim := database.CreateSuperAdminUser2()
	retired := database.CreateDevice(attacker.OrganizationID, "proto")
	victimDevice := database.CreateDevice(victim.OrganizationID, "proto")
	successor := database.CreateDevice(attacker.OrganizationID, "proto")
	// Both the spoofed and legitimate reports can use the same MAC/IP; serials remain distinct.
	_, err = database.DB.ExecContext(t.Context(), `UPDATE device SET serial_number='reported-serial-'||id::text,mac_address='AA:BB:CC:DD:EE:FF' WHERE id=ANY($1::bigint[])`, []int64{retired.DatabaseID, victimDevice.DatabaseID, successor.DatabaseID})
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), `UPDATE discovered_device SET ip_address='192.0.2.22',port='8080' WHERE id IN(SELECT discovered_device_id FROM device WHERE id=ANY($1::bigint[]))`, []int64{retired.DatabaseID, victimDevice.DatabaseID, successor.DatabaseID})
	require.NoError(t, err)
	var nodeID int64
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,'spoof-report-node',$2,$3,'CONFIRMED') RETURNING id`, attacker.OrganizationID, []byte("fixture-identity"), []byte("01234567890123456789012345678901")).Scan(&nodeID))
	_, err = database.DB.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)", nodeID, retired.DatabaseID, attacker.OrganizationID)
	require.NoError(t, err)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginHost(t.Context(), attacker.OrganizationID, retired.DatabaseID, 0)
	require.NoError(t, err)
	require.NoError(t, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error { return nil }))
	require.NoError(t, attempt.Finish(t.Context(), nil)) // acceptance retains unknown evidence
	_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", retired.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), "DELETE FROM fleet_node_device WHERE device_id=$1", retired.DatabaseID)
	require.NoError(t, err)
	victimAttempt, err := service.BeginHost(t.Context(), victim.OrganizationID, victimDevice.DatabaseID, 0)
	require.NoError(t, err, "a node-reported identity must not deny another organization")
	require.NoError(t, victimAttempt.Finish(t.Context(), nil))
	_, err = service.BeginHost(t.Context(), attacker.OrganizationID, successor.DatabaseID, 0)
	require.Error(t, err, "same-organization rediscovery must retain the old physical fence across node scopes")
	_, err = service.Reserve(t.Context(), attacker.OrganizationID, successor.DatabaseID)
	require.Error(t, err)
	keys, err := sqlc.New(database.DB).GetDeviceWriteIdentityKeys(t.Context(), successor.DatabaseID)
	require.NoError(t, err)
	drained, err := sqlc.New(database.DB).IsRetiredDeviceWriteDrained(t.Context(), keys)
	require.NoError(t, err)
	require.False(t, drained)
}
