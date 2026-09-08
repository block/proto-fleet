package rollout

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRolloutStackDispatchUsesSavedAssignmentMetadata(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, f.allMiners()...)
	f.apply(t, "fw-2")
	f.files.metadata = map[string]files.FirmwareMetadata{
		"fw-2": {TargetManufacturer: "Other", TargetModel: "Other", FirmwareVersion: "9.0.0"},
	}
	f.svc.EnforceTick(t.Context())
	require.Len(t, f.dispatcher.artifacts, 1)
	assert.Equal(t, checksum2, f.dispatcher.artifacts[0].Checksum)
	assert.Equal(t, files.FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "2.0.0"}, f.dispatcher.artifacts[0].Metadata)
}

func TestRolloutStackLateJoinerAndReturningTargetKeepDistinctBaselines(t *testing.T) {
	f := newFixture(t, 3)
	ch := f.channel(t, pilotOf1, "miner-0", "miner-1")
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	row, err := f.svc.store.Queries(t.Context()).GetFirmwareRolloutWithChannel(t.Context(), sqlc.GetFirmwareRolloutWithChannelParams{RolloutID: started.ID, OrgID: f.orgID})
	require.NoError(t, err)
	targets, err := f.svc.listTargets(t.Context(), row.FirmwareRollout)
	require.NoError(t, err)
	original := targets[0]
	require.Equal(t, "miner-0", original.DeviceIdentifier)
	require.True(t, original.VerifiedAt.Valid)
	require.True(t, original.BaselineAt.Valid)

	_, err = f.svc.UpdateChannel(t.Context(), f.orgID, ch.ID, ChannelSpec{Name: ch.Name, Scope: Scope{DeviceIdentifiers: []string{"miner-1"}}, Behavior: pilotOf1})
	require.NoError(t, err)
	_, err = f.svc.syncMembership(t.Context(), row.FirmwareRollout)
	require.NoError(t, err)
	_, err = f.svc.UpdateChannel(t.Context(), f.orgID, ch.ID, ChannelSpec{Name: ch.Name, Scope: Scope{DeviceIdentifiers: f.allMiners()}, Behavior: pilotOf1})
	require.NoError(t, err)
	targets, err = f.svc.syncMembership(t.Context(), row.FirmwareRollout)
	require.NoError(t, err)
	require.Len(t, targets, 3)
	for _, target := range targets {
		switch target.DeviceIdentifier {
		case "miner-0":
			assert.False(t, target.ExcludedAt.Valid)
			assert.False(t, target.VerifiedAt.Valid, "returners must verify again")
			assert.Equal(t, original.Position, target.Position)
			assert.Equal(t, original.BatchIndex, target.BatchIndex)
			assert.Equal(t, original.BaselineAt, target.BaselineAt)
		case "miner-2":
			assert.False(t, target.BaselineAt.Valid, "late joiners have no initial snapshot")
			assert.False(t, target.BaselineStatus.Valid)
			assert.False(t, target.Position.Valid)
			assert.False(t, target.BatchIndex.Valid)
			assert.False(t, target.view(row.FirmwareRollout).HasBaseline)
		}
	}
}

func TestRolloutStackActiveRetryIncludesOlderSuppression(t *testing.T) {
	f := newFixture(t, 3)
	ch := f.channel(t, allAtOnce, "miner-0", "miner-1")
	first := f.apply(t, "fw-2")
	_, _, err := f.svc.CancelRollout(t.Context(), f.orgID, first.ID, byOperator)
	require.NoError(t, err)
	_, err = f.svc.UpdateChannel(t.Context(), f.orgID, ch.ID, ChannelSpec{Name: ch.Name, Scope: Scope{DeviceIdentifiers: f.allMiners()}, Behavior: allAtOnce})
	require.NoError(t, err)
	f.svc.EnforceTick(t.Context())
	active := f.latestRollout(t)
	require.NotEqual(t, first.ID, active.ID)
	require.Equal(t, StatusActive, active.Status)
	require.Len(t, active.Devices, 1, "earlier canceled miners remain suppressed")
	retried, err := f.svc.RetryFailedDevices(t.Context(), f.orgID, active.ID, byOperator)
	require.NoError(t, err)
	assert.Equal(t, active.ID, retried.ID)
	assert.Len(t, retried.Devices, 3)
	assert.Equal(t, PhaseQueued, phaseOf(*retried, "miner-0"))
	assert.Equal(t, PhaseQueued, phaseOf(*retried, "miner-1"))
	assert.Equal(t, active.Revision+1, retried.Revision)
	assert.Equal(t, StatusCanceled, f.rollout(t, first.ID).Status)
}

func TestRolloutStackLateJoinerRequiresHashing(t *testing.T) {
	f := newFixture(t, 2)
	ch := f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.setStatus(t, "miner-1", "NEEDS_MINING_POOL")
	_, err := f.svc.UpdateChannel(t.Context(), f.orgID, ch.ID, ChannelSpec{Name: ch.Name, Scope: Scope{DeviceIdentifiers: f.allMiners()}, Behavior: allAtOnce})
	require.NoError(t, err)
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.setReportedVersion(t, "miner-1", "2.0.0")
	f.svc.EnforceTick(t.Context())
	assert.Equal(t, PhaseInProgress, phaseOf(f.rollout(t, started.ID), "miner-1"), "without a baseline, the contract requires current hashing")
	f.setStatus(t, "miner-1", "ACTIVE")
	f.svc.EnforceTick(t.Context())
	assert.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
}

func TestRolloutStackVerificationIsPersistedAndFinishedHistoryStable(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, allAtOnce, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	before := f.rollout(t, started.ID)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	verified := f.rollout(t, started.ID)
	assert.Equal(t, PhaseDone, phaseOf(verified, "miner-0"))
	assert.Equal(t, before.Revision+1, verified.Revision, "provenance and verification are one reconciliation")
	f.setStatus(t, "miner-0", "OFFLINE")
	observed := f.rollout(t, started.ID)
	assert.Equal(t, verified.Revision, observed.Revision)
	assert.Equal(t, verified.DeviceCounts, observed.DeviceCounts, "live health cannot silently change persisted phases")

	_, err := f.conn.ExecContext(t.Context(), `UPDATE firmware_rollout_device SET halted_at = now(), halt_reason = 'failed' WHERE rollout_id = $1 AND device_id = $2`, started.ID, f.deviceIDs["miner-1"])
	require.NoError(t, err)
	f.svc.EnforceTick(t.Context())
	finished := f.rollout(t, started.ID)
	require.Equal(t, StatusCompletedWithFailures, finished.Status)
	require.Equal(t, int32(1), finished.DeviceCounts.Done)
	require.Equal(t, int32(1), finished.DeviceCounts.Failed)
	f.setReportedVersion(t, "miner-0", "different-version")
	assert.Equal(t, finished.DeviceCounts, f.rollout(t, started.ID).DeviceCounts)
}

func TestRolloutStackDriftAtReviewGateDispatchesCorrection(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, Behavior{Method: MethodPilotThenContinue, PilotSize: 1, AutoContinue: true}, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)
	f.setReportedVersion(t, "miner-0", "1.0.0")
	f.backdateSends(t)
	f.svc.EnforceTick(t.Context())
	assert.Equal(t, []string{"miner-0", "miner-0"}, f.dispatcher.sentIdentifiers())
	assert.NotEqual(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	assert.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage, "recovery restarts the review gate")
	f.svc.EnforceTick(t.Context())
	assert.Equal(t, StageRest, f.rollout(t, started.ID).Stage)
}

func TestRolloutStackNewerProvenanceRequiresCorrectiveSend(t *testing.T) {
	f := newFixture(t, 2)
	f.channel(t, pilotOf1, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(t.Context())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(t.Context())
	require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))
	_, err := f.conn.ExecContext(t.Context(), `UPDATE device_firmware_deployment SET firmware_checksum = $1, deployed_at = clock_timestamp() WHERE device_id = $2`, checksum1, f.deviceIDs["miner-0"])
	require.NoError(t, err)
	f.svc.EnforceTick(t.Context())
	drifted := f.rollout(t, started.ID)
	assert.NotEqual(t, PhaseDone, phaseOf(drifted, "miner-0"))
	assert.Equal(t, checksum1, deviceOf(t, drifted, "miner-0").LastDeployedFirmwareChecksum)
	require.Len(t, f.dispatcher.sent, 1, "recent dispatch is not reissued early")
	f.svc.EnforceTick(t.Context())
	assert.Equal(t, checksum1, deviceOf(t, f.rollout(t, started.ID), "miner-0").LastDeployedFirmwareChecksum, "another tick cannot overwrite newer provenance without sending")
	f.backdateSends(t)
	f.svc.EnforceTick(t.Context())
	require.Len(t, f.dispatcher.sent, 2)
	f.svc.EnforceTick(t.Context())
	assert.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))
}
