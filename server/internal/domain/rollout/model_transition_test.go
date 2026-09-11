package rollout

import (
	"context"
	"fmt"
	"testing"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/stretchr/testify/require"
)

func setModelTransition(t *testing.T, f *fixture, manufacturer, model string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `UPDATE discovered_device SET manufacturer = $1, model = $2
		WHERE id = (SELECT discovered_device_id FROM device WHERE id = $3)`, manufacturer, model, f.deviceIDs["miner-0"])
	require.NoError(t, err)
}

// auditedModelDispatcher keeps the existing fake command transport and records
// the same durable batch identity the real dispatcher returns. Tests control
// device outcomes separately from enqueueing and reported firmware versions.
type auditedModelDispatcher struct {
	t       *testing.T
	f       *fixture
	userID  int64
	batchID int64
	calls   int
}

func useAuditedModelDispatcher(t *testing.T, f *fixture) *auditedModelDispatcher {
	t.Helper()
	d := &auditedModelDispatcher{t: t, f: f}
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `INSERT INTO "user" (user_id, username, password_hash)
		VALUES ('model-dispatch-user', 'model-dispatch-user', 'test') RETURNING id`).Scan(&d.userID))
	f.svc.commands = d
	return d
}

func (d *auditedModelDispatcher) FirmwareUpdateArtifact(ctx context.Context, selector *commandpb.DeviceSelector, checksum string, metadata files.FirmwareMetadata) (*command.CommandResult, error) {
	d.calls++
	result, err := d.f.dispatcher.FirmwareUpdateArtifact(ctx, selector, checksum, metadata)
	require.NoError(d.t, err)
	result.BatchIdentifier = fmt.Sprintf("00000000-0000-0000-0000-%012d", d.calls)
	require.NoError(d.t, d.f.conn.QueryRowContext(ctx, `INSERT INTO command_batch_log
		(uuid, type, created_by, status, devices_count, payload, organization_id)
		VALUES ($1, 'FirmwareUpdate', $2, 'PROCESSING', $3, jsonb_build_object('firmware_checksum', $4::text), $5)
		RETURNING id`, result.BatchIdentifier, d.userID, result.DispatchedCount, checksum, d.f.orgID).Scan(&d.batchID))
	return result, nil
}

func (d *auditedModelDispatcher) finish(identifier, status string) {
	d.t.Helper()
	_, err := d.f.conn.ExecContext(d.t.Context(), `INSERT INTO command_on_device_log
		(command_batch_log_id, device_id, status, org_id) VALUES ($1, $2, $3::device_command_status_enum, $4)
		ON CONFLICT (command_batch_log_id, device_id) DO UPDATE SET status = EXCLUDED.status`,
		d.batchID, d.f.deviceIDs[identifier], status, d.f.orgID)
	require.NoError(d.t, err)
}

func TestDispatchedModelTransitionStillRequiresSuccessfulUpdate(t *testing.T) {
	for _, pair := range []struct{ name, manufacturer, model string }{
		{"model", "Proto", "Proto Rig"},
		{"manufacturer", "ProtoOS", "Rig"},
	} {
		t.Run(pair.name, func(t *testing.T) {
			f := newFixture(t, 1)
			dispatcher := useAuditedModelDispatcher(t, f)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			setModelTransition(t, f, pair.manufacturer, pair.model)

			f.svc.EnforceTick(t.Context())
			waiting := f.rollout(t, started.ID)
			require.Equal(t, StatusActive, waiting.Status, "a renamed target must not disappear and complete the rollout")
			require.Equal(t, PhaseInProgress, phaseOf(waiting, "miner-0"))
			require.Zero(t, waiting.DeviceCounts.Excluded)

			f.finishUpdate(t, "miner-0", "2.0.0")
			dispatcher.finish("miner-0", "SUCCESS")
			f.svc.EnforceTick(t.Context())
			done := f.rollout(t, started.ID)
			require.Equal(t, StatusCompleted, done.Status)
			require.Equal(t, PhaseDone, phaseOf(done, "miner-0"))
			require.Equal(t, checksum2, done.Devices[0].LastDeployedFirmwareChecksum)
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "the renamed target was verified without redispatch")
		})
	}
}

func TestUnverifiedModelTransitionFailsWithoutIncompatibleRedispatch(t *testing.T) {
	for _, evidence := range []struct {
		name    string
		prepare func(*testing.T, *fixture)
	}{
		{"wrong version", func(*testing.T, *fixture) {}},
		{"offline", func(t *testing.T, f *fixture) {
			f.setReportedVersion(t, "miner-0", "2.0.0")
			f.setStatus(t, "miner-0", "OFFLINE")
		}},
		{"no hashing", func(t *testing.T, f *fixture) {
			f.setReportedVersion(t, "miner-0", "2.0.0")
			f.setStatus(t, "miner-0", "INACTIVE")
		}},
		{"foreign command", func(t *testing.T, f *fixture) {
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.queueFirmwareCommand(t, "miner-0", "fw-1", "PENDING")
		}},
	} {
		t.Run(evidence.name, func(t *testing.T) {
			f := newFixture(t, 1)
			dispatcher := useAuditedModelDispatcher(t, f)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			dispatcher.finish("miner-0", "SUCCESS")
			setModelTransition(t, f, "Proto", "Proto Rig")
			evidence.prepare(t, f)
			f.svc.EnforceTick(t.Context())
			waiting := f.rollout(t, started.ID)
			require.Equal(t, StatusActive, waiting.Status, "the already-issued attempt keeps its normal grace period")
			require.Equal(t, PhaseInProgress, phaseOf(waiting, "miner-0"))

			f.backdateSends(t)
			f.svc.EnforceTick(t.Context())
			failed := f.rollout(t, started.ID)
			require.Equal(t, StatusCompletedWithFailures, failed.Status)
			require.Equal(t, PhaseFailed, phaseOf(failed, "miner-0"))
			require.Contains(t, failed.Devices[0].LastError, "manufacturer or model changed")
			require.Zero(t, failed.DeviceCounts.Excluded)
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "incompatible firmware must never be resent")
		})
	}
}

func TestQueuedModelTransitionCannotVerifyFromReportedVersionAlone(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	setModelTransition(t, f, "Proto", "Proto Rig")
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	failed := f.rollout(t, started.ID)
	require.Equal(t, StatusCompletedWithFailures, failed.Status)
	require.Equal(t, PhaseFailed, phaseOf(failed, "miner-0"))
	require.Empty(t, failed.Devices[0].LastDeployedFirmwareChecksum, "without a real dispatch there is no artifact provenance")
	require.Empty(t, f.dispatcher.sentIdentifiers())
}

type preflightModelTransitionDispatcher struct {
	t     *testing.T
	f     *fixture
	calls int
}

func (d *preflightModelTransitionDispatcher) FirmwareUpdateArtifact(_ context.Context, selector *commandpb.DeviceSelector, _ string, _ files.FirmwareMetadata) (*command.CommandResult, error) {
	d.calls++
	require.Equal(d.t, []string{"miner-0"}, selector.GetIncludeDevices().GetDeviceIdentifiers())
	// Simulate discovery changing after the rollout read but before command
	// preflight. A policy filter skips the miner before compatibility checking.
	setModelTransition(d.t, d.f, "Proto", "Proto Rig")
	return &command.CommandResult{Skipped: []command.SkippedDevice{{DeviceIdentifier: "miner-0", Reason: "policy preflight skip"}}}, nil
}

func TestPreflightSkippedModelTransitionCannotAuthorizeProvenance(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	dispatcher := &preflightModelTransitionDispatcher{t: t, f: f}
	f.svc.commands = dispatcher
	f.svc.EnforceTick(t.Context())
	require.Equal(t, 1, dispatcher.calls)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	rows, err := f.svc.store.GetQueries(t.Context()).ListFirmwareRolloutDevices(t.Context(), started.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].LastSentAt.Valid, "a skipped attempt still uses normal retry pacing")
	require.False(t, rows[0].LastDispatchedAt.Valid)
	require.False(t, rows[0].VerifiedAt.Valid)
	require.Empty(t, rows[0].LastDeployedFirmwareChecksum, "the unexecuted command cannot prove which artifact produced the version")
	f.backdateSends(t)
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StatusCompletedWithFailures, f.rollout(t, started.ID).Status)
	require.Equal(t, 1, dispatcher.calls, "no second incompatible dispatch")
}

func TestModelTransitionWithPriorManagedProvenanceCanVerify(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	// A prior managed deployment is independently authorized evidence. The
	// engine need not send firmware again solely to prove a reported rename.
	_, err := f.conn.ExecContext(t.Context(), `INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version)
		VALUES ($1, $2, '2.0.0')`, f.deviceIDs["miner-0"], checksum2)
	require.NoError(t, err)
	setModelTransition(t, f, "Proto", "Proto Rig")
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	done := f.rollout(t, started.ID)
	require.Equal(t, StatusCompleted, done.Status)
	require.Equal(t, PhaseDone, phaseOf(done, "miner-0"))
	require.Empty(t, f.dispatcher.sentIdentifiers())
	rows, err := f.svc.store.GetQueries(t.Context()).ListFirmwareRolloutDevices(t.Context(), started.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.False(t, rows[0].LastDispatchedAt.Valid)
}

func TestSameVersionModelTransitionRequiresSuccessfulDispatch(t *testing.T) {
	for _, outcome := range []string{"PENDING", "FAILED", "SUCCESS"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t, 1)
			dispatcher := useAuditedModelDispatcher(t, f)
			f.channel(t, allAtOnce, "miner-0")
			// Existing artifact B and assigned artifact A report the same version.
			// Discovery initially reports the old pair, then learns B's new pair
			// after A was queued. Only A's successful result can establish A's
			// provenance; its queue position or failure cannot rewrite B's record.
			f.setReportedVersion(t, "miner-0", "2.0.0")
			_, err := f.conn.ExecContext(t.Context(), `INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version)
				VALUES ($1, $2, '2.0.0')`, f.deviceIDs["miner-0"], checksum1)
			require.NoError(t, err)
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			setModelTransition(t, f, "Proto", "Proto Rig")
			if outcome != "PENDING" {
				dispatcher.finish("miner-0", outcome)
			}
			f.svc.EnforceTick(t.Context())
			current := f.rollout(t, started.ID)
			if outcome == "SUCCESS" {
				require.Equal(t, StatusCompleted, current.Status)
				require.Equal(t, PhaseDone, phaseOf(current, "miner-0"))
				require.Equal(t, checksum2, current.Devices[0].LastDeployedFirmwareChecksum)
			} else {
				require.Equal(t, StatusActive, current.Status)
				require.Equal(t, PhaseInProgress, phaseOf(current, "miner-0"))
				require.Equal(t, checksum1, current.Devices[0].LastDeployedFirmwareChecksum)
				f.backdateSends(t)
				f.svc.EnforceTick(t.Context())
				failed := f.rollout(t, started.ID)
				require.Equal(t, StatusCompletedWithFailures, failed.Status)
				require.Equal(t, checksum1, failed.Devices[0].LastDeployedFirmwareChecksum)
			}
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
		})
	}
}

func TestModelTransitionPreservesStagedReview(t *testing.T) {
	for _, behavior := range []Behavior{
		{Method: MethodPilotThenContinue, PilotSize: 1},
		{Method: MethodBatched, BatchSize: 1, ReviewAfterEachBatch: true},
	} {
		t.Run(behavior.Method, func(t *testing.T) {
			f := newFixture(t, 2)
			dispatcher := useAuditedModelDispatcher(t, f)
			f.channel(t, behavior, f.allMiners()...)
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
			setModelTransition(t, f, "Proto", "Proto Rig")
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.svc.EnforceTick(t.Context())
			require.Equal(t, StageBatch, f.rollout(t, started.ID).Stage, "reported version alone cannot release the current batch")
			dispatcher.finish("miner-0", "SUCCESS")
			f.svc.EnforceTick(t.Context())
			review := f.rollout(t, started.ID)
			require.Equal(t, StageAwaitingReview, review.Stage)
			require.Equal(t, PhaseDone, phaseOf(review, "miner-0"))
			require.Equal(t, PhaseQueued, phaseOf(review, "miner-1"))
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "a rename cannot bypass the operator review gate")
		})
	}
}
