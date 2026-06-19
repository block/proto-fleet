package curtailment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestResponseProfileService_CreatePersistsSiteScopedFixedKW(t *testing.T) {
	t.Parallel()

	targetKW := 2500.0
	curtailBatchSize := int32(100)
	store := newResponseProfileFakeStore()
	svc := NewResponseProfileService(store)

	profile, err := svc.Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                   42,
			ProfileName:             "  Standard shed  ",
			SiteID:                  ptrInt64(7),
			Mode:                    models.ModeFixedKw,
			TargetKW:                &targetKW,
			CurtailBatchSize:        &curtailBatchSize,
			CurtailBatchIntervalSec: 15,
			RestoreBatchSize:        25,
			RestoreBatchIntervalSec: 30,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, int64(101), profile.ID)
	assert.Equal(t, "Standard shed", profile.ProfileName)
	require.NotNil(t, profile.SiteID)
	assert.Equal(t, int64(7), *profile.SiteID)
	assert.Equal(t, models.StrategyLeastEfficientFirst, profile.Strategy)
	assert.Equal(t, models.LevelFull, profile.Level)
	assert.Equal(t, models.PriorityNormal, profile.Priority)
	require.NotNil(t, profile.CurtailBatchSize)
	assert.Equal(t, int32(100), *profile.CurtailBatchSize)
	assert.Equal(t, int32(15), profile.CurtailBatchIntervalSec)
	assert.Equal(t, int32(25), profile.RestoreBatchSize)
	assert.Equal(t, int32(30), profile.RestoreBatchIntervalSec)
	require.NotNil(t, store.created)
	assert.Equal(t, int64(42), store.created.OrgID)
	assert.Equal(t, int64(7), store.siteCheckSiteID)
}

func TestResponseProfileService_CreateAllowsWholeOrgScope(t *testing.T) {
	t.Parallel()

	targetKW := 2500.0
	store := newResponseProfileFakeStore()
	svc := NewResponseProfileService(store)

	profile, err := svc.Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:       42,
			ProfileName: "Whole org shed",
			Mode:        models.ModeFixedKw,
			TargetKW:    &targetKW,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Nil(t, profile.SiteID)
	assert.Equal(t, 0, store.siteCheckCount)
}

func TestResponseProfileService_CreateAppliesBackendBatchDefaultsWithoutOverwritingImmediateRestore(t *testing.T) {
	t.Parallel()

	targetKW := 2500.0
	store := newResponseProfileFakeStore()
	svc := NewResponseProfileService(store)

	profile, err := svc.Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:       42,
			ProfileName: "Standard shed",
			SiteID:      ptrInt64(7),
			Mode:        models.ModeFixedKw,
			TargetKW:    &targetKW,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Nil(t, profile.CurtailBatchSize)
	assert.Equal(t, DefaultResponseProfileCurtailBatchIntervalSec, profile.CurtailBatchIntervalSec)
	assert.Equal(t, DefaultResponseProfileRestoreBatchSize, profile.RestoreBatchSize)
	assert.Equal(t, int32(0), profile.RestoreBatchIntervalSec)
}

func TestResponseProfileService_UpdatePreservesImmediateRestoreInterval(t *testing.T) {
	t.Parallel()

	targetKW := 2500.0
	store := newResponseProfileFakeStore()
	svc := NewResponseProfileService(store)

	profile, err := svc.Update(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			ID:                      101,
			OrgID:                   42,
			ProfileName:             "Standard shed",
			Mode:                    models.ModeFixedKw,
			TargetKW:                &targetKW,
			RestoreBatchSize:        DefaultResponseProfileRestoreBatchSize,
			RestoreBatchIntervalSec: 0,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, int32(0), profile.RestoreBatchIntervalSec)
	require.NotNil(t, store.updated)
	assert.Equal(t, int32(0), store.updated.RestoreBatchIntervalSec)
}

func TestResponseProfileService_CreateRejectsUnknownSite(t *testing.T) {
	t.Parallel()

	targetKW := 1000.0
	store := newResponseProfileFakeStore()
	store.siteBelongs = false
	svc := NewResponseProfileService(store)

	_, err := svc.Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:       42,
			ProfileName: "Standard shed",
			SiteID:      ptrInt64(404),
			Mode:        models.ModeFixedKw,
			TargetKW:    &targetKW,
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestResponseProfileService_CreateRejectsFullFleetWithFixedKWParams(t *testing.T) {
	t.Parallel()

	targetKW := 1000.0
	svc := NewResponseProfileService(newResponseProfileFakeStore())

	_, err := svc.Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:       42,
			ProfileName: "Emergency shed",
			Mode:        models.ModeFullFleet,
			TargetKW:    &targetKW,
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestResponseProfileService_CreateRejectsPersistedNumericOverflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		profile models.ResponseProfile
	}{
		{
			name: "oversized target",
			profile: models.ResponseProfile{
				OrgID:       42,
				ProfileName: "Huge target",
				Mode:        models.ModeFixedKw,
				TargetKW:    ptrFloat64(responseProfileNumericMax + 1),
			},
		},
		{
			name: "oversized tolerance",
			profile: models.ResponseProfile{
				OrgID:       42,
				ProfileName: "Huge tolerance",
				Mode:        models.ModeFixedKw,
				TargetKW:    ptrFloat64(responseProfileNumericMax),
				ToleranceKW: ptrFloat64(responseProfileNumericMax + 1),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewResponseProfileService(newResponseProfileFakeStore()).Create(t.Context(), SaveResponseProfileRequest{
				Profile: tc.profile,
			})

			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
		})
	}
}

func TestResponseProfileService_CreateRejectsNonAdminOverrides(t *testing.T) {
	t.Parallel()

	targetKW := 1000.0
	slowInterval := nonAdminRestoreBatchIntervalMax + 1

	tests := []struct {
		name   string
		mutate func(*models.ResponseProfile)
	}{
		{
			name: "slow curtail batching",
			mutate: func(profile *models.ResponseProfile) {
				batchSize := int32(25)
				profile.CurtailBatchSize = &batchSize
				profile.CurtailBatchIntervalSec = slowInterval
			},
		},
		{
			name: "slow restore batching",
			mutate: func(profile *models.ResponseProfile) {
				profile.RestoreBatchIntervalSec = slowInterval
			},
		},
		{
			name: "force maintenance inclusion",
			mutate: func(profile *models.ResponseProfile) {
				profile.IncludeMaintenance = true
				profile.ForceIncludeMaintenance = true
			},
		},
		{
			name: "full fleet mode",
			mutate: func(profile *models.ResponseProfile) {
				profile.Mode = models.ModeFullFleet
				profile.TargetKW = nil
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := models.ResponseProfile{
				OrgID:       42,
				ProfileName: "Standard shed",
				Mode:        models.ModeFixedKw,
				TargetKW:    &targetKW,
			}
			tc.mutate(&profile)

			_, err := NewResponseProfileService(newResponseProfileFakeStore()).Create(t.Context(), SaveResponseProfileRequest{
				Profile: profile,
			})

			require.Error(t, err)
			assert.True(t, fleeterror.IsForbiddenError(err))
		})
	}
}

func TestResponseProfileService_CreateRejectsCurtailIntervalWithoutBatchSize(t *testing.T) {
	t.Parallel()

	targetKW := 1000.0
	_, err := NewResponseProfileService(newResponseProfileFakeStore()).Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                   42,
			ProfileName:             "Standard shed",
			Mode:                    models.ModeFixedKw,
			TargetKW:                &targetKW,
			CurtailBatchIntervalSec: 15,
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "curtail_batch_interval_sec")
}

func TestResponseProfileService_DeleteRejectsReferencedProfile(t *testing.T) {
	t.Parallel()

	store := newResponseProfileFakeStore()
	store.automationRuleCount = 1
	svc := NewResponseProfileService(store)

	err := svc.Delete(t.Context(), 42, 101, nil)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "referenced by automation")
	assert.Equal(t, 0, store.deleteCalls)
}

type responseProfileFakeStore struct {
	siteBelongs         bool
	siteCheckCount      int
	siteCheckOrgID      int64
	siteCheckSiteID     int64
	created             *models.ResponseProfile
	updated             *models.ResponseProfile
	deleteCalls         int
	automationRuleCount int64
	profiles            []*models.ResponseProfile
}

func newResponseProfileFakeStore() *responseProfileFakeStore {
	return &responseProfileFakeStore{
		siteBelongs: true,
	}
}

func (s *responseProfileFakeStore) ListResponseProfiles(context.Context, int64) ([]*models.ResponseProfile, error) {
	return s.profiles, nil
}

func (s *responseProfileFakeStore) GetResponseProfile(_ context.Context, _ int64, profileID int64) (*models.ResponseProfile, error) {
	for _, profile := range s.profiles {
		if profile.ID == profileID {
			return profile, nil
		}
	}
	return nil, fleeterror.NewNotFoundErrorf("curtailment response profile not found: %d", profileID)
}

func (s *responseProfileFakeStore) CreateResponseProfile(_ context.Context, profile models.ResponseProfile) (*models.ResponseProfile, error) {
	profile.ID = 101
	s.created = &profile
	return &profile, nil
}

func (s *responseProfileFakeStore) UpdateResponseProfile(_ context.Context, profile models.ResponseProfile, _ *int64) (*models.ResponseProfile, error) {
	s.updated = &profile
	return &profile, nil
}

func (s *responseProfileFakeStore) DeleteResponseProfile(context.Context, int64, int64, *int64) error {
	s.deleteCalls++
	return nil
}

func (s *responseProfileFakeStore) CountAutomationRulesByResponseProfile(context.Context, int64, int64) (int64, error) {
	return s.automationRuleCount, nil
}

func (s *responseProfileFakeStore) SiteBelongsToOrg(_ context.Context, orgID, siteID int64) (bool, error) {
	s.siteCheckCount++
	s.siteCheckOrgID = orgID
	s.siteCheckSiteID = siteID
	return s.siteBelongs, nil
}

func ptrInt64(v int64) *int64 {
	return &v
}

func ptrFloat64(v float64) *float64 {
	return &v
}
