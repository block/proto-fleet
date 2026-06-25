package curtailment

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	maxResponseProfileNameLength = 64

	// Response profile defaults are intentionally backend-owned so the
	// frontend can omit empty form fields without baking policy into the UI.
	DefaultResponseProfileCurtailBatchIntervalSec int32 = 0
	DefaultResponseProfileRestoreBatchSize        int32 = 50
	DefaultResponseProfileRestoreBatchIntervalSec int32 = 5
	MaxPostEventCooldownSec                       int32 = 24 * 60 * 60

	responseProfileBatchSizeMax int32   = 10000
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
	Profile             models.ResponseProfile
	CanUseAdminControls bool
	ExpectedSiteID      *int64
	ExpectedScopeJSON   []byte
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

func (s *ResponseProfileService) Create(ctx context.Context, req SaveResponseProfileRequest) (*models.ResponseProfile, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	profile, err := s.validateAndNormalize(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.store.CreateResponseProfile(ctx, profile)
}

func (s *ResponseProfileService) Update(ctx context.Context, req SaveResponseProfileRequest) (*models.ResponseProfile, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	if req.Profile.ID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("profile_id must be set")
	}
	profile, err := s.validateAndNormalize(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.store.UpdateResponseProfile(ctx, profile, req.ExpectedSiteID, req.ExpectedScopeJSON)
}

func (s *ResponseProfileService) Delete(ctx context.Context, orgID, profileID int64, expectedSiteID *int64, expectedScopeJSON []byte) error {
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
	return s.store.DeleteResponseProfile(ctx, orgID, profileID, expectedSiteID, expectedScopeJSON)
}

func (s *ResponseProfileService) validateAndNormalize(ctx context.Context, req SaveResponseProfileRequest) (models.ResponseProfile, error) {
	profile := normalizeResponseProfile(req.Profile)
	if profile.OrgID <= 0 {
		return models.ResponseProfile{}, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if err := validateResponseProfileName(profile.ProfileName); err != nil {
		return models.ResponseProfile{}, err
	}
	scope, err := ResponseProfileScope(profile)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	explicitWholeOrgScope := isExplicitWholeOrgScopeJSON(profile.ScopeJSON)
	for _, siteID := range normalizeScope(scope).SiteIDs {
		belongs, err := s.store.SiteBelongsToOrg(ctx, profile.OrgID, siteID)
		if err != nil {
			return models.ResponseProfile{}, err
		}
		if !belongs {
			return models.ResponseProfile{}, fleeterror.NewNotFoundErrorf("site not found: %d", siteID)
		}
	}
	scopeJSON, err := MarshalScopeJSON(scope)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	if normalizeScope(scope).Type == models.ScopeTypeWholeOrg && explicitWholeOrgScope {
		scopeJSON = []byte(`{"whole_org":true}`)
	}
	profile.ScopeJSON = scopeJSON
	profile.SiteID = responseProfileLegacySiteID(scope)
	if err := validateResponseProfileBehavior(profile, req.CanUseAdminControls); err != nil {
		return models.ResponseProfile{}, err
	}
	return profile, nil
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
	if profile.RestoreBatchSize == 0 {
		profile.RestoreBatchSize = DefaultResponseProfileRestoreBatchSize
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
		ToleranceKW:             toleranceKW,
		IncludeMaintenance:      profile.IncludeMaintenance,
		ForceIncludeMaintenance: profile.ForceIncludeMaintenance,
	}); err != nil {
		return err
	}
	if profile.Mode == models.ModeFixedKw && profile.TargetKW == nil {
		return fleeterror.NewInvalidArgumentError("target_kw is required for FIXED_KW response profiles")
	}
	if profile.Mode == models.ModeFullFleet && (profile.TargetKW != nil || profile.ToleranceKW != nil) {
		return fleeterror.NewInvalidArgumentError("target_kw and tolerance_kw must be unset for FULL_FLEET response profiles")
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
	if profile.RestoreBatchSize <= 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_size must be > 0, got %d",
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
