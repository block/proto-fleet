package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
)

func TestAcceptedMutationRequiresAuditedCompletionBeforeTransfer(t *testing.T) {
	database, org, device, message, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	require.NoError(t, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error { return nil }))
	require.NoError(t, attempt.Finish(t.Context(), nil))
	require.Error(t, attempt.Finish(t.Context(), nil), "duplicate acknowledgement must not clear the attempt")
	inspections, err := service.InspectRecovery(t.Context(), org, device)
	require.NoError(t, err)
	require.Len(t, inspections, 1)
	i := inspections[0]
	require.Equal(t, "unknown", i.Attempt.Status)
	require.Equal(t, "host", i.FenceState, "acceptance must not quarantine normal host operation")
	// Another host request can be accepted without establishing completion of its predecessor.
	next, err := service.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	require.NoError(t, devicewrite.Dispatch(next.Context(t.Context()), func(context.Context) error { return nil }))
	require.NoError(t, next.Finish(t.Context(), nil))
	_, err = service.Reserve(t.Context(), org, device)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	inspections, err = service.InspectRecovery(t.Context(), org, device)
	require.NoError(t, err)
	require.Len(t, inspections, 2)
	for range inspections {
		current, err := service.InspectRecovery(t.Context(), org, device)
		require.NoError(t, err)
		i := current[0]
		evidence := devicewrite.RecoveryEvidence{AttemptID: i.Attempt.AttemptID, OrganizationID: org, DeviceID: device, AttemptEpoch: i.Attempt.Epoch, FenceEpoch: i.FenceEpoch, FenceState: i.FenceState, QuarantineOrigin: i.QuarantineOrigin, OwnerInstanceID: i.Attempt.OwnerInstanceID, OperationKind: i.Attempt.OperationKind, PriorStatus: i.Attempt.Status, IdentitySHA256: i.IdentitySHA256, Operator: "host operator", MaintenanceTermination: "supervisor: all dispatchers stopped", OwnerTermination: "supervisor: exact owner exited", TransportQuiescence: "transport queues drained", DeviceQuiescence: "authoritative completed device jobs", ConfigurationVerification: "qualified configuration verification"}
		_, err = service.ResolveOffline(t.Context(), evidence, true)
		require.NoError(t, err)
		if len(current) > 1 {
			_, err = service.Reserve(t.Context(), org, device)
			require.Error(t, err, "resolving one accepted mutation cannot clear another")
		}
	}
	var audits int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_recovery").Scan(&audits))
	require.Equal(t, 2, audits)
	epoch, err := service.Reserve(t.Context(), org, device)
	require.NoError(t, err)
	drained, err := service.Drained(t.Context(), org, device, epoch)
	require.NoError(t, err)
	require.True(t, drained)
	require.NoError(t, service.ReleaseHost(t.Context(), org, device, epoch))
}
