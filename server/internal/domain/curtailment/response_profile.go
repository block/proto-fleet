package curtailment

import (
	"context"
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
	return s.store.UpdateResponseProfile(ctx, profile, req.ExpectedSiteID)
}

func (s *ResponseProfileService) Delete(ctx context.Context, orgID, profileID int64, expectedSiteID *int64) error {
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
	return s.store.DeleteResponseProfile(ctx, orgID, profileID, expectedSiteID)
}

func (s *ResponseProfileService) validateAndNormalize(ctx context.Context, req SaveResponseProfileRequest) (models.ResponseProfile, error) {
	profile := normalizeResponseProfile(req.Profile)
	if profile.OrgID <= 0 {
		return models.ResponseProfile{}, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if err := validateResponseProfileName(profile.ProfileName); err != nil {
		return models.ResponseProfile{}, err
	}
	if profile.SiteID != nil {
		if *profile.SiteID <= 0 {
			return models.ResponseProfile{}, fleeterror.NewInvalidArgumentError("site_id must be positive when set")
		}
		belongs, err := s.store.SiteBelongsToOrg(ctx, profile.OrgID, *profile.SiteID)
		if err != nil {
			return models.ResponseProfile{}, err
		}
		if !belongs {
			return models.ResponseProfile{}, fleeterror.NewNotFoundErrorf("site not found: %d", *profile.SiteID)
		}
	}
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
	if err := validatePreviewRequest(PreviewRequest{
		OrgID:    profile.OrgID,
		Scope:    responseProfileScope(profile),
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
	return nil
}

func responseProfileRequiresAdminControls(profile models.ResponseProfile) bool {
	return profile.Mode == models.ModeFullFleet ||
		profile.ForceIncludeMaintenance ||
		profile.CurtailBatchIntervalSec > nonAdminRestoreBatchIntervalMax ||
		profile.RestoreBatchIntervalSec > nonAdminRestoreBatchIntervalMax
}

func responseProfileScope(profile models.ResponseProfile) Scope {
	if profile.SiteID == nil {
		return Scope{Type: models.ScopeTypeWholeOrg}
	}
	return Scope{Type: models.ScopeTypeSite, SiteID: *profile.SiteID}
}

func float64Value(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
