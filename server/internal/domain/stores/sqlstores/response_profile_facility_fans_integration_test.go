package sqlstores_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_ResponseProfileFacilityFanSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	ctx := t.Context()
	const (
		orgID          = int64(1)
		otherOrgID     = int64(2)
		siteID         = int64(10)
		otherSiteID    = int64(11)
		otherOrgSiteID = int64(20)
		fanID          = int64(31)
		otherSiteFanID = int64(32)
		otherOrgFanID  = int64(33)
		fanOff         = int32(45)
		fanStart       = int32(90)
		updatedFanOff  = int32(60)
		updatedFanOn   = int32(120)
	)
	_, err := db.ExecContext(ctx, `
		INSERT INTO organization (id, org_id, name)
		VALUES
			($1, 'response-profile-fan-org', 'Response Profile Fan Org'),
			($2, 'other-response-profile-fan-org', 'Other Response Profile Fan Org')
	`, orgID, otherOrgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO site (id, org_id, name, slug)
		VALUES
			($1, $2, 'Fan Site', 'fan-site'),
			($3, $2, 'Other Fan Site', 'other-fan-site'),
			($4, $5, 'Other Org Fan Site', 'other-org-fan-site')
	`, siteID, orgID, otherSiteID, otherOrgSiteID, otherOrgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO infrastructure_device (
			id,
			org_id,
			site_id,
			building_name,
			name,
			device_kind,
			fan_count,
			enabled,
			driver_type,
			driver_config
		) VALUES
			($1, $2, $3, 'Building 1', 'Exhaust fans', 'fan_group', 12, FALSE, 'modbus_tcp', '{}'),
			($4, $2, $5, 'Building 2', 'Other-site fans', 'fan_group', 8, TRUE, 'modbus_tcp', '{}'),
			($6, $7, $8, 'Building 3', 'Other-org fans', 'fan_group', 6, TRUE, 'modbus_tcp', '{}')
	`, fanID, orgID, siteID, otherSiteFanID, otherSiteID, otherOrgFanID, otherOrgID, otherOrgSiteID)
	require.NoError(t, err)

	service := domainCurtailment.NewResponseProfileService(sqlstores.NewSQLCurtailmentStore(db))
	created, err := service.Create(ctx, domainCurtailment.SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                orgID,
			ProfileName:          "Fan-coordinated shed",
			SiteID:               pointerTo(siteID),
			Mode:                 models.ModeFullFleet,
			FacilityFanDeviceIDs: []int64{fanID},
			FanOffDelaySec:       fanOff,
			FanRestoreDelaySec:   fanStart,
		},
		CanUseAdminControls: true,
	})
	require.NoError(t, err)
	assert.Equal(t, []int64{fanID}, created.FacilityFanDeviceIDs)
	assert.Equal(t, fanOff, created.FanOffDelaySec)
	assert.Equal(t, fanStart, created.FanRestoreDelaySec)

	loaded, err := service.Get(ctx, orgID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []int64{fanID}, loaded.FacilityFanDeviceIDs)
	assert.Equal(t, fanOff, loaded.FanOffDelaySec)
	assert.Equal(t, fanStart, loaded.FanRestoreDelaySec)

	updatedInput := *created
	updatedInput.FanOffDelaySec = updatedFanOff
	updatedInput.FanRestoreDelaySec = updatedFanOn
	updated, err := service.Update(ctx, domainCurtailment.SaveResponseProfileRequest{
		Profile:             updatedInput,
		CanUseAdminControls: true,
		ExpectedSiteID:      created.SiteID,
		ExpectedScopeJSON:   created.ScopeJSON,
	})
	require.NoError(t, err)
	assert.Equal(t, updatedFanOff, updated.FanOffDelaySec)
	assert.Equal(t, updatedFanOn, updated.FanRestoreDelaySec)

	_, err = service.Create(ctx, domainCurtailment.SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                orgID,
			ProfileName:          "Wrong-site fan",
			SiteID:               pointerTo(siteID),
			Mode:                 models.ModeFullFleet,
			FacilityFanDeviceIDs: []int64{otherSiteFanID},
		},
		CanUseAdminControls: true,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = service.Create(ctx, domainCurtailment.SaveResponseProfileRequest{
		Profile: models.ResponseProfile{
			OrgID:                orgID,
			ProfileName:          "Cross-org fan",
			SiteID:               pointerTo(siteID),
			Mode:                 models.ModeFullFleet,
			FacilityFanDeviceIDs: []int64{otherOrgFanID},
		},
		CanUseAdminControls: true,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func pointerTo[T any](value T) *T {
	return &value
}
