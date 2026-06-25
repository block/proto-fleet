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
	deviceSites, err := h.responseProfileDeviceSitesForProfiles(ctx, info.OrganizationID, profiles)
	if err != nil {
		return nil, err
	}
	siteAllowed := make(map[int64]bool)
	for _, profile := range profiles {
		siteContexts, err := h.responseProfileSiteResourceContexts(ctx, info.OrganizationID, profile, deviceSites, false)
		if err != nil {
			return nil, err
		}
		allowed := true
		for _, siteContext := range siteContexts {
			if siteContext.SiteID == nil {
				continue
			}
			siteAllowedValue, ok := siteAllowed[*siteContext.SiteID]
			if !ok {
				if _, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, siteContext); err != nil {
					if fleeterror.IsForbiddenError(err) {
						siteAllowed[*siteContext.SiteID] = false
						allowed = false
						break
					}
					return nil, err
				}
				siteAllowedValue = true
				siteAllowed[*siteContext.SiteID] = true
			}
			if !siteAllowedValue {
				allowed = false
				break
			}
		}
		if !allowed {
			continue
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
	info, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if h.responseProfiles == nil {
		return nil, errCurtailmentNotImplemented("CreateCurtailmentResponseProfile")
	}
	siteContexts, err := h.responseProfileResourceContexts(ctx, info.OrganizationID, req.Msg.GetScopes(), req.Msg.GetSite(), true)
	if err != nil {
		return nil, err
	}
	if err := requireSiteContextPermissions(ctx, authz.PermCurtailmentManage, siteContexts); err != nil {
		return nil, err
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
	existing, err := h.getResponseProfileWithSitePermission(ctx, info.OrganizationID, req.Msg.GetProfileId())
	if err != nil {
		return nil, err
	}
	profile, err := responseProfileFromUpdateRequest(info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	if err := h.requireResponseProfileSitePermission(ctx, info.OrganizationID, authz.PermCurtailmentManage, &profile, true); err != nil {
		return nil, err
	}
	updated, err := h.responseProfiles.Update(ctx, domainCurtailment.SaveResponseProfileRequest{
		Profile:             profile,
		CanUseAdminControls: canUseAdminControls(info),
		ExpectedSiteID:      cloneInt64Ptr(existing.SiteID),
		ExpectedScopeJSON:   cloneBytes(existing.ScopeJSON),
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
	profile, err := h.getResponseProfileWithSitePermission(ctx, info.OrganizationID, req.Msg.GetProfileId())
	if err != nil {
		return nil, err
	}
	if err := h.responseProfiles.Delete(ctx, info.OrganizationID, req.Msg.GetProfileId(), cloneInt64Ptr(profile.SiteID), cloneBytes(profile.ScopeJSON)); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteCurtailmentResponseProfileResponse{}), nil
}

func (h *Handler) getResponseProfileWithSitePermission(ctx context.Context, orgID, profileID int64) (*models.ResponseProfile, error) {
	profile, err := h.responseProfiles.Get(ctx, orgID, profileID)
	if err != nil {
		return nil, err
	}
	if err := h.requireResponseProfileSitePermission(ctx, orgID, authz.PermCurtailmentManage, profile, false); err != nil {
		return nil, err
	}
	return profile, nil
}

func (h *Handler) responseProfileResourceContexts(
	ctx context.Context,
	orgID int64,
	scopes []*pb.CurtailmentScope,
	site *pb.ScopeSite,
	requireKnownDevices bool,
) ([]authz.ResourceContext, error) {
	if len(scopes) > 0 {
		scope, err := toCompositeScope(scopes)
		if err != nil {
			return nil, err
		}
		return h.responseProfileScopeResourceContexts(ctx, orgID, scope, nil, requireKnownDevices)
	}
	if site == nil {
		return nil, nil
	}
	siteID := site.GetSiteId()
	return []authz.ResourceContext{{SiteID: &siteID}}, nil
}

func (h *Handler) requireResponseProfileSitePermission(
	ctx context.Context,
	orgID int64,
	permission string,
	profile *models.ResponseProfile,
	requireKnownDevices bool,
) error {
	siteContexts, err := h.responseProfileSiteResourceContexts(ctx, orgID, profile, nil, requireKnownDevices)
	if err != nil {
		return err
	}
	return requireSiteContextPermissions(ctx, permission, siteContexts)
}

func requireSiteContextPermissions(ctx context.Context, permission string, siteContexts []authz.ResourceContext) error {
	for _, siteContext := range siteContexts {
		if siteContext.SiteID == nil {
			continue
		}
		if _, err := middleware.RequirePermission(ctx, permission, siteContext); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) responseProfileDeviceSitesForProfiles(
	ctx context.Context,
	orgID int64,
	profiles []*models.ResponseProfile,
) (map[string]*int64, error) {
	var deviceIdentifiers []string
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		scope, err := domainCurtailment.ResponseProfileScope(*profile)
		if err != nil {
			return nil, err
		}
		deviceIdentifiers = append(deviceIdentifiers, scope.DeviceIdentifiers...)
	}
	deviceIdentifiers = uniqueResponseProfileDeviceIdentifiers(deviceIdentifiers)
	if len(deviceIdentifiers) == 0 {
		return map[string]*int64{}, nil
	}
	return h.responseProfiles.ListDeviceSites(ctx, orgID, deviceIdentifiers)
}

func (h *Handler) responseProfileSiteResourceContexts(
	ctx context.Context,
	orgID int64,
	profile *models.ResponseProfile,
	deviceSites map[string]*int64,
	requireKnownDevices bool,
) ([]authz.ResourceContext, error) {
	if profile == nil {
		return nil, nil
	}
	scope, err := domainCurtailment.ResponseProfileScope(*profile)
	if err != nil {
		return nil, err
	}
	return h.responseProfileScopeResourceContexts(ctx, orgID, scope, deviceSites, requireKnownDevices)
}

func (h *Handler) responseProfileScopeResourceContexts(
	ctx context.Context,
	orgID int64,
	scope domainCurtailment.Scope,
	deviceSites map[string]*int64,
	requireKnownDevices bool,
) ([]authz.ResourceContext, error) {
	siteIDs := siteIDsFromResourceContexts(siteResourceContextsForScope(scope))
	deviceIdentifiers := uniqueResponseProfileDeviceIdentifiers(scope.DeviceIdentifiers)
	if len(deviceIdentifiers) == 0 {
		return siteResourceContextsForScope(domainCurtailment.Scope{SiteIDs: siteIDs}), nil
	}
	if deviceSites == nil {
		var err error
		deviceSites, err = h.responseProfiles.ListDeviceSites(ctx, orgID, deviceIdentifiers)
		if err != nil {
			return nil, err
		}
	}
	for _, deviceIdentifier := range deviceIdentifiers {
		siteID, ok := deviceSites[deviceIdentifier]
		if !ok {
			if requireKnownDevices {
				return nil, fleeterror.NewNotFoundError("one or more device identifiers were not found")
			}
			continue
		}
		if siteID != nil {
			siteIDs = append(siteIDs, *siteID)
		}
	}
	return siteResourceContextsForScope(domainCurtailment.Scope{SiteIDs: siteIDs}), nil
}

func siteIDsFromResourceContexts(contexts []authz.ResourceContext) []int64 {
	siteIDs := make([]int64, 0, len(contexts))
	for _, context := range contexts {
		if context.SiteID != nil {
			siteIDs = append(siteIDs, *context.SiteID)
		}
	}
	return siteIDs
}

func uniqueResponseProfileDeviceIdentifiers(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func cloneInt64Ptr(v *int64) *int64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func cloneBytes(v []byte) []byte {
	if len(v) == 0 {
		return nil
	}
	return append([]byte(nil), v...)
}

func responseProfileFromCreateRequest(orgID int64, msg *pb.CreateCurtailmentResponseProfileRequest) (models.ResponseProfile, error) {
	profile, err := responseProfileFromPayload(
		orgID,
		0,
		msg.GetProfileName(),
		msg.GetSite(),
		msg.GetScopes(),
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
		msg.GetPostEventCooldownSec(),
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
		msg.GetScopes(),
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
		msg.GetPostEventCooldownSec(),
	)
}

func responseProfileFromPayload(
	orgID int64,
	profileID int64,
	name string,
	site *pb.ScopeSite,
	scopes []*pb.CurtailmentScope,
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
	postEventCooldownSec uint32,
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
	postEventCooldownInt, err := uint32ToInt32Strict("post_event_cooldown_sec", postEventCooldownSec)
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
		PostEventCooldownSec:    postEventCooldownInt,
	}
	if site != nil {
		siteID := site.GetSiteId()
		profile.SiteID = &siteID
	}
	if len(scopes) > 0 {
		scope, err := toCompositeScope(scopes)
		if err != nil {
			return models.ResponseProfile{}, err
		}
		scopeJSON, err := domainCurtailment.MarshalScopeJSON(scope)
		if err != nil {
			return models.ResponseProfile{}, err
		}
		if scope.Type == models.ScopeTypeWholeOrg {
			scopeJSON = []byte(`{"whole_org":true}`)
		}
		profile.ScopeJSON = scopeJSON
		profile.SiteID = legacySiteIDForScope(scope)
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
		PostEventCooldownSec:    uint32Saturating(profile.PostEventCooldownSec),
		CreatedAt:               profileTimeProto(profile.CreatedAt),
		UpdatedAt:               profileTimeProto(profile.UpdatedAt),
	}
	if profile.SiteID != nil {
		out.Site = &pb.ScopeSite{SiteId: *profile.SiteID}
	}
	if scope, hasScope, err := domainCurtailment.ScopeFromJSON(profile.ScopeJSON); err == nil && hasScope {
		if scopes := protoScopesFromDomainScope(scope); len(scopes) > 0 {
			out.Scopes = scopes
		}
	} else if profile.SiteID != nil {
		scope, err := domainCurtailment.ResponseProfileScope(*profile)
		if err != nil {
			return out
		}
		if scopes := protoScopesFromDomainScope(scope); len(scopes) > 0 {
			out.Scopes = scopes
		}
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

func legacySiteIDForScope(scope domainCurtailment.Scope) *int64 {
	if scope.Type != models.ScopeTypeSite || len(scope.SiteIDs) != 1 {
		return nil
	}
	siteID := scope.SiteIDs[0]
	return &siteID
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
