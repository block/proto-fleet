package betweenchannel

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

type recordingMembershipLaneStore struct {
	LaneStore
	previewRequest PreviewMembershipChangeRequest
	updateRequest  UpdateMembershipRequest
}

type recordingCreateLaneStore struct {
	LaneStore
	previewRequest PreviewLaneRequest
	createRequest  CreateLaneRequest
}

func (s *recordingCreateLaneStore) PreviewLane(
	_ context.Context,
	req PreviewLaneRequest,
) (InitialEnforcementPreview, error) {
	s.previewRequest = req
	return InitialEnforcementPreview{Targets: req.ReleaseTargets}, nil
}

func (s *recordingCreateLaneStore) CreateLane(
	_ context.Context,
	req CreateLaneRequest,
) (*Lane, error) {
	s.createRequest = req
	return &Lane{ID: req.ID, OrgID: req.OrgID, Label: req.Label}, nil
}

func TestServiceAllowsEmptyRolloutLanePreviewAndCreation(t *testing.T) {
	t.Parallel()

	store := &recordingCreateLaneStore{}
	service := NewService(store, nil)
	targets := []ReleaseTarget{{
		FirmwareFileID:  "firmware-a",
		Manufacturer:    "Proto",
		Model:           "Alpha",
		FirmwareVersion: "1.0.0",
		SHA256:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}}

	preview, err := service.PreviewLane(t.Context(), PreviewLaneRequest{
		OrgID:          42,
		ReleaseTargets: targets,
	})
	require.NoError(t, err)
	assert.Empty(t, preview.Miners)

	lane, err := service.CreateLane(t.Context(), CreateLaneRequest{
		OrgID:          42,
		Label:          "Empty lane",
		ReleaseTargets: targets,
		IdempotencyKey: "empty-lane",
		ActorUserID:    9,
	})
	require.NoError(t, err)
	require.NotNil(t, lane)
	assert.Empty(t, store.createRequest.DeviceIdentifiers)
	assert.NotEqual(t, uuid.Nil, store.createRequest.ID)
	assert.NotEqual(t, uuid.Nil, store.createRequest.ChangeID)
}

func (s *recordingMembershipLaneStore) PreviewMembershipChange(
	_ context.Context,
	req PreviewMembershipChangeRequest,
) (MembershipChangePreview, error) {
	s.previewRequest = req
	return MembershipChangePreview{}, nil
}

func (s *recordingMembershipLaneStore) UpdateMembership(
	_ context.Context,
	req UpdateMembershipRequest,
) (UpdateMembershipResult, error) {
	s.updateRequest = req
	return UpdateMembershipResult{}, nil
}

func TestServiceUpdateMembershipNormalizesAndFingerprintsActorIdentity(t *testing.T) {
	t.Parallel()

	credential := "apikey:membership-1"
	store := &recordingMembershipLaneStore{}
	service := NewService(store, nil)
	request := UpdateMembershipRequest{
		OrgID:             42,
		LaneID:            uuid.New(),
		ExpectedRevision:  7,
		AddIdentifiers:    []string{"miner-b", "miner-a"},
		RemoveIdentifiers: []string{"miner-c"},
		ConfirmFirmware:   true,
		ConfirmReassign:   true,
		IdempotencyKey:    "membership-change-1",
		Reason:            "rebalance lane",
		ActorUserID:       9,
		ActorType:         rollout.ActorTypeAPIKey,
		ActorCredentialID: &credential,
	}

	_, err := service.UpdateMembership(t.Context(), request)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, store.updateRequest.ChangeID)
	assert.Len(t, store.updateRequest.RequestFingerprint, 64)
	assert.Equal(t, []string{"miner-a", "miner-b"}, store.updateRequest.AddIdentifiers)
	assert.Equal(t, []string{"miner-c"}, store.updateRequest.RemoveIdentifiers)
}

func TestFingerprintMembershipUpdateIncludesConfirmationFlags(t *testing.T) {
	t.Parallel()

	base := UpdateMembershipRequest{
		OrgID:             42,
		LaneID:            uuid.New(),
		ExpectedRevision:  7,
		AddIdentifiers:    []string{"miner-a"},
		RemoveIdentifiers: []string{"miner-b"},
		IdempotencyKey:    "membership-change-confirmations",
		Reason:            "rebalance lane",
		ActorUserID:       9,
		ActorType:         rollout.ActorTypeUser,
	}
	baseline, err := fingerprintMembershipUpdate(base)
	require.NoError(t, err)

	withFirmware := base
	withFirmware.ConfirmFirmware = true
	firmwareFingerprint, err := fingerprintMembershipUpdate(withFirmware)
	require.NoError(t, err)
	assert.NotEqual(t, baseline, firmwareFingerprint)

	withReassign := base
	withReassign.ConfirmReassign = true
	reassignFingerprint, err := fingerprintMembershipUpdate(withReassign)
	require.NoError(t, err)
	assert.NotEqual(t, baseline, reassignFingerprint)
	assert.NotEqual(t, firmwareFingerprint, reassignFingerprint)
}

func TestServiceMembershipChangeRejectsDuplicateAndOverlappingOperations(t *testing.T) {
	t.Parallel()

	service := NewService(&recordingMembershipLaneStore{}, nil)
	base := PreviewMembershipChangeRequest{
		OrgID:          42,
		LaneID:         uuid.New(),
		AddIdentifiers: []string{"miner-a"},
	}

	duplicate := base
	duplicate.AddIdentifiers = []string{"miner-a", "miner-a"}
	_, err := service.PreviewMembershipChange(t.Context(), duplicate)
	require.Error(t, err)

	overlap := base
	overlap.RemoveIdentifiers = []string{"miner-a"}
	_, err = service.PreviewMembershipChange(t.Context(), overlap)
	require.Error(t, err)
}
