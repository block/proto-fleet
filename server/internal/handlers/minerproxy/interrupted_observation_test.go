package minerproxy

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestInterruptedObservationRecoveryAllowsRetiredIdentityReuse(t *testing.T) {
	for _, failure := range []string{"process interrupted", "completion failed"} {
		t.Run(failure, func(t *testing.T) {
			database, orgID, deviceID, h, target := proxyFenceFixture(t)
			if failure == "completion failed" {
				_, err := database.DB.ExecContext(t.Context(), `CREATE FUNCTION reject_read_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected finish failure'; END $$; CREATE TRIGGER reject_read_finish BEFORE UPDATE ON device_write_attempt FOR EACH ROW EXECUTE FUNCTION reject_read_finish()`)
				require.NoError(t, err)
			}
			called := false
			operation := func(ctx context.Context) error {
				// Inspect before transport: classification and owner must already be committed.
				inspections, err := h.writeService.InspectRecovery(ctx, orgID, deviceID)
				require.NoError(t, err)
				require.Len(t, inspections, 1)
				require.Equal(t, "observation", inspections[0].Attempt.OperationKind)
				require.NotEqual(t, uuid.Nil, inspections[0].Attempt.OwnerInstanceID)
				require.Error(t, devicewrite.Dispatch(ctx, func(context.Context) error { called = true; return nil }))
				require.False(t, called, "read-only admission cannot be upgraded into a mutation")
				if failure == "process interrupted" {
					panic("simulated interrupted process")
				}
				return devicewrite.Observe(ctx, func(context.Context) error { return nil })
			}
			if failure == "process interrupted" {
				require.Panics(t, func() {
					_ = h.writeService.WithHostObservation(t.Context(), orgID, deviceID, target.binding.WriteEpoch, operation)
				})
			} else {
				err := h.writeService.WithHostObservation(t.Context(), orgID, deviceID, target.binding.WriteEpoch, operation)
				require.ErrorContains(t, err, "injected finish failure")
				_, err = database.DB.ExecContext(t.Context(), "DROP TRIGGER reject_read_finish ON device_write_attempt")
				require.NoError(t, err)
			}
			require.Equal(t, "running", proxyAttemptStatus(t, database, deviceID))
			successorID := retireAndRediscoverProxyIdentity(t, database, target)
			_, err := h.writeService.BeginHost(t.Context(), orgID, successorID, 0)
			require.Error(t, err, "interrupted read remains fenced until authoritative operator recovery")
			inspections, err := h.writeService.InspectRecovery(t.Context(), orgID, deviceID)
			require.NoError(t, err)
			require.Len(t, inspections, 1)
			evidence := inspections[0].EvidenceTemplate
			evidence.Operator = "test host operator"
			evidence.MaintenanceTermination = "test receipt: every dispatcher stopped"
			_, err = h.writeService.ResolveOffline(t.Context(), evidence, true)
			require.Error(t, err, "elapsed time or retirement is not owner termination evidence")
			evidence.OwnerTermination = "test receipt: exact process owner terminated"
			_, err = h.writeService.ResolveOffline(t.Context(), evidence, true)
			require.NoError(t, err, "durably read-only admission needs no mutation outcome evidence")
			attempt, err := h.writeService.BeginHost(t.Context(), orgID, successorID, 0)
			require.NoError(t, err)
			require.NoError(t, attempt.Finish(t.Context(), nil))
			var audits int
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_recovery WHERE attempt_id=$1", evidence.AttemptID).Scan(&audits))
			require.Equal(t, 1, audits)
		})
	}
}
