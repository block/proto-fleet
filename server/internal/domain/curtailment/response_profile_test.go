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
			PostEventCooldownSec:    600,
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
	assert.Equal(t, int32(600), profile.PostEventCooldownSec)
	require.NotNil(t, store.created)
	assert.Equal(t, int64(42), store.created.OrgID)
	assert.Equal(t, int64(7), store.siteCheckSiteID)
	assert.Equal(t, int32(600), store.created.PostEventCooldownSec)
}

func TestResponseProfileService_CreatePersistsCompositeSiteAndMinerScope(t *testing.T) {
	t.Parallel()

	targetKW := 2500.0
	store := newResponseProfileFakeStore()
	svc := NewResponseProfileService(store)

	profile, err := svc.Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:       42,
			ProfileName: "Combined shed",
			Mode:        models.ModeFixedKw,
			TargetKW:    &targetKW,
			ScopeJSON:   []byte(`{"site_ids":[7,7],"device_identifiers":["miner-a","miner-a","miner-b"]}`),
		},
	})

	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Nil(t, profile.SiteID, "mixed scopes must not masquerade as legacy single-site profiles")
	assert.Equal(t, 1, store.siteCheckCount, "sites are validated by id without expanding site miners")
	assert.Equal(t, int64(7), store.siteCheckSiteID)
	assert.JSONEq(t, `{"site_ids":[7],"device_identifiers":["miner-a","miner-b"]}`, string(profile.ScopeJSON))
	require.NotNil(t, store.created)
	assert.JSONEq(t, `{"site_ids":[7],"device_identifiers":["miner-a","miner-b"]}`, string(store.created.ScopeJSON))
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

func TestResponseProfileService_CreatePersistsFacilityFanSettings(t *testing.T) {
	t.Parallel()

	store := newResponseProfileFakeStore()
	store.infrastructureDevices[31] = models.ResponseProfileInfrastructureDevice{ID: 31, SiteID: 7, Enabled: true}
	store.infrastructureDevices[32] = models.ResponseProfileInfrastructureDevice{ID: 32, SiteID: 7, Enabled: false}
	svc := NewResponseProfileService(store)

	profile, err := svc.Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                42,
			ProfileName:          "Fan-coordinated shed",
			SiteID:               ptrInt64(7),
			Mode:                 models.ModeFullFleet,
			FacilityFanDeviceIDs: []int64{31, 32, 31},
			FanOffDelaySec:       45,
			FanRestoreDelaySec:   90,
		},
		CanUseAdminControls: true,
	})

	require.NoError(t, err)
	assert.Equal(t, []int64{31, 32}, profile.FacilityFanDeviceIDs)
	assert.Equal(t, int32(45), profile.FanOffDelaySec)
	assert.Equal(t, int32(90), profile.FanRestoreDelaySec)
	require.NotNil(t, store.created)
	assert.Equal(t, []int64{31, 32}, store.created.FacilityFanDeviceIDs)
}

func TestResponseProfileService_CreateRejectsUnknownFacilityFan(t *testing.T) {
	t.Parallel()

	_, err := NewResponseProfileService(newResponseProfileFakeStore()).Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                42,
			ProfileName:          "Unknown fan",
			Mode:                 models.ModeFullFleet,
			FacilityFanDeviceIDs: []int64{404},
		},
		CanUseAdminControls: true,
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestResponseProfileService_CreateAllowsFacilityFanOutsideProfileScope(t *testing.T) {
	t.Parallel()

	store := newResponseProfileFakeStore()
	store.infrastructureDevices[31] = models.ResponseProfileInfrastructureDevice{ID: 31, SiteID: 8, Enabled: true}

	profile, err := NewResponseProfileService(store).Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                42,
			ProfileName:          "Wrong-site fan",
			SiteID:               ptrInt64(7),
			Mode:                 models.ModeFullFleet,
			FacilityFanDeviceIDs: []int64{31},
		},
		CanUseAdminControls: true,
	})

	require.NoError(t, err)
	assert.Equal(t, []int64{31}, profile.FacilityFanDeviceIDs)
}

func TestResponseProfileService_CreateAllowsFacilityFanOutsideExplicitMinerSite(t *testing.T) {
	t.Parallel()

	store := newResponseProfileFakeStore()
	store.infrastructureDevices[31] = models.ResponseProfileInfrastructureDevice{ID: 31, SiteID: 9, Enabled: true}
	store.deviceSites["miner-a"] = ptrInt64(8)

	profile, err := NewResponseProfileService(store).Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                42,
			ProfileName:          "Explicit miner fan",
			Mode:                 models.ModeFullFleet,
			ScopeJSON:            []byte(`{"device_identifiers":["miner-a"]}`),
			FacilityFanDeviceIDs: []int64{31},
		},
		CanUseAdminControls: true,
	})

	require.NoError(t, err)
	assert.Equal(t, []int64{31}, profile.FacilityFanDeviceIDs)
}

func TestResponseProfileService_CreateRejectsDeviceSetScope(t *testing.T) {
	t.Parallel()

	targetKW := 2500.0
	_, err := NewResponseProfileService(newResponseProfileFakeStore()).Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:       42,
			ProfileName: "Device set shed",
			Mode:        models.ModeFixedKw,
			TargetKW:    &targetKW,
			ScopeJSON:   []byte(`{"device_set_ids":["set-a"]}`),
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsUnimplementedError(err))
	assert.Contains(t, err.Error(), "device-set scope is not implemented")
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
	assert.Equal(t, int32(0), profile.RestoreBatchSize)
	assert.Equal(t, int32(0), profile.RestoreBatchIntervalSec)
	assert.Equal(t, int32(0), profile.PostEventCooldownSec)
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
			RestoreBatchSize:        0,
			RestoreBatchIntervalSec: 0,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, int32(0), profile.RestoreBatchIntervalSec)
	assert.Equal(t, int32(0), profile.RestoreBatchSize)
	require.NotNil(t, store.updated)
	assert.Equal(t, int32(0), store.updated.RestoreBatchIntervalSec)
	assert.Equal(t, int32(0), store.updated.RestoreBatchSize)
}

func TestResponseProfileService_UpdateRejectsFacilityFansWhenProfileHasAutomationRules(t *testing.T) {
	t.Parallel()

	store := newResponseProfileFakeStore()
	store.automationRuleCount = 1
	store.infrastructureDevices[31] = models.ResponseProfileInfrastructureDevice{ID: 31, SiteID: 7, Enabled: true}
	svc := NewResponseProfileService(store)

	_, err := svc.Update(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			ID:                      101,
			OrgID:                   42,
			ProfileName:             "Automated shed",
			Mode:                    models.ModeFullFleet,
			FacilityFanDeviceIDs:    []int64{31},
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 30,
		},
		CanUseAdminControls: true,
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "used by automation rules cannot include facility fans")
	assert.Nil(t, store.updated)
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

func TestResponseProfileService_CreateRejectsPostEventCooldownOverLimit(t *testing.T) {
	t.Parallel()

	targetKW := 1000.0
	_, err := NewResponseProfileService(newResponseProfileFakeStore()).Create(t.Context(), SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                42,
			ProfileName:          "Standard shed",
			Mode:                 models.ModeFixedKw,
			TargetKW:             &targetKW,
			PostEventCooldownSec: MaxPostEventCooldownSec + 1,
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "post_event_cooldown_sec")
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
			name: "all paired policy",
			mutate: func(profile *models.ResponseProfile) {
				profile.Mode = models.ModeFullFleet
				profile.TargetKW = nil
				profile.ForceIncludeAllPairedMiners = true
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

func TestResponseProfileService_CreateRejectsAllPairedPolicyOutsideFullFleet(t *testing.T) {
	t.Parallel()

	targetKW := 1000.0
	_, err := NewResponseProfileService(newResponseProfileFakeStore()).Create(t.Context(), SaveResponseProfileRequest{
		CanUseAdminControls: true,
		Profile: models.ResponseProfile{
			OrgID:                       42,
			ProfileName:                 "Standard shed",
			Mode:                        models.ModeFixedKw,
			TargetKW:                    &targetKW,
			ForceIncludeAllPairedMiners: true,
		},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "force_include_all_paired_miners")
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

	err := svc.Delete(t.Context(), 42, 101, nil, nil)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "referenced by automation")
	assert.Equal(t, 0, store.deleteCalls)
}

type responseProfileFakeStore struct {
	siteBelongs           bool
	siteCheckCount        int
	siteCheckOrgID        int64
	siteCheckSiteID       int64
	created               *models.ResponseProfile
	updated               *models.ResponseProfile
	deleteCalls           int
	automationRuleCount   int64
	profiles              []*models.ResponseProfile
	infrastructureDevices map[int64]models.ResponseProfileInfrastructureDevice
	deviceSites           map[string]*int64
}

func newResponseProfileFakeStore() *responseProfileFakeStore {
	return &responseProfileFakeStore{
		siteBelongs:           true,
		infrastructureDevices: map[int64]models.ResponseProfileInfrastructureDevice{},
		deviceSites:           map[string]*int64{},
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

func (s *responseProfileFakeStore) ListResponseProfileDeviceSites(_ context.Context, _ int64, deviceIdentifiers []string) (map[string]*int64, error) {
	out := make(map[string]*int64, len(deviceIdentifiers))
	for _, identifier := range deviceIdentifiers {
		if siteID, ok := s.deviceSites[identifier]; ok {
			out[identifier] = siteID
		}
	}
	return out, nil
}

func (s *responseProfileFakeStore) ListResponseProfileInfrastructureDevices(_ context.Context, _ int64, deviceIDs []int64) (map[int64]models.ResponseProfileInfrastructureDevice, error) {
	out := make(map[int64]models.ResponseProfileInfrastructureDevice, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		if device, ok := s.infrastructureDevices[deviceID]; ok {
			out[deviceID] = device
		}
	}
	return out, nil
}

func (s *responseProfileFakeStore) CreateResponseProfile(_ context.Context, profile models.ResponseProfile, _ map[int64]models.ResponseProfileInfrastructureDevice) (*models.ResponseProfile, error) {
	profile.ID = 101
	s.created = &profile
	return &profile, nil
}

func (s *responseProfileFakeStore) UpdateResponseProfile(_ context.Context, profile models.ResponseProfile, _ map[int64]models.ResponseProfileInfrastructureDevice, _ *int64, _ []byte, _ models.ResponseProfileFanSettings) (*models.ResponseProfile, error) {
	s.updated = &profile
	return &profile, nil
}

func (s *responseProfileFakeStore) DeleteResponseProfile(context.Context, int64, int64, *int64, []byte) error {
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
