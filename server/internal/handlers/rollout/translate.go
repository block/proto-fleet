package rollout

import (
	"math"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/channel"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func parseRolloutID(value string) (uuid.UUID, error) {
	result, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fleeterror.NewInvalidArgumentError("rollout_id must be a UUID")
	}
	return result, nil
}

func parseLaneID(value string) (uuid.UUID, error) {
	result, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fleeterror.NewInvalidArgumentError("lane_id must be a UUID")
	}
	return result, nil
}

func actorIdentityFromSession(
	info *session.Info,
) (rolloutDomain.ActorType, *string) {
	if info.Actor != "" {
		return rolloutDomain.ActorTypeSystem, nil
	}
	actorType := rolloutDomain.ActorTypeUser
	if info.AuthMethod == session.AuthMethodAPIKey {
		actorType = rolloutDomain.ActorTypeAPIKey
	}
	credentialID := info.CredentialID()
	if credentialID == "" {
		return actorType, nil
	}
	return actorType, &credentialID
}

func nonNegativeUint64(value int64) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}

func nonNegativeUint32(value int32) uint32 {
	if value < 0 {
		return 0
	}
	return uint32(value)
}

func nonNegativeInt64Uint32(value int64) uint32 {
	if value <= 0 {
		return 0
	}
	if value > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value) //nolint:gosec // Guarded by the MaxUint32 bound above.
}

func laneToProto(input *betweenchannel.Lane) *pb.RolloutLane {
	if input == nil {
		return nil
	}
	result := &pb.RolloutLane{
		LaneId:                    input.ID.String(),
		Label:                     input.Label,
		Description:               input.Description,
		CurrentChannelId:          input.CurrentChannelID,
		Revision:                  uint64(input.Revision), //nolint:gosec // Database constraint requires a positive revision.
		MemberCount:               nonNegativeUint32(input.MemberCount),
		CreatedAt:                 timestamppb.New(input.CreatedAt),
		UpdatedAt:                 timestamppb.New(input.UpdatedAt),
		Channels:                  make([]*pb.RolloutLaneChannel, 0, len(input.Channels)),
		Models:                    make([]*pb.RolloutLaneModel, 0, len(input.Models)),
		ScalarProjectionAvailable: input.ScalarProjectionAvailable,
		TopologyEnabled:           input.TopologyEnabled,
		FirmwareConvergence:       firmwareConvergenceToProto(input.FirmwareConvergence),
	}
	for _, inputChannel := range input.Channels {
		channel := &pb.RolloutLaneChannel{
			ChannelId:    inputChannel.ChannelID,
			ReleaseSetId: inputChannel.ReleaseSetID,
			Position:     uint32(inputChannel.Position), //nolint:gosec // Database constraint requires a nonnegative position.
			Current:      inputChannel.ChannelID == input.CurrentChannelID,
			CreatedAt:    timestamppb.New(inputChannel.CreatedAt),
		}
		if inputChannel.RolloutID != nil {
			value := inputChannel.RolloutID.String()
			channel.RolloutId = &value
		}
		result.Channels = append(result.Channels, channel)
	}
	for index := range input.Models {
		result.Models = append(result.Models, laneModelToProto(&input.Models[index]))
	}
	return result
}

func laneModelToProto(input *betweenchannel.LaneModel) *pb.RolloutLaneModel {
	if input == nil {
		return nil
	}
	result := &pb.RolloutLaneModel{
		LaneModelId:      input.ID.String(),
		ModelIdentityKey: input.ModelIdentityKey,
		Revision:         nonNegativeUint64(input.Revision),
		Manufacturer:     input.Manufacturer,
		Model:            input.Model,
		CurrentChannelId: input.CurrentChannelID,
		CurrentFirmwareTarget: laneModelFirmwareTargetToProto(
			input.CurrentFirmwareTarget,
		),
		MemberCount: nonNegativeUint32(input.MemberCount),
		Bindings: &pb.RolloutLaneModelBindingSummary{
			ActiveCount:     nonNegativeInt64Uint32(input.Bindings.ActiveCount),
			HistoricalCount: nonNegativeInt64Uint32(input.Bindings.HistoricalCount),
		},
		FirmwareConvergence: firmwareConvergenceToProto(input.FirmwareConvergence),
		Channels:            make([]*pb.RolloutLaneModelChannel, 0, len(input.Channels)),
		Compatibility:       laneModelCompatibilityToProto(input.Compatibility),
	}
	for _, channel := range input.Channels {
		target := channel.FirmwareTarget
		result.Channels = append(result.Channels, &pb.RolloutLaneModelChannel{
			ChannelId:      channel.ChannelID,
			Position:       nonNegativeUint32(channel.Position),
			Current:        channel.Current,
			FirmwareTarget: laneModelFirmwareTargetToProto(&target),
			CreatedAt:      timestamppb.New(channel.CreatedAt),
		})
	}
	return result
}

func laneModelFirmwareTargetToProto(
	input *betweenchannel.LaneModelFirmwareTarget,
) *pb.RolloutLaneModelFirmwareTarget {
	if input == nil {
		return nil
	}
	return &pb.RolloutLaneModelFirmwareTarget{
		ReleaseTargetId: input.ReleaseTargetID,
		ReleaseSetId:    input.ReleaseSetID,
		FirmwareFileId:  input.FirmwareFileID,
		FirmwareVersion: input.FirmwareVersion,
		Sha256:          input.SHA256,
	}
}

func firmwareConvergenceToProto(
	input betweenchannel.FirmwareConvergenceStatus,
) *pb.RolloutLaneFirmwareConvergenceStatus {
	result := &pb.RolloutLaneFirmwareConvergenceStatus{
		TotalCount:     nonNegativeUint32(input.TotalCount),
		PendingCount:   nonNegativeUint32(input.PendingCount),
		UpdatingCount:  nonNegativeUint32(input.UpdatingCount),
		VerifyingCount: nonNegativeUint32(input.VerifyingCount),
		ConfirmedCount: nonNegativeUint32(input.ConfirmedCount),
		AttentionCount: nonNegativeUint32(input.AttentionCount),
		Members:        make([]*pb.FirmwareTransitionMiner, 0, len(input.Members)),
	}
	for _, member := range input.Members {
		result.Members = append(result.Members, firmwareTransitionMinerToProto(member))
	}
	return result
}

func laneModelCompatibilityToProto(
	input betweenchannel.LaneModelCompatibility,
) pb.RolloutLaneModelCompatibility {
	switch input {
	case betweenchannel.LaneModelCompatible:
		return pb.RolloutLaneModelCompatibility_ROLLOUT_LANE_MODEL_COMPATIBILITY_COMPATIBLE
	case betweenchannel.LaneModelTargetUnavailable:
		return pb.RolloutLaneModelCompatibility_ROLLOUT_LANE_MODEL_COMPATIBILITY_TARGET_UNAVAILABLE
	default:
		return pb.RolloutLaneModelCompatibility_ROLLOUT_LANE_MODEL_COMPATIBILITY_UNSPECIFIED
	}
}

func laneMemberToProto(input betweenchannel.LaneMember) *pb.RolloutLaneMember {
	result := &pb.RolloutLaneMember{
		DeviceIdentifier:        input.DeviceIdentifier,
		Manufacturer:            input.Manufacturer,
		Model:                   input.Model,
		ObservedFirmwareVersion: optionalString(input.ObservedFirmwareVersion),
		ChannelId:               input.ChannelID,
		ChannelPosition:         nonNegativeUint32(input.ChannelPosition),
		OnCurrentChannel:        input.OnCurrentChannel,
		PinnedReleaseVersion:    input.PinnedReleaseVersion,
	}
	if input.Enforcement != nil {
		result.Enforcement = firmwareTransitionMinerToProto(*input.Enforcement)
	}
	return result
}

func firmwareTransitionMinerToProto(input channel.FirmwareTransitionMiner) *pb.FirmwareTransitionMiner {
	return &pb.FirmwareTransitionMiner{
		DeviceIdentifier:              input.DeviceIdentifier,
		Manufacturer:                  input.Manufacturer,
		Model:                         input.Model,
		LatestObservedFirmwareVersion: optionalString(input.LatestObservedFirmwareVersion),
		TargetFirmwareVersion:         input.TargetFirmwareVersion,
		State:                         firmwareTransitionStateToProto(input.State),
		LastError:                     optionalString(input.LastError),
		UpdatedAt:                     timestamppb.New(input.UpdatedAt),
	}
}

func firmwareTransitionStateToProto(
	state channel.FirmwareTransitionState,
) pb.FirmwareTransitionState {
	switch state {
	case channel.FirmwareTransitionPending:
		return pb.FirmwareTransitionState_FIRMWARE_TRANSITION_STATE_PENDING
	case channel.FirmwareTransitionUpdating:
		return pb.FirmwareTransitionState_FIRMWARE_TRANSITION_STATE_UPDATING
	case channel.FirmwareTransitionVerifying:
		return pb.FirmwareTransitionState_FIRMWARE_TRANSITION_STATE_VERIFYING
	case channel.FirmwareTransitionConfirmed:
		return pb.FirmwareTransitionState_FIRMWARE_TRANSITION_STATE_CONFIRMED
	case channel.FirmwareTransitionNeedsAttention:
		return pb.FirmwareTransitionState_FIRMWARE_TRANSITION_STATE_NEEDS_ATTENTION
	default:
		return pb.FirmwareTransitionState_FIRMWARE_TRANSITION_STATE_UNSPECIFIED
	}
}

func lanePreviewToProto(input betweenchannel.InitialEnforcementPreview) *pb.RolloutLanePreview {
	result := &pb.RolloutLanePreview{
		Targets:                          make([]*pb.RolloutLanePreviewTarget, 0, len(input.Targets)),
		Miners:                           make([]*pb.RolloutLanePreviewMiner, 0, len(input.Miners)),
		MatchingCount:                    nonNegativeUint32(input.MatchingCount),
		MismatchedCount:                  nonNegativeUint32(input.MismatchedCount),
		UnknownCount:                     nonNegativeUint32(input.UnknownCount),
		Reassignments:                    make([]*pb.RolloutLaneMembershipReassignment, 0, len(input.Reassignments)),
		RequiresReassignmentConfirmation: input.RequiresReassignConfirmation,
		ReassignmentConfirmationToken:    input.ReassignmentConfirmationToken,
	}
	for _, target := range input.Targets {
		result.Targets = append(result.Targets, &pb.RolloutLanePreviewTarget{
			FirmwareFileId:  target.FirmwareFileID,
			Manufacturer:    target.Manufacturer,
			Model:           target.Model,
			FirmwareVersion: target.FirmwareVersion,
		})
	}
	for _, miner := range input.Miners {
		currentVersion := miner.CurrentFirmwareVersion
		result.Miners = append(result.Miners, &pb.RolloutLanePreviewMiner{
			DeviceIdentifier:       miner.DeviceIdentifier,
			Manufacturer:           miner.Manufacturer,
			Model:                  miner.Model,
			CurrentFirmwareVersion: optionalString(currentVersion),
			TargetFirmwareVersion:  miner.TargetFirmwareVersion,
			TargetFirmwareFileId:   miner.TargetFirmwareFileID,
			Status:                 initialFirmwareStatusToProto(miner.Status),
		})
	}
	for _, reassignment := range input.Reassignments {
		result.Reassignments = append(result.Reassignments, &pb.RolloutLaneMembershipReassignment{
			DeviceIdentifier: reassignment.DeviceIdentifier,
			SourceLaneId:     reassignment.SourceLaneID.String(),
			SourceLaneLabel:  reassignment.SourceLaneLabel,
		})
	}
	return result
}

func initialFirmwareStatusToProto(
	status betweenchannel.InitialFirmwareStatus,
) pb.InitialFirmwareMatchStatus {
	switch status {
	case betweenchannel.InitialFirmwareMatch:
		return pb.InitialFirmwareMatchStatus_INITIAL_FIRMWARE_MATCH_STATUS_MATCHING
	case betweenchannel.InitialFirmwareMismatch:
		return pb.InitialFirmwareMatchStatus_INITIAL_FIRMWARE_MATCH_STATUS_MISMATCHED
	case betweenchannel.InitialFirmwareUnknown:
		return pb.InitialFirmwareMatchStatus_INITIAL_FIRMWARE_MATCH_STATUS_UNKNOWN
	default:
		return pb.InitialFirmwareMatchStatus_INITIAL_FIRMWARE_MATCH_STATUS_UNSPECIFIED
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func batchesFromProto(inputs []*pb.CreateRolloutBatch) []rolloutDomain.CreateBatch {
	result := make([]rolloutDomain.CreateBatch, 0, len(inputs))
	for _, inputBatch := range inputs {
		batch := rolloutDomain.CreateBatch{
			Label:   inputBatch.GetLabel(),
			Members: make([]rolloutDomain.CreateMember, 0, len(inputBatch.GetMembers())),
		}
		for _, inputMember := range inputBatch.GetMembers() {
			batch.Members = append(batch.Members, rolloutDomain.CreateMember{
				DeviceIdentifier: inputMember.GetDeviceIdentifier(),
				SourceSnapshot:   snapshotFromProto(inputMember.GetSourceSnapshot()),
				TargetSnapshot:   snapshotFromProto(inputMember.GetTargetSnapshot()),
				RevertSnapshot:   snapshotFromProto(inputMember.GetRevertSnapshot()),
			})
		}
		result = append(result, batch)
	}
	return result
}

func createRequestFromProto(
	input *pb.CreateRolloutRequest,
	info *session.Info,
) rolloutDomain.CreateRequest {
	actorType, actorCredentialID := actorIdentityFromSession(info)
	return rolloutDomain.CreateRequest{
		OrgID:              info.OrganizationID,
		Name:               input.GetName(),
		StrategyKey:        input.GetStrategyKey(),
		SourceChannelID:    input.SourceChannelId,
		TargetChannelID:    input.TargetChannelId,
		SourceReleaseSetID: input.SourceReleaseSetId,
		TargetReleaseSetID: input.TargetReleaseSetId,
		SourceSnapshot:     snapshotFromProto(input.GetSourceSnapshot()),
		TargetSnapshot:     snapshotFromProto(input.GetTargetSnapshot()),
		RevertSnapshot:     snapshotFromProto(input.GetRevertSnapshot()),
		HashratePolicy:     hashratePolicyFromProto(input.GetHashratePolicy()),
		Batches:            batchesFromProto(input.GetBatches()),
		IdempotencyKey:     input.GetIdempotencyKey(),
		Reason:             input.GetReason(),
		ActorUserID:        info.UserID,
		ActorType:          actorType,
		ActorCredentialID:  actorCredentialID,
	}
}

func rolloutToProto(input *rolloutDomain.Rollout) *pb.Rollout {
	if input == nil {
		return nil
	}
	result := &pb.Rollout{
		RolloutId:          input.ID.String(),
		Name:               input.Name,
		StrategyKey:        input.StrategyKey,
		State:              stateToProto(input.State),
		Revision:           nonNegativeUint64(input.Revision),
		SourceChannelId:    input.SourceChannelID,
		TargetChannelId:    input.TargetChannelID,
		SourceReleaseSetId: input.SourceReleaseSetID,
		TargetReleaseSetId: input.TargetReleaseSetID,
		SourceSnapshot:     snapshotToProto(input.SourceSnapshot),
		TargetSnapshot:     snapshotToProto(input.TargetSnapshot),
		RevertSnapshot:     snapshotToProto(input.RevertSnapshot),
		HashratePolicy:     hashratePolicyToProto(input.HashratePolicy),
		Reason:             input.Reason,
		StartedAt:          timestampFromPtr(input.StartedAt),
		PausedAt:           timestampFromPtr(input.PausedAt),
		AbortedAt:          timestampFromPtr(input.AbortedAt),
		CompletedAt:        timestampFromPtr(input.CompletedAt),
		RevertingAt:        timestampFromPtr(input.RevertingAt),
		RevertedAt:         timestampFromPtr(input.RevertedAt),
		CreatedAt:          timestamppb.New(input.CreatedAt),
		UpdatedAt:          timestamppb.New(input.UpdatedAt),
		Batches:            make([]*pb.RolloutBatch, 0, len(input.Batches)),
		Members:            make([]*pb.RolloutMember, 0, len(input.Members)),
		Causes:             make([]*pb.RolloutCause, 0, len(input.Causes)),
		ParentId:           optionalUUIDString(input.GroupID),
		LaneId:             optionalUUIDString(input.LaneID),
		LaneModelId:        optionalUUIDString(input.LaneModelID),
		ModelIdentityKey:   optionalString(input.ModelIdentityKey),
		Manufacturer:       optionalString(input.Manufacturer),
		Model:              optionalString(input.Model),
	}
	for index := range input.Batches {
		result.Batches = append(result.Batches, batchToProto(&input.Batches[index]))
	}
	for index := range input.Members {
		result.Members = append(result.Members, memberToProto(&input.Members[index]))
	}
	for index := range input.Causes {
		result.Causes = append(result.Causes, causeToProto(&input.Causes[index]))
	}
	return result
}

func batchToProto(input *rolloutDomain.Batch) *pb.RolloutBatch {
	result := &pb.RolloutBatch{
		BatchId:          input.ID,
		Position:         nonNegativeUint32(input.Position),
		Label:            input.Label,
		State:            batchStateToProto(input.State),
		Revision:         nonNegativeUint64(input.Revision),
		Members:          make([]*pb.RolloutMember, 0, len(input.Members)),
		CompletedAt:      timestampFromPtr(input.CompletedAt),
		AdmissionAttempt: nonNegativeUint32(input.AdmissionAttempt),
	}
	if input.State != rolloutDomain.BatchStateCompleted || input.CompletedAt != nil {
		result.EvidenceSummary = &pb.RolloutBatchEvidenceSummary{
			Status:                             evidenceStatusToProto(input.EvidenceStatus),
			TotalCount:                         nonNegativeUint64(input.EvidenceTotalCount),
			PairedCount:                        nonNegativeUint64(input.EvidencePairedCount),
			CumulativeBaselineHashrateHs:       input.CumulativeBaselineHashrateHS,
			CumulativeCurrentHashrateHs:        input.CumulativeCurrentHashrateHS,
			CumulativeDeltaBasisPoints:         input.CumulativeDeltaBasisPoints,
			LatestPolicyBucketHashrateHs:       input.LatestPolicyBucketHashrateHS,
			LatestPolicyBucketDeltaBasisPoints: input.LatestPolicyBucketDeltaBasisPoints,
			HealthySince:                       timestampFromPtr(input.HealthySince),
			LastPolicyBucketBoundary:           timestampFromPtr(input.LastPolicyBucketBoundary),
			EvaluatedAt:                        timestampFromPtr(input.EvaluatedAt),
			PostWindowFinalized:                input.PostWindowFinalized,
			PostWindowFinalizedAt:              timestampFromPtr(input.PostWindowFinalizedAt),
			ErrorMessage:                       input.EvidenceErrorMessage,
			CancellationReason:                 input.EvidenceCancellationReason,
			CancelledAt:                        timestampFromPtr(input.EvidenceCancelledAt),
		}
	}
	for index := range input.Members {
		result.Members = append(result.Members, memberToProto(&input.Members[index]))
	}
	return result
}

func hashratePolicyFromProto(input *pb.RolloutHashratePolicy) *rolloutDomain.HashratePolicy {
	if input == nil {
		return nil
	}
	return &rolloutDomain.HashratePolicy{
		MaxDropBasisPoints:     uint32ToInt32Saturating(input.GetMaxDropBasisPoints()),
		HealthyDurationSeconds: uint32ToInt32Saturating(input.GetHealthyDurationSeconds()),
	}
}

func uint32ToInt32Saturating(value uint32) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(value) //nolint:gosec // Guarded by the MaxInt32 bound above.
}

func hashratePolicyToProto(input *rolloutDomain.HashratePolicy) *pb.RolloutHashratePolicy {
	if input == nil {
		return nil
	}
	return &pb.RolloutHashratePolicy{
		MaxDropBasisPoints:     nonNegativeUint32(input.MaxDropBasisPoints),
		HealthyDurationSeconds: nonNegativeUint32(input.HealthyDurationSeconds),
	}
}

func evidenceStatusToProto(status rolloutDomain.EvidenceStatus) pb.RolloutEvidenceStatus {
	switch status {
	case rolloutDomain.EvidenceStatusPending:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_PENDING
	case rolloutDomain.EvidenceStatusCollecting:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_COLLECTING
	case rolloutDomain.EvidenceStatusUnavailable:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_UNAVAILABLE
	case rolloutDomain.EvidenceStatusObserving:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_OBSERVING
	case rolloutDomain.EvidenceStatusHealthy:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_HEALTHY
	case rolloutDomain.EvidenceStatusHeld:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_HELD
	case rolloutDomain.EvidenceStatusStale:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_STALE
	case rolloutDomain.EvidenceStatusAutomationError:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_AUTOMATION_ERROR
	case rolloutDomain.EvidenceStatusFinalized:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_FINALIZED
	case rolloutDomain.EvidenceStatusCancelled:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_CANCELLED
	default:
		return pb.RolloutEvidenceStatus_ROLLOUT_EVIDENCE_STATUS_UNSPECIFIED
	}
}

func memberToProto(input *rolloutDomain.Member) *pb.RolloutMember {
	result := &pb.RolloutMember{
		MemberId:                 input.ID,
		BatchId:                  input.BatchID,
		DeviceIdentifier:         input.DeviceIdentifier,
		Position:                 nonNegativeUint32(input.Position),
		State:                    memberStateToProto(input.State),
		Revision:                 nonNegativeUint64(input.Revision),
		SourceSnapshot:           snapshotToProto(input.SourceSnapshot),
		TargetSnapshot:           snapshotToProto(input.TargetSnapshot),
		RevertSnapshot:           snapshotToProto(input.RevertSnapshot),
		EnforcementId:            input.EnforcementID,
		CommandBatchUuid:         input.CommandBatchUUID,
		LastError:                input.LastError,
		AdmittedAt:               timestampFromPtr(input.AdmittedAt),
		SettledAt:                timestampFromPtr(input.SettledAt),
		Evidence:                 make([]*pb.RolloutEvidence, 0, len(input.Evidence)),
		ModelIdentityKey:         optionalString(input.ModelIdentityKey),
		ModelIdentityValidatedAt: timestampFromPtr(input.ModelIdentityValidatedAt),
	}
	for index := range input.Evidence {
		result.Evidence = append(result.Evidence, evidenceToProto(&input.Evidence[index]))
	}
	return result
}

func groupToProto(input *rolloutDomain.Group) *pb.RolloutGroup {
	if input == nil {
		return nil
	}
	result := &pb.RolloutGroup{
		ParentId:          input.ID.String(),
		LaneId:            input.LaneID.String(),
		Name:              input.Name,
		Reason:            input.Reason,
		CreatedAt:         timestamppb.New(input.CreatedAt),
		UpdatedAt:         timestamppb.New(input.UpdatedAt),
		Children:          make([]*pb.Rollout, 0, len(input.Children)),
		ResultRevision:    nonNegativeUint64(input.ResultRevision),
		TerminalOutcome:   groupTerminalOutcomeToProto(input.TerminalOutcome),
		ResultReady:       input.ResultReady,
		Lifecycle:         groupLifecycleToProto(input.Lifecycle),
		Activity:          groupActivityToProto(input.Activity),
		NeedsAction:       input.NeedsAction,
		EvidenceReadiness: groupEvidenceReadinessToProto(input.EvidenceReadiness),
		Models:            make([]*pb.RolloutGroupModelSummary, 0, len(input.ModelSnapshots)),
	}
	for index := range input.Children {
		result.Children = append(result.Children, rolloutToProto(&input.Children[index]))
	}
	for _, model := range input.ModelSnapshots {
		manufacturer, _ := model.Snapshot["manufacturer"].(string)
		modelName, _ := model.Snapshot["model"].(string)
		memberCount := uint32(0)
		if value, ok := model.Snapshot["member_count"].(float64); ok && value > 0 {
			memberCount = uint32(value) //nolint:gosec // Snapshot count is bounded by created rollout members.
		}
		result.Models = append(result.Models, &pb.RolloutGroupModelSummary{
			LaneModelId:           model.LaneModelID.String(),
			ModelIdentityKey:      model.ModelIdentityKey,
			Manufacturer:          manufacturer,
			Model:                 modelName,
			SourceChannelId:       model.SourceChannelID,
			TargetChannelId:       model.TargetChannelID,
			SourceReleaseTargetId: model.SourceReleaseTargetID,
			TargetReleaseTargetId: model.TargetReleaseTargetID,
			MemberCount:           memberCount,
			ChildRolloutId:        optionalUUIDString(model.ChildRolloutID),
		})
	}
	return result
}

func groupLifecycleToProto(value rolloutDomain.GroupLifecycle) pb.RolloutGroupLifecycle {
	switch value {
	case rolloutDomain.GroupLifecycleActive:
		return pb.RolloutGroupLifecycle_ROLLOUT_GROUP_LIFECYCLE_ACTIVE
	case rolloutDomain.GroupLifecycleTerminal:
		return pb.RolloutGroupLifecycle_ROLLOUT_GROUP_LIFECYCLE_TERMINAL
	default:
		return pb.RolloutGroupLifecycle_ROLLOUT_GROUP_LIFECYCLE_UNSPECIFIED
	}
}

func groupActivityToProto(value rolloutDomain.GroupActivity) pb.RolloutGroupActivity {
	switch value {
	case rolloutDomain.GroupActivityFailedAdmission:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_FAILED_ADMISSION
	case rolloutDomain.GroupActivityAttentionRequired:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_ATTENTION_REQUIRED
	case rolloutDomain.GroupActivityReview:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_REVIEW
	case rolloutDomain.GroupActivityPaused:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_PAUSED
	case rolloutDomain.GroupActivityReverting:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_REVERTING
	case rolloutDomain.GroupActivityFinalizing:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_FINALIZING
	case rolloutDomain.GroupActivityRunning:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_RUNNING
	case rolloutDomain.GroupActivityCreated:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_CREATED
	case rolloutDomain.GroupActivitySettled:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_SETTLED
	default:
		return pb.RolloutGroupActivity_ROLLOUT_GROUP_ACTIVITY_UNSPECIFIED
	}
}

func groupTerminalOutcomeToProto(
	value rolloutDomain.GroupTerminalOutcome,
) pb.RolloutGroupTerminalOutcome {
	switch value {
	case rolloutDomain.GroupTerminalOutcomePending:
		return pb.RolloutGroupTerminalOutcome_ROLLOUT_GROUP_TERMINAL_OUTCOME_PENDING
	case rolloutDomain.GroupTerminalOutcomeSuccessful:
		return pb.RolloutGroupTerminalOutcome_ROLLOUT_GROUP_TERMINAL_OUTCOME_SUCCESSFUL
	case rolloutDomain.GroupTerminalOutcomeReverted:
		return pb.RolloutGroupTerminalOutcome_ROLLOUT_GROUP_TERMINAL_OUTCOME_REVERTED
	case rolloutDomain.GroupTerminalOutcomeAborted:
		return pb.RolloutGroupTerminalOutcome_ROLLOUT_GROUP_TERMINAL_OUTCOME_ABORTED
	case rolloutDomain.GroupTerminalOutcomeCompletedWithFailures:
		return pb.RolloutGroupTerminalOutcome_ROLLOUT_GROUP_TERMINAL_OUTCOME_COMPLETED_WITH_FAILURES
	case rolloutDomain.GroupTerminalOutcomeMixed:
		return pb.RolloutGroupTerminalOutcome_ROLLOUT_GROUP_TERMINAL_OUTCOME_MIXED
	default:
		return pb.RolloutGroupTerminalOutcome_ROLLOUT_GROUP_TERMINAL_OUTCOME_UNSPECIFIED
	}
}

func groupEvidenceReadinessToProto(
	value rolloutDomain.GroupEvidenceReadiness,
) pb.RolloutGroupEvidenceReadiness {
	switch value {
	case rolloutDomain.GroupEvidencePending:
		return pb.RolloutGroupEvidenceReadiness_ROLLOUT_GROUP_EVIDENCE_READINESS_PENDING
	case rolloutDomain.GroupEvidenceReady:
		return pb.RolloutGroupEvidenceReadiness_ROLLOUT_GROUP_EVIDENCE_READINESS_READY
	default:
		return pb.RolloutGroupEvidenceReadiness_ROLLOUT_GROUP_EVIDENCE_READINESS_UNSPECIFIED
	}
}

func optionalUUIDString(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}

func evidenceToProto(input *rolloutDomain.Evidence) *pb.RolloutEvidence {
	return &pb.RolloutEvidence{
		EvidenceId:      input.ID,
		Phase:           evidencePhaseToProto(input.Phase),
		WindowStart:     timestamppb.New(input.WindowStart),
		WindowEnd:       timestamppb.New(input.WindowEnd),
		ObservedAt:      timestampFromPtr(input.ObservedAt),
		AvgHashrateHs:   input.AvgHashrateHS,
		AvgPowerW:       input.AvgPowerW,
		AvgTemperatureC: input.AvgTemperatureC,
		ErrorCount:      input.ErrorCount,
		SampleCount:     input.SampleCount,
	}
}

func causeToProto(input *rolloutDomain.Cause) *pb.RolloutCause {
	fromState := pb.RolloutState_ROLLOUT_STATE_UNSPECIFIED
	if input.FromState != nil {
		fromState = stateToProto(*input.FromState)
	}
	return &pb.RolloutCause{
		CauseId:         input.ID,
		MemberId:        input.MemberID,
		Operation:       string(input.Operation),
		Reason:          input.Reason,
		ActorUserId:     input.ActorUserID,
		FromState:       fromState,
		ToState:         stateToProto(input.ToState),
		RolloutRevision: nonNegativeUint64(input.RolloutRevision),
		CreatedAt:       timestamppb.New(input.CreatedAt),
	}
}

func rolloutStatesFromProto(values []pb.RolloutState) ([]rolloutDomain.State, error) {
	result := make([]rolloutDomain.State, 0, len(values))
	for _, value := range values {
		if value == pb.RolloutState_ROLLOUT_STATE_UNSPECIFIED {
			continue
		}
		state, ok := stateFromProto(value)
		if !ok {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"unsupported rollout state %v",
				value,
			)
		}
		result = append(result, state)
	}
	return result, nil
}

func stateFromProto(value pb.RolloutState) (rolloutDomain.State, bool) {
	switch value {
	case pb.RolloutState_ROLLOUT_STATE_UNSPECIFIED:
		return "", false
	case pb.RolloutState_ROLLOUT_STATE_CREATED:
		return rolloutDomain.StateCreated, true
	case pb.RolloutState_ROLLOUT_STATE_RUNNING:
		return rolloutDomain.StateRunning, true
	case pb.RolloutState_ROLLOUT_STATE_PAUSED:
		return rolloutDomain.StatePaused, true
	case pb.RolloutState_ROLLOUT_STATE_REVIEW:
		return rolloutDomain.StateReview, true
	case pb.RolloutState_ROLLOUT_STATE_ABORTED:
		return rolloutDomain.StateAborted, true
	case pb.RolloutState_ROLLOUT_STATE_COMPLETED:
		return rolloutDomain.StateCompleted, true
	case pb.RolloutState_ROLLOUT_STATE_COMPLETED_WITH_FAILURES:
		return rolloutDomain.StateCompletedWithFailures, true
	case pb.RolloutState_ROLLOUT_STATE_REVERTING:
		return rolloutDomain.StateReverting, true
	case pb.RolloutState_ROLLOUT_STATE_REVERTED:
		return rolloutDomain.StateReverted, true
	default:
		return "", false
	}
}

func stateToProto(value rolloutDomain.State) pb.RolloutState {
	switch value {
	case rolloutDomain.StateCreated:
		return pb.RolloutState_ROLLOUT_STATE_CREATED
	case rolloutDomain.StateRunning:
		return pb.RolloutState_ROLLOUT_STATE_RUNNING
	case rolloutDomain.StatePaused:
		return pb.RolloutState_ROLLOUT_STATE_PAUSED
	case rolloutDomain.StateReview:
		return pb.RolloutState_ROLLOUT_STATE_REVIEW
	case rolloutDomain.StateAborted:
		return pb.RolloutState_ROLLOUT_STATE_ABORTED
	case rolloutDomain.StateCompleted:
		return pb.RolloutState_ROLLOUT_STATE_COMPLETED
	case rolloutDomain.StateCompletedWithFailures:
		return pb.RolloutState_ROLLOUT_STATE_COMPLETED_WITH_FAILURES
	case rolloutDomain.StateReverting:
		return pb.RolloutState_ROLLOUT_STATE_REVERTING
	case rolloutDomain.StateReverted:
		return pb.RolloutState_ROLLOUT_STATE_REVERTED
	default:
		return pb.RolloutState_ROLLOUT_STATE_UNSPECIFIED
	}
}

func batchStateToProto(value rolloutDomain.BatchState) pb.RolloutBatchState {
	switch value {
	case rolloutDomain.BatchStatePending:
		return pb.RolloutBatchState_ROLLOUT_BATCH_STATE_PENDING
	case rolloutDomain.BatchStateAdmitted:
		return pb.RolloutBatchState_ROLLOUT_BATCH_STATE_ADMITTED
	case rolloutDomain.BatchStateCompleted:
		return pb.RolloutBatchState_ROLLOUT_BATCH_STATE_COMPLETED
	case rolloutDomain.BatchStateCancelled:
		return pb.RolloutBatchState_ROLLOUT_BATCH_STATE_CANCELLED
	default:
		return pb.RolloutBatchState_ROLLOUT_BATCH_STATE_UNSPECIFIED
	}
}

func memberStateToProto(value rolloutDomain.MemberState) pb.RolloutMemberState {
	switch value {
	case rolloutDomain.MemberStatePending:
		return pb.RolloutMemberState_ROLLOUT_MEMBER_STATE_PENDING
	case rolloutDomain.MemberStateAdmitted:
		return pb.RolloutMemberState_ROLLOUT_MEMBER_STATE_ADMITTED
	case rolloutDomain.MemberStateSucceeded:
		return pb.RolloutMemberState_ROLLOUT_MEMBER_STATE_SUCCEEDED
	case rolloutDomain.MemberStateFailed:
		return pb.RolloutMemberState_ROLLOUT_MEMBER_STATE_FAILED
	case rolloutDomain.MemberStateAttentionRequired:
		return pb.RolloutMemberState_ROLLOUT_MEMBER_STATE_ATTENTION_REQUIRED
	case rolloutDomain.MemberStateCancelled:
		return pb.RolloutMemberState_ROLLOUT_MEMBER_STATE_CANCELLED
	case rolloutDomain.MemberStateReverting:
		return pb.RolloutMemberState_ROLLOUT_MEMBER_STATE_REVERTING
	case rolloutDomain.MemberStateReverted:
		return pb.RolloutMemberState_ROLLOUT_MEMBER_STATE_REVERTED
	default:
		return pb.RolloutMemberState_ROLLOUT_MEMBER_STATE_UNSPECIFIED
	}
}

func evidencePhaseToProto(value rolloutDomain.EvidencePhase) pb.RolloutEvidencePhase {
	switch value {
	case rolloutDomain.EvidencePhaseBaseline:
		return pb.RolloutEvidencePhase_ROLLOUT_EVIDENCE_PHASE_BASELINE
	case rolloutDomain.EvidencePhasePost:
		return pb.RolloutEvidencePhase_ROLLOUT_EVIDENCE_PHASE_POST
	default:
		return pb.RolloutEvidencePhase_ROLLOUT_EVIDENCE_PHASE_UNSPECIFIED
	}
}

func snapshotFromProto(value *structpb.Struct) map[string]any {
	return value.AsMap()
}

func snapshotToProto(value map[string]any) *structpb.Struct {
	result, err := structpb.NewStruct(value)
	if err != nil {
		return &structpb.Struct{}
	}
	return result
}

func timestampFromPtr(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}
