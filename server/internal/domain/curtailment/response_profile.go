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

const maxResponseProfileNameLength = 64

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
	return s.store.UpdateResponseProfile(ctx, profile)
}

func (s *ResponseProfileService) Delete(ctx context.Context, orgID, profileID int64) error {
	if s == nil || s.store == nil {
		return fleeterror.NewUnimplementedError("curtailment response profile service is not configured")
	}
	if orgID <= 0 {
		return fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if profileID <= 0 {
		return fleeterror.NewInvalidArgumentError("profile_id must be set")
	}
	return s.store.DeleteResponseProfile(ctx, orgID, profileID)
}

func (s *ResponseProfileService) validateAndNormalize(ctx context.Context, req SaveResponseProfileRequest) (models.ResponseProfile, error) {
	profile := normalizeResponseProfile(req.Profile)
	if profile.OrgID <= 0 {
		return models.ResponseProfile{}, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if err := validateResponseProfileName(profile.ProfileName); err != nil {
		return models.ResponseProfile{}, err
	}
	if profile.SiteID <= 0 {
		return models.ResponseProfile{}, fleeterror.NewInvalidArgumentError("site_id must be set")
	}
	belongs, err := s.store.SiteBelongsToOrg(ctx, profile.OrgID, profile.SiteID)
	if err != nil {
		return models.ResponseProfile{}, err
	}
	if !belongs {
		return models.ResponseProfile{}, fleeterror.NewNotFoundErrorf("site not found: %d", profile.SiteID)
	}
	if err := validateResponseProfileBehavior(profile, req.CanUseAdminControls); err != nil {
		return models.ResponseProfile{}, err
	}
	if profile.MaxDurationSeconds != nil && !req.CanUseAdminControls {
		orgConfig, err := s.store.GetOrgConfig(ctx, profile.OrgID)
		if err != nil {
			return models.ResponseProfile{}, err
		}
		if orgConfig.MaxDurationDefaultSec > 0 &&
			*profile.MaxDurationSeconds > orgConfig.MaxDurationDefaultSec {
			return models.ResponseProfile{}, fleeterror.NewForbiddenErrorf(
				"only admins can set max_duration_seconds above org default %d",
				orgConfig.MaxDurationDefaultSec,
			)
		}
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
		Scope:    Scope{Type: models.ScopeTypeSite, SiteID: profile.SiteID},
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
	if profile.TargetKW != nil && math.IsInf(*profile.TargetKW, 0) {
		return fleeterror.NewInvalidArgumentErrorf("target_kw must be finite, got %v", *profile.TargetKW)
	}
	if profile.ToleranceKW != nil && math.IsInf(*profile.ToleranceKW, 0) {
		return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be finite, got %v", *profile.ToleranceKW)
	}
	if profile.RestoreBatchSize < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_size must be >= 0, got %d",
			profile.RestoreBatchSize,
		)
	}
	if profile.RestoreBatchSize > 10000 {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_size must be <= 10000, got %d",
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
	if profile.MinCurtailedDurationSec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"min_curtailed_duration_sec must be >= 0, got %d",
			profile.MinCurtailedDurationSec,
		)
	}
	if profile.MaxDurationSeconds != nil {
		if *profile.MaxDurationSeconds <= 0 {
			return fleeterror.NewInvalidArgumentErrorf(
				"max_duration_seconds must be > 0, got %d",
				*profile.MaxDurationSeconds,
			)
		}
		if *profile.MaxDurationSeconds > maxFiniteDurationSeconds {
			return fleeterror.NewInvalidArgumentErrorf(
				"max_duration_seconds must be <= %d, got %d",
				maxFiniteDurationSeconds,
				*profile.MaxDurationSeconds,
			)
		}
	}
	if profile.ForceIncludeMaintenance && !canUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can set force_include_maintenance")
	}
	return nil
}

func float64Value(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
