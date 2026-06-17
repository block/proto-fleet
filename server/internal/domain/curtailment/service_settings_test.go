package curtailment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

func TestService_Settings(t *testing.T) {
	store := newFakeStore()
	store.orgConfigByOrg[1] = &models.OrgConfig{
		OrgID:                 1,
		MaxDurationDefaultSec: 14400,
		CandidateMinPowerW:    1500,
		PostEventCooldownSec:  600,
	}
	service := NewService(store)

	settings, err := service.GetSettings(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, int32(600), settings.PostEventCooldownSec)

	updated, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		OrgID:                1,
		PostEventCooldownSec: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), updated.PostEventCooldownSec)

	settings, err = service.GetSettings(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, int32(0), settings.PostEventCooldownSec)
}

func TestService_UpdateSettingsValidatesCooldown(t *testing.T) {
	store := newFakeStore()
	store.orgConfigByOrg[1] = &models.OrgConfig{OrgID: 1}
	service := NewService(store)

	_, err := service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		OrgID:                1,
		PostEventCooldownSec: -1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "post_event_cooldown_sec must be >= 0")

	_, err = service.UpdateSettings(context.Background(), UpdateSettingsRequest{
		OrgID:                1,
		PostEventCooldownSec: MaxPostEventCooldownSec + 1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "post_event_cooldown_sec must be <=")
}
