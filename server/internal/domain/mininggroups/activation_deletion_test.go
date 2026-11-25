package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSoftDeleteDevicesRejectsPendingActivationPoolIntent(t *testing.T) {
	for _, kind := range []string{"retained container", "partial board transfer"} {
		t.Run(kind, func(t *testing.T) {
			ctx, f, _, _, _, boards, _ := newActivationPoolFixture(t)
			q := sqlc.New(f.conn)
			if kind == "partial board transfer" {
				completePendingActivation(t, ctx, f, boards[0])
			} else {
				// A completed activation from before partial transfers retains its container assignment.
				_, err := f.conn.ExecContext(ctx, `WITH request AS (
     INSERT INTO mining_group_request(organization_id,request_id,device_id,fingerprint)
     VALUES($1,gen_random_uuid(),$2,'deletion-test') RETURNING *)
     INSERT INTO mining_group_pool_handoff(organization_id,operation_id,device_id,captured,placement_pending)
     SELECT organization_id,request_id,device_id,'{}',true FROM request`, f.target.OrganizationID, f.target.DeviceID)
				require.NoError(t, err)
			}
			other := uuid.NewString()
			discovery, err := q.UpsertDiscoveredDevice(ctx, sqlc.UpsertDiscoveredDeviceParams{
				OrgID: f.target.OrganizationID, DeviceIdentifier: other, IpAddress: "127.0.254.2", Port: "8080", UrlScheme: "http", IsActive: true, DriverName: "proto",
			})
			require.NoError(t, err)
			_, err = q.InsertDevice(ctx, sqlc.InsertDeviceParams{OrgID: f.target.OrganizationID, DiscoveredDeviceID: discovery, DeviceIdentifier: other, MacAddress: "02:00:00:00:fe:02"})
			require.NoError(t, err)
			var assignmentsBefore, assignmentsAfter string
			assignmentState := `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY target_kind,target_id),'[]')::text FROM pool_configuration_assignment a WHERE device_id=$1`
			require.NoError(t, f.conn.QueryRowContext(ctx, assignmentState, f.target.DeviceID).Scan(&assignmentsBefore))
			count, err := sqlstores.NewSQLDeviceStore(f.conn).SoftDeleteDevices(ctx, []string{other, f.target.DeviceIdentifier}, f.target.OrganizationID)
			require.True(t, fleeterror.IsFailedPreconditionError(err), "pending pool intent must produce an actionable precondition error: %v", err)
			require.ErrorContains(t, err, "pool")
			require.Zero(t, count)
			var devices, discoveries, permits int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device WHERE org_id=$1 AND device_identifier IN ($2,$3) AND deleted_at IS NULL`, f.target.OrganizationID, other, f.target.DeviceIdentifier).Scan(&devices))
			require.Equal(t, 2, devices, "the entire deletion batch must roll back")
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND device_identifier IN ($2,$3) AND deleted_at IS NULL`, f.target.OrganizationID, other, f.target.DeviceIdentifier).Scan(&discoveries))
			require.Equal(t, 2, discoveries)
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_deletion_unpair_permit WHERE organization_id=$1`, f.target.OrganizationID).Scan(&permits))
			require.Zero(t, permits)
			require.NoError(t, f.conn.QueryRowContext(ctx, assignmentState, f.target.DeviceID).Scan(&assignmentsAfter))
			require.JSONEq(t, assignmentsBefore, assignmentsAfter, "deletion must preserve every assignment and its version")
			pending, err := q.HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			require.True(t, pending)
			if kind == "partial board transfer" {
				intents, err := q.ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
				require.NoError(t, err)
				require.Len(t, intents, 1)
				require.Equal(t, boards[0], intents[0].HashboardID)
			}
		})
	}
}
