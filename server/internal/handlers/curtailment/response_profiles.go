package curtailment

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

func (h *Handler) ListCurtailmentResponseProfiles(ctx context.Context, _ *connect.Request[pb.ListCurtailmentResponseProfilesRequest]) (*connect.Response[pb.ListCurtailmentResponseProfilesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.responseProfiles == nil {
		return nil, errCurtailmentNotImplemented("ListCurtailmentResponseProfiles")
	}
	profiles, err := h.responseProfiles.List(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := make([]*pb.CurtailmentResponseProfile, 0, len(profiles))
	siteAllowed := make(map[int64]bool)
	for _, profile := range profiles {
		if profile.SiteID != nil {
			allowed, ok := siteAllowed[*profile.SiteID]
			if !ok {
				if err := requireResponseProfileSitePermission(ctx, authz.PermCurtailmentManage, profile); err != nil {
					if fleeterror.IsForbiddenError(err) {
						siteAllowed[*profile.SiteID] = false
						continue
					}
					return nil, err
				}
				allowed = true
				siteAllowed[*profile.SiteID] = true
			}
			if !allowed {
				continue
			}
		}
		out = append(out, toResponseProfileProto(profile))
	}
	return connect.NewResponse(&pb.ListCurtailmentResponseProfilesResponse{Profiles: out}), nil
}

func (h *Handler) GetCurtailmentResponseProfile(ctx context.Context, req *connect.Request[pb.GetCurtailmentResponseProfileRequest]) (*connect.Response[pb.GetCurtailmentResponseProfileResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.responseProfiles == nil {
		return nil, errCurtailmentNotImplemented("GetCurtailmentResponseProfile")
	}
	profile, err := h.getResponseProfileWithSitePermission(ctx, info.OrganizationID, req.Msg.GetProfileId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetCurtailmentResponseProfileResponse{Profile: toResponseProfileProto(profile)}), nil
}

func (h *Handler) CreateCurtailmentResponseProfile(ctx context.Context, req *connect.Request[pb.CreateCurtailmentResponseProfileRequest]) (*connect.Response[pb.CreateCurtailmentResponseProfileResponse], error) {
	info, err := requireOrgPermissionWithOptionalSiteContext(ctx, authz.PermCurtailmentManage, responseProfileSiteResourceContext(req.Msg.GetSite()))
	if err != nil {
		return nil, err
	}
	if h.responseProfiles == nil {
		return nil, errCurtailmentNotImplemented("CreateCurtailmentResponseProfile")
	}
	profile, err := responseProfileFromCreateRequest(info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	created, err := h.responseProfiles.Create(ctx, domainCurtailment.SaveResponseProfileRequest{
		Profile:             profile,
		CanUseAdminControls: canUseAdminControls(info),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateCurtailmentResponseProfileResponse{Profile: toResponseProfileProto(created)}), nil
}

func (h *Handler) UpdateCurtailmentResponseProfile(ctx context.Context, req *connect.Request[pb.UpdateCurtailmentResponseProfileRequest]) (*connect.Response[pb.UpdateCurtailmentResponseProfileResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.responseProfiles == nil {
		return nil, errCurtailmentNotImplemented("UpdateCurtailmentResponseProfile")
	}
	if _, err := h.getResponseProfileWithSitePermission(ctx, info.OrganizationID, req.Msg.GetProfileId()); err != nil {
		return nil, err
	}
	profile, err := responseProfileFromUpdateRequest(info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	if err := requireResponseProfileSitePermission(ctx, authz.PermCurtailmentManage, &profile); err != nil {
		return nil, err
	}
	updated, err := h.responseProfiles.Update(ctx, domainCurtailment.SaveResponseProfileRequest{
		Profile:             profile,
		CanUseAdminControls: canUseAdminControls(info),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateCurtailmentResponseProfileResponse{Profile: toResponseProfileProto(updated)}), nil
}

func (h *Handler) DeleteCurtailmentResponseProfile(ctx context.Context, req *connect.Request[pb.DeleteCurtailmentResponseProfileRequest]) (*connect.Response[pb.DeleteCurtailmentResponseProfileResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.responseProfiles == nil {
		return nil, errCurtailmentNotImplemented("DeleteCurtailmentResponseProfile")
	}
	if _, err := h.getResponseProfileWithSitePermission(ctx, info.OrganizationID, req.Msg.GetProfileId()); err != nil {
		return nil, err
	}
	if err := h.responseProfiles.Delete(ctx, info.OrganizationID, req.Msg.GetProfileId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteCurtailmentResponseProfileResponse{}), nil
}

func (h *Handler) getResponseProfileWithSitePermission(ctx context.Context, orgID, profileID int64) (*models.ResponseProfile, error) {
	profile, err := h.responseProfiles.Get(ctx, orgID, profileID)
	if err != nil {
		return nil, err
	}
	if err := requireResponseProfileSitePermission(ctx, authz.PermCurtailmentManage, profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func responseProfileSiteResourceContext(site *pb.ScopeSite) authz.ResourceContext {
	if site == nil {
		return authz.ResourceContext{}
	}
	siteID := site.GetSiteId()
	return authz.ResourceContext{SiteID: &siteID}
}

func requireResponseProfileSitePermission(ctx context.Context, permission string, profile *models.ResponseProfile) error {
	if profile == nil || profile.SiteID == nil {
		return nil
	}
	_, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: profile.SiteID})
	return err
}

func responseProfileFromCreateRequest(orgID int64, msg *pb.CreateCurtailmentResponseProfileRequest) (models.ResponseProfile, error) {
	profile, err := responseProfileFromPayload(
		orgID,
		0,
		msg.GetProfileName(),
		msg.GetSite(),
		msg.GetMode(),
		msg.GetStrategy(),
		msg.GetLevel(),
		msg.GetPriority(),
		msg.GetFixedKw(),
		msg.GetModeParams() != nil,
		msg.CurtailBatchSize,
		msg.CurtailBatchIntervalSec,
		msg.RestoreBatchSize,
		msg.RestoreBatchIntervalSec,
		msg.GetIncludeMaintenance(),
		msg.GetForceIncludeMaintenance(),
	)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	return profile, nil
}

func responseProfileFromUpdateRequest(orgID int64, msg *pb.UpdateCurtailmentResponseProfileRequest) (models.ResponseProfile, error) {
	return responseProfileFromPayload(
		orgID,
		msg.GetProfileId(),
		msg.GetProfileName(),
		msg.GetSite(),
		msg.GetMode(),
		msg.GetStrategy(),
		msg.GetLevel(),
		msg.GetPriority(),
		msg.GetFixedKw(),
		msg.GetModeParams() != nil,
		msg.CurtailBatchSize,
		msg.CurtailBatchIntervalSec,
		msg.RestoreBatchSize,
		msg.RestoreBatchIntervalSec,
		msg.GetIncludeMaintenance(),
		msg.GetForceIncludeMaintenance(),
	)
}

func responseProfileFromPayload(
	orgID int64,
	profileID int64,
	name string,
	site *pb.ScopeSite,
	modeProto pb.CurtailmentMode,
	strategyProto pb.CurtailmentStrategy,
	levelProto pb.CurtailmentLevel,
	priorityProto pb.CurtailmentPriority,
	fixedKw *pb.FixedKwParams,
	hasModeParams bool,
	curtailBatchSize *uint32,
	curtailBatchIntervalSec *uint32,
	restoreBatchSize *uint32,
	restoreBatchIntervalSec *uint32,
	includeMaintenance bool,
	forceIncludeMaintenance bool,
) (models.ResponseProfile, error) {
	mode, fixedKw, err := toRequestMode(modeProto, fixedKw, hasModeParams)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	curtailBatchSizeInt, err := optionalUint32ToInt32("curtail_batch_size", curtailBatchSize)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	curtailBatchIntervalInt, err := optionalUint32ToInt32Default(
		"curtail_batch_interval_sec",
		curtailBatchIntervalSec,
		domainCurtailment.DefaultResponseProfileCurtailBatchIntervalSec,
	)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	restoreBatchSizeInt, err := optionalUint32ToInt32Default(
		"restore_batch_size",
		restoreBatchSize,
		domainCurtailment.DefaultResponseProfileRestoreBatchSize,
	)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	restoreBatchIntervalInt, err := optionalUint32ToInt32Default(
		"restore_batch_interval_sec",
		restoreBatchIntervalSec,
		domainCurtailment.DefaultResponseProfileRestoreBatchIntervalSec,
	)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	var targetKW *float64
	var toleranceKW *float64
	if fixedKw != nil {
		v := fixedKw.GetTargetKw()
		targetKW = &v
		if fixedKw.ToleranceKw != nil {
			v := fixedKw.GetToleranceKw()
			toleranceKW = &v
		}
	}
	profile := models.ResponseProfile{
		ID:                      profileID,
		OrgID:                   orgID,
		ProfileName:             name,
		Mode:                    mode,
		Strategy:                strategyName(strategyProto),
		Level:                   levelName(levelProto),
		Priority:                priorityName(priorityProto),
		TargetKW:                targetKW,
		ToleranceKW:             toleranceKW,
		CurtailBatchSize:        curtailBatchSizeInt,
		CurtailBatchIntervalSec: curtailBatchIntervalInt,
		RestoreBatchSize:        restoreBatchSizeInt,
		RestoreBatchIntervalSec: restoreBatchIntervalInt,
		IncludeMaintenance:      includeMaintenance,
		ForceIncludeMaintenance: forceIncludeMaintenance,
	}
	if site != nil {
		siteID := site.GetSiteId()
		profile.SiteID = &siteID
	}
	return profile, nil
}

func toResponseProfileProto(profile *models.ResponseProfile) *pb.CurtailmentResponseProfile {
	if profile == nil {
		return nil
	}
	out := &pb.CurtailmentResponseProfile{
		ProfileId:               profile.ID,
		ProfileName:             profile.ProfileName,
		Mode:                    modeProto(profile.Mode),
		Strategy:                strategyProto(profile.Strategy),
		Level:                   levelProto(profile.Level),
		Priority:                priorityProto(profile.Priority),
		CurtailBatchSize:        uint32PtrSaturating(profile.CurtailBatchSize),
		CurtailBatchIntervalSec: uint32Saturating(profile.CurtailBatchIntervalSec),
		RestoreBatchSize:        uint32Saturating(profile.RestoreBatchSize),
		RestoreBatchIntervalSec: uint32Saturating(profile.RestoreBatchIntervalSec),
		IncludeMaintenance:      profile.IncludeMaintenance,
		ForceIncludeMaintenance: profile.ForceIncludeMaintenance,
		CreatedAt:               profileTimeProto(profile.CreatedAt),
		UpdatedAt:               profileTimeProto(profile.UpdatedAt),
	}
	if profile.SiteID != nil {
		out.Site = &pb.ScopeSite{SiteId: *profile.SiteID}
	}
	if profile.Mode == models.ModeFixedKw && profile.TargetKW != nil {
		fixedKw := &pb.FixedKwParams{TargetKw: *profile.TargetKW}
		if profile.ToleranceKW != nil {
			fixedKw.ToleranceKw = profile.ToleranceKW
		}
		out.ModeParams = &pb.CurtailmentResponseProfile_FixedKw{FixedKw: fixedKw}
	}
	return out
}

func optionalUint32ToInt32(field string, v *uint32) (*int32, error) {
	if v == nil {
		return nil, nil
	}
	converted, err := uint32ToInt32Strict(field, *v)
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

func optionalUint32ToInt32Default(field string, v *uint32, defaultValue int32) (int32, error) {
	if v == nil {
		return defaultValue, nil
	}
	return uint32ToInt32Strict(field, *v)
}

func uint32PtrSaturating(v *int32) *uint32 {
	if v == nil {
		return nil
	}
	out := uint32Saturating(*v)
	return &out
}

func profileTimeProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
