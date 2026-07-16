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
	facilityFanDeviceSites, err := h.responseProfileFacilityFanDeviceSitesForProfiles(ctx, info.OrganizationID, profiles)
	if err != nil {
		return nil, err
	}
	siteAllowed := make(map[int64]bool)
	facilityFanSiteAllowed := make(map[int64]bool)
	orgWideAllowed := false
	orgWideChecked := false
	for _, profile := range profiles {
		requirements, err := h.responseProfileResourceContextRequirements(ctx, info.OrganizationID, profile, deviceSites, false)
		if err != nil {
			return nil, err
		}
		allowed, err := resourceContextRequirementsAllowed(
			ctx,
			authz.PermCurtailmentManage,
			requirements,
			siteAllowed,
			&orgWideAllowed,
			&orgWideChecked,
		)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		allowed, err = facilityFanSiteAccessAllowed(
			ctx,
			profile.FacilityFanDeviceIDs,
			facilityFanDeviceSites,
			facilityFanSiteAllowed,
		)
		if err != nil {
			return nil, err
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
	requirements, err := h.responseProfileResourceContexts(ctx, info.OrganizationID, req.Msg.GetScopes(), req.Msg.GetSite(), true)
	if err != nil {
		return nil, err
	}
	if err := requireResourceContextPermissions(ctx, authz.PermCurtailmentManage, requirements); err != nil {
		return nil, err
	}
	profile, err := responseProfileFromCreateRequest(info.OrganizationID, req.Msg)
	if err != nil {
		return nil, err
	}
	authorizedFacilityFanDevices, err := h.authorizeFacilityFanDevices(ctx, info.OrganizationID, profile.FacilityFanDeviceIDs)
	if err != nil {
		return nil, err
	}
	created, err := h.responseProfiles.Create(ctx, domainCurtailment.SaveResponseProfileRequest{
		Profile:                      profile,
		CanUseAdminControls:          canUseAdminControls(info),
		AuthorizedFacilityFanDevices: authorizedFacilityFanDevices,
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
	if responseProfileUpdateOmitsScope(req.Msg) {
		preserveScope, err := responseProfileUpdateShouldPreserveOmittedScope(existing)
		if err != nil {
			return nil, err
		}
		if preserveScope {
			profile.SiteID = cloneInt64Ptr(existing.SiteID)
			profile.ScopeJSON = cloneBytes(existing.ScopeJSON)
		}
	}
	if !req.Msg.GetReplaceFacilityFanSettings() {
		profile.FacilityFanDeviceIDs = append([]int64(nil), existing.FacilityFanDeviceIDs...)
		profile.FanOffDelaySec = existing.FanOffDelaySec
		profile.FanRestoreDelaySec = existing.FanRestoreDelaySec
	}
	if err := h.requireResponseProfileSitePermission(ctx, info.OrganizationID, authz.PermCurtailmentManage, &profile, true); err != nil {
		return nil, err
	}
	authorizedFacilityFanDevices, err := h.authorizeFacilityFanDevices(ctx, info.OrganizationID, profile.FacilityFanDeviceIDs)
	if err != nil {
		return nil, err
	}
	updated, err := h.responseProfiles.Update(ctx, domainCurtailment.SaveResponseProfileRequest{
		Profile:                      profile,
		CanUseAdminControls:          canUseAdminControls(info),
		ExpectedSiteID:               cloneInt64Ptr(existing.SiteID),
		ExpectedScopeJSON:            cloneBytes(existing.ScopeJSON),
		ExpectedFacilityFanSettings:  responseProfileFanSettings(existing),
		AuthorizedFacilityFanDevices: authorizedFacilityFanDevices,
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

func responseProfileFanSettings(profile *models.ResponseProfile) models.ResponseProfileFanSettings {
	return models.ResponseProfileFanSettings{
		FacilityFanDeviceIDs: append([]int64(nil), profile.FacilityFanDeviceIDs...),
		FanOffDelaySec:       profile.FanOffDelaySec,
		FanRestoreDelaySec:   profile.FanRestoreDelaySec,
	}
}

func (h *Handler) getResponseProfileWithSitePermission(ctx context.Context, orgID, profileID int64) (*models.ResponseProfile, error) {
	profile, err := h.responseProfiles.Get(ctx, orgID, profileID)
	if err != nil {
		return nil, err
	}
	if err := h.requireResponseProfileSitePermission(ctx, orgID, authz.PermCurtailmentManage, profile, false); err != nil {
		return nil, err
	}
	if err := h.requireFacilityFanSitePermissions(ctx, orgID, profile.FacilityFanDeviceIDs); err != nil {
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
) (scopeResourceContextRequirements, error) {
	if len(scopes) > 0 {
		return h.scopeResourceContextRequirementsFromProto(ctx, orgID, scopes, nil, requireKnownDevices)
	}
	if site == nil {
		return scopeResourceContextRequirements{}, nil
	}
	siteID := site.GetSiteId()
	return scopeResourceContextRequirements{siteContexts: []authz.ResourceContext{{SiteID: &siteID}}}, nil
}

func (h *Handler) requireResponseProfileSitePermission(
	ctx context.Context,
	orgID int64,
	permission string,
	profile *models.ResponseProfile,
	requireKnownDevices bool,
) error {
	requirements, err := h.responseProfileResourceContextRequirements(ctx, orgID, profile, nil, requireKnownDevices)
	if err != nil {
		return err
	}
	return requireResourceContextPermissions(ctx, permission, requirements)
}

func (h *Handler) requireFacilityFanSitePermissions(ctx context.Context, orgID int64, deviceIDs []int64) error {
	_, err := h.authorizeFacilityFanDevices(ctx, orgID, deviceIDs)
	return err
}

func (h *Handler) authorizeFacilityFanDevices(
	ctx context.Context,
	orgID int64,
	deviceIDs []int64,
) (map[int64]models.ResponseProfileInfrastructureDevice, error) {
	devices, err := h.responseProfiles.FacilityFanDevices(ctx, orgID, deviceIDs)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil, fleeterror.NewNotFoundError("one or more infrastructure devices were not found")
		}
		return nil, err
	}
	seenSiteIDs := make(map[int64]struct{}, len(devices))
	for _, device := range devices {
		if _, seen := seenSiteIDs[device.SiteID]; seen {
			continue
		}
		seenSiteIDs[device.SiteID] = struct{}{}
		siteID := device.SiteID
		resourceContext := authz.ResourceContext{SiteID: &siteID}
		readable, err := middleware.HasPermission(ctx, authz.PermSiteRead, resourceContext)
		if err != nil {
			return nil, err
		}
		if !readable {
			return nil, fleeterror.NewNotFoundError("one or more infrastructure devices were not found")
		}
		if _, err := middleware.RequirePermission(ctx, authz.PermCurtailmentManage, resourceContext); err != nil {
			return nil, err
		}
	}
	return devices, nil
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
	if h.responseProfiles == nil {
		return nil, nil
	}
	return h.responseProfiles.ListDeviceSites(ctx, orgID, deviceIdentifiers)
}

func (h *Handler) responseProfileFacilityFanDeviceSitesForProfiles(
	ctx context.Context,
	orgID int64,
	profiles []*models.ResponseProfile,
) (map[int64]int64, error) {
	seen := make(map[int64]struct{})
	var deviceIDs []int64
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		for _, deviceID := range profile.FacilityFanDeviceIDs {
			if _, ok := seen[deviceID]; ok {
				continue
			}
			seen[deviceID] = struct{}{}
			deviceIDs = append(deviceIDs, deviceID)
		}
	}
	return h.responseProfiles.FacilityFanDeviceSites(ctx, orgID, deviceIDs)
}

func facilityFanSiteAccessAllowed(
	ctx context.Context,
	deviceIDs []int64,
	deviceSites map[int64]int64,
	siteAllowed map[int64]bool,
) (bool, error) {
	for _, deviceID := range deviceIDs {
		siteID, ok := deviceSites[deviceID]
		if !ok {
			return false, fleeterror.NewNotFoundErrorf("infrastructure device not found: %d", deviceID)
		}
		if allowed, checked := siteAllowed[siteID]; checked {
			if !allowed {
				return false, nil
			}
			continue
		}

		resourceContext := authz.ResourceContext{SiteID: &siteID}
		readable, err := middleware.HasPermission(ctx, authz.PermSiteRead, resourceContext)
		if err != nil {
			return false, err
		}
		manageable, err := middleware.HasPermission(ctx, authz.PermCurtailmentManage, resourceContext)
		if err != nil {
			return false, err
		}
		allowed := readable && manageable
		siteAllowed[siteID] = allowed
		if !allowed {
			return false, nil
		}
	}
	return true, nil
}

func (h *Handler) responseProfileResourceContextRequirements(
	ctx context.Context,
	orgID int64,
	profile *models.ResponseProfile,
	deviceSites map[string]*int64,
	requireKnownDevices bool,
) (scopeResourceContextRequirements, error) {
	if profile == nil {
		return scopeResourceContextRequirements{}, nil
	}
	scope, err := domainCurtailment.ResponseProfileScope(*profile)
	if err != nil {
		return scopeResourceContextRequirements{}, err
	}
	return h.scopeResourceContextRequirements(ctx, orgID, scope, deviceSites, requireKnownDevices)
}

func resourceContextRequirementsAllowed(
	ctx context.Context,
	permission string,
	requirements scopeResourceContextRequirements,
	siteAllowed map[int64]bool,
	orgWideAllowed *bool,
	orgWideChecked *bool,
) (bool, error) {
	if requirements.requireOrgWide {
		if !*orgWideChecked {
			*orgWideChecked = true
			if _, err := middleware.RequireOrgWidePermission(ctx, permission); err != nil {
				if fleeterror.IsForbiddenError(err) {
					*orgWideAllowed = false
				} else {
					return false, err
				}
			} else {
				*orgWideAllowed = true
			}
		}
		if !*orgWideAllowed {
			return false, nil
		}
	}
	for _, siteContext := range requirements.siteContexts {
		if siteContext.SiteID == nil {
			continue
		}
		siteAllowedValue, ok := siteAllowed[*siteContext.SiteID]
		if !ok {
			if _, err := middleware.RequirePermission(ctx, permission, siteContext); err != nil {
				if fleeterror.IsForbiddenError(err) {
					siteAllowed[*siteContext.SiteID] = false
					return false, nil
				}
				return false, err
			}
			siteAllowedValue = true
			siteAllowed[*siteContext.SiteID] = true
		}
		if !siteAllowedValue {
			return false, nil
		}
	}
	return true, nil
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

func responseProfileUpdateOmitsScope(msg *pb.UpdateCurtailmentResponseProfileRequest) bool {
	return msg.GetSite() == nil && len(msg.GetScopes()) == 0
}

func responseProfileUpdateShouldPreserveOmittedScope(profile *models.ResponseProfile) (bool, error) {
	if profile == nil {
		return false, nil
	}
	scope, err := domainCurtailment.ResponseProfileScope(*profile)
	if err != nil {
		return false, err
	}
	switch scope.Type {
	case models.ScopeTypeMixed, models.ScopeTypeDeviceList, models.ScopeTypeDeviceSets:
		return true, nil
	case models.ScopeTypeWholeOrg, models.ScopeTypeSite, "":
		return false, nil
	default:
		return false, nil
	}
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
		msg.GetForceIncludeAllPairedMiners(),
		msg.GetFacilityFanDeviceIds(),
		msg.GetFanOffDelaySec(),
		msg.GetFanRestoreDelaySec(),
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
		msg.GetForceIncludeAllPairedMiners(),
		msg.GetFacilityFanDeviceIds(),
		msg.GetFanOffDelaySec(),
		msg.GetFanRestoreDelaySec(),
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
	forceIncludeAllPairedMiners bool,
	facilityFanDeviceIDs []int64,
	fanOffDelaySec uint32,
	fanRestoreDelaySec uint32,
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
		0,
	)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	restoreBatchIntervalInt, err := optionalUint32ToInt32Default(
		"restore_batch_interval_sec",
		restoreBatchIntervalSec,
		0,
	)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	postEventCooldownInt, err := uint32ToInt32Strict("post_event_cooldown_sec", postEventCooldownSec)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	fanOffDelayInt, err := uint32ToInt32Strict("fan_off_delay_sec", fanOffDelaySec)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	fanRestoreDelayInt, err := uint32ToInt32Strict("fan_restore_delay_sec", fanRestoreDelaySec)
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
		ID:                          profileID,
		OrgID:                       orgID,
		ProfileName:                 name,
		Mode:                        mode,
		Strategy:                    strategyName(strategyProto),
		Level:                       levelName(levelProto),
		Priority:                    priorityName(priorityProto),
		TargetKW:                    targetKW,
		ToleranceKW:                 toleranceKW,
		CurtailBatchSize:            curtailBatchSizeInt,
		CurtailBatchIntervalSec:     curtailBatchIntervalInt,
		RestoreBatchSize:            restoreBatchSizeInt,
		RestoreBatchIntervalSec:     restoreBatchIntervalInt,
		IncludeMaintenance:          includeMaintenance,
		ForceIncludeMaintenance:     forceIncludeMaintenance,
		ForceIncludeAllPairedMiners: forceIncludeAllPairedMiners,
		PostEventCooldownSec:        postEventCooldownInt,
		FacilityFanDeviceIDs:        append([]int64(nil), facilityFanDeviceIDs...),
		FanOffDelaySec:              fanOffDelayInt,
		FanRestoreDelaySec:          fanRestoreDelayInt,
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
		ProfileId:                   profile.ID,
		ProfileName:                 profile.ProfileName,
		Mode:                        modeProto(profile.Mode),
		Strategy:                    strategyProto(profile.Strategy),
		Level:                       levelProto(profile.Level),
		Priority:                    priorityProto(profile.Priority),
		CurtailBatchSize:            uint32PtrSaturating(profile.CurtailBatchSize),
		CurtailBatchIntervalSec:     uint32Saturating(profile.CurtailBatchIntervalSec),
		RestoreBatchSize:            uint32Saturating(profile.RestoreBatchSize),
		RestoreBatchIntervalSec:     uint32Saturating(profile.RestoreBatchIntervalSec),
		IncludeMaintenance:          profile.IncludeMaintenance,
		ForceIncludeMaintenance:     profile.ForceIncludeMaintenance,
		ForceIncludeAllPairedMiners: profile.ForceIncludeAllPairedMiners,
		PostEventCooldownSec:        uint32Saturating(profile.PostEventCooldownSec),
		FacilityFanDeviceIds:        append([]int64(nil), profile.FacilityFanDeviceIDs...),
		FanOffDelaySec:              uint32Saturating(profile.FanOffDelaySec),
		FanRestoreDelaySec:          uint32Saturating(profile.FanRestoreDelaySec),
		CreatedAt:                   profileTimeProto(profile.CreatedAt),
		UpdatedAt:                   profileTimeProto(profile.UpdatedAt),
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
