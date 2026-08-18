package rollout

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
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

func laneToProto(input *betweenchannel.Lane) *pb.RolloutLane {
	if input == nil {
		return nil
	}
	result := &pb.RolloutLane{
		LaneId:           input.ID.String(),
		Label:            input.Label,
		Description:      input.Description,
		CurrentChannelId: input.CurrentChannelID,
		Revision:         uint64(input.Revision), //nolint:gosec // Database constraint requires a positive revision.
		CreatedAt:        timestamppb.New(input.CreatedAt),
		UpdatedAt:        timestamppb.New(input.UpdatedAt),
		Channels:         make([]*pb.RolloutLaneChannel, 0, len(input.Channels)),
		InitialEnforcement: &pb.RolloutLaneInitialEnforcementStatus{
			TotalCount:     nonNegativeUint32(input.InitialEnforcement.TotalCount),
			PendingCount:   nonNegativeUint32(input.InitialEnforcement.PendingCount),
			UpdatingCount:  nonNegativeUint32(input.InitialEnforcement.UpdatingCount),
			ConfirmedCount: nonNegativeUint32(input.InitialEnforcement.ConfirmedCount),
			AttentionCount: nonNegativeUint32(input.InitialEnforcement.AttentionCount),
		},
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
	return result
}

func lanePreviewToProto(input betweenchannel.InitialEnforcementPreview) *pb.RolloutLanePreview {
	result := &pb.RolloutLanePreview{
		Targets:         make([]*pb.RolloutLanePreviewTarget, 0, len(input.Targets)),
		Miners:          make([]*pb.RolloutLanePreviewMiner, 0, len(input.Miners)),
		MatchingCount:   nonNegativeUint32(input.MatchingCount),
		MismatchedCount: nonNegativeUint32(input.MismatchedCount),
		UnknownCount:    nonNegativeUint32(input.UnknownCount),
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
		BatchId:  input.ID,
		Position: nonNegativeUint32(input.Position),
		Label:    input.Label,
		State:    batchStateToProto(input.State),
		Revision: nonNegativeUint64(input.Revision),
		Members:  make([]*pb.RolloutMember, 0, len(input.Members)),
	}
	for index := range input.Members {
		result.Members = append(result.Members, memberToProto(&input.Members[index]))
	}
	return result
}

func memberToProto(input *rolloutDomain.Member) *pb.RolloutMember {
	result := &pb.RolloutMember{
		MemberId:         input.ID,
		BatchId:          input.BatchID,
		DeviceIdentifier: input.DeviceIdentifier,
		Position:         nonNegativeUint32(input.Position),
		State:            memberStateToProto(input.State),
		Revision:         nonNegativeUint64(input.Revision),
		SourceSnapshot:   snapshotToProto(input.SourceSnapshot),
		TargetSnapshot:   snapshotToProto(input.TargetSnapshot),
		RevertSnapshot:   snapshotToProto(input.RevertSnapshot),
		EnforcementId:    input.EnforcementID,
		CommandBatchUuid: input.CommandBatchUUID,
		LastError:        input.LastError,
		AdmittedAt:       timestampFromPtr(input.AdmittedAt),
		SettledAt:        timestampFromPtr(input.SettledAt),
		Evidence:         make([]*pb.RolloutEvidence, 0, len(input.Evidence)),
	}
	for index := range input.Evidence {
		result.Evidence = append(result.Evidence, evidenceToProto(&input.Evidence[index]))
	}
	return result
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
