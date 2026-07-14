package cohort

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func toCreateCohortParams(req *pb.CreateCohortRequest, info *session.Info) (models.CreateCohortParams, error) {
	desiredConfig := desiredConfigFromProto(req.GetDesiredConfig())
	desiredConfigJSON, err := desiredConfig.MarshalJSON()
	if err != nil {
		return models.CreateCohortParams{}, err
	}
	var sourceDeviceSetID *int64
	if x, ok := req.GetInitialMembers().(*pb.CreateCohortRequest_SourceDeviceSetId); ok {
		sourceDeviceSetID = &x.SourceDeviceSetId
	}
	var selector *models.CohortDeviceSelector
	if x, ok := req.GetInitialMembers().(*pb.CreateCohortRequest_Select); ok && x.Select != nil {
		selector = &models.CohortDeviceSelector{
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
	return models.CreateCohortParams{
		OrgID:                 info.OrganizationID,
		Label:                 req.GetLabel(),
		OwnerUserID:           ownerUserID,
		OwnerUsername:         ownerUsername,
		ExpiresAt:             timestampToPtr(req.GetExpiresAt()),
		DesiredFirmwareFileID: nonEmptyPtr(req.GetDesiredFirmwareFileId()),
		DesiredConfig:         desiredConfig,
		DesiredConfigJSON:     desiredConfigJSON,
		Purpose:               req.GetPurpose(),
		SourceActorType:       deriveSourceActorType(info),
		SourceActorID:         deriveSourceActorID(info),
		IdempotencyKey:        nonEmptyPtr(req.GetIdempotencyKey()),
		DeviceIdentifiers:     req.GetDeviceIdentifiers().GetDeviceIdentifiers(),
		SourceDeviceSetID:     sourceDeviceSetID,
		DeviceSelector:        selector,
	}, nil
}

func toUpdateCohortParams(req *pb.UpdateCohortRequest, orgID int64) (models.UpdateCohortParams, error) {
	desiredConfig := desiredConfigFromProto(req.GetDesiredConfig())
	desiredConfigJSON, err := desiredConfig.MarshalJSON()
	if err != nil {
		return models.UpdateCohortParams{}, err
	}
	return models.UpdateCohortParams{
		OrgID:                    orgID,
		CohortID:                 req.GetCohortId(),
		Label:                    stringPtrFromOptional(req.Label),
		Purpose:                  stringPtrFromOptional(req.Purpose),
		ExpiresAt:                timestampToPtr(req.GetExpiresAt()),
		ClearExpiresAt:           req.GetClearExpiresAt(),
		DesiredFirmwareFileID:    stringPtrFromOptional(req.DesiredFirmwareFileId),
		DesiredFirmwareFileIDSet: req.DesiredFirmwareFileId != nil,
		DesiredConfig:            desiredConfig,
		DesiredConfigJSON:        desiredConfigJSON,
		DesiredConfigJSONSet:     req.GetDesiredConfig() != nil,
		ClearDesiredConfig:       req.GetClearDesiredConfig(),
	}, nil
}

func toSetCohortFirmwareTargetParams(req *pb.SetCohortFirmwareTargetRequest, info *session.Info) models.SetCohortFirmwareTargetParams {
	return models.SetCohortFirmwareTargetParams{
		OrgID:          info.OrganizationID,
		CohortID:       req.GetCohortId(),
		ActorUserID:    info.UserID,
		ActorRole:      info.Role,
		Manufacturer:   stringPtrFromOptional(req.Manufacturer),
		Model:          stringPtrFromOptional(req.Model),
		FirmwareFileID: stringPtrFromOptional(req.FirmwareFileId),
	}
}

func toListCohortsParams(req *pb.ListCohortsRequest, orgID int64) models.ListCohortsParams {
	return models.ListCohortsParams{
		OrgID:           orgID,
		IncludeReleased: req.GetIncludeReleased(),
		PageSize:        req.GetPageSize(),
		PageToken:       req.GetPageToken(),
		Search:          req.GetSearch(),
	}
}

func toCohortFirmwareVersionHistoryParams(req *pb.GetCohortFirmwareVersionHistoryRequest, orgID int64) models.CohortFirmwareVersionHistoryParams {
	params := models.CohortFirmwareVersionHistoryParams{OrgID: orgID, CohortID: req.GetCohortId()}
	if req.GetStartTime() != nil {
		params.StartTime = req.GetStartTime().AsTime()
	}
	if req.GetEndTime() != nil {
		params.EndTime = req.GetEndTime().AsTime()
	}
	if req.GetGranularity() != nil {
		params.Granularity = req.GetGranularity().AsDuration()
	}
	return params
}

func toListCohortsByOwnerParams(req *pb.GetMyCohortsRequest, info *session.Info) models.ListCohortsByOwnerParams {
	return models.ListCohortsByOwnerParams{
		OrgID:           info.OrganizationID,
		OwnerUserID:     info.UserID,
		IncludeReleased: req.GetIncludeReleased(),
		PageSize:        req.GetPageSize(),
		PageToken:       req.GetPageToken(),
		Search:          req.GetSearch(),
	}
}

func toMembershipMutationParams(orgID int64, userID int64, role string, cohortID int64, deviceIdentifiers []string) models.MembershipMutationParams {
	return models.MembershipMutationParams{
		OrgID:             orgID,
		CohortID:          cohortID,
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
		Filter:    toCohortDeviceFilter(req.GetFilter()),
	}
}

func toCohortDeviceFilter(filter *pb.CohortDeviceFilter) models.CohortDeviceFilter {
	if filter == nil {
		return models.CohortDeviceFilter{}
	}
	assignments := make([]models.CohortDeviceAssignment, 0, len(filter.GetAssignments()))
	for _, assignment := range filter.GetAssignments() {
		switch assignment {
		case pb.CohortDeviceAssignment_COHORT_DEVICE_ASSIGNMENT_UNSPECIFIED:
			continue
		case pb.CohortDeviceAssignment_COHORT_DEVICE_ASSIGNMENT_AVAILABLE:
			assignments = append(assignments, models.CohortDeviceAssignmentAvailable)
		case pb.CohortDeviceAssignment_COHORT_DEVICE_ASSIGNMENT_RESERVED:
			assignments = append(assignments, models.CohortDeviceAssignmentReserved)
		}
	}
	return models.CohortDeviceFilter{
		Assignments:    assignments,
		CohortIDs:      filter.GetCohortIds(),
		OwnerUserIDs:   filter.GetOwnerUserIds(),
		IncludeUnowned: filter.GetIncludeUnowned(),
		Manufacturers:  filter.GetManufacturers(),
		Models:         filter.GetModels(),
		Search:         filter.GetSearch(),
	}
}

func toProtoCohort(cohort *models.Cohort) *pb.Cohort {
	if cohort == nil {
		return nil
	}
	return &pb.Cohort{
		Summary:         toProtoCohortSummary(cohort),
		Members:         toProtoMembers(cohort.Members),
		FirmwareTargets: toProtoFirmwareTargets(cohort.FirmwareTargets),
	}
}

func toProtoCohortFirmwareVersionHistory(history models.CohortFirmwareVersionHistory) *pb.GetCohortFirmwareVersionHistoryResponse {
	points := make([]*pb.CohortFirmwareVersionHistoryPoint, 0, len(history.Points))
	for _, point := range history.Points {
		versions := make([]*pb.CohortFirmwareVersionCount, 0, len(point.Versions))
		for _, version := range point.Versions {
			versions = append(versions, &pb.CohortFirmwareVersionCount{
				FirmwareVersion: version.FirmwareVersion,
				DeviceCount:     version.DeviceCount,
			})
		}
		points = append(points, &pb.CohortFirmwareVersionHistoryPoint{
			Timestamp: timestamppb.New(point.Timestamp),
			Versions:  versions,
		})
	}
	return &pb.GetCohortFirmwareVersionHistoryResponse{MemberCount: history.MemberCount, Points: points}
}

func toProtoCohortSummary(cohort *models.Cohort) *pb.CohortSummary {
	if cohort == nil {
		return nil
	}
	out := &pb.CohortSummary{
		Id:                    cohort.ID,
		Label:                 cohort.Label,
		IsDefault:             cohort.IsDefault,
		OwnerUsername:         ptrToString(cohort.OwnerUsername),
		ExpiresAt:             timePtrToTimestamp(cohort.ExpiresAt),
		DesiredFirmwareFileId: ptrToString(cohort.DesiredFirmwareFileID),
		DesiredConfig:         desiredConfigToProto(cohort.DesiredConfig),
		State:                 toProtoState(cohort.State),
		Purpose:               cohort.Purpose,
		SourceActorType:       string(cohort.SourceActorType),
		SourceActorId:         ptrToString(cohort.SourceActorID),
		IdempotencyKey:        ptrToString(cohort.IdempotencyKey),
		CreatedAt:             timestamppb.New(cohort.CreatedAt),
		UpdatedAt:             timestamppb.New(cohort.UpdatedAt),
		ExplicitMemberCount:   cohort.ExplicitMemberCount,
		FirmwareTargets:       toProtoFirmwareTargets(cohort.FirmwareTargets),
		FirmwareProgress:      toProtoFirmwareProgress(cohort.FirmwareProgress),
		ConfigProgress:        toProtoConfigProgress(cohort.ConfigProgress),
	}
	if cohort.OwnerUserID != nil {
		out.OwnerUserId = cohort.OwnerUserID
	}
	return out
}

func toProtoCohortSummaries(cohorts []*models.Cohort) []*pb.CohortSummary {
	out := make([]*pb.CohortSummary, 0, len(cohorts))
	for _, cohort := range cohorts {
		out = append(out, toProtoCohortSummary(cohort))
	}
	return out
}

func toProtoMembers(members []models.CohortMember) []*pb.CohortMember {
	out := make([]*pb.CohortMember, 0, len(members))
	for _, member := range members {
		pbMember := &pb.CohortMember{
			CohortId:         member.CohortID,
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

func toProtoFirmwareTargets(targets []models.CohortFirmwareTarget) []*pb.CohortFirmwareTarget {
	out := make([]*pb.CohortFirmwareTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, &pb.CohortFirmwareTarget{
			Manufacturer:   target.Manufacturer,
			Model:          target.Model,
			FirmwareFileId: ptrToString(target.FirmwareFileID),
		})
	}
	return out
}

func toProtoCohortDevices(devices []models.CohortDevice) []*pb.CohortDevice {
	out := make([]*pb.CohortDevice, 0, len(devices))
	for _, device := range devices {
		pbDevice := &pb.CohortDevice{
			DeviceIdentifier: device.DeviceIdentifier,
			EffectiveCohort:  toProtoCohortSummary(&device.EffectiveCohort),
			Display:          toProtoDeviceDisplay(device.Display),
			FirmwareStatus:   toProtoFirmwareStatus(device.FirmwareStatus),
			ConfigStatuses:   toProtoConfigStatuses(device.ConfigStatuses),
		}
		out = append(out, pbDevice)
	}
	return out
}

func toProtoConfigStatuses(statuses []models.CohortConfigStatus) []*pb.CohortConfigStatus {
	out := make([]*pb.CohortConfigStatus, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, &pb.CohortConfigStatus{
			Dimension: toProtoConfigDimension(status.Dimension), Supported: status.Supported,
			State: toProtoConfigLifecycleState(status.State), RetryCount: status.RetryCount,
			LastError: ptrToString(status.LastError), LastDispatchedAt: timePtrToTimestamp(status.LastDispatchedAt),
			ConfirmedAt: timePtrToTimestamp(status.ConfirmedAt), ObservedAt: timePtrToTimestamp(status.ObservedAt),
		})
	}
	return out
}

func toProtoConfigProgress(progress []models.CohortConfigProgress) []*pb.CohortConfigProgress {
	out := make([]*pb.CohortConfigProgress, 0, len(progress))
	for _, item := range progress {
		out = append(out, &pb.CohortConfigProgress{
			Dimension: toProtoConfigDimension(item.Dimension), TargetedCount: item.TargetedCount,
			UnsupportedCount: item.UnsupportedCount, WaitingCount: item.WaitingCount,
			ApplyingCount: item.ApplyingCount, VerifyingCount: item.VerifyingCount,
			ConvergedCount: item.ConvergedCount, HeldCount: item.HeldCount, FailedCount: item.FailedCount,
		})
	}
	return out
}

func toProtoConfigDimension(dimension models.CohortConfigDimension) pb.CohortConfigDimension {
	if dimension == models.CohortConfigDimensionPools {
		return pb.CohortConfigDimension_COHORT_CONFIG_DIMENSION_POOLS
	}
	return pb.CohortConfigDimension_COHORT_CONFIG_DIMENSION_UNSPECIFIED
}

func toProtoConfigLifecycleState(state models.CohortConfigLifecycleState) pb.CohortConfigLifecycleState {
	switch state {
	case models.CohortConfigStateUnsupported:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_UNSUPPORTED
	case models.CohortConfigStateWaitingForObservation:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_WAITING_FOR_OBSERVATION
	case models.CohortConfigStateApplying:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_APPLYING
	case models.CohortConfigStateVerifying:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_VERIFYING
	case models.CohortConfigStateConverged:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_CONVERGED
	case models.CohortConfigStateHeld:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_HELD
	case models.CohortConfigStateFailed:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_FAILED
	default:
		return pb.CohortConfigLifecycleState_COHORT_CONFIG_LIFECYCLE_STATE_UNSPECIFIED
	}
}

func toProtoDeviceDisplay(display models.CohortDeviceDisplay) *pb.CohortDeviceDisplay {
	return &pb.CohortDeviceDisplay{
		Name:            display.Name,
		WorkerName:      display.WorkerName,
		Manufacturer:    display.Manufacturer,
		Model:           display.Model,
		IpAddress:       display.IPAddress,
		SerialNumber:    display.SerialNumber,
		FirmwareVersion: display.FirmwareVersion,
	}
}

func toProtoFirmwareStatus(status *models.CohortFirmwareStatus) *pb.CohortFirmwareStatus {
	if status == nil {
		return nil
	}
	return &pb.CohortFirmwareStatus{
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

func toProtoFirmwareProgress(progress models.CohortFirmwareProgress) *pb.CohortFirmwareProgress {
	if progress.TargetedCount == 0 {
		return nil
	}
	return &pb.CohortFirmwareProgress{
		TargetedCount:       progress.TargetedCount,
		CompleteCount:       progress.CompleteCount,
		QueuedCount:         progress.QueuedCount,
		UpdatingCount:       progress.UpdatingCount,
		VerifyingCount:      progress.VerifyingCount,
		NeedsAttentionCount: progress.NeedsAttentionCount,
		UnknownCount:        progress.UnknownCount,
	}
}

func toProtoState(state models.CohortState) pb.CohortState {
	switch state {
	case models.CohortStateActive:
		return pb.CohortState_COHORT_STATE_ACTIVE
	case models.CohortStateReleased:
		return pb.CohortState_COHORT_STATE_RELEASED
	default:
		return pb.CohortState_COHORT_STATE_UNSPECIFIED
	}
}

func toProtoFirmwareRolloutState(state models.CohortFirmwareRolloutState) pb.CohortFirmwareRolloutState {
	switch state {
	case models.CohortFirmwareRolloutStateNoTarget:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_NO_TARGET
	case models.CohortFirmwareRolloutStateQueued:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_QUEUED
	case models.CohortFirmwareRolloutStateUpdating:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_UPDATING
	case models.CohortFirmwareRolloutStateVerifying:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_VERIFYING
	case models.CohortFirmwareRolloutStateComplete:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_COMPLETE
	case models.CohortFirmwareRolloutStateNeedsAttention:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_NEEDS_ATTENTION
	case models.CohortFirmwareRolloutStateUnknown:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_UNKNOWN
	default:
		return pb.CohortFirmwareRolloutState_COHORT_FIRMWARE_ROLLOUT_STATE_UNSPECIFIED
	}
}

func desiredConfigFromProto(config *pb.CohortDesiredConfig) *models.CohortDesiredConfig {
	if config == nil || config.GetPools() == nil {
		return nil
	}
	return &models.CohortDesiredConfig{Pools: &models.CohortPoolDesiredConfig{
		PrimaryPoolID: config.GetPools().GetPrimaryPoolId(),
		Backup1PoolID: config.GetPools().Backup_1PoolId,
		Backup2PoolID: config.GetPools().Backup_2PoolId,
	}}
}

func desiredConfigToProto(config *models.CohortDesiredConfig) *pb.CohortDesiredConfig {
	if config == nil || config.Pools == nil {
		return nil
	}
	return &pb.CohortDesiredConfig{Pools: &pb.CohortPoolDesiredConfig{
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
	if info.Actor == session.ActorCohort {
		return models.SourceActorCohort
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
