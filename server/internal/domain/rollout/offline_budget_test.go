package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/require"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

// queuedBudgetDispatcher controls miner execution while persisting the real
// queue state in the same transaction as rollout dispatch bookkeeping.
type queuedBudgetDispatcher struct {
	f *fixture
}

func useQueuedBudgetDispatcher(f *fixture) {
	f.svc.commands = &queuedBudgetDispatcher{f: f}
}

func (d *queuedBudgetDispatcher) FirmwareUpdateArtifact(ctx context.Context, selector *commandpb.DeviceSelector, checksum string, metadata files.FirmwareMetadata) (*command.CommandResult, error) {
	result, err := d.f.dispatcher.FirmwareUpdateArtifact(ctx, selector, checksum, metadata)
	if err != nil {
		return nil, err
	}
	result.BatchIdentifier = fmt.Sprintf("00000000-0000-0000-0000-%012d", len(d.f.dispatcher.sent))
	payload, err := json.Marshal(map[string]string{"firmware_checksum": checksum})
	if err != nil {
		return nil, fmt.Errorf("marshal queued firmware payload: %w", err)
	}
	for _, identifier := range result.DispatchedDeviceIdentifiers {
		if err := d.f.svc.store.GetQueries(ctx).CreateQueueMessage(ctx, sqlc.CreateQueueMessageParams{
			CommandBatchLogUuid: result.BatchIdentifier, CommandType: "FirmwareUpdate",
			DeviceID: d.f.deviceIDs[identifier], Status: sqlc.QueueStatusEnumPENDING,
			Payload: pqtype.NullRawMessage{RawMessage: payload, Valid: true},
		}); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (f *fixture) finishQueuedBudgetCommand(t *testing.T, identifier, status string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `UPDATE queue_message SET status = $1::queue_status_enum WHERE device_id = $2`, status, f.deviceIDs[identifier])
	require.NoError(t, err)
}

func TestOfflineBudgetRetainsLongRunningCommand(t *testing.T) {
	for _, status := range []string{"PENDING", "PROCESSING"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, 2)
			useQueuedBudgetDispatcher(f)
			f.channel(t, Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: 1}, f.allMiners()...)
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			f.finishQueuedBudgetCommand(t, "miner-0", status)
			_, err := f.conn.ExecContext(t.Context(), `UPDATE firmware_rollout_device SET attempts = $1, last_sent_at = now() - INTERVAL '1 hour' WHERE rollout_id = $2 AND device_id = $3`, MaxAttempts, started.ID, f.deviceIDs["miner-0"])
			require.NoError(t, err)
			for range 3 {
				f.svc.EnforceTick(t.Context())
			}
			current := f.rollout(t, started.ID)
			require.Equal(t, PhaseRetrying, phaseOf(current, "miner-0"), "a live command cannot exhaust attempts")
			require.Equal(t, PhaseQueued, phaseOf(current, "miner-1"), "age and online observations cannot release its reservation")
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "no duplicate dispatch and no dispatch above the budget")

			// Failure is terminal and this miner never went offline. The next
			// miner can use the released slot, and only now may attempts exhaust.
			f.finishQueuedBudgetCommand(t, "miner-0", "FAILED")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, PhaseFailed, phaseOf(f.rollout(t, started.ID), "miner-0"))
			require.Equal(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers())
		})
	}
}

func TestExcludedOfflineTargetKeepsChannelSlot(t *testing.T) {
	f := newFixture(t, 2)
	useQueuedBudgetDispatcher(f)
	behavior := Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: 1}
	f.channel(t, behavior, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.setStatus(t, "miner-0", "OFFLINE")
	f.svc.EnforceTick(t.Context())
	_, err := f.svc.UpdateChannel(t.Context(), f.orgID, f.channelID, ChannelSpec{
		Name: "Test channel", Scope: Scope{DeviceIdentifiers: []string{"miner-1"}}, Behavior: behavior,
	})
	require.NoError(t, err)
	f.finishQueuedBudgetCommand(t, "miner-0", "SUCCESS")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, PhaseExcluded, phaseOf(f.rollout(t, started.ID), "miner-0"))
	require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "excluded offline targets still consume the shared limit")
	f.setStatus(t, "miner-0", "ACTIVE")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers(), "observing recovery releases the slot")
}

func TestPausedRolloutObservesReservationRecovery(t *testing.T) {
	f := newFixture(t, 2)
	useQueuedBudgetDispatcher(f)
	f.channel(t, Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: 1}, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	paused, err := f.svc.PauseRollout(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	f.setStatus(t, "miner-0", "OFFLINE")
	f.svc.EnforceTick(t.Context())
	f.setStatus(t, "miner-0", "ACTIVE")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, paused.Revision, f.rollout(t, started.ID).Revision, "reservation observations do not rewrite rollout history")
	require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
	_, err = f.svc.ResumeRollout(t.Context(), f.orgID, started.ID, byOperator)
	require.NoError(t, err)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers(), "the offline/online cycle while paused released its slot, although the command still runs")
}

func TestTerminalRolloutKeepsReservationUntilRelease(t *testing.T) {
	for _, end := range []string{"canceled", "completed", "failed", "deleted"} {
		t.Run(end, func(t *testing.T) {
			f := newFixture(t, 1)
			f.addMiner(t, "miner-1", "Other")
			useQueuedBudgetDispatcher(f)
			f.files.artifacts["fw-other"] = files.FirmwareArtifact{
				FileID: "fw-other", Checksum: checksum1,
				Metadata: files.FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Other", FirmwareVersion: "3.0.0"},
			}
			f.channel(t, Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: 1}, f.allMiners()...)
			started := f.apply(t, "fw-2")
			_, err := f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{{Manufacturer: "Proto", Model: "Other", FirmwareFileID: "fw-other"}}, nil)
			require.NoError(t, err)
			f.svc.EnforceTick(t.Context())
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			switch end {
			case "completed":
				f.finishUpdate(t, "miner-0", "2.0.0")
				f.svc.EnforceTick(t.Context())
				require.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
			case "failed":
				f.setStatus(t, "miner-0", "OFFLINE")
				f.finishQueuedBudgetCommand(t, "miner-0", "FAILED")
				_, err = f.conn.ExecContext(t.Context(), `UPDATE firmware_rollout_device SET attempts = $1, last_sent_at = now() - INTERVAL '1 hour' WHERE rollout_id = $2`, MaxAttempts, started.ID)
				require.NoError(t, err)
				f.svc.EnforceTick(t.Context())
				require.Equal(t, StatusCompletedWithFailures, f.rollout(t, started.ID).Status)
			default:
				_, _, err = f.svc.CancelRollout(t.Context(), f.orgID, started.ID, byOperator)
				require.NoError(t, err)
			}
			f.backdateSends(t)
			f.svc.EnforceTick(t.Context())
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "terminal rollout history retains its live command reservation")
			f.setStatus(t, "miner-0", "OFFLINE")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			if end == "deleted" {
				softDeleteSelectedMiner(t, f, f.deviceIDs["miner-0"])
			} else {
				f.setStatus(t, "miner-0", "ACTIVE")
			}
			f.svc.EnforceTick(t.Context())
			require.Equal(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers(), "recovery or deletion releases the slot even while the old command is pending")
		})
	}
}
