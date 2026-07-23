package minerchannel

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/minerchannel/v1"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func toCreateMinerChannelParams(req *pb.CreateMinerChannelRequest, info *session.Info) (models.CreateMinerChannelParams, error) {
	desiredConfig := desiredConfigFromProto(req.GetDesiredConfig())
	desiredConfigJSON, err := desiredConfig.MarshalJSON()
	if err != nil {
		return models.CreateMinerChannelParams{}, err
	}
	var sourceDeviceSetID *int64
	if x, ok := req.GetInitialMembers().(*pb.CreateMinerChannelRequest_SourceDeviceSetId); ok {
		sourceDeviceSetID = &x.SourceDeviceSetId
	}
	var selector *models.MinerChannelDeviceSelector
	if x, ok := req.GetInitialMembers().(*pb.CreateMinerChannelRequest_Select); ok && x.Select != nil {
		selector = &models.MinerChannelDeviceSelector{
			Count:   x.Select.GetCount(),
			Product: stringPtrFromOptional(x.Select.Product),
			Model:   stringPtrFromOptional(x.Select.Model),
		}
	}
	var ownerUserID *int64
	var ownerUsername *string
	if req.GetClaimOwnership() || req.GetExpiresAt() != nil {
		ownerUserID = &info.UserID
		username := info.Username
		ownerUsername = &username
	}
	return models.CreateMinerChannelParams{
		OrgID:             info.OrganizationID,
		Label:             req.GetLabel(),
		OwnerUserID:       ownerUserID,
		OwnerUsername:     ownerUsername,
		ExpiresAt:         timestampToPtr(req.GetExpiresAt()),
		DesiredConfig:     desiredConfig,
		DesiredConfigJSON: desiredConfigJSON,
		Purpose:           req.GetPurpose(),
		SourceActorType:   deriveSourceActorType(info),
		SourceActorID:     deriveSourceActorID(info),
		IdempotencyKey:    nonEmptyPtr(req.GetIdempotencyKey()),
		DeviceIdentifiers: req.GetDeviceIdentifiers().GetDeviceIdentifiers(),
		SourceDeviceSetID: sourceDeviceSetID,
		DeviceSelector:    selector,
	}, nil
}

func toUpdateMinerChannelParams(req *pb.UpdateMinerChannelRequest, orgID int64) (models.UpdateMinerChannelParams, error) {
	desiredConfig := desiredConfigFromProto(req.GetDesiredConfig())
	desiredConfigJSON, err := desiredConfig.MarshalJSON()
	if err != nil {
		return models.UpdateMinerChannelParams{}, err
	}
	return models.UpdateMinerChannelParams{
		OrgID:                orgID,
		MinerChannelID:       req.GetMinerChannelId(),
		Label:                stringPtrFromOptional(req.Label),
		Purpose:              stringPtrFromOptional(req.Purpose),
		ExpiresAt:            timestampToPtr(req.GetExpiresAt()),
		ClearExpiresAt:       req.GetClearExpiresAt(),
		DesiredConfig:        desiredConfig,
		DesiredConfigJSON:    desiredConfigJSON,
		DesiredConfigJSONSet: req.GetDesiredConfig() != nil,
		ClearDesiredConfig:   req.GetClearDesiredConfig(),
	}, nil
}

func toSetMinerChannelFirmwareTargetParams(req *pb.SetMinerChannelFirmwareTargetRequest, info *session.Info) models.SetMinerChannelFirmwareTargetParams {
	return models.SetMinerChannelFirmwareTargetParams{
		OrgID:          info.OrganizationID,
		MinerChannelID: req.GetMinerChannelId(),
		ActorUserID:    info.UserID,
		ActorRole:      info.Role,
		Manufacturer:   stringPtrFromOptional(req.Manufacturer),
		Model:          stringPtrFromOptional(req.Model),
		FirmwareFileID: stringPtrFromOptional(req.FirmwareFileId),
	}
}

func toListMinerChannelsParams(req *pb.ListMinerChannelsRequest, orgID int64) models.ListMinerChannelsParams {
	return models.ListMinerChannelsParams{
		OrgID:           orgID,
		IncludeReleased: req.GetIncludeReleased(),
		PageSize:        req.GetPageSize(),
		PageToken:       req.GetPageToken(),
		Search:          req.GetSearch(),
	}
}

func toListMinerChannelsByOwnerParams(req *pb.GetMyMinerChannelsRequest, info *session.Info) models.ListMinerChannelsByOwnerParams {
	return models.ListMinerChannelsByOwnerParams{
		OrgID:           info.OrganizationID,
		OwnerUserID:     info.UserID,
		IncludeReleased: req.GetIncludeReleased(),
		PageSize:        req.GetPageSize(),
		PageToken:       req.GetPageToken(),
		Search:          req.GetSearch(),
	}
}

func toMembershipMutationParams(orgID int64, userID int64, role string, minerChannelID int64, deviceIdentifiers []string) models.MembershipMutationParams {
	return models.MembershipMutationParams{
		OrgID:             orgID,
		MinerChannelID:    minerChannelID,
		ActorUserID:       userID,
		ActorRole:         role,
		DeviceIdentifiers: deviceIdentifiers,
	}
}

func toListDevicesParams(req *pb.ListDevicesRequest, orgID int64) models.ListDevicesParams {
	return models.ListDevicesParams{
		OrgID:     orgID,
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
		Filter:    toMinerChannelDeviceFilter(req.GetFilter()),
	}
}

func toMinerChannelDeviceFilter(filter *pb.MinerChannelDeviceFilter) models.MinerChannelDeviceFilter {
	if filter == nil {
		return models.MinerChannelDeviceFilter{}
	}
	assignments := make([]models.MinerChannelDeviceAssignment, 0, len(filter.GetAssignments()))
	for _, assignment := range filter.GetAssignments() {
		switch assignment {
		case pb.MinerChannelDeviceAssignment_MINER_CHANNEL_DEVICE_ASSIGNMENT_UNSPECIFIED:
			continue
		case pb.MinerChannelDeviceAssignment_MINER_CHANNEL_DEVICE_ASSIGNMENT_AVAILABLE:
			assignments = append(assignments, models.MinerChannelDeviceAssignmentAvailable)
		case pb.MinerChannelDeviceAssignment_MINER_CHANNEL_DEVICE_ASSIGNMENT_RESERVED:
			assignments = append(assignments, models.MinerChannelDeviceAssignmentReserved)
		}
	}
	return models.MinerChannelDeviceFilter{
		Assignments:     assignments,
		MinerChannelIDs: filter.GetMinerChannelIds(),
		OwnerUserIDs:    filter.GetOwnerUserIds(),
		IncludeUnowned:  filter.GetIncludeUnowned(),
		Manufacturers:   filter.GetManufacturers(),
		Models:          filter.GetModels(),
		Search:          filter.GetSearch(),
	}
}

func toProtoMinerChannel(minerChannel *models.MinerChannel) *pb.MinerChannel {
	if minerChannel == nil {
		return nil
	}
	return &pb.MinerChannel{
		Summary:         toProtoMinerChannelSummary(minerChannel),
		Members:         toProtoMembers(minerChannel.Members),
		FirmwareTargets: toProtoFirmwareTargets(minerChannel.FirmwareTargets),
	}
}

func toProtoMinerChannelSummary(minerChannel *models.MinerChannel) *pb.MinerChannelSummary {
	if minerChannel == nil {
		return nil
	}
	out := &pb.MinerChannelSummary{
		Id:                  minerChannel.ID,
		Label:               minerChannel.Label,
		IsDefault:           minerChannel.IsDefault,
		OwnerUsername:       ptrToString(minerChannel.OwnerUsername),
		ExpiresAt:           timePtrToTimestamp(minerChannel.ExpiresAt),
		DesiredConfig:       desiredConfigToProto(minerChannel.DesiredConfig),
		State:               toProtoState(minerChannel.State),
		Purpose:             minerChannel.Purpose,
		SourceActorType:     string(minerChannel.SourceActorType),
		SourceActorId:       ptrToString(minerChannel.SourceActorID),
		IdempotencyKey:      ptrToString(minerChannel.IdempotencyKey),
		CreatedAt:           timestamppb.New(minerChannel.CreatedAt),
		UpdatedAt:           timestamppb.New(minerChannel.UpdatedAt),
		ExplicitMemberCount: minerChannel.ExplicitMemberCount,
		FirmwareTargets:     toProtoFirmwareTargets(minerChannel.FirmwareTargets),
		FirmwareProgress:    toProtoFirmwareProgress(minerChannel.FirmwareProgress),
		ConfigProgress:      toProtoConfigProgress(minerChannel.ConfigProgress),
	}
	if minerChannel.OwnerUserID != nil {
		out.OwnerUserId = minerChannel.OwnerUserID
	}
	return out
}

func toProtoMinerChannelSummaries(minerChannels []*models.MinerChannel) []*pb.MinerChannelSummary {
	out := make([]*pb.MinerChannelSummary, 0, len(minerChannels))
	for _, minerChannel := range minerChannels {
		out = append(out, toProtoMinerChannelSummary(minerChannel))
	}
	return out
}

func toProtoMembers(members []models.MinerChannelMember) []*pb.MinerChannelMember {
	out := make([]*pb.MinerChannelMember, 0, len(members))
	for _, member := range members {
		pbMember := &pb.MinerChannelMember{
			MinerChannelId:   member.MinerChannelID,
			DeviceIdentifier: member.DeviceIdentifier,
			AddedAt:          timestamppb.New(member.AddedAt),
			Display:          toProtoDeviceDisplay(member.Display),
			FirmwareStatus:   toProtoFirmwareStatus(member.FirmwareStatus),
			ConfigStatuses:   toProtoConfigStatuses(member.ConfigStatuses),
		}
		out = append(out, pbMember)
	}
	return out
}

func toProtoFirmwareTargets(targets []models.MinerChannelFirmwareTarget) []*pb.MinerChannelFirmwareTarget {
	out := make([]*pb.MinerChannelFirmwareTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, &pb.MinerChannelFirmwareTarget{
			Manufacturer:   target.Manufacturer,
			Model:          target.Model,
			FirmwareFileId: ptrToString(target.FirmwareFileID),
		})
	}
	return out
}

func toProtoMinerChannelDevices(devices []models.MinerChannelDevice) []*pb.MinerChannelDevice {
	out := make([]*pb.MinerChannelDevice, 0, len(devices))
	for _, device := range devices {
		pbDevice := &pb.MinerChannelDevice{
			DeviceIdentifier:      device.DeviceIdentifier,
			EffectiveMinerChannel: toProtoMinerChannelSummary(&device.EffectiveMinerChannel),
			Display:               toProtoDeviceDisplay(device.Display),
			FirmwareStatus:        toProtoFirmwareStatus(device.FirmwareStatus),
			ConfigStatuses:        toProtoConfigStatuses(device.ConfigStatuses),
		}
		out = append(out, pbDevice)
	}
	return out
}

func toProtoConfigStatuses(statuses []models.MinerChannelConfigStatus) []*pb.MinerChannelConfigStatus {
	out := make([]*pb.MinerChannelConfigStatus, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, &pb.MinerChannelConfigStatus{
			Dimension: toProtoConfigDimension(status.Dimension), Supported: status.Supported,
			State: toProtoConfigLifecycleState(status.State), RetryCount: status.RetryCount,
			LastError: ptrToString(status.LastError), LastDispatchedAt: timePtrToTimestamp(status.LastDispatchedAt),
			ConfirmedAt: timePtrToTimestamp(status.ConfirmedAt), ObservedAt: timePtrToTimestamp(status.ObservedAt),
		})
	}
	return out
}

func toProtoConfigProgress(progress []models.MinerChannelConfigProgress) []*pb.MinerChannelConfigProgress {
	out := make([]*pb.MinerChannelConfigProgress, 0, len(progress))
	for _, item := range progress {
		out = append(out, &pb.MinerChannelConfigProgress{
			Dimension: toProtoConfigDimension(item.Dimension), TargetedCount: item.TargetedCount,
			UnsupportedCount: item.UnsupportedCount, WaitingCount: item.WaitingCount,
			ApplyingCount: item.ApplyingCount, VerifyingCount: item.VerifyingCount,
			ConvergedCount: item.ConvergedCount, HeldCount: item.HeldCount, FailedCount: item.FailedCount,
		})
	}
	return out
}

func toProtoConfigDimension(dimension models.MinerChannelConfigDimension) pb.MinerChannelConfigDimension {
	if dimension == models.MinerChannelConfigDimensionPools {
		return pb.MinerChannelConfigDimension_MINER_CHANNEL_CONFIG_DIMENSION_POOLS
	}
	return pb.MinerChannelConfigDimension_MINER_CHANNEL_CONFIG_DIMENSION_UNSPECIFIED
}

func toProtoConfigLifecycleState(state models.MinerChannelConfigLifecycleState) pb.MinerChannelConfigLifecycleState {
	switch state {
	case models.MinerChannelConfigStateUnsupported:
		return pb.MinerChannelConfigLifecycleState_MINER_CHANNEL_CONFIG_LIFECYCLE_STATE_UNSUPPORTED
	case models.MinerChannelConfigStateWaitingForObservation:
		return pb.MinerChannelConfigLifecycleState_MINER_CHANNEL_CONFIG_LIFECYCLE_STATE_WAITING_FOR_OBSERVATION
	case models.MinerChannelConfigStateApplying:
		return pb.MinerChannelConfigLifecycleState_MINER_CHANNEL_CONFIG_LIFECYCLE_STATE_APPLYING
	case models.MinerChannelConfigStateVerifying:
		return pb.MinerChannelConfigLifecycleState_MINER_CHANNEL_CONFIG_LIFECYCLE_STATE_VERIFYING
	case models.MinerChannelConfigStateConverged:
		return pb.MinerChannelConfigLifecycleState_MINER_CHANNEL_CONFIG_LIFECYCLE_STATE_CONVERGED
	case models.MinerChannelConfigStateHeld:
		return pb.MinerChannelConfigLifecycleState_MINER_CHANNEL_CONFIG_LIFECYCLE_STATE_HELD
	case models.MinerChannelConfigStateFailed:
		return pb.MinerChannelConfigLifecycleState_MINER_CHANNEL_CONFIG_LIFECYCLE_STATE_FAILED
	default:
		return pb.MinerChannelConfigLifecycleState_MINER_CHANNEL_CONFIG_LIFECYCLE_STATE_UNSPECIFIED
	}
}

func toProtoDeviceDisplay(display models.MinerChannelDeviceDisplay) *pb.MinerChannelDeviceDisplay {
	return &pb.MinerChannelDeviceDisplay{
		Name:            display.Name,
		WorkerName:      display.WorkerName,
		Manufacturer:    display.Manufacturer,
		Model:           display.Model,
		IpAddress:       display.IPAddress,
		SerialNumber:    display.SerialNumber,
		FirmwareVersion: display.FirmwareVersion,
	}
}

func toProtoFirmwareStatus(status *models.MinerChannelFirmwareStatus) *pb.MinerChannelFirmwareStatus {
	if status == nil {
		return nil
	}
	return &pb.MinerChannelFirmwareStatus{
		TargetFirmwareFileId:   status.TargetFirmwareFileID,
		TargetFirmwareVersion:  status.TargetFirmwareVersion,
		CurrentFirmwareVersion: status.CurrentFirmwareVersion,
		State:                  toProtoFirmwareRolloutState(status.State),
		RetryCount:             status.RetryCount,
		LastError:              ptrToString(status.LastError),
		LastDispatchedAt:       timePtrToTimestamp(status.LastDispatchedAt),
		ConfirmedAt:            timePtrToTimestamp(status.ConfirmedAt),
		ObservedAt:             timePtrToTimestamp(status.ObservedAt),
	}
}

func toProtoFirmwareProgress(progress models.MinerChannelFirmwareProgress) *pb.MinerChannelFirmwareProgress {
	if progress.TargetedCount == 0 {
		return nil
	}
	return &pb.MinerChannelFirmwareProgress{
		TargetedCount:       progress.TargetedCount,
		CompleteCount:       progress.CompleteCount,
		QueuedCount:         progress.QueuedCount,
		UpdatingCount:       progress.UpdatingCount,
		VerifyingCount:      progress.VerifyingCount,
		NeedsAttentionCount: progress.NeedsAttentionCount,
		UnknownCount:        progress.UnknownCount,
	}
}

func toProtoState(state models.MinerChannelState) pb.MinerChannelState {
	switch state {
	case models.MinerChannelStateActive:
		return pb.MinerChannelState_MINER_CHANNEL_STATE_ACTIVE
	case models.MinerChannelStateReleased:
		return pb.MinerChannelState_MINER_CHANNEL_STATE_RELEASED
	default:
		return pb.MinerChannelState_MINER_CHANNEL_STATE_UNSPECIFIED
	}
}

func toProtoFirmwareRolloutState(state models.MinerChannelFirmwareRolloutState) pb.MinerChannelFirmwareRolloutState {
	switch state {
	case models.MinerChannelFirmwareRolloutStateNoTarget:
		return pb.MinerChannelFirmwareRolloutState_MINER_CHANNEL_FIRMWARE_ROLLOUT_STATE_NO_TARGET
	case models.MinerChannelFirmwareRolloutStateQueued:
		return pb.MinerChannelFirmwareRolloutState_MINER_CHANNEL_FIRMWARE_ROLLOUT_STATE_QUEUED
	case models.MinerChannelFirmwareRolloutStateUpdating:
		return pb.MinerChannelFirmwareRolloutState_MINER_CHANNEL_FIRMWARE_ROLLOUT_STATE_UPDATING
	case models.MinerChannelFirmwareRolloutStateVerifying:
		return pb.MinerChannelFirmwareRolloutState_MINER_CHANNEL_FIRMWARE_ROLLOUT_STATE_VERIFYING
	case models.MinerChannelFirmwareRolloutStateComplete:
		return pb.MinerChannelFirmwareRolloutState_MINER_CHANNEL_FIRMWARE_ROLLOUT_STATE_COMPLETE
	case models.MinerChannelFirmwareRolloutStateNeedsAttention:
		return pb.MinerChannelFirmwareRolloutState_MINER_CHANNEL_FIRMWARE_ROLLOUT_STATE_NEEDS_ATTENTION
	case models.MinerChannelFirmwareRolloutStateUnknown:
		return pb.MinerChannelFirmwareRolloutState_MINER_CHANNEL_FIRMWARE_ROLLOUT_STATE_UNKNOWN
	default:
		return pb.MinerChannelFirmwareRolloutState_MINER_CHANNEL_FIRMWARE_ROLLOUT_STATE_UNSPECIFIED
	}
}

func desiredConfigFromProto(config *pb.MinerChannelDesiredConfig) *models.MinerChannelDesiredConfig {
	if config == nil || config.GetPools() == nil {
		return nil
	}
	return &models.MinerChannelDesiredConfig{Pools: &models.MinerChannelPoolDesiredConfig{
		PrimaryPoolID: config.GetPools().GetPrimaryPoolId(),
		Backup1PoolID: config.GetPools().Backup_1PoolId,
		Backup2PoolID: config.GetPools().Backup_2PoolId,
	}}
}

func desiredConfigToProto(config *models.MinerChannelDesiredConfig) *pb.MinerChannelDesiredConfig {
	if config == nil || config.Pools == nil {
		return nil
	}
	return &pb.MinerChannelDesiredConfig{Pools: &pb.MinerChannelPoolDesiredConfig{
		PrimaryPoolId:  config.Pools.PrimaryPoolID,
		Backup_1PoolId: config.Pools.Backup1PoolID,
		Backup_2PoolId: config.Pools.Backup2PoolID,
	}}
}

func timestampToPtr(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}

func timePtrToTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func stringPtrFromOptional(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func ptrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func deriveSourceActorType(info *session.Info) models.SourceActorType {
	if info == nil {
		return models.SourceActorUser
	}
	if info.Actor == session.ActorScheduler {
		return models.SourceActorScheduler
	}
	if info.Actor == session.ActorMinerChannel {
		return models.SourceActorMinerChannel
	}
	if info.AuthMethod == session.AuthMethodAPIKey {
		return models.SourceActorAPIKey
	}
	return models.SourceActorUser
}

func deriveSourceActorID(info *session.Info) *string {
	if info == nil || info.Actor == session.ActorScheduler {
		return nil
	}
	id := info.CredentialID()
	if id == "" {
		return nil
	}
	return &id
}
