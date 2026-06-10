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
	maxDuration := int32(3600)
	store := newResponseProfileFakeStore()
	svc := NewResponseProfileService(store)

	profile, err := svc.Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                   42,
			ProfileName:             "  Standard shed  ",
			SiteID:                  7,
			Mode:                    models.ModeFixedKw,
			TargetKW:                &targetKW,
			RestoreBatchSize:        25,
			RestoreBatchIntervalSec: 30,
			MaxDurationSeconds:      &maxDuration,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, int64(101), profile.ID)
	assert.Equal(t, "Standard shed", profile.ProfileName)
	assert.Equal(t, int64(7), profile.SiteID)
	assert.Equal(t, models.StrategyLeastEfficientFirst, profile.Strategy)
	assert.Equal(t, models.LevelFull, profile.Level)
	assert.Equal(t, models.PriorityNormal, profile.Priority)
	require.NotNil(t, store.created)
	assert.Equal(t, int64(42), store.created.OrgID)
	assert.Equal(t, int64(7), store.siteCheckSiteID)
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
			SiteID:      404,
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
			SiteID:      7,
			Mode:        models.ModeFullFleet,
			TargetKW:    &targetKW,
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestResponseProfileService_CreateRejectsNonAdminOverrides(t *testing.T) {
	t.Parallel()

	targetKW := 1000.0
	tooLong := int32(7201)

	tests := []struct {
		name   string
		mutate func(*models.ResponseProfile)
	}{
		{
			name: "max duration above org default",
			mutate: func(profile *models.ResponseProfile) {
				profile.MaxDurationSeconds = &tooLong
			},
		},
		{
			name: "force maintenance inclusion",
			mutate: func(profile *models.ResponseProfile) {
				profile.IncludeMaintenance = true
				profile.ForceIncludeMaintenance = true
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := models.ResponseProfile{
				OrgID:       42,
				ProfileName: "Standard shed",
				SiteID:      7,
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

type responseProfileFakeStore struct {
	orgConfig       *models.OrgConfig
	siteBelongs     bool
	siteCheckOrgID  int64
	siteCheckSiteID int64
	created         *models.ResponseProfile
	updated         *models.ResponseProfile
	profiles        []*models.ResponseProfile
}

func newResponseProfileFakeStore() *responseProfileFakeStore {
	return &responseProfileFakeStore{
		orgConfig: &models.OrgConfig{
			OrgID:                 42,
			MaxDurationDefaultSec: 7200,
		},
		siteBelongs: true,
	}
}

func (s *responseProfileFakeStore) GetOrgConfig(context.Context, int64) (*models.OrgConfig, error) {
	return s.orgConfig, nil
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

func (s *responseProfileFakeStore) UpdateResponseProfile(_ context.Context, profile models.ResponseProfile) (*models.ResponseProfile, error) {
	s.updated = &profile
	return &profile, nil
}

func (s *responseProfileFakeStore) DeleteResponseProfile(context.Context, int64, int64) error {
	return nil
}

func (s *responseProfileFakeStore) SiteBelongsToOrg(_ context.Context, orgID, siteID int64) (bool, error) {
	s.siteCheckOrgID = orgID
	s.siteCheckSiteID = siteID
	return s.siteBelongs, nil
}
