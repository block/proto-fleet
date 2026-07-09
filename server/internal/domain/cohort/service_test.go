package cohort

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

func TestCreateCohort_ValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	svc := NewService(mocks.NewMockCohortStore(gomock.NewController(t)))

	_, err := svc.CreateCohort(t.Context(), models.CreateCohortParams{
		Label:   "   ",
		Purpose: "test",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = svc.CreateCohort(t.Context(), models.CreateCohortParams{
		Label:   "reservation",
		Purpose: "   ",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestCreateCohort_RejectsEmptyAndDuplicateDeviceIdentifiers(t *testing.T) {
	t.Parallel()

	svc := NewService(mocks.NewMockCohortStore(gomock.NewController(t)))

	_, err := svc.CreateCohort(t.Context(), validCreateParams(nil))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = svc.CreateCohort(t.Context(), validCreateParams([]string{"miner-1", "   "}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = svc.CreateCohort(t.Context(), validCreateParams([]string{"miner-1", "miner-1"}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestCreateCohort_NormalizesAndPersists(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	emptyFirmwareFileID := "   "
	expected := &models.Cohort{
		ID:      11,
		OrgID:   7,
		Label:   "reservation",
		Purpose: "test firmware",
		State:   models.CohortStateActive,
	}

	store.EXPECT().
		CreateCohort(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.CreateCohortParams)
			return ok &&
				params.Label == "reservation" &&
				params.Purpose == "test firmware" &&
				params.SourceActorType == models.SourceActorUser &&
				params.DesiredFirmwareFileID == nil &&
				len(params.DeviceIdentifiers) == 2 &&
				params.DeviceIdentifiers[0] == "miner-1" &&
				params.DeviceIdentifiers[1] == "miner-2"
		})).
		Return(expected, nil)

	got, err := svc.CreateCohort(context.Background(), models.CreateCohortParams{
		OrgID:                 7,
		Label:                 "  reservation  ",
		Purpose:               "  test firmware  ",
		DesiredFirmwareFileID: &emptyFirmwareFileID,
		DeviceIdentifiers:     []string{"  miner-1  ", "  miner-2  "},
	})
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestCreateCohort_NormalizesSelectorFilters(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	product := "  TestCorp  "
	model := "  TestMiner  "
	expected := &models.Cohort{ID: 11, OrgID: 7, Label: "reservation", Purpose: "test", State: models.CohortStateActive}
	store.EXPECT().
		CreateCohort(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.CreateCohortParams)
			return ok &&
				params.DeviceSelector != nil &&
				params.DeviceSelector.Count == 2 &&
				params.DeviceSelector.Product != nil &&
				*params.DeviceSelector.Product == "TestCorp" &&
				params.DeviceSelector.Model != nil &&
				*params.DeviceSelector.Model == "TestMiner"
		})).
		Return(expected, nil)

	got, err := svc.CreateCohort(context.Background(), models.CreateCohortParams{
		OrgID:          7,
		Label:          "reservation",
		Purpose:        "test",
		DeviceSelector: &models.CohortDeviceSelector{Count: 2, Product: &product, Model: &model},
	})
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestCreateCohort_RejectsInvalidSelector(t *testing.T) {
	t.Parallel()

	svc := NewService(mocks.NewMockCohortStore(gomock.NewController(t)))

	_, err := svc.CreateCohort(t.Context(), models.CreateCohortParams{
		OrgID:             7,
		Label:             "reservation",
		Purpose:           "test",
		DeviceIdentifiers: []string{"miner-1"},
		DeviceSelector:    &models.CohortDeviceSelector{Count: 1},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = svc.CreateCohort(t.Context(), models.CreateCohortParams{
		OrgID:          7,
		Label:          "reservation",
		Purpose:        "test",
		DeviceSelector: &models.CohortDeviceSelector{},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestCreateCohort_ResolvesDesiredFirmwareTarget(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
		"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
	}))

	firmwareFileID := "fw-1"
	expected := cohortWithMembers(11, []models.CohortMember{
		cohortMember("miner-1", "Proto", "Rig"),
	})
	store.EXPECT().
		CreateCohort(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.CreateCohortParams)
			return ok &&
				params.DesiredFirmwareFileID != nil &&
				*params.DesiredFirmwareFileID == firmwareFileID &&
				params.DesiredFirmwareTargetManufacturer == "Proto" &&
				params.DesiredFirmwareTargetModel == "Rig"
		})).
		Return(expected, nil)

	got, err := svc.CreateCohort(context.Background(), models.CreateCohortParams{
		OrgID:                 7,
		Label:                 "reservation",
		Purpose:               "firmware",
		DesiredFirmwareFileID: &firmwareFileID,
		DeviceIdentifiers:     []string{"miner-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestCreateCohort_RejectsInvalidDesiredFirmwareFileID(t *testing.T) {
	t.Parallel()

	svc := NewService(
		mocks.NewMockCohortStore(gomock.NewController(t)),
		WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{}),
	)

	firmwareFileID := "missing-fw"
	_, err := svc.CreateCohort(context.Background(), models.CreateCohortParams{
		OrgID:                 7,
		Label:                 "reservation",
		Purpose:               "firmware",
		DesiredFirmwareFileID: &firmwareFileID,
		DeviceIdentifiers:     []string{"miner-1"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestCreateCohort_RejectsMismatchedDesiredFirmwareTarget(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
		"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
	}))

	firmwareFileID := "fw-1"
	store.EXPECT().
		CreateCohort(gomock.Any(), gomock.Any()).
		Return(cohortWithMembers(11, []models.CohortMember{
			cohortMember("miner-1", "Bitmain", "S21"),
		}), nil)

	_, err := svc.CreateCohort(context.Background(), models.CreateCohortParams{
		OrgID:                 7,
		Label:                 "reservation",
		Purpose:               "firmware",
		DesiredFirmwareFileID: &firmwareFileID,
		DeviceIdentifiers:     []string{"miner-1"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "does not match cohort miner type")
}

func TestCreateCohort_RejectsMixedMinerTypes(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	store.EXPECT().
		CreateCohort(gomock.Any(), gomock.Any()).
		Return(cohortWithMembers(11, []models.CohortMember{
			cohortMember("miner-1", "Proto", "Rig"),
			cohortMember("miner-2", "Bitmain", "S21"),
		}), nil)

	_, err := svc.CreateCohort(context.Background(), models.CreateCohortParams{
		OrgID:             7,
		Label:             "reservation",
		Purpose:           "mixed",
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "single manufacturer and model")
}

func TestAuthorizeCohortOwnerMutation_OwnerlessRequiresSuperAdmin(t *testing.T) {
	t.Parallel()

	cohort := &models.Cohort{ID: 42, OrgID: 7, Label: "standing", State: models.CohortStateActive}
	err := authorizeCohortOwnerMutation(cohort, 1, "FIELD_TECH")
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))

	err = authorizeCohortOwnerMutation(cohort, 1, "ADMIN")
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))

	err = authorizeCohortOwnerMutation(cohort, 1, "SUPER_ADMIN")
	require.NoError(t, err)
}

func TestReleaseCohort_AdminCannotReleaseAnotherOwnersCohort(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	cohort := &models.Cohort{
		ID:          42,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: ptrInt64(99),
		State:       models.CohortStateActive,
	}
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(cohort, nil)

	_, err := svc.ReleaseCohort(context.Background(), models.MembershipMutationParams{
		OrgID:       7,
		CohortID:    42,
		ActorUserID: 1,
		ActorRole:   "ADMIN",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
}

func TestSetCohortFirmwareTarget_AdminCannotMutateAnotherOwnersCohort(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	cohort := cohortWithMembers(42, []models.CohortMember{
		cohortMember("miner-1", "Proto", "Rig"),
	})
	cohort.OwnerUserID = ptrInt64(99)
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(cohort, nil)

	_, err := svc.SetCohortFirmwareTarget(context.Background(), models.SetCohortFirmwareTargetParams{
		OrgID:        7,
		CohortID:     42,
		ActorUserID:  1,
		ActorRole:    "ADMIN",
		Manufacturer: stringPtr("Proto"),
		Model:        stringPtr("Rig"),
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
}

func TestAuthorizeDeviceMoves_OwnerlessSourceRequiresAdmin(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	params := models.MembershipMutationParams{
		OrgID:             7,
		CohortID:          11,
		ActorUserID:       1,
		ActorRole:         "FIELD_TECH",
		DeviceIdentifiers: []string{"miner-1"},
	}
	store.EXPECT().
		ListCohortDeviceOwnership(gomock.Any(), int64(7), []string{"miner-1"}).
		Return([]models.CohortDeviceOwnership{{
			DeviceIdentifier: "miner-1",
			CohortID:         99,
		}}, nil)

	err := svc.authorizeDeviceMoves(context.Background(), params)
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
}

func TestAddDevicesToCohort_RejectsMismatchedDesiredFirmwareTarget(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
		"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
	}))

	actorUserID := int64(1)
	firmwareFileID := "fw-1"
	target := &models.Cohort{
		ID:                    11,
		OrgID:                 7,
		Label:                 "reservation",
		OwnerUserID:           &actorUserID,
		State:                 models.CohortStateActive,
		DesiredFirmwareFileID: &firmwareFileID,
	}
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(11)).Return(target, nil)
	store.EXPECT().
		ListCohortDeviceOwnership(gomock.Any(), int64(7), []string{"miner-1"}).
		Return(nil, nil)
	store.EXPECT().
		MoveDevicesToCohort(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.MembershipMutationParams)
			return ok &&
				params.DesiredFirmwareTargetManufacturer == "Proto" &&
				params.DesiredFirmwareTargetModel == "Rig"
		})).
		Return(cohortWithMembers(11, []models.CohortMember{
			cohortMember("miner-1", "Bitmain", "S21"),
		}), nil)

	_, err := svc.AddDevicesToCohort(context.Background(), models.MembershipMutationParams{
		OrgID:             7,
		CohortID:          11,
		ActorUserID:       actorUserID,
		ActorRole:         "FIELD_TECH",
		DeviceIdentifiers: []string{"miner-1"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "does not match cohort miner type")
}

func TestAddDevicesToCohort_RejectsMixedMinerTypes(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	actorUserID := int64(1)
	target := &models.Cohort{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: &actorUserID,
		State:       models.CohortStateActive,
	}
	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(11)).Return(target, nil)
	store.EXPECT().
		ListCohortDeviceOwnership(gomock.Any(), int64(7), []string{"miner-2"}).
		Return(nil, nil)
	store.EXPECT().
		MoveDevicesToCohort(gomock.Any(), gomock.Any()).
		Return(cohortWithMembers(11, []models.CohortMember{
			cohortMember("miner-1", "Proto", "Rig"),
			cohortMember("miner-2", "Bitmain", "S21"),
		}), nil)

	_, err := svc.AddDevicesToCohort(context.Background(), models.MembershipMutationParams{
		OrgID:             7,
		CohortID:          11,
		ActorUserID:       actorUserID,
		ActorRole:         "FIELD_TECH",
		DeviceIdentifiers: []string{"miner-2"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "single manufacturer and model")
}

func TestUpdateCohort_DefaultRejectsLegacyFirmwareSet(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	firmwareFileID := "fw-1"
	defaultCohort := &models.Cohort{ID: 42, OrgID: 7, Label: "Default", IsDefault: true, State: models.CohortStateActive}

	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(defaultCohort, nil)

	_, err := svc.UpdateCohort(context.Background(), models.UpdateCohortParams{
		OrgID:                    7,
		CohortID:                 42,
		DesiredFirmwareFileID:    &firmwareFileID,
		DesiredFirmwareFileIDSet: true,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "per manufacturer and model")
}

func TestSetCohortFirmwareTarget_DefaultAllowsPerModelTargets(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
		"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
	}))

	firmwareFileID := "fw-1"
	defaultCohort := &models.Cohort{ID: 42, OrgID: 7, Label: "Default", IsDefault: true, State: models.CohortStateActive}
	updated := &models.Cohort{
		ID:        42,
		OrgID:     7,
		Label:     "Default",
		IsDefault: true,
		State:     models.CohortStateActive,
		FirmwareTargets: []models.CohortFirmwareTarget{{
			CohortID:       42,
			OrgID:          7,
			Manufacturer:   "Proto",
			Model:          "Rig",
			FirmwareFileID: &firmwareFileID,
		}},
	}

	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(defaultCohort, nil)
	store.EXPECT().
		SetCohortFirmwareTarget(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.SetCohortFirmwareTargetParams)
			return ok &&
				params.OrgID == 7 &&
				params.CohortID == 42 &&
				params.Manufacturer != nil &&
				*params.Manufacturer == "Proto" &&
				params.Model != nil &&
				*params.Model == "Rig" &&
				params.FirmwareFileID != nil &&
				*params.FirmwareFileID == firmwareFileID
		})).
		Return(updated, nil)

	got, err := svc.SetCohortFirmwareTarget(context.Background(), models.SetCohortFirmwareTargetParams{
		OrgID:          7,
		CohortID:       42,
		ActorUserID:    1,
		ActorRole:      "SUPER_ADMIN",
		FirmwareFileID: &firmwareFileID,
	})
	require.NoError(t, err)
	assert.Equal(t, updated, got)
}

func TestSetCohortFirmwareTarget_RejectsFirmwareMismatch(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
		"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
	}))

	firmwareFileID := "fw-1"
	_, err := svc.SetCohortFirmwareTarget(context.Background(), models.SetCohortFirmwareTargetParams{
		OrgID:          7,
		CohortID:       42,
		ActorUserID:    1,
		ActorRole:      "SUPER_ADMIN",
		Manufacturer:   stringPtr("Bitmain"),
		Model:          stringPtr("S21"),
		FirmwareFileID: &firmwareFileID,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "does not match the requested target")
}

func TestUpdateCohort_DefaultRejectsNonFirmwareMutation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	label := "new label"
	defaultCohort := &models.Cohort{ID: 42, OrgID: 7, Label: "Default", IsDefault: true, State: models.CohortStateActive}

	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(defaultCohort, nil)

	_, err := svc.UpdateCohort(context.Background(), models.UpdateCohortParams{
		OrgID:    7,
		CohortID: 42,
		Label:    &label,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestUpdateCohort_AuditsSuccessfulFieldUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	oldExpiresAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	newExpiresAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	before := &models.Cohort{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		State:       models.CohortStateActive,
		ExpiresAt:   &oldExpiresAt,
		IsDefault:   false,
		OwnerUserID: ptrInt64(1),
	}
	after := &models.Cohort{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		State:       models.CohortStateActive,
		ExpiresAt:   &newExpiresAt,
		IsDefault:   false,
		OwnerUserID: ptrInt64(1),
	}

	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(11)).Return(before, nil)
	store.EXPECT().
		UpdateCohort(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.UpdateCohortParams)
			return ok && params.ExpiresAt != nil && params.ExpiresAt.Equal(newExpiresAt)
		})).
		Return(after, nil)

	got, err := svc.UpdateCohort(context.Background(), models.UpdateCohortParams{
		OrgID:     7,
		CohortID:  11,
		ExpiresAt: &newExpiresAt,
	})
	require.NoError(t, err)
	assert.Equal(t, after, got)
	require.Len(t, audit.events, 1)

	event := audit.events[0]
	assert.Equal(t, activityTypeUpdated, event.Type)
	assert.Equal(t, activitymodels.CategoryFleetManagement, event.Category)
	assert.Equal(t, int64(7), *event.OrganizationID)
	assert.Equal(t, "cohort", *event.ScopeType)
	assert.Equal(t, "reservation", *event.ScopeLabel)
	assert.Nil(t, event.ScopeCount)
	assert.Equal(t, "cohort_fields_updated", event.Metadata["update_kind"])
	assert.ElementsMatch(t, []string{"expires_at"}, event.Metadata["changed_fields"])
	assert.Equal(t, oldExpiresAt, event.Metadata["old_expires_at"])
	assert.Equal(t, newExpiresAt, event.Metadata["new_expires_at"])
}

func TestUpdateCohort_FailedValidationEmitsNoActivity(t *testing.T) {
	t.Parallel()

	audit := &recordingAuditLogger{}
	svc := NewService(mocks.NewMockCohortStore(gomock.NewController(t)), WithAuditLogger(audit))

	label := "   "
	_, err := svc.UpdateCohort(context.Background(), models.UpdateCohortParams{
		OrgID:    7,
		CohortID: 11,
		Label:    &label,
	})
	require.Error(t, err)
	assert.Empty(t, audit.events)
}

func TestSetCohortFirmwareTarget_AuditsSetAndClear(t *testing.T) {
	t.Parallel()

	t.Run("set", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		store := mocks.NewMockCohortStore(ctrl)
		audit := &recordingAuditLogger{}
		svc := NewService(store, WithAuditLogger(audit))

		oldFirmwareFileID := "fw-old"
		newFirmwareFileID := "fw-new"
		before := &models.Cohort{
			ID:        42,
			OrgID:     7,
			Label:     "Default",
			IsDefault: true,
			State:     models.CohortStateActive,
			FirmwareTargets: []models.CohortFirmwareTarget{{
				Manufacturer:   "Proto",
				Model:          "Rig",
				FirmwareFileID: &oldFirmwareFileID,
			}},
		}
		after := &models.Cohort{
			ID:        42,
			OrgID:     7,
			Label:     "Default",
			IsDefault: true,
			State:     models.CohortStateActive,
			FirmwareTargets: []models.CohortFirmwareTarget{{
				Manufacturer:   "Proto",
				Model:          "Rig",
				FirmwareFileID: &newFirmwareFileID,
			}},
		}
		store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(before, nil)
		store.EXPECT().SetCohortFirmwareTarget(gomock.Any(), gomock.Any()).Return(after, nil)

		got, err := svc.SetCohortFirmwareTarget(context.Background(), models.SetCohortFirmwareTargetParams{
			OrgID:          7,
			CohortID:       42,
			ActorUserID:    1,
			ActorRole:      "SUPER_ADMIN",
			Manufacturer:   stringPtr("Proto"),
			Model:          stringPtr("Rig"),
			FirmwareFileID: &newFirmwareFileID,
		})
		require.NoError(t, err)
		assert.Equal(t, after, got)
		require.Len(t, audit.events, 1)
		assertFirmwareTargetAudit(t, audit.events[0], "Proto", "Rig", oldFirmwareFileID, newFirmwareFileID)
	})

	t.Run("clear", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		store := mocks.NewMockCohortStore(ctrl)
		audit := &recordingAuditLogger{}
		svc := NewService(store, WithAuditLogger(audit))

		oldFirmwareFileID := "fw-old"
		before := &models.Cohort{
			ID:        42,
			OrgID:     7,
			Label:     "Default",
			IsDefault: true,
			State:     models.CohortStateActive,
			FirmwareTargets: []models.CohortFirmwareTarget{{
				Manufacturer:   "Proto",
				Model:          "Rig",
				FirmwareFileID: &oldFirmwareFileID,
			}},
		}
		after := &models.Cohort{ID: 42, OrgID: 7, Label: "Default", IsDefault: true, State: models.CohortStateActive}
		store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(42)).Return(before, nil)
		store.EXPECT().SetCohortFirmwareTarget(gomock.Any(), gomock.Any()).Return(after, nil)

		got, err := svc.SetCohortFirmwareTarget(context.Background(), models.SetCohortFirmwareTargetParams{
			OrgID:        7,
			CohortID:     42,
			ActorUserID:  1,
			ActorRole:    "SUPER_ADMIN",
			Manufacturer: stringPtr("Proto"),
			Model:        stringPtr("Rig"),
		})
		require.NoError(t, err)
		assert.Equal(t, after, got)
		require.Len(t, audit.events, 1)
		assertFirmwareTargetAudit(t, audit.events[0], "Proto", "Rig", oldFirmwareFileID, nil)
	})
}

func TestAddDevicesToCohort_AuditsCountOnlyMembershipUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	target := &models.Cohort{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: ptrInt64(1),
		State:       models.CohortStateActive,
	}
	updated := cohortWithMembers(11, []models.CohortMember{
		cohortMember("miner-1", "Proto", "Rig"),
		cohortMember("miner-2", "Proto", "Rig"),
	})

	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(11)).Return(target, nil)
	store.EXPECT().
		ListCohortDeviceOwnership(gomock.Any(), int64(7), []string{"miner-1", "miner-2"}).
		Return(nil, nil)
	store.EXPECT().MoveDevicesToCohort(gomock.Any(), gomock.Any()).Return(updated, nil)

	got, err := svc.AddDevicesToCohort(context.Background(), models.MembershipMutationParams{
		OrgID:             7,
		CohortID:          11,
		ActorUserID:       1,
		ActorRole:         "FIELD_TECH",
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	require.NoError(t, err)
	assert.Equal(t, updated, got)
	require.Len(t, audit.events, 1)
	assertMembershipAudit(t, audit.events[0], "members_added", 2, 2)
	assert.NotContains(t, audit.events[0].Metadata, "device_identifiers")
}

func TestRemoveDevicesFromCohort_AuditsCountOnlyMembershipUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	target := &models.Cohort{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: ptrInt64(1),
		State:       models.CohortStateActive,
	}
	updated := &models.Cohort{ID: 11, OrgID: 7, Label: "reservation", State: models.CohortStateActive}

	store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(11)).Return(target, nil)
	store.EXPECT().RemoveDevicesAndGetCohort(gomock.Any(), gomock.Any()).Return(updated, nil)

	got, err := svc.RemoveDevicesFromCohort(context.Background(), models.MembershipMutationParams{
		OrgID:             7,
		CohortID:          11,
		ActorUserID:       1,
		ActorRole:         "FIELD_TECH",
		DeviceIdentifiers: []string{"miner-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, updated, got)
	require.Len(t, audit.events, 1)
	assertMembershipAudit(t, audit.events[0], "members_removed", 1, -1)
	assert.NotContains(t, audit.events[0].Metadata, "device_identifiers")
}

func TestUpdateCohort_ValidatesDesiredFirmwareTarget(t *testing.T) {
	t.Parallel()

	t.Run("matching target", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		store := mocks.NewMockCohortStore(ctrl)
		svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
			"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
		}))

		firmwareFileID := "fw-1"
		target := cohortWithMembers(11, []models.CohortMember{
			cohortMember("miner-1", "Proto", "Rig"),
			cohortMember("miner-2", "Proto", "Rig"),
		})
		updated := &models.Cohort{ID: 11, OrgID: 7, Label: "reservation", State: models.CohortStateActive, DesiredFirmwareFileID: &firmwareFileID}

		store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(11)).Return(target, nil)
		store.EXPECT().
			UpdateCohort(gomock.Any(), gomock.Cond(func(v any) bool {
				params, ok := v.(models.UpdateCohortParams)
				return ok && params.DesiredFirmwareFileIDSet && params.DesiredFirmwareFileID != nil && *params.DesiredFirmwareFileID == firmwareFileID
			})).
			Return(updated, nil)
		store.EXPECT().
			SetCohortFirmwareTarget(gomock.Any(), gomock.Cond(func(v any) bool {
				params, ok := v.(models.SetCohortFirmwareTargetParams)
				return ok &&
					params.OrgID == 7 &&
					params.CohortID == 11 &&
					params.Manufacturer != nil &&
					*params.Manufacturer == "Proto" &&
					params.Model != nil &&
					*params.Model == "Rig" &&
					params.FirmwareFileID != nil &&
					*params.FirmwareFileID == firmwareFileID
			})).
			Return(updated, nil)

		got, err := svc.UpdateCohort(context.Background(), models.UpdateCohortParams{
			OrgID:                    7,
			CohortID:                 11,
			DesiredFirmwareFileID:    &firmwareFileID,
			DesiredFirmwareFileIDSet: true,
		})
		require.NoError(t, err)
		assert.Equal(t, updated, got)
	})

	t.Run("mismatched target", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		store := mocks.NewMockCohortStore(ctrl)
		svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
			"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
		}))

		firmwareFileID := "fw-1"
		target := cohortWithMembers(11, []models.CohortMember{
			cohortMember("miner-1", "Bitmain", "S21"),
		})

		store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(11)).Return(target, nil)

		_, err := svc.UpdateCohort(context.Background(), models.UpdateCohortParams{
			OrgID:                    7,
			CohortID:                 11,
			DesiredFirmwareFileID:    &firmwareFileID,
			DesiredFirmwareFileIDSet: true,
		})
		require.Error(t, err)
		assert.True(t, fleeterror.IsInvalidArgumentError(err))
		assert.Contains(t, err.Error(), "does not match cohort miner type")
	})

	t.Run("mixed cohort target", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		store := mocks.NewMockCohortStore(ctrl)
		svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
			"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
		}))

		firmwareFileID := "fw-1"
		target := cohortWithMembers(11, []models.CohortMember{
			cohortMember("miner-1", "Proto", "Rig"),
			cohortMember("miner-2", "Bitmain", "S21"),
		})

		store.EXPECT().GetCohort(gomock.Any(), int64(7), int64(11)).Return(target, nil)

		_, err := svc.UpdateCohort(context.Background(), models.UpdateCohortParams{
			OrgID:                    7,
			CohortID:                 11,
			DesiredFirmwareFileID:    &firmwareFileID,
			DesiredFirmwareFileIDSet: true,
		})
		require.Error(t, err)
		assert.True(t, fleeterror.IsInvalidArgumentError(err))
		assert.Contains(t, err.Error(), "single manufacturer and model")
	})
}

func TestDeleteCohort_DelegatesRelease(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockCohortStore(ctrl)
	svc := NewService(store)

	released := &models.Cohort{ID: 42, OrgID: 7, Label: "done", State: models.CohortStateReleased}
	store.EXPECT().ReleaseCohort(gomock.Any(), int64(7), int64(42)).Return(released, nil)

	got, err := svc.DeleteCohort(context.Background(), 7, 42)
	require.NoError(t, err)
	assert.Equal(t, released, got)
}

func validCreateParams(deviceIdentifiers []string) models.CreateCohortParams {
	return models.CreateCohortParams{
		OrgID:             7,
		Label:             "reservation",
		Purpose:           "test",
		DeviceIdentifiers: deviceIdentifiers,
	}
}

type fakeFirmwareMetadataProvider map[string]files.FirmwareMetadata

func (p fakeFirmwareMetadataProvider) GetFirmwareMetadata(fileID string) (files.FirmwareMetadata, error) {
	metadata, ok := p[fileID]
	if !ok {
		return files.FirmwareMetadata{}, fleeterror.NewNotFoundErrorf("firmware file %q not found", fileID)
	}
	return metadata, nil
}

func cohortWithMembers(id int64, members []models.CohortMember) *models.Cohort {
	return &models.Cohort{
		ID:      id,
		OrgID:   7,
		Label:   "reservation",
		State:   models.CohortStateActive,
		Members: members,
	}
}

func cohortMember(deviceIdentifier, manufacturer, model string) models.CohortMember {
	return models.CohortMember{
		OrgID:            7,
		DeviceIdentifier: deviceIdentifier,
		Display: models.CohortDeviceDisplay{
			Manufacturer: manufacturer,
			Model:        model,
		},
	}
}

type recordingAuditLogger struct {
	events []activitymodels.Event
}

func (l *recordingAuditLogger) Log(_ context.Context, event activitymodels.Event) {
	l.events = append(l.events, event)
}

func ptrInt64(value int64) *int64 {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func assertFirmwareTargetAudit(t *testing.T, event activitymodels.Event, manufacturer, model string, oldFirmwareFileID string, newFirmwareFileID any) {
	t.Helper()

	assert.Equal(t, activityTypeUpdated, event.Type)
	assert.Equal(t, "cohort", *event.ScopeType)
	assert.Equal(t, "Default", *event.ScopeLabel)
	assert.Nil(t, event.ScopeCount)
	assert.Equal(t, "firmware_target_updated", event.Metadata["update_kind"])
	assert.Equal(t, manufacturer, event.Metadata["manufacturer"])
	assert.Equal(t, model, event.Metadata["model"])
	assert.Equal(t, oldFirmwareFileID, event.Metadata["old_firmware_file_id"])
	assert.Equal(t, newFirmwareFileID, event.Metadata["new_firmware_file_id"])
}

func assertMembershipAudit(t *testing.T, event activitymodels.Event, updateKind string, affectedCount, memberCountDelta int) {
	t.Helper()

	assert.Equal(t, activityTypeUpdated, event.Type)
	assert.Equal(t, "cohort", *event.ScopeType)
	assert.Equal(t, "reservation", *event.ScopeLabel)
	require.NotNil(t, event.ScopeCount)
	assert.Equal(t, affectedCount, *event.ScopeCount)
	assert.Equal(t, updateKind, event.Metadata["update_kind"])
	assert.Equal(t, affectedCount, event.Metadata["affected_member_count"])
	assert.Equal(t, memberCountDelta, event.Metadata["member_count_delta"])
}
