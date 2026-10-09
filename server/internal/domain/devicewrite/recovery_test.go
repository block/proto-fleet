package devicewrite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestOfflineRecoveryBindsEvidenceAndRestoresReservationPath(t *testing.T) {
	database, org, device, message, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	uncertain := errors.New("physical acknowledgement missing")
	require.Error(t, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error { return uncertain }))
	require.NoError(t, attempt.Finish(t.Context(), uncertain))
	_, err = service.Reserve(t.Context(), org, device)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	inspections, err := service.InspectRecovery(t.Context(), org, device)
	require.NoError(t, err)
	require.Len(t, inspections, 1)
	i := inspections[0]
	evidence := devicewrite.RecoveryEvidence{AttemptID: i.Attempt.AttemptID, OrganizationID: org, DeviceID: device, AttemptEpoch: i.Attempt.Epoch, FenceEpoch: i.FenceEpoch, FenceState: i.FenceState, QuarantineOrigin: i.QuarantineOrigin, OwnerInstanceID: i.Attempt.OwnerInstanceID, OperationKind: i.Attempt.OperationKind, PriorStatus: i.Attempt.Status, IdentitySHA256: i.IdentitySHA256, Operator: "host operator", MaintenanceTermination: "supervisor receipt: all dispatchers stopped", OwnerTermination: "supervisor receipt: exact owner exited", TransportQuiescence: "node and plugin dispatchers stopped", DeviceQuiescence: "physical isolation and completed cold reset", ConfigurationVerification: "qualified verification receipt"}
	for _, field := range []string{"owner", "epoch", "device", "identity", "classification", "termination", "quiescence", "configuration"} {
		t.Run(field, func(t *testing.T) {
			bad := evidence
			switch field {
			case "owner":
				bad.OwnerInstanceID = uuid.New()
			case "epoch":
				bad.FenceEpoch++
			case "device":
				bad.DeviceID++
			case "identity":
				bad.IdentitySHA256 = "wrong"
			case "classification":
				bad.OperationKind = "observation"
			case "termination":
				bad.OwnerTermination = ""
			case "quiescence":
				bad.DeviceQuiescence = ""
			case "configuration":
				bad.ConfigurationVerification = ""
			}
			_, err := service.ResolveOffline(t.Context(), bad, true)
			require.Error(t, err)
			var count int
			require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_recovery`).Scan(&count))
			require.Zero(t, count)
		})
	}
	preview, err := service.ResolveOffline(t.Context(), evidence, false)
	require.NoError(t, err)
	require.True(t, preview.Preview)
	require.Equal(t, uuid.Nil, preview.RecoveryID)
	_, err = service.Reserve(t.Context(), org, device)
	require.Error(t, err, "preview cannot clear safety evidence")
	// Audit failure must roll back resolution and epoch advancement together.
	_, err = database.DB.ExecContext(t.Context(), `CREATE FUNCTION reject_recovery() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER reject_recovery BEFORE INSERT ON device_write_recovery FOR EACH ROW EXECUTE FUNCTION reject_recovery()`)
	require.NoError(t, err)
	_, err = service.ResolveOffline(t.Context(), evidence, true)
	require.ErrorContains(t, err, "audit unavailable")
	unchanged, err := service.InspectRecovery(t.Context(), org, device)
	require.NoError(t, err)
	require.Len(t, unchanged, 1)
	require.Equal(t, i.FenceEpoch, unchanged[0].FenceEpoch)
	_, err = database.DB.ExecContext(t.Context(), `DROP TRIGGER reject_recovery ON device_write_recovery`)
	require.NoError(t, err)
	result, err := service.ResolveOffline(t.Context(), evidence, true)
	require.NoError(t, err)
	require.False(t, result.Preview)
	require.NotEqual(t, uuid.Nil, result.RecoveryID)
	require.Equal(t, i.FenceEpoch+1, result.FenceEpoch)
	require.Error(t, attempt.Recheck(t.Context()), "resolved attempts and old epochs must never dispatch again")
	var status, actor, prior string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT a.status,r.database_actor,r.prior_status FROM device_write_attempt a JOIN device_write_recovery r USING(attempt_id) WHERE a.attempt_id=$1`, evidence.AttemptID).Scan(&status, &actor, &prior))
	require.Equal(t, "resolved", status)
	require.Equal(t, "unknown", prior)
	require.NotEmpty(t, actor)
	_, err = service.ResolveOffline(t.Context(), evidence, true)
	require.Error(t, err, "stale evidence cannot resolve twice")
	epoch, err := service.Reserve(t.Context(), org, device)
	require.NoError(t, err)
	drained, err := service.Drained(t.Context(), org, device, epoch)
	require.NoError(t, err)
	require.True(t, drained)
	require.NoError(t, service.ReleaseHost(t.Context(), org, device, epoch))
}

func TestOfflineRecoveryCanRestoreVerifiedQuarantinedHostCustody(t *testing.T) {
	for _, origin := range []string{"reserved", "active"} {
		t.Run(origin, func(t *testing.T) {
			database, org, device, message, _ := queuedFenceFixture(t)
			service := devicewrite.NewService(database.DB)
			attempt, err := service.BeginQueuedHost(t.Context(), message)
			require.NoError(t, err)
			uncertainty := errors.New("physical operation uncertain")
			require.Error(t, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error { return uncertainty }))
			_, err = service.Reserve(t.Context(), org, device)
			require.NoError(t, err)
			if origin == "active" {
				_, err = database.DB.ExecContext(t.Context(), `UPDATE device_write_fence SET state='active' WHERE device_id=$1`, device)
				require.NoError(t, err)
			}
			require.NoError(t, attempt.Finish(t.Context(), uncertainty))
			rows, err := service.InspectRecovery(t.Context(), org, device)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			i := rows[0]
			require.Equal(t, "quarantined", i.FenceState)
			evidence := devicewrite.RecoveryEvidence{AttemptID: i.Attempt.AttemptID, OrganizationID: org, DeviceID: device, AttemptEpoch: i.Attempt.Epoch, FenceEpoch: i.FenceEpoch, FenceState: i.FenceState, QuarantineOrigin: i.QuarantineOrigin, OwnerInstanceID: i.Attempt.OwnerInstanceID, OperationKind: i.Attempt.OperationKind, PriorStatus: i.Attempt.Status, IdentitySHA256: i.IdentitySHA256, Operator: "host operator", MaintenanceTermination: "all dispatchers stopped", OwnerTermination: "exact process terminated", TransportQuiescence: "all transport work ended", DeviceQuiescence: "device jobs conclusively ended", ConfigurationVerification: "host destination and exact credentials verified", RestoreHost: true}
			result, err := service.ResolveOffline(t.Context(), evidence, true)
			if origin == "active" {
				require.True(t, fleeterror.IsFailedPreconditionError(err))
				var count int
				require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_recovery`).Scan(&count))
				require.Zero(t, count)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "host", result.FenceState)
			require.Error(t, attempt.Recheck(t.Context()))
			fence, err := sqlc.New(database.DB).LockDeviceWriteFenceRecord(t.Context(), sqlc.LockDeviceWriteFenceRecordParams{DeviceID: device, OrganizationID: org})
			require.NoError(t, err)
			require.Equal(t, "host", fence.State)
		})
	}
}
