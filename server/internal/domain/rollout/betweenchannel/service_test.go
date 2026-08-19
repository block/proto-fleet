package betweenchannel

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

type recordingDeleteLaneStore struct {
	LaneStore
	requests []DeleteLaneRequest
}

func (s *recordingDeleteLaneStore) DeleteLane(
	_ context.Context,
	req DeleteLaneRequest,
) error {
	s.requests = append(s.requests, req)
	return nil
}

func TestServiceDeleteLaneFingerprintsActorIdentityForIdempotency(t *testing.T) {
	t.Parallel()

	identity := "apikey:lane-delete-1"
	request := DeleteLaneRequest{
		OrgID:             42,
		LaneID:            uuid.New(),
		ExpectedRevision:  3,
		IdempotencyKey:    "delete-lane-retry",
		Reason:            "retire test lane",
		ActorUserID:       9,
		ActorType:         rollout.ActorTypeAPIKey,
		ActorCredentialID: &identity,
	}
	store := &recordingDeleteLaneStore{}
	service := NewService(store, nil)

	require.NoError(t, service.DeleteLane(t.Context(), request))
	require.NoError(t, service.DeleteLane(t.Context(), request))

	otherIdentity := "apikey:lane-delete-2"
	otherCredential := request
	otherCredential.ActorCredentialID = &otherIdentity
	require.NoError(t, service.DeleteLane(t.Context(), otherCredential))

	userActor := request
	userActor.ActorType = rollout.ActorTypeUser
	require.NoError(t, service.DeleteLane(t.Context(), userActor))

	require.Len(t, store.requests, 4)
	assert.Equal(t, request.IdempotencyKey, store.requests[0].IdempotencyKey)
	assert.Len(t, store.requests[0].RequestFingerprint, 64)
	assert.Equal(
		t,
		store.requests[0].RequestFingerprint,
		store.requests[1].RequestFingerprint,
		"an exact idempotent retry must retain its fingerprint",
	)
	assert.NotEqual(
		t,
		store.requests[0].RequestFingerprint,
		store.requests[2].RequestFingerprint,
		"credential identity must participate in the fingerprint",
	)
	assert.NotEqual(
		t,
		store.requests[0].RequestFingerprint,
		store.requests[3].RequestFingerprint,
		"actor type must participate in the fingerprint",
	)
}

func TestValidateDeleteLaneActorIdentityPairing(t *testing.T) {
	t.Parallel()

	identity := "credential-1"
	base := DeleteLaneRequest{
		OrgID:            42,
		LaneID:           uuid.New(),
		ExpectedRevision: 1,
		IdempotencyKey:   "delete-lane",
		Reason:           "retire test lane",
		ActorUserID:      9,
	}
	tests := []struct {
		name         string
		actorType    rollout.ActorType
		credentialID *string
		wantErr      string
	}{
		{name: "user without credential", actorType: rollout.ActorTypeUser},
		{
			name:         "user with credential",
			actorType:    rollout.ActorTypeUser,
			credentialID: &identity,
		},
		{
			name:      "API key requires credential",
			actorType: rollout.ActorTypeAPIKey,
			wantErr:   "API key actor credential ID is required",
		},
		{
			name:         "API key with credential",
			actorType:    rollout.ActorTypeAPIKey,
			credentialID: &identity,
		},
		{name: "system without credential", actorType: rollout.ActorTypeSystem},
		{
			name:         "system rejects credential",
			actorType:    rollout.ActorTypeSystem,
			credentialID: &identity,
			wantErr:      "system actor credential ID must be omitted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			req := base
			req.ActorType = test.actorType
			req.ActorCredentialID = test.credentialID
			err := validateDeleteLaneRequest(req)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantErr)
		})
	}
}

func TestMapStoreErrorDistinguishesMissingLaneFromIdempotencyConflict(t *testing.T) {
	t.Parallel()

	notFound := mapStoreError(ErrLaneNotFound)
	assert.True(t, fleeterror.IsNotFoundError(notFound), "got %v", notFound)

	conflict := mapStoreError(ErrIdempotencyConflict)
	assert.True(t, fleeterror.IsAlreadyExistsError(conflict), "got %v", conflict)
}

func TestReassignmentConfirmationTokenBindsCanonicalSourceState(t *testing.T) {
	t.Parallel()

	laneA := uuid.New()
	laneB := uuid.New()
	req := PreviewLaneRequest{
		OrgID:             42,
		DeviceIdentifiers: []string{"miner-b", "miner-a"},
		ReleaseTargets: []ReleaseTarget{{
			FirmwareFileID:  "firmware-a",
			Manufacturer:    "Proto",
			Model:           "Alpha",
			FirmwareVersion: "2.0.0",
			SHA256:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	}
	preview := InitialEnforcementPreview{
		RequiresReassignConfirmation: true,
		Reassignments: []MembershipReassignment{
			{
				DeviceIdentifier:   "miner-b",
				SourceLaneID:       laneB,
				SourceChannelID:    12,
				SourceLaneRevision: 4,
			},
			{
				DeviceIdentifier:   "miner-a",
				SourceLaneID:       laneA,
				SourceChannelID:    11,
				SourceLaneRevision: 3,
			},
		},
	}

	token, err := ReassignmentConfirmationToken(req, preview)
	require.NoError(t, err)
	assert.Len(t, token, 64)

	reorderedReq := req
	reorderedReq.DeviceIdentifiers = []string{"miner-a", "miner-b"}
	reorderedPreview := preview
	reorderedPreview.Reassignments = []MembershipReassignment{
		preview.Reassignments[1],
		preview.Reassignments[0],
	}
	reorderedToken, err := ReassignmentConfirmationToken(reorderedReq, reorderedPreview)
	require.NoError(t, err)
	assert.Equal(t, token, reorderedToken)

	changedRevision := preview
	changedRevision.Reassignments = append([]MembershipReassignment(nil), preview.Reassignments...)
	changedRevision.Reassignments[0].SourceLaneRevision++
	changedToken, err := ReassignmentConfirmationToken(req, changedRevision)
	require.NoError(t, err)
	assert.NotEqual(t, token, changedToken)

	changedChannel := preview
	changedChannel.Reassignments = append([]MembershipReassignment(nil), preview.Reassignments...)
	changedChannel.Reassignments[0].SourceChannelID++
	changedToken, err = ReassignmentConfirmationToken(req, changedChannel)
	require.NoError(t, err)
	assert.NotEqual(t, token, changedToken)
}

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
