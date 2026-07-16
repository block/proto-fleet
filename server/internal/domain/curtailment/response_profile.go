package curtailment

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	maxResponseProfileNameLength = 64

	DefaultResponseProfileCurtailBatchIntervalSec int32 = 0
	MaxPostEventCooldownSec                       int32 = 24 * 60 * 60

	// RestoreBatchSizeMax is the explicit safety limit for one restore wave.
	// Positive restore_batch_size inputs are bounded to this value; immediate
	// restore uses the same ceiling instead of reintroducing hidden adaptive
	// clamps.
	RestoreBatchSizeMax int32 = 10000

	responseProfileBatchSizeMax int32   = RestoreBatchSizeMax
	responseProfileNumericMax   float64 = 999999999.999
)

// ResponseProfileService validates and persists reusable curtailment response
// behavior. It does not execute profiles; automation owns trigger binding.
type ResponseProfileService struct {
	store interfaces.ResponseProfileStore
}

func NewResponseProfileService(store interfaces.ResponseProfileStore) *ResponseProfileService {
	return &ResponseProfileService{store: store}
}

type SaveResponseProfileRequest struct {
	Profile                      models.ResponseProfile
	CanUseAdminControls          bool
	ExpectedSiteID               *int64
	ExpectedScopeJSON            []byte
	ExpectedFacilityFanSettings  models.ResponseProfileFanSettings
	AuthorizedFacilityFanDevices map[int64]models.ResponseProfileInfrastructureDevice
}

func (s *ResponseProfileService) List(ctx context.Context, orgID int64) ([]*models.ResponseProfile, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	return s.store.ListResponseProfiles(ctx, orgID)
}

func (s *ResponseProfileService) Get(ctx context.Context, orgID, profileID int64) (*models.ResponseProfile, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if profileID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("profile_id must be set")
	}
	return s.store.GetResponseProfile(ctx, orgID, profileID)
}

func (s *ResponseProfileService) ListDeviceSites(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string]*int64, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if len(deviceIdentifiers) == 0 {
		return map[string]*int64{}, nil
	}
	return s.store.ListResponseProfileDeviceSites(ctx, orgID, deviceIdentifiers)
}

// FacilityFanDeviceSites resolves each requested facility fan to its site.
// List handlers use the batched result to enforce fan-site visibility without
// issuing one infrastructure lookup per response profile.
func (s *ResponseProfileService) FacilityFanDeviceSites(ctx context.Context, orgID int64, deviceIDs []int64) (map[int64]int64, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	deviceIDs, devices, err := s.loadFacilityFanDevices(ctx, orgID, deviceIDs)
	if err != nil {
		return nil, err
	}

	deviceSites := make(map[int64]int64, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		deviceSites[deviceID] = devices[deviceID].SiteID
	}
	return deviceSites, nil
}

// FacilityFanDevices resolves the full fan snapshot used for authorization and
// the subsequent locked profile save. The store rejects the mutation if any
// device moves sites after this snapshot is authorized.
func (s *ResponseProfileService) FacilityFanDevices(ctx context.Context, orgID int64, deviceIDs []int64) (map[int64]models.ResponseProfileInfrastructureDevice, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	_, devices, err := s.loadFacilityFanDevices(ctx, orgID, deviceIDs)
	return devices, err
}

func (s *ResponseProfileService) Create(ctx context.Context, req SaveResponseProfileRequest) (*models.ResponseProfile, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	profile, infrastructureDevices, err := s.validateAndNormalize(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.store.CreateResponseProfile(ctx, profile, infrastructureDevices)
}

func (s *ResponseProfileService) Update(ctx context.Context, req SaveResponseProfileRequest) (*models.ResponseProfile, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	if req.Profile.ID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("profile_id must be set")
	}
	profile, infrastructureDevices, err := s.validateAndNormalize(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(profile.FacilityFanDeviceIDs) > 0 {
		count, err := s.store.CountAutomationRulesByResponseProfile(ctx, profile.OrgID, profile.ID)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, fleeterror.NewFailedPreconditionError(
				"response profiles used by automation rules cannot include facility fans until fan sequencing is available",
			)
		}
	}
	return s.store.UpdateResponseProfile(
		ctx,
		profile,
		infrastructureDevices,
		req.ExpectedSiteID,
		req.ExpectedScopeJSON,
		req.ExpectedFacilityFanSettings,
	)
}

func (s *ResponseProfileService) Delete(
	ctx context.Context,
	orgID,
	profileID int64,
	expectedSiteID *int64,
	expectedScopeJSON []byte,
	expectedFacilityFanSettings models.ResponseProfileFanSettings,
) error {
	if s == nil || s.store == nil {
		return fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	if orgID <= 0 {
		return fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if profileID <= 0 {
		return fleeterror.NewInvalidArgumentError("profile_id must be set")
	}
	count, err := s.store.CountAutomationRulesByResponseProfile(ctx, orgID, profileID)
	if err != nil {
		return err
	}
	if count > 0 {
		return fleeterror.NewFailedPreconditionError(
			"curtailment response profile is referenced by automation rules; delete or update those rules first",
		)
	}
	return s.store.DeleteResponseProfile(
		ctx,
		orgID,
		profileID,
		expectedSiteID,
		expectedScopeJSON,
		expectedFacilityFanSettings,
	)
}

func (s *ResponseProfileService) validateAndNormalize(ctx context.Context, req SaveResponseProfileRequest) (models.ResponseProfile, map[int64]models.ResponseProfileInfrastructureDevice, error) {
	profile := normalizeResponseProfile(req.Profile)
	if profile.OrgID <= 0 {
		return models.ResponseProfile{}, nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if err := validateResponseProfileName(profile.ProfileName); err != nil {
		return models.ResponseProfile{}, nil, err
	}
	scope, err := ResponseProfileScope(profile)
	if err != nil {
		return models.ResponseProfile{}, nil, err
	}
	explicitWholeOrgScope := isExplicitWholeOrgScopeJSON(profile.ScopeJSON)
	for _, siteID := range normalizeScope(scope).SiteIDs {
		belongs, err := s.store.SiteBelongsToOrg(ctx, profile.OrgID, siteID)
		if err != nil {
			return models.ResponseProfile{}, nil, err
		}
		if !belongs {
			return models.ResponseProfile{}, nil, fleeterror.NewNotFoundErrorf("site not found: %d", siteID)
		}
	}
	var infrastructureDevices map[int64]models.ResponseProfileInfrastructureDevice
	profile.FacilityFanDeviceIDs, infrastructureDevices, err = s.validateFacilityFanDevices(
		ctx,
		profile.OrgID,
		profile.FacilityFanDeviceIDs,
		req.AuthorizedFacilityFanDevices,
	)
	if err != nil {
		return models.ResponseProfile{}, nil, err
	}
	scopeJSON, err := MarshalScopeJSON(scope)
	if err != nil {
		return models.ResponseProfile{}, nil, err
	}
	if normalizeScope(scope).Type == models.ScopeTypeWholeOrg && explicitWholeOrgScope {
		scopeJSON = []byte(`{"whole_org":true}`)
	}
	profile.ScopeJSON = scopeJSON
	profile.SiteID = responseProfileLegacySiteID(scope)
	if err := validateResponseProfileBehavior(profile, req.CanUseAdminControls); err != nil {
		return models.ResponseProfile{}, nil, err
	}
	return profile, infrastructureDevices, nil
}

func (s *ResponseProfileService) validateFacilityFanDevices(
	ctx context.Context,
	orgID int64,
	deviceIDs []int64,
	authorizedDevices map[int64]models.ResponseProfileInfrastructureDevice,
) ([]int64, map[int64]models.ResponseProfileInfrastructureDevice, error) {
	var devices map[int64]models.ResponseProfileInfrastructureDevice
	var err error
	if authorizedDevices == nil {
		deviceIDs, devices, err = s.loadFacilityFanDevices(ctx, orgID, deviceIDs)
		if err != nil {
			return nil, nil, err
		}
	} else {
		if hasNonPositiveInt64(deviceIDs) {
			return nil, nil, fleeterror.NewInvalidArgumentError("facility_fan_device_ids must be positive")
		}
		deviceIDs = uniquePositiveInt64s(deviceIDs)
		if len(deviceIDs) != len(authorizedDevices) {
			return nil, nil, fleeterror.NewFailedPreconditionError("authorized infrastructure devices do not match the response profile")
		}
		devices = make(map[int64]models.ResponseProfileInfrastructureDevice, len(authorizedDevices))
		for _, deviceID := range deviceIDs {
			device, ok := authorizedDevices[deviceID]
			if !ok || device.ID != deviceID {
				return nil, nil, fleeterror.NewFailedPreconditionError("authorized infrastructure devices do not match the response profile")
			}
			devices[deviceID] = device
		}
	}

	for _, deviceID := range deviceIDs {
		device := devices[deviceID]
		if !device.Enabled {
			slog.WarnContext(
				ctx,
				"response profile references disabled infrastructure device",
				"org_id", orgID,
				"infrastructure_device_id", deviceID,
			)
		}
	}
	return deviceIDs, devices, nil
}

func (s *ResponseProfileService) loadFacilityFanDevices(
	ctx context.Context,
	orgID int64,
	deviceIDs []int64,
) ([]int64, map[int64]models.ResponseProfileInfrastructureDevice, error) {
	if hasNonPositiveInt64(deviceIDs) {
		return nil, nil, fleeterror.NewInvalidArgumentError("facility_fan_device_ids must be positive")
	}
	deviceIDs = uniquePositiveInt64s(deviceIDs)
	if len(deviceIDs) == 0 {
		return nil, map[int64]models.ResponseProfileInfrastructureDevice{}, nil
	}

	devices, err := s.store.ListResponseProfileInfrastructureDevices(ctx, orgID, deviceIDs)
	if err != nil {
		return nil, nil, err
	}
	for _, deviceID := range deviceIDs {
		if _, ok := devices[deviceID]; !ok {
			// Missing, deleted, and cross-organization devices are intentionally
			// indistinguishable so profile validation cannot expose OT inventory.
			return nil, nil, fleeterror.NewNotFoundErrorf("infrastructure device not found: %d", deviceID)
		}
	}

	return deviceIDs, devices, nil
}

func normalizeResponseProfile(profile models.ResponseProfile) models.ResponseProfile {
	profile.ProfileName = strings.TrimSpace(profile.ProfileName)
	if profile.Strategy == "" {
		profile.Strategy = models.StrategyLeastEfficientFirst
	}
	if profile.Level == "" {
		profile.Level = models.LevelFull
	}
	if profile.Priority == "" {
		profile.Priority = models.PriorityNormal
	}
	if profile.CurtailBatchIntervalSec == 0 {
		profile.CurtailBatchIntervalSec = DefaultResponseProfileCurtailBatchIntervalSec
	}
	return profile
}

func validateResponseProfileName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fleeterror.NewInvalidArgumentError("profile_name is required")
	}
	if n := utf8.RuneCountInString(name); n > maxResponseProfileNameLength {
		return fleeterror.NewInvalidArgumentErrorf(
			"profile_name must be at most %d characters, got %d",
			maxResponseProfileNameLength,
			n,
		)
	}
	return nil
}

func validateResponseProfileBehavior(profile models.ResponseProfile, canUseAdminControls bool) error {
	targetKW, toleranceKW := float64Value(profile.TargetKW), float64Value(profile.ToleranceKW)
	scope, err := ResponseProfileScope(profile)
	if err != nil {
		return err
	}
	if _, err := resolveScope(scope); err != nil {
		return err
	}
	if err := validatePreviewRequest(PreviewRequest{
		OrgID:    profile.OrgID,
		Scope:    scope,
		Mode:     profile.Mode,
		Strategy: profile.Strategy,
		Level:    profile.Level,
		Priority: profile.Priority,
		TargetKW: targetKW,
		// nil tolerance is equivalent to Start's omitted/zero tolerance.
		ToleranceKW:                 toleranceKW,
		IncludeMaintenance:          profile.IncludeMaintenance,
		ForceIncludeMaintenance:     profile.ForceIncludeMaintenance,
		ForceIncludeAllPairedMiners: profile.ForceIncludeAllPairedMiners,
	}); err != nil {
		return err
	}
	if profile.Mode == models.ModeFixedKw && profile.TargetKW == nil {
		return fleeterror.NewInvalidArgumentError("target_kw is required for FIXED_KW response profiles")
	}
	if profile.Mode == models.ModeFullFleet && (profile.TargetKW != nil || profile.ToleranceKW != nil) {
		return fleeterror.NewInvalidArgumentError("target_kw and tolerance_kw must be unset for FULL_FLEET response profiles")
	}
	if profile.FanOffDelaySec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"fan_off_delay_sec must be >= 0, got %d",
			profile.FanOffDelaySec,
		)
	}
	if profile.FanRestoreDelaySec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"fan_restore_delay_sec must be >= 0, got %d",
			profile.FanRestoreDelaySec,
		)
	}
	if responseProfileRequiresAdminControls(profile) && !canUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can save response profiles with admin-only controls")
	}
	if profile.TargetKW != nil && math.IsInf(*profile.TargetKW, 0) {
		return fleeterror.NewInvalidArgumentErrorf("target_kw must be finite, got %v", *profile.TargetKW)
	}
	if profile.TargetKW != nil && *profile.TargetKW > responseProfileNumericMax {
		return fleeterror.NewInvalidArgumentErrorf("target_kw must be <= %.3f, got %v", responseProfileNumericMax, *profile.TargetKW)
	}
	if profile.ToleranceKW != nil && math.IsInf(*profile.ToleranceKW, 0) {
		return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be finite, got %v", *profile.ToleranceKW)
	}
	if profile.ToleranceKW != nil && *profile.ToleranceKW > responseProfileNumericMax {
		return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be <= %.3f, got %v", responseProfileNumericMax, *profile.ToleranceKW)
	}
	if profile.CurtailBatchSize != nil && *profile.CurtailBatchSize <= 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"curtail_batch_size must be > 0 when set, got %d",
			*profile.CurtailBatchSize,
		)
	}
	if profile.CurtailBatchSize != nil && *profile.CurtailBatchSize > responseProfileBatchSizeMax {
		return fleeterror.NewInvalidArgumentErrorf(
			"curtail_batch_size must be <= %d, got %d",
			responseProfileBatchSizeMax,
			*profile.CurtailBatchSize,
		)
	}
	if profile.CurtailBatchIntervalSec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"curtail_batch_interval_sec must be >= 0, got %d",
			profile.CurtailBatchIntervalSec,
		)
	}
	if profile.CurtailBatchSize == nil && profile.CurtailBatchIntervalSec > 0 {
		return fleeterror.NewInvalidArgumentError(
			"curtail_batch_interval_sec must be 0 when curtail_batch_size is unset",
		)
	}
	if profile.CurtailBatchIntervalSec > restoreBatchIntervalUpperBoundSec {
		return fleeterror.NewInvalidArgumentErrorf(
			"curtail_batch_interval_sec must be <= %d, got %d",
			restoreBatchIntervalUpperBoundSec,
			profile.CurtailBatchIntervalSec,
		)
	}
	if profile.CurtailBatchIntervalSec > nonAdminRestoreBatchIntervalMax && !canUseAdminControls {
		return fleeterror.NewForbiddenErrorf(
			"only admins can set curtail_batch_interval_sec above %d",
			nonAdminRestoreBatchIntervalMax,
		)
	}
	if profile.RestoreBatchSize < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_size must be >= 0, got %d",
			profile.RestoreBatchSize,
		)
	}
	if profile.RestoreBatchSize > responseProfileBatchSizeMax {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_size must be <= %d, got %d",
			responseProfileBatchSizeMax,
			profile.RestoreBatchSize,
		)
	}
	if profile.RestoreBatchIntervalSec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_interval_sec must be >= 0, got %d",
			profile.RestoreBatchIntervalSec,
		)
	}
	if profile.RestoreBatchIntervalSec > restoreBatchIntervalUpperBoundSec {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_interval_sec must be <= %d, got %d",
			restoreBatchIntervalUpperBoundSec,
			profile.RestoreBatchIntervalSec,
		)
	}
	if profile.RestoreBatchIntervalSec > nonAdminRestoreBatchIntervalMax && !canUseAdminControls {
		return fleeterror.NewForbiddenErrorf(
			"only admins can set restore_batch_interval_sec above %d",
			nonAdminRestoreBatchIntervalMax,
		)
	}
	if profile.ForceIncludeMaintenance && !canUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can set force_include_maintenance")
	}
	if profile.ForceIncludeAllPairedMiners && !canUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can set force_include_all_paired_miners")
	}
	if err := validatePostEventCooldownSec(profile.PostEventCooldownSec); err != nil {
		return err
	}
	return nil
}

func validatePostEventCooldownSec(value int32) error {
	if value < 0 {
		return fleeterror.NewInvalidArgumentError("post_event_cooldown_sec must be >= 0")
	}
	if value > MaxPostEventCooldownSec {
		return fleeterror.NewInvalidArgumentErrorf(
			"post_event_cooldown_sec must be <= %d, got %d",
			MaxPostEventCooldownSec,
			value,
		)
	}
	return nil
}

func responseProfileRequiresAdminControls(profile models.ResponseProfile) bool {
	return profile.Mode == models.ModeFullFleet ||
		profile.ForceIncludeMaintenance ||
		profile.ForceIncludeAllPairedMiners ||
		profile.CurtailBatchIntervalSec > nonAdminRestoreBatchIntervalMax ||
		profile.RestoreBatchIntervalSec > nonAdminRestoreBatchIntervalMax
}

func ResponseProfileScope(profile models.ResponseProfile) (Scope, error) {
	scope, hasScope, err := ScopeFromJSON(profile.ScopeJSON)
	if err != nil {
		return Scope{}, err
	}
	if hasScope {
		return scope, nil
	}
	if profile.SiteID != nil {
		return Scope{Type: models.ScopeTypeSite, SiteID: *profile.SiteID}, nil
	}
	return Scope{Type: models.ScopeTypeWholeOrg}, nil
}

func ScopeFromJSON(scopeJSON []byte) (Scope, bool, error) {
	if len(scopeJSON) == 0 {
		return Scope{}, false, nil
	}
	var payload struct {
		WholeOrg          bool     `json:"whole_org"`
		SiteID            int64    `json:"site_id"`
		SiteIDs           []int64  `json:"site_ids"`
		DeviceSetIDs      []string `json:"device_set_ids"`
		DeviceIdentifiers []string `json:"device_identifiers"`
	}
	if err := json.Unmarshal(scopeJSON, &payload); err != nil {
		return Scope{}, false, fleeterror.NewInvalidArgumentErrorf("invalid scope_json: %v", err)
	}
	hasScope := payload.WholeOrg ||
		payload.SiteID != 0 ||
		len(payload.SiteIDs) > 0 ||
		len(payload.DeviceSetIDs) > 0 ||
		len(payload.DeviceIdentifiers) > 0
	if !hasScope {
		return Scope{}, false, nil
	}
	if payload.SiteID < 0 || hasNonPositiveInt64(payload.SiteIDs) {
		return Scope{}, false, fleeterror.NewInvalidArgumentError("site_ids must be positive")
	}
	if payload.WholeOrg {
		return Scope{Type: models.ScopeTypeWholeOrg}, true, nil
	}
	return normalizeScope(Scope{
		SiteID:            payload.SiteID,
		SiteIDs:           payload.SiteIDs,
		DeviceSetIDs:      payload.DeviceSetIDs,
		DeviceIdentifiers: payload.DeviceIdentifiers,
	}), true, nil
}

func responseProfileLegacySiteID(scope Scope) *int64 {
	scope = normalizeScope(scope)
	if scope.Type != models.ScopeTypeSite || len(scope.SiteIDs) != 1 {
		return nil
	}
	siteID := scope.SiteIDs[0]
	return &siteID
}

func isExplicitWholeOrgScopeJSON(scopeJSON []byte) bool {
	if len(scopeJSON) == 0 {
		return false
	}
	var payload struct {
		WholeOrg bool `json:"whole_org"`
	}
	return json.Unmarshal(scopeJSON, &payload) == nil && payload.WholeOrg
}

func hasNonPositiveInt64(values []int64) bool {
	for _, value := range values {
		if value <= 0 {
			return true
		}
	}
	return false
}

func float64Value(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
