package betweenchannel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

func TestValidateTransitionTargetsFailsClosed(t *testing.T) {
	t.Parallel()

	source := []DeviceTransition{
		{
			DeviceIdentifier:      "miner-a",
			Manufacturer:          "TestCorp",
			Model:                 "Alpha",
			SourceReleaseTargetID: 1,
			SourceFirmwareVersion: "1.0.0",
			SourceSHA256:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			DeviceIdentifier:      "miner-b",
			Manufacturer:          "TestCorp",
			Model:                 "Beta",
			SourceReleaseTargetID: 2,
			SourceFirmwareVersion: "2.0.0",
			SourceSHA256:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}

	tests := []struct {
		name    string
		targets []ReleaseTarget
		wantErr string
	}{
		{
			name: "missing model",
			targets: []ReleaseTarget{{
				Manufacturer:    "TestCorp",
				Model:           "Alpha",
				FirmwareFileID:  "alpha-2",
				FirmwareVersion: "1.1.0",
				SHA256:          "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			}},
			wantErr: "TestCorp Beta",
		},
		{
			name: "same release",
			targets: []ReleaseTarget{
				{
					Manufacturer:    "TestCorp",
					Model:           "Alpha",
					FirmwareFileID:  "alpha-1",
					FirmwareVersion: "1.0.0",
					SHA256:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				},
				{
					Manufacturer:    "TestCorp",
					Model:           "Beta",
					FirmwareFileID:  "beta-3",
					FirmwareVersion: "3.0.0",
					SHA256:          "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				},
			},
			wantErr: "already targets source release",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateTransitionTargets(source, test.targets)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestValidateTransitionTargetsAcceptsEveryDistinctModel(t *testing.T) {
	t.Parallel()

	err := validateTransitionTargets(
		[]DeviceTransition{{
			DeviceIdentifier:      "miner-a",
			Manufacturer:          "TestCorp",
			Model:                 "Alpha",
			SourceReleaseTargetID: 1,
			SourceFirmwareVersion: "1.0.0",
			SourceSHA256:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		[]ReleaseTarget{{
			Manufacturer:    "testcorp",
			Model:           "alpha",
			FirmwareFileID:  "alpha-2",
			FirmwareVersion: "2.0.0",
			SHA256:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}},
	)
	require.NoError(t, err)
}

func TestStrategyRejectsGenericRolloutCreation(t *testing.T) {
	t.Parallel()

	err := NewStrategy(nil).ValidateCreate(t.Context(), rollout.CreateRequest{})
	require.ErrorIs(t, err, ErrLaneConflict)
}

func TestStrategyValidateRevertRequiresSettledSucceededMember(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		members []rollout.Member
		wantErr bool
	}{
		{
			name: "no succeeded members",
			members: []rollout.Member{{
				State: rollout.MemberStateCancelled,
			}},
			wantErr: true,
		},
		{
			name: "admitted member remains",
			members: []rollout.Member{
				{State: rollout.MemberStateSucceeded},
				{State: rollout.MemberStateAdmitted},
			},
			wantErr: true,
		},
		{
			name: "succeeded member after settlement",
			members: []rollout.Member{
				{State: rollout.MemberStateSucceeded},
				{State: rollout.MemberStateCancelled},
			},
		},
	}

	strategy := NewStrategy(nil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := strategy.ValidateRevert(t.Context(), rollout.RevertValidationRequest{
				Rollout: rollout.Rollout{Members: test.members},
			})
			if test.wantErr {
				require.ErrorIs(t, err, ErrMembershipConflict)
				return
			}
			require.NoError(t, err)
		})
	}
}
