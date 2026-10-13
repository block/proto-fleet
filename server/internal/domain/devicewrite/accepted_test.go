package devicewrite_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"

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

func TestOrderedQueuedWritesRetireWithoutOfflineRecovery(t *testing.T) {
	for _, kind := range []commandtype.Type{commandtype.UpdateMiningPools, commandtype.UpdateMinerPassword, commandtype.StartMining, commandtype.StopMining, commandtype.SetCoolingMode, commandtype.SetPowerTarget, commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig, commandtype.SetTankMining, commandtype.SetModuleMining, commandtype.SetContainerControl} {
		t.Run(kind.String(), func(t *testing.T) {
			for _, lostResponse := range []bool{false, true} {
				t.Run(map[bool]string{false: "success", true: "lost response"}[lostResponse], func(t *testing.T) {
					database, org, device, message, _ := queuedFenceFixture(t, kind)
					service := devicewrite.NewService(database.DB)
					attempt, err := service.BeginQueuedHost(t.Context(), message)
					require.NoError(t, err)
					var outcome error
					if lostResponse {
						outcome = context.DeadlineExceeded
					}
					require.Equal(t, outcome, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error { return outcome }))
					require.NoError(t, attempt.Finish(t.Context(), outcome))
					var state string
					require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE device_id=$1", device).Scan(&state))
					require.Equal(t, "complete", state)
					epoch, err := service.Reserve(t.Context(), org, device)
					require.NoError(t, err)
					drained, err := service.Drained(t.Context(), org, device, epoch)
					require.NoError(t, err)
					require.True(t, drained)
					inspections, err := service.InspectRecovery(t.Context(), org, device)
					require.NoError(t, err)
					require.Empty(t, inspections)
				})
			}
		})
	}
}

func TestOrderedQueuedWriteDrainsOnlyAfterCanceledCallReturns(t *testing.T) {
	database, org, device, message, _ := queuedFenceFixture(t, commandtype.UpdateMiningPools)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	done := make(chan error, 1)
	go func() {
		done <- devicewrite.Dispatch(attempt.Context(ctx), func(context.Context) error { close(entered); <-release; return context.DeadlineExceeded })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("configuration call did not start")
	}
	cancel()
	epoch, err := service.Reserve(t.Context(), org, device)
	require.NoError(t, err)
	drained, err := service.Drained(t.Context(), org, device, epoch)
	require.NoError(t, err)
	require.False(t, drained, "cancellation cannot release a call that is still executing")
	require.Error(t, service.ReleaseHost(t.Context(), org, device, epoch))
	finish()
	outcome := <-done
	require.ErrorIs(t, outcome, context.DeadlineExceeded)
	require.NoError(t, attempt.Finish(t.Context(), outcome))
	drained, err = service.Drained(t.Context(), org, device, epoch)
	require.NoError(t, err)
	require.True(t, drained)
	require.NoError(t, service.ReleaseHost(t.Context(), org, device, epoch))
	_, err = service.BeginQueuedHost(t.Context(), message)
	require.Error(t, err, "the previous owner's queued retry must fail its epoch check")
}

func TestNestedWriteCompletionPolicy(t *testing.T) {
	for _, nestedKind := range []commandtype.Type{commandtype.Reboot, commandtype.FirmwareUpdate, commandtype.Type(-1)} {
		t.Run(nestedKind.String(), func(t *testing.T) {
			database, org, device, message, _ := queuedFenceFixture(t, commandtype.UpdateMiningPools)
			service := devicewrite.NewService(database.DB)
			attempt, err := service.BeginQueuedHost(t.Context(), message)
			require.NoError(t, err)
			ctx := attempt.Context(t.Context())
			call := func(ctx context.Context) error {
				return devicewrite.Dispatch(ctx, func(context.Context) error { return nil })
			}
			if nestedKind == commandtype.Type(-1) {
				err = service.RunHost(ctx, org, device, message.WriteEpoch, func(context.Context) error { return nil })
			} else {
				err = service.WithHostAttempt(ctx, org, device, message.WriteEpoch, nestedKind, call)
			}
			require.NoError(t, err)
			require.NoError(t, attempt.Finish(t.Context(), nil))
			var state string
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status FROM device_write_attempt WHERE device_id=$1", device).Scan(&state))
			_, err = service.Reserve(t.Context(), org, device)
			if nestedKind == commandtype.Type(-1) {
				require.Equal(t, "complete", state)
				require.NoError(t, err)
			} else {
				require.Equal(t, "unknown", state)
				require.Error(t, err)
			}
		})
	}
}
