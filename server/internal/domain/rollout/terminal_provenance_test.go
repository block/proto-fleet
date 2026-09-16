package rollout

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestCanceledRolloutAdoptsCompletedDispatchWithoutRewritingHistory(t *testing.T) {
	for _, reconcile := range []string{"tick", "immediate retry", "excluded target"} {
		t.Run(reconcile, func(t *testing.T) {
			f := newFixture(t, 1)
			ctx := t.Context()
			useAuditedModelDispatcher(t, f)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(ctx)
			if reconcile == "excluded target" {
				_, err := f.svc.UpdateChannel(ctx, f.orgID, f.channelID, ChannelSpec{Name: "Test channel", Behavior: allAtOnce})
				require.NoError(t, err)
				row, err := f.svc.store.GetQueries(ctx).GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: started.ID, OrgID: f.orgID})
				require.NoError(t, err)
				_, err = f.svc.prepareRollout(ctx, row)
				require.NoError(t, err)
				require.Equal(t, PhaseExcluded, phaseOf(f.rollout(t, started.ID), "miner-0"))
			}
			_, _, err := f.svc.CancelRollout(ctx, f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			q := f.svc.store.GetQueries(ctx)
			canceled, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: started.ID, OrgID: f.orgID})
			require.NoError(t, err)
			targets, err := q.ListFirmwareRolloutDevices(ctx, started.ID)
			require.NoError(t, err)
			require.Len(t, targets, 1)
			readTargetHistory := func() string {
				t.Helper()
				var history string
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT row_to_json(rd)::text
					FROM firmware_rollout_device rd WHERE rollout_id = $1`, started.ID).Scan(&history))
				return history
			}
			history := readTargetHistory()
			readDeployment := func() string {
				t.Helper()
				var checksum string
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT COALESCE((
					SELECT firmware_checksum FROM device_firmware_deployment WHERE device_id = $1), '')`,
					f.deviceIDs["miner-0"]).Scan(&checksum))
				return checksum
			}

			// Cancellation cannot revoke an issued command. Neither a pending
			// command nor success before the matching report proves deployment.
			f.finishUpdate(t, "miner-0", "2.0.0")
			f.svc.EnforceTick(ctx)
			require.Empty(t, readDeployment())
			f.setReportedVersion(t, "miner-0", "1.0.0")
			require.NoError(t, q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
				DeviceID: f.deviceIDs["miner-0"], Uuid: targets[0].LastDispatchedBatchUuid.String,
				Status: sqlc.DeviceCommandStatusEnumSUCCESS, UpdatedAt: time.Now(),
			}))
			f.svc.EnforceTick(ctx)
			require.Empty(t, readDeployment())
			f.finishUpdate(t, "miner-0", "2.0.0")
			candidates, err := q.ListTerminalFirmwareRolloutsNeedingProvenance(ctx)
			require.NoError(t, err)
			require.Len(t, candidates, 1, "the current successful completion is actionable despite cancellation")

			var retried *Rollout
			if reconcile == "immediate retry" {
				retried, err = f.svc.RetryFailedDevices(ctx, f.orgID, started.ID, byOperator)
				require.NoError(t, err)
			} else {
				f.svc.EnforceTick(ctx)
				var suppressed int
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM firmware_rollout_suppressed_device
					WHERE channel_id = $1 AND device_id = $2 AND assignment_generation = $3`,
					f.channelID, f.deviceIDs["miner-0"], canceled.AssignmentGeneration).Scan(&suppressed))
				require.Equal(t, 1, suppressed, "live provenance must not release the operator's cancellation")
				require.Equal(t, started.ID, f.latestRollout(t).ID)
			}
			require.Equal(t, checksum2, readDeployment(), "successful in-flight work is adopted after cancellation")
			current, err := q.GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: started.ID, OrgID: f.orgID})
			require.NoError(t, err)
			require.Greater(t, current.Revision, canceled.Revision, "new live deployment evidence invalidates the detail revision")
			current.Revision, current.RevisionTxid, current.UpdatedAt = canceled.Revision, canceled.RevisionTxid, canceled.UpdatedAt
			require.Equal(t, canceled, current, "canceled lifecycle, finish time and operator action remain historical")
			require.Equal(t, history, readTargetHistory(), "adoption must not rewrite halted, excluded or verified target history")
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())

			if reconcile == "excluded target" {
				_, err := f.svc.UpdateChannel(ctx, f.orgID, f.channelID, ChannelSpec{
					Name: "Test channel", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}}, Behavior: allAtOnce,
				})
				require.NoError(t, err)
			}
			if retried == nil {
				retried, err = f.svc.RetryFailedDevices(ctx, f.orgID, started.ID, byOperator)
				require.NoError(t, err)
			}
			f.svc.EnforceTick(ctx)
			require.Equal(t, StatusCompleted, f.rollout(t, retried.ID).Status)
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "explicit retry must not reflash the already installed artifact")
			require.Equal(t, history, readTargetHistory())
		})
	}
}

func TestCanceledRolloutRequiresSuccessfulDispatchEvenAfterVersionChange(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "pending"
		if failed {
			name = "failed"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 1)
			ctx := t.Context()
			useAuditedModelDispatcher(t, f)
			_, err := f.conn.ExecContext(ctx, `INSERT INTO device_firmware_deployment
				(device_id, firmware_checksum, firmware_version, deployed_at)
				VALUES ($1, $2, '1.0.0', now() - INTERVAL '1 hour')`, f.deviceIDs["miner-0"], checksum1)
			require.NoError(t, err)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(ctx)
			_, _, err = f.svc.CancelRollout(ctx, f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			if failed {
				dispatched := latestProvenanceDispatch(t, f, started.ID)
				completeProvenanceCommand(t, f, dispatched.LastDispatchedBatchUuid.String, sqlc.DeviceCommandStatusEnumFAILED)
			}
			f.finishUpdate(t, "miner-0", "2.0.0")
			candidates, err := f.svc.store.GetQueries(ctx).ListTerminalFirmwareRolloutsNeedingProvenance(ctx)
			require.NoError(t, err)
			require.Empty(t, candidates, "pending and failed terminal outcomes must not rescan retained history")
			f.svc.EnforceTick(ctx)
			_, err = f.svc.RetryFailedDevices(ctx, f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			observed := latestProvenanceDispatch(t, f, started.ID)
			require.NotEqual(t, checksum2, observed.LastDeployedFirmwareChecksum,
				"terminal observation and immediate retry require exact success, even with known earlier provenance")
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
		})
	}
}

func TestCompletedRolloutCannotReadoptSuccessSupersededByForeignCompletion(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	useAuditedModelDispatcher(t, f)
	f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	own := latestProvenanceDispatch(t, f, started.ID)
	completeProvenanceCommand(t, f, own.LastDispatchedBatchUuid.String, sqlc.DeviceCommandStatusEnumSUCCESS)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	require.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
	require.EqualValues(t, 1, f.rigGroup(t).OnTargetCount)

	foreign := createProvenanceCommand(t, f, map[string]string{"firmware_checksum": checksum1}, false)
	completeProvenanceCommand(t, f, foreign, sqlc.DeviceCommandStatusEnumSUCCESS)
	require.Zero(t, f.rigGroup(t).OnTargetCount)
	candidates, err := f.svc.store.GetQueries(ctx).ListTerminalFirmwareRolloutsNeedingProvenance(ctx)
	require.NoError(t, err)
	require.Empty(t, candidates, "obsolete successes must be filtered before locking terminal history")
	f.svc.EnforceTick(ctx)
	corrective := f.latestRollout(t)
	require.NotEqual(t, started.ID, corrective.ID, "terminal reconciliation cannot hide a later artifact change")
	require.Equal(t, StatusActive, corrective.Status)
	require.Empty(t, deviceOf(t, corrective, "miner-0").LastDeployedFirmwareChecksum)
	require.Equal(t, []string{"miner-0", "miner-0"}, f.dispatcher.sentIdentifiers())
	require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"), "the completed rollout keeps its historical phase")

	correction := latestProvenanceDispatch(t, f, corrective.ID)
	completeProvenanceCommand(t, f, correction.LastDispatchedBatchUuid.String, sqlc.DeviceCommandStatusEnumSUCCESS)
	f.svc.EnforceTick(ctx)
	require.Equal(t, StatusCompleted, f.rollout(t, corrective.ID).Status)
	require.EqualValues(t, 1, f.rigGroup(t).OnTargetCount)
}

func TestTerminalProvenanceLookupRequiresRetainedCurrentWitness(t *testing.T) {
	for _, missing := range []string{"witness", "current result"} {
		t.Run(missing, func(t *testing.T) {
			f := newFixture(t, 1)
			ctx := t.Context()
			useAuditedModelDispatcher(t, f)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(ctx)
			_, _, err := f.svc.CancelRollout(ctx, f.orgID, started.ID, byOperator)
			require.NoError(t, err)
			own := latestProvenanceDispatch(t, f, started.ID)
			completeProvenanceCommand(t, f, own.LastDispatchedBatchUuid.String, sqlc.DeviceCommandStatusEnumSUCCESS)
			current := createProvenanceCommand(t, f, map[string]string{"firmware_checksum": checksum2}, false)
			completeProvenanceCommand(t, f, current, sqlc.DeviceCommandStatusEnumSUCCESS)
			f.finishUpdate(t, "miner-0", "2.0.0")
			q := f.svc.store.GetQueries(ctx)
			candidates, err := q.ListTerminalFirmwareRolloutsNeedingProvenance(ctx)
			require.NoError(t, err)
			require.Len(t, candidates, 1, "a different successful batch for the same artifact is valid current evidence")
			if missing == "witness" {
				_, err = f.conn.ExecContext(ctx, `UPDATE device_firmware_deployment SET last_command_batch_uuid = NULL WHERE device_id = $1`, f.deviceIDs["miner-0"])
			} else {
				_, err = f.conn.ExecContext(ctx, `DELETE FROM command_on_device_log WHERE command_batch_log_id = (
					SELECT id FROM command_batch_log WHERE uuid = $1)`, current)
			}
			require.NoError(t, err)
			candidates, err = q.ListTerminalFirmwareRolloutsNeedingProvenance(ctx)
			require.NoError(t, err)
			require.Empty(t, candidates, "an old retained success cannot substitute for missing current evidence")
			f.svc.EnforceTick(ctx)
			require.Empty(t, latestProvenanceDispatch(t, f, started.ID).LastDeployedFirmwareChecksum)
			require.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
		})
	}
}
