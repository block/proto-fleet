package minerchannel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	poolpb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

type fakePoolReferenceProvider struct {
	orgID int64
	ids   map[int64]bool
}

func (f fakePoolReferenceProvider) GetPool(_ context.Context, orgID, poolID int64) (*poolpb.Pool, error) {
	if orgID != f.orgID || !f.ids[poolID] {
		return nil, fleeterror.NewNotFoundError("pool not found")
	}
	return &poolpb.Pool{PoolId: poolID}, nil
}

func TestValidateDesiredConfigPoolReferences(t *testing.T) {
	svc := NewService(nil, WithPoolReferenceProvider(fakePoolReferenceProvider{orgID: 7, ids: map[int64]bool{1: true, 2: true, 3: true}}))
	backup1, backup2 := int64(2), int64(3)
	require.NoError(t, svc.validateDesiredConfig(context.Background(), 7, &models.MinerChannelDesiredConfig{Pools: &models.MinerChannelPoolDesiredConfig{
		PrimaryPoolID: 1, Backup1PoolID: &backup1, Backup2PoolID: &backup2,
	}}, nil))

	duplicate := int64(1)
	err := svc.validateDesiredConfig(context.Background(), 7, &models.MinerChannelDesiredConfig{Pools: &models.MinerChannelPoolDesiredConfig{
		PrimaryPoolID: 1, Backup1PoolID: &duplicate,
	}}, nil)
	require.ErrorContains(t, err, "different pool")

	err = svc.validateDesiredConfig(context.Background(), 8, &models.MinerChannelDesiredConfig{Pools: &models.MinerChannelPoolDesiredConfig{PrimaryPoolID: 1}}, nil)
	require.ErrorContains(t, err, "not an active pool in this organization")
}

func TestParseMinerChannelDesiredConfigClearAndTypedJSON(t *testing.T) {
	config, err := models.ParseMinerChannelDesiredConfig(nil)
	require.NoError(t, err)
	require.Nil(t, config)

	raw, err := (&models.MinerChannelDesiredConfig{Pools: &models.MinerChannelPoolDesiredConfig{PrimaryPoolID: 42}}).MarshalJSON()
	require.NoError(t, err)
	require.JSONEq(t, `{"pools":{"primary_pool_id":42}}`, string(raw))
	parsed, err := models.ParseMinerChannelDesiredConfig(raw)
	require.NoError(t, err)
	require.Equal(t, int64(42), parsed.Pools.PrimaryPoolID)
}

func TestCreateMinerChannel_ValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	svc := NewService(mocks.NewMockMinerChannelStore(gomock.NewController(t)))

	_, err := svc.CreateMinerChannel(t.Context(), models.CreateMinerChannelParams{
		Label:   "   ",
		Purpose: "test",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = svc.CreateMinerChannel(t.Context(), models.CreateMinerChannelParams{
		Label:   "reservation",
		Purpose: "   ",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestCreateMinerChannel_RejectsEmptyAndDuplicateDeviceIdentifiers(t *testing.T) {
	t.Parallel()

	svc := NewService(mocks.NewMockMinerChannelStore(gomock.NewController(t)))

	_, err := svc.CreateMinerChannel(t.Context(), validCreateParams(nil))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = svc.CreateMinerChannel(t.Context(), validCreateParams([]string{"miner-1", "   "}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = svc.CreateMinerChannel(t.Context(), validCreateParams([]string{"miner-1", "miner-1"}))
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestCreateMinerChannel_NormalizesAndPersists(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store)

	expected := &models.MinerChannel{
		ID:      11,
		OrgID:   7,
		Label:   "reservation",
		Purpose: "test firmware",
		State:   models.MinerChannelStateActive,
	}

	store.EXPECT().
		CreateMinerChannel(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.CreateMinerChannelParams)
			return ok &&
				params.Label == "reservation" &&
				params.Purpose == "test firmware" &&
				params.SourceActorType == models.SourceActorUser &&
				len(params.DeviceIdentifiers) == 2 &&
				params.DeviceIdentifiers[0] == "miner-1" &&
				params.DeviceIdentifiers[1] == "miner-2"
		})).
		Return(expected, nil)

	got, err := svc.CreateMinerChannel(context.Background(), models.CreateMinerChannelParams{
		OrgID:             7,
		Label:             "  reservation  ",
		Purpose:           "  test firmware  ",
		DeviceIdentifiers: []string{"  miner-1  ", "  miner-2  "},
	})
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestCreateMinerChannel_NormalizesSelectorFilters(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store)

	product := "  TestCorp  "
	model := "  TestMiner  "
	expected := &models.MinerChannel{ID: 11, OrgID: 7, Label: "reservation", Purpose: "test", State: models.MinerChannelStateActive}
	store.EXPECT().
		CreateMinerChannel(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.CreateMinerChannelParams)
			return ok &&
				params.DeviceSelector != nil &&
				params.DeviceSelector.Count == 2 &&
				params.DeviceSelector.Product != nil &&
				*params.DeviceSelector.Product == "TestCorp" &&
				params.DeviceSelector.Model != nil &&
				*params.DeviceSelector.Model == "TestMiner"
		})).
		Return(expected, nil)

	got, err := svc.CreateMinerChannel(context.Background(), models.CreateMinerChannelParams{
		OrgID:          7,
		Label:          "reservation",
		Purpose:        "test",
		DeviceSelector: &models.MinerChannelDeviceSelector{Count: 2, Product: &product, Model: &model},
	})
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestCreateMinerChannel_RejectsInvalidSelector(t *testing.T) {
	t.Parallel()

	svc := NewService(mocks.NewMockMinerChannelStore(gomock.NewController(t)))

	_, err := svc.CreateMinerChannel(t.Context(), models.CreateMinerChannelParams{
		OrgID:             7,
		Label:             "reservation",
		Purpose:           "test",
		DeviceIdentifiers: []string{"miner-1"},
		DeviceSelector:    &models.MinerChannelDeviceSelector{Count: 1},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = svc.CreateMinerChannel(t.Context(), models.CreateMinerChannelParams{
		OrgID:          7,
		Label:          "reservation",
		Purpose:        "test",
		DeviceSelector: &models.MinerChannelDeviceSelector{},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestCreateMinerChannel_RejectsMixedMinerTypes(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store)

	store.EXPECT().
		CreateMinerChannel(gomock.Any(), gomock.Any()).
		Return(minerChannelWithMembers(11, []models.MinerChannelMember{
			minerChannelMember("miner-1", "Proto", "Rig"),
			minerChannelMember("miner-2", "Bitmain", "S21"),
		}), nil)

	_, err := svc.CreateMinerChannel(context.Background(), models.CreateMinerChannelParams{
		OrgID:             7,
		Label:             "reservation",
		Purpose:           "mixed",
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "single manufacturer and model")
}

func TestAuthorizeMinerChannelOwnerMutation_OwnerlessRequiresSuperAdmin(t *testing.T) {
	t.Parallel()

	minerChannel := &models.MinerChannel{ID: 42, OrgID: 7, Label: "standing", State: models.MinerChannelStateActive}
	err := authorizeMinerChannelOwnerMutation(minerChannel, 1, "FIELD_TECH")
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))

	err = authorizeMinerChannelOwnerMutation(minerChannel, 1, "ADMIN")
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))

	err = authorizeMinerChannelOwnerMutation(minerChannel, 1, "SUPER_ADMIN")
	require.NoError(t, err)
}

func TestReleaseMinerChannel_AdminCannotReleaseAnotherOwnersMinerChannel(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store)

	minerChannel := &models.MinerChannel{
		ID:          42,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: ptrInt64(99),
		State:       models.MinerChannelStateActive,
	}
	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(42)).Return(minerChannel, nil)

	_, err := svc.ReleaseMinerChannel(context.Background(), models.MembershipMutationParams{
		OrgID:          7,
		MinerChannelID: 42,
		ActorUserID:    1,
		ActorRole:      "ADMIN",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
}

func TestSetMinerChannelFirmwareTarget_AdminCannotMutateAnotherOwnersMinerChannel(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store)

	minerChannel := minerChannelWithMembers(42, []models.MinerChannelMember{
		minerChannelMember("miner-1", "Proto", "Rig"),
	})
	minerChannel.OwnerUserID = ptrInt64(99)
	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(42)).Return(minerChannel, nil)

	_, err := svc.SetMinerChannelFirmwareTarget(context.Background(), models.SetMinerChannelFirmwareTargetParams{
		OrgID:          7,
		MinerChannelID: 42,
		ActorUserID:    1,
		ActorRole:      "ADMIN",
		Manufacturer:   stringPtr("Proto"),
		Model:          stringPtr("Rig"),
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
}

func TestAuthorizeDeviceMoves_OwnerlessSourceRequiresAdmin(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store)

	params := models.MembershipMutationParams{
		OrgID:             7,
		MinerChannelID:    11,
		ActorUserID:       1,
		ActorRole:         "FIELD_TECH",
		DeviceIdentifiers: []string{"miner-1"},
	}
	store.EXPECT().
		ListMinerChannelDeviceOwnership(gomock.Any(), int64(7), []string{"miner-1"}).
		Return([]models.MinerChannelDeviceOwnership{{
			DeviceIdentifier: "miner-1",
			MinerChannelID:   99,
		}}, nil)

	err := svc.authorizeDeviceMoves(context.Background(), params)
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
}

func TestAddDevicesToMinerChannel_RejectsMixedMinerTypes(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store)

	actorUserID := int64(1)
	target := &models.MinerChannel{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: &actorUserID,
		State:       models.MinerChannelStateActive,
	}
	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(11)).Return(target, nil)
	store.EXPECT().
		ListMinerChannelDeviceOwnership(gomock.Any(), int64(7), []string{"miner-2"}).
		Return(nil, nil)
	store.EXPECT().
		MoveDevicesToMinerChannel(gomock.Any(), gomock.Any()).
		Return(minerChannelWithMembers(11, []models.MinerChannelMember{
			minerChannelMember("miner-1", "Proto", "Rig"),
			minerChannelMember("miner-2", "Bitmain", "S21"),
		}), nil)

	_, err := svc.AddDevicesToMinerChannel(context.Background(), models.MembershipMutationParams{
		OrgID:             7,
		MinerChannelID:    11,
		ActorUserID:       actorUserID,
		ActorRole:         "FIELD_TECH",
		DeviceIdentifiers: []string{"miner-2"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "single manufacturer and model")
}

func TestSetMinerChannelFirmwareTarget_DefaultAllowsPerModelTargets(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
		"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
	}))

	firmwareFileID := "fw-1"
	defaultMinerChannel := &models.MinerChannel{ID: 42, OrgID: 7, Label: "Default", IsDefault: true, State: models.MinerChannelStateActive}
	updated := &models.MinerChannel{
		ID:        42,
		OrgID:     7,
		Label:     "Default",
		IsDefault: true,
		State:     models.MinerChannelStateActive,
		FirmwareTargets: []models.MinerChannelFirmwareTarget{{
			MinerChannelID: 42,
			OrgID:          7,
			Manufacturer:   "Proto",
			Model:          "Rig",
			FirmwareFileID: &firmwareFileID,
		}},
	}

	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(42)).Return(defaultMinerChannel, nil)
	store.EXPECT().
		SetMinerChannelFirmwareTarget(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.SetMinerChannelFirmwareTargetParams)
			return ok &&
				params.OrgID == 7 &&
				params.MinerChannelID == 42 &&
				params.Manufacturer != nil &&
				*params.Manufacturer == "Proto" &&
				params.Model != nil &&
				*params.Model == "Rig" &&
				params.FirmwareFileID != nil &&
				*params.FirmwareFileID == firmwareFileID
		})).
		Return(updated, nil)

	got, err := svc.SetMinerChannelFirmwareTarget(context.Background(), models.SetMinerChannelFirmwareTargetParams{
		OrgID:          7,
		MinerChannelID: 42,
		ActorUserID:    1,
		ActorRole:      "SUPER_ADMIN",
		FirmwareFileID: &firmwareFileID,
	})
	require.NoError(t, err)
	assert.Equal(t, updated, got)
}

func TestSetMinerChannelFirmwareTarget_RejectsFirmwareMismatch(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store, WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
		"fw-1": {TargetManufacturer: "Proto", TargetModel: "Rig"},
	}))

	firmwareFileID := "fw-1"
	_, err := svc.SetMinerChannelFirmwareTarget(context.Background(), models.SetMinerChannelFirmwareTargetParams{
		OrgID:          7,
		MinerChannelID: 42,
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

func TestUpdateMinerChannel_DefaultRejectsNonFirmwareMutation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store)

	label := "new label"
	defaultMinerChannel := &models.MinerChannel{ID: 42, OrgID: 7, Label: "Default", IsDefault: true, State: models.MinerChannelStateActive}

	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(42)).Return(defaultMinerChannel, nil)

	_, err := svc.UpdateMinerChannel(context.Background(), models.UpdateMinerChannelParams{
		OrgID:          7,
		MinerChannelID: 42,
		Label:          &label,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestUpdateMinerChannel_AuditsSuccessfulFieldUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	oldExpiresAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	newExpiresAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	before := &models.MinerChannel{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		State:       models.MinerChannelStateActive,
		ExpiresAt:   &oldExpiresAt,
		IsDefault:   false,
		OwnerUserID: ptrInt64(1),
	}
	after := &models.MinerChannel{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		State:       models.MinerChannelStateActive,
		ExpiresAt:   &newExpiresAt,
		IsDefault:   false,
		OwnerUserID: ptrInt64(1),
	}

	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(11)).Return(before, nil)
	store.EXPECT().
		UpdateMinerChannel(gomock.Any(), gomock.Cond(func(v any) bool {
			params, ok := v.(models.UpdateMinerChannelParams)
			return ok && params.ExpiresAt != nil && params.ExpiresAt.Equal(newExpiresAt)
		})).
		Return(after, nil)

	got, err := svc.UpdateMinerChannel(context.Background(), models.UpdateMinerChannelParams{
		OrgID:          7,
		MinerChannelID: 11,
		ExpiresAt:      &newExpiresAt,
	})
	require.NoError(t, err)
	assert.Equal(t, after, got)
	require.Len(t, audit.events, 1)

	event := audit.events[0]
	assert.Equal(t, activityTypeUpdated, event.Type)
	assert.Equal(t, activitymodels.CategoryFleetManagement, event.Category)
	assert.Equal(t, int64(7), *event.OrganizationID)
	assert.Equal(t, "miner_channel", *event.ScopeType)
	assert.Equal(t, "reservation", *event.ScopeLabel)
	assert.Nil(t, event.ScopeCount)
	assert.Equal(t, "miner_channel_fields_updated", event.Metadata["update_kind"])
	assert.ElementsMatch(t, []string{"expires_at"}, event.Metadata["changed_fields"])
	assert.Equal(t, oldExpiresAt, event.Metadata["old_expires_at"])
	assert.Equal(t, newExpiresAt, event.Metadata["new_expires_at"])
}

func TestUpdateMinerChannel_FailedValidationEmitsNoActivity(t *testing.T) {
	t.Parallel()

	audit := &recordingAuditLogger{}
	svc := NewService(mocks.NewMockMinerChannelStore(gomock.NewController(t)), WithAuditLogger(audit))

	label := "   "
	_, err := svc.UpdateMinerChannel(context.Background(), models.UpdateMinerChannelParams{
		OrgID:          7,
		MinerChannelID: 11,
		Label:          &label,
	})
	require.Error(t, err)
	assert.Empty(t, audit.events)
}

func TestSetMinerChannelFirmwareTarget_AuditsSetAndClear(t *testing.T) {
	t.Parallel()

	t.Run("set", func(t *testing.T) {
		t.Parallel()

		ctrl := gomock.NewController(t)
		store := mocks.NewMockMinerChannelStore(ctrl)
		audit := &recordingAuditLogger{}
		svc := NewService(store, WithAuditLogger(audit))

		oldFirmwareFileID := "fw-old"
		newFirmwareFileID := "fw-new"
		before := &models.MinerChannel{
			ID:        42,
			OrgID:     7,
			Label:     "Default",
			IsDefault: true,
			State:     models.MinerChannelStateActive,
			FirmwareTargets: []models.MinerChannelFirmwareTarget{{
				Manufacturer:   "Proto",
				Model:          "Rig",
				FirmwareFileID: &oldFirmwareFileID,
			}},
		}
		after := &models.MinerChannel{
			ID:        42,
			OrgID:     7,
			Label:     "Default",
			IsDefault: true,
			State:     models.MinerChannelStateActive,
			FirmwareTargets: []models.MinerChannelFirmwareTarget{{
				Manufacturer:   "Proto",
				Model:          "Rig",
				FirmwareFileID: &newFirmwareFileID,
			}},
		}
		store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(42)).Return(before, nil)
		store.EXPECT().SetMinerChannelFirmwareTarget(gomock.Any(), gomock.Any()).Return(after, nil)

		got, err := svc.SetMinerChannelFirmwareTarget(context.Background(), models.SetMinerChannelFirmwareTargetParams{
			OrgID:          7,
			MinerChannelID: 42,
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
		store := mocks.NewMockMinerChannelStore(ctrl)
		audit := &recordingAuditLogger{}
		svc := NewService(store, WithAuditLogger(audit))

		oldFirmwareFileID := "fw-old"
		before := &models.MinerChannel{
			ID:        42,
			OrgID:     7,
			Label:     "Default",
			IsDefault: true,
			State:     models.MinerChannelStateActive,
			FirmwareTargets: []models.MinerChannelFirmwareTarget{{
				Manufacturer:   "Proto",
				Model:          "Rig",
				FirmwareFileID: &oldFirmwareFileID,
			}},
		}
		after := &models.MinerChannel{ID: 42, OrgID: 7, Label: "Default", IsDefault: true, State: models.MinerChannelStateActive}
		store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(42)).Return(before, nil)
		store.EXPECT().SetMinerChannelFirmwareTarget(gomock.Any(), gomock.Any()).Return(after, nil)

		got, err := svc.SetMinerChannelFirmwareTarget(context.Background(), models.SetMinerChannelFirmwareTargetParams{
			OrgID:          7,
			MinerChannelID: 42,
			ActorUserID:    1,
			ActorRole:      "SUPER_ADMIN",
			Manufacturer:   stringPtr("Proto"),
			Model:          stringPtr("Rig"),
		})
		require.NoError(t, err)
		assert.Equal(t, after, got)
		require.Len(t, audit.events, 1)
		assertFirmwareTargetAudit(t, audit.events[0], "Proto", "Rig", oldFirmwareFileID, nil)
	})
}

func TestAddDevicesToMinerChannel_AuditsCountOnlyMembershipUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	target := &models.MinerChannel{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: ptrInt64(1),
		State:       models.MinerChannelStateActive,
	}
	updated := minerChannelWithMembers(11, []models.MinerChannelMember{
		minerChannelMember("miner-1", "Proto", "Rig"),
		minerChannelMember("miner-2", "Proto", "Rig"),
	})

	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(11)).Return(target, nil)
	store.EXPECT().
		ListMinerChannelDeviceOwnership(gomock.Any(), int64(7), []string{"miner-1", "miner-2"}).
		Return(nil, nil)
	store.EXPECT().MoveDevicesToMinerChannel(gomock.Any(), gomock.Any()).Return(updated, nil)

	got, err := svc.AddDevicesToMinerChannel(context.Background(), models.MembershipMutationParams{
		OrgID:             7,
		MinerChannelID:    11,
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

func TestRemoveDevicesFromMinerChannel_AuditsCountOnlyMembershipUpdate(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	target := &models.MinerChannel{
		ID:          11,
		OrgID:       7,
		Label:       "reservation",
		OwnerUserID: ptrInt64(1),
		State:       models.MinerChannelStateActive,
	}
	updated := &models.MinerChannel{ID: 11, OrgID: 7, Label: "reservation", State: models.MinerChannelStateActive}

	store.EXPECT().GetMinerChannel(gomock.Any(), int64(7), int64(11)).Return(target, nil)
	store.EXPECT().RemoveDevicesAndGetMinerChannel(gomock.Any(), gomock.Any()).Return(updated, nil)

	got, err := svc.RemoveDevicesFromMinerChannel(context.Background(), models.MembershipMutationParams{
		OrgID:             7,
		MinerChannelID:    11,
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

func TestDeleteMinerChannel_DelegatesRelease(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	store := mocks.NewMockMinerChannelStore(ctrl)
	svc := NewService(store)

	released := &models.MinerChannel{ID: 42, OrgID: 7, Label: "done", State: models.MinerChannelStateReleased}
	store.EXPECT().ReleaseMinerChannel(gomock.Any(), int64(7), int64(42)).Return(released, nil)

	got, err := svc.DeleteMinerChannel(context.Background(), 7, 42)
	require.NoError(t, err)
	assert.Equal(t, released, got)
}

func TestDeriveFirmwareRolloutState(t *testing.T) {
	t.Parallel()

	dispatching := models.EnforcementStateDispatching
	dispatched := models.EnforcementStateDispatched
	failed := models.EnforcementStateFailed
	pending := models.EnforcementStatePending

	tests := []struct {
		name   string
		status *models.MinerChannelFirmwareStatus
		want   models.MinerChannelFirmwareRolloutState
	}{
		{
			name:   "no target",
			status: &models.MinerChannelFirmwareStatus{},
			want:   models.MinerChannelFirmwareRolloutStateNoTarget,
		},
		{
			name: "dispatching takes precedence",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID: "fw-1",
				DeviceStatus:         "UPDATING",
				EnforcementState:     &dispatching,
			},
			want: models.MinerChannelFirmwareRolloutStateUpdating,
		},
		{
			name: "device updating",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID:  "fw-1",
				TargetFirmwareVersion: "1.2.0",
				DeviceStatus:          "UPDATING",
			},
			want: models.MinerChannelFirmwareRolloutStateUpdating,
		},
		{
			name: "device reboot required",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID:  "fw-1",
				TargetFirmwareVersion: "1.2.0",
				DeviceStatus:          "REBOOT_REQUIRED",
			},
			want: models.MinerChannelFirmwareRolloutStateUpdating,
		},
		{
			name: "complete",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID:   "fw-1",
				TargetFirmwareVersion:  "1.2.0",
				CurrentFirmwareVersion: "1.2.0",
			},
			want: models.MinerChannelFirmwareRolloutStateComplete,
		},
		{
			name: "verifying",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID:   "fw-1",
				TargetFirmwareVersion:  "1.2.0",
				CurrentFirmwareVersion: "1.1.0",
				EnforcementState:       &dispatched,
			},
			want: models.MinerChannelFirmwareRolloutStateVerifying,
		},
		{
			name: "failed needs attention without target version",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID:   "fw-1",
				CurrentFirmwareVersion: "1.1.0",
				EnforcementState:       &failed,
			},
			want: models.MinerChannelFirmwareRolloutStateNeedsAttention,
		},
		{
			name: "retrying needs attention",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID:  "fw-1",
				TargetFirmwareVersion: "1.2.0",
				EnforcementState:      &pending,
				RetryCount:            1,
			},
			want: models.MinerChannelFirmwareRolloutStateNeedsAttention,
		},
		{
			name: "policy hold needs attention",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID:  "fw-1",
				TargetFirmwareVersion: "1.2.0",
				EnforcementState:      &pending,
				LastError:             stringPtr("command policy held dispatch"),
			},
			want: models.MinerChannelFirmwareRolloutStateNeedsAttention,
		},
		{
			name: "drifted queues another pass",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID:   "fw-1",
				TargetFirmwareVersion:  "1.2.0",
				CurrentFirmwareVersion: "1.1.0",
			},
			want: models.MinerChannelFirmwareRolloutStateQueued,
		},
		{
			name: "queued",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID:  "fw-1",
				TargetFirmwareVersion: "1.2.0",
			},
			want: models.MinerChannelFirmwareRolloutStateQueued,
		},
		{
			name: "unknown when target version is unavailable",
			status: &models.MinerChannelFirmwareStatus{
				TargetFirmwareFileID: "fw-1",
			},
			want: models.MinerChannelFirmwareRolloutStateUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, deriveFirmwareRolloutState(tt.status))
		})
	}
}

func TestHydrateMinerChannelFirmwareUsesMetadataVersionAndAggregatesProgress(t *testing.T) {
	t.Parallel()

	svc := NewService(
		mocks.NewMockMinerChannelStore(gomock.NewController(t)),
		WithFirmwareMetadataProvider(fakeFirmwareMetadataProvider{
			"fw-1": {FirmwareVersion: "1.2.0"},
		}),
	)

	minerChannel := minerChannelWithMembers(11, []models.MinerChannelMember{
		{
			OrgID:            7,
			DeviceIdentifier: "miner-1",
			FirmwareStatus: &models.MinerChannelFirmwareStatus{
				DeviceIdentifier:       "miner-1",
				TargetFirmwareFileID:   "fw-1",
				TargetFirmwareVersion:  "cached-version",
				CurrentFirmwareVersion: "1.2.0",
			},
		},
		{
			OrgID:            7,
			DeviceIdentifier: "miner-2",
			FirmwareStatus: &models.MinerChannelFirmwareStatus{
				DeviceIdentifier:       "miner-2",
				TargetFirmwareFileID:   "fw-1",
				TargetFirmwareVersion:  "cached-version",
				CurrentFirmwareVersion: "1.1.0",
			},
		},
	})

	svc.hydrateMinerChannelFirmware(context.Background(), minerChannel)

	require.NotNil(t, minerChannel.Members[0].FirmwareStatus)
	assert.Equal(t, "1.2.0", minerChannel.Members[0].FirmwareStatus.TargetFirmwareVersion)
	assert.Equal(t, models.MinerChannelFirmwareRolloutStateComplete, minerChannel.Members[0].FirmwareStatus.State)
	require.NotNil(t, minerChannel.Members[1].FirmwareStatus)
	assert.Equal(t, models.MinerChannelFirmwareRolloutStateQueued, minerChannel.Members[1].FirmwareStatus.State)
	assert.Equal(t, int32(2), minerChannel.FirmwareProgress.TargetedCount)
	assert.Equal(t, int32(1), minerChannel.FirmwareProgress.CompleteCount)
	assert.Equal(t, int32(1), minerChannel.FirmwareProgress.QueuedCount)
}

func validCreateParams(deviceIdentifiers []string) models.CreateMinerChannelParams {
	return models.CreateMinerChannelParams{
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

func minerChannelWithMembers(id int64, members []models.MinerChannelMember) *models.MinerChannel {
	return &models.MinerChannel{
		ID:      id,
		OrgID:   7,
		Label:   "reservation",
		State:   models.MinerChannelStateActive,
		Members: members,
	}
}

func minerChannelMember(deviceIdentifier, manufacturer, model string) models.MinerChannelMember {
	return models.MinerChannelMember{
		OrgID:            7,
		DeviceIdentifier: deviceIdentifier,
		Display: models.MinerChannelDeviceDisplay{
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
	assert.Equal(t, "miner_channel", *event.ScopeType)
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
	assert.Equal(t, "miner_channel", *event.ScopeType)
	assert.Equal(t, "reservation", *event.ScopeLabel)
	require.NotNil(t, event.ScopeCount)
	assert.Equal(t, affectedCount, *event.ScopeCount)
	assert.Equal(t, updateKind, event.Metadata["update_kind"])
	assert.Equal(t, affectedCount, event.Metadata["affected_member_count"])
	assert.Equal(t, memberCountDelta, event.Metadata["member_count_delta"])
}
