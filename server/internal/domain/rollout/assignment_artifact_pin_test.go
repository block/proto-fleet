package rollout

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
)

type trackingPinnedFiles struct {
	FirmwareFiles
	pins      map[string]int
	beforePin func(string)
}

func (f *trackingPinnedFiles) PinFirmwareArtifact(checksum string) (func(), error) {
	if f.beforePin != nil {
		f.beforePin(checksum)
	}
	release, err := f.FirmwareFiles.PinFirmwareArtifact(checksum)
	if err != nil {
		return nil, err
	}
	f.pins[checksum]++
	return func() {
		f.pins[checksum]--
		release()
	}, nil
}

type pinCommitObserver struct {
	Transactor
	afterCommit func()
}

func (tx *pinCommitObserver) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	err := tx.Transactor.RunInTx(ctx, fn)
	if err == nil {
		tx.afterCommit()
	}
	return err
}

func TestAssignmentPinsPayloadUntilCommit(t *testing.T) {
	for _, operation := range []string{"apply", "rollback"} {
		t.Run(operation, func(t *testing.T) {
			f := newFixture(t, 1)
			f.channel(t, allAtOnce, "miner-0")
			var current Rollout
			if operation == "rollback" {
				f.apply(t, "fw-1")
				current = f.apply(t, "fw-2")
			}
			pinned := &trackingPinnedFiles{FirmwareFiles: f.files, pins: map[string]int{}}
			f.svc.files = pinned
			f.svc.tx = &pinCommitObserver{Transactor: f.svc.tx, afterCommit: func() {
				require.Positive(t, pinned.pins[checksum1], "the artifact must remain pinned after the SQL transaction commits")
			}}
			if operation == "apply" {
				f.apply(t, "fw-1")
			} else {
				_, _, err := f.svc.RollbackFirmware(t.Context(), f.orgID, current.ID, byOperator)
				require.NoError(t, err)
			}
			require.Zero(t, pinned.pins[checksum1], "the temporary pin is released when the operation returns")
		})
	}
}

func TestApplyRejectsPayloadDeletedAfterResolution(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	pinned := &trackingPinnedFiles{FirmwareFiles: f.files, pins: map[string]int{}, beforePin: func(string) {
		f.files.deleted["fw-2"] = true
	}}
	f.svc.files = pinned
	_, err := f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
	require.Error(t, err)
	info, ok := ReasonOf(err)
	require.True(t, ok)
	require.Equal(t, ReasonArtifactMissing, info.Reason)
	assignments, err := f.svc.store.GetQueries(t.Context()).ListReleaseChannelFirmware(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Empty(t, assignments)
	rollouts, _, _, err := f.svc.ListRollouts(t.Context(), f.orgID, RolloutFilter{})
	require.NoError(t, err)
	require.Empty(t, rollouts)
}

func TestFailedAssignmentReleasesPayloadPin(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	pinned := &trackingPinnedFiles{FirmwareFiles: f.files, pins: map[string]int{}}
	f.svc.files = pinned
	_, err := f.svc.ApplyFirmware(t.Context(), f.orgID, SystemActor, f.channelID, []Assignment{rigAssignment("fw-2")}, nil)
	require.ErrorContains(t, err, "owning user")
	require.Zero(t, pinned.pins[checksum2], "rolling back a failed assignment must not retain its pin")
}

func TestRollbackRejectsMissingPredecessorWithoutChangingAssignment(t *testing.T) {
	f := newFixture(t, 1)
	f.channel(t, allAtOnce, "miner-0")
	f.apply(t, "fw-1")
	current := f.apply(t, "fw-2")
	q := f.svc.store.GetQueries(t.Context())
	params := sqlc.GetReleaseChannelFirmwareParams{ChannelID: f.channelID, Manufacturer: "Proto", Model: "Rig"}
	before, err := q.GetReleaseChannelFirmware(t.Context(), params)
	require.NoError(t, err)
	f.files.deleted["fw-1"] = true
	_, _, err = f.svc.RollbackFirmware(t.Context(), f.orgID, current.ID, byOperator)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	info, ok := ReasonOf(err)
	require.True(t, ok)
	require.Equal(t, ReasonArtifactMissing, info.Reason)
	require.ErrorContains(t, err, "Upload it again")
	after, err := q.GetReleaseChannelFirmware(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, StatusActive, f.rollout(t, current.ID).Status)
	require.Equal(t, current.Revision, f.rollout(t, current.ID).Revision)
}
