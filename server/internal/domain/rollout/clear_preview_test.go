package rollout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClearingPreviewMatchesApply(t *testing.T) {
	for _, state := range []string{"missing", "already cleared", "assigned"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t, 1)
			ctx := t.Context()
			f.channel(t, pilotOf1, f.allMiners()...)
			clearing := []Assignment{rigAssignment("")}
			var rolloutID int64
			if state != "missing" {
				rolloutID = f.apply(t, "fw-2").ID
			}
			if state == "already cleared" {
				_, err := f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, clearing, nil)
				require.NoError(t, err)
			}
			before := f.rigGroup(t)
			var revision int64
			if rolloutID != 0 {
				revision = f.rollout(t, rolloutID).Revision
			}

			plans, err := f.svc.PreviewFirmware(ctx, f.orgID, f.channelID, clearing, nil)
			require.NoError(t, err)
			require.Len(t, plans, 1)
			assert.Equal(t, state != "assigned", plans[0].Unchanged)
			assert.Empty(t, plans[0].FirmwareFileID)
			assert.Empty(t, plans[0].FirmwareVersion)
			assert.Empty(t, plans[0].FirmwareChecksum)
			assert.Zero(t, plans[0].TargetCount)
			assert.Zero(t, plans[0].OnTargetCount)
			assert.Zero(t, plans[0].BatchCount)
			afterPreview := f.rigGroup(t)
			assert.Equal(t, before.AssignmentGeneration, afterPreview.AssignmentGeneration, "preview must not clear the assignment")
			assert.Equal(t, before.FirmwareChecksum, afterPreview.FirmwareChecksum)

			started, err := f.svc.ApplyFirmware(ctx, f.orgID, testActor, f.channelID, clearing, nil)
			require.NoError(t, err)
			assert.Empty(t, started, "clearing never starts a rollout, including a changed assignment")
			after := f.rigGroup(t)
			assert.Empty(t, after.FirmwareChecksum)
			if state != "assigned" {
				assert.Equal(t, before.AssignmentGeneration, after.AssignmentGeneration)
				if rolloutID != 0 {
					assert.Equal(t, revision, f.rollout(t, rolloutID).Revision, "a repeated clear must not mutate the canceled rollout")
				}
			} else {
				assert.Equal(t, before.AssignmentGeneration+1, after.AssignmentGeneration)
				current := f.rollout(t, rolloutID)
				assert.Equal(t, StatusCanceled, current.Status)
				assert.Equal(t, CancelReasonCleared, current.CancelReason)
			}
		})
	}
}
