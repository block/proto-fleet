package cohort

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
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

	_, err := svc.CreateCohort(t.Context(), validCreateParams([]string{"miner-1", "   "}))
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
		DeviceIdentifiers:     []string{"miner-1", "miner-2"},
	})
	require.NoError(t, err)
	assert.Equal(t, expected, got)
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
