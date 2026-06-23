package cohort

import (
	"encoding/json"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func toCreateCohortParams(req *pb.CreateCohortRequest, info *session.Info) (models.CreateCohortParams, error) {
	if _, ok := req.GetInitialMembers().(*pb.CreateCohortRequest_SourceDeviceSetId); ok {
		return models.CreateCohortParams{}, fleeterror.NewUnimplementedError("source_device_set_id cohort creation is not implemented yet")
	}
	desiredConfig, err := structToJSON(req.GetDesiredConfig())
	if err != nil {
		return models.CreateCohortParams{}, err
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
		DesiredConfigJSON:     desiredConfig,
		Purpose:               req.GetPurpose(),
		SourceActorType:       deriveSourceActorType(info),
		SourceActorID:         deriveSourceActorID(info),
		IdempotencyKey:        nonEmptyPtr(req.GetIdempotencyKey()),
		DeviceIdentifiers:     req.GetDeviceIdentifiers().GetDeviceIdentifiers(),
	}, nil
}

func toUpdateCohortParams(req *pb.UpdateCohortRequest, orgID int64) (models.UpdateCohortParams, error) {
	desiredConfig, err := structToJSON(req.GetDesiredConfig())
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
		DesiredConfigJSON:        desiredConfig,
		DesiredConfigJSONSet:     req.GetDesiredConfig() != nil,
		ClearDesiredConfig:       req.GetClearDesiredConfig(),
	}, nil
}

func toListCohortsParams(req *pb.ListCohortsRequest, orgID int64) models.ListCohortsParams {
	return models.ListCohortsParams{
		OrgID:           orgID,
		IncludeReleased: req.GetIncludeReleased(),
	}
}

func toListCohortsByOwnerParams(req *pb.GetMyCohortsRequest, info *session.Info) models.ListCohortsByOwnerParams {
	return models.ListCohortsByOwnerParams{
		OrgID:           info.OrganizationID,
		OwnerUserID:     info.UserID,
		IncludeReleased: req.GetIncludeReleased(),
	}
}

func toProtoCohort(cohort *models.Cohort) *pb.Cohort {
	if cohort == nil {
		return nil
	}
	return &pb.Cohort{
		Summary: toProtoCohortSummary(cohort),
		Members: toProtoMembers(cohort.Members),
	}
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
		DesiredConfig:         jsonToStruct(cohort.DesiredConfigJSON),
		State:                 toProtoState(cohort.State),
		Purpose:               cohort.Purpose,
		SourceActorType:       string(cohort.SourceActorType),
		SourceActorId:         ptrToString(cohort.SourceActorID),
		IdempotencyKey:        ptrToString(cohort.IdempotencyKey),
		CreatedAt:             timestamppb.New(cohort.CreatedAt),
		UpdatedAt:             timestamppb.New(cohort.UpdatedAt),
		ExplicitMemberCount:   cohort.ExplicitMemberCount,
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
		}
		if member.SiteID != nil {
			pbMember.SiteId = member.SiteID
		}
		out = append(out, pbMember)
	}
	return out
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

func structToJSON(s *structpb.Struct) (json.RawMessage, error) {
	if s == nil {
		return nil, nil
	}
	b, err := protojson.Marshal(s)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid desired_config: %v", err)
	}
	return json.RawMessage(b), nil
}

func jsonToStruct(raw json.RawMessage) *structpb.Struct {
	if len(raw) == 0 {
		return nil
	}
	var s structpb.Struct
	if err := protojson.Unmarshal(raw, &s); err != nil {
		return nil
	}
	return &s
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
