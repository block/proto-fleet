package infrastructure_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

const (
	testOrgID      = int64(1)
	otherOrgID     = int64(2)
	testSiteID     = int64(10)
	otherOrgSiteID = int64(20)
)

func newTestService(t *testing.T) (*infrastructure.Service, *sql.DB) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	conn := testutil.GetTestDB(t)
	seed := []string{
		fmt.Sprintf(`INSERT INTO organization (id, org_id, name) VALUES (%d, 'test-org-1', 'Test Org 1')`, testOrgID),
		fmt.Sprintf(`INSERT INTO organization (id, org_id, name) VALUES (%d, 'test-org-2', 'Test Org 2')`, otherOrgID),
		fmt.Sprintf(`INSERT INTO site (id, org_id, name, slug) VALUES (%d, %d, 'Denton', 'denton')`, testSiteID, testOrgID),
		fmt.Sprintf(`INSERT INTO site (id, org_id, name, slug) VALUES (%d, %d, 'Miami', 'miami')`, otherOrgSiteID, otherOrgID),
	}
	for _, stmt := range seed {
		_, err := conn.Exec(stmt)
		require.NoError(t, err)
	}
	store := sqlstores.NewSQLInfrastructureDeviceStore(conn)
	siteStore := sqlstores.NewSQLSiteStore(conn)
	return infrastructure.NewService(store, siteStore, infrastructure.NewDefaultDriverRegistry()), conn
}

func validModbusConfig() json.RawMessage {
	return json.RawMessage(`{"endpoint":"10.1.2.3","port":502,"unit_id":5,"register_address":2001,"write_mode":"holding_register"}`)
}

func createParams(mutate func(*models.CreateParams)) models.CreateParams {
	params := models.CreateParams{
		OrgID:        testOrgID,
		SiteID:       testSiteID,
		BuildingName: "Building 1",
		Name:         "Zone A exhaust fans",
		DeviceKind:   models.KindFanGroup,
		FanCount:     12,
		Enabled:      true,
		DriverType:   "modbus_tcp",
		DriverConfig: validModbusConfig(),
	}
	if mutate != nil {
		mutate(&params)
	}
	return params
}

func TestService_CreateGetListUpdateDelete_DatabaseIntegration(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := t.Context()

	created, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)
	assert.Equal(t, "Zone A exhaust fans", created.Name)
	assert.Equal(t, "Denton", created.SiteLabel)
	assert.Equal(t, int32(12), created.FanCount)
	assert.True(t, created.Enabled)
	assert.JSONEq(t, string(validModbusConfig()), string(created.DriverConfig))

	got, err := svc.Get(ctx, testOrgID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)

	// Single-fan devices normalize fan_count to 1.
	single, err := svc.Create(ctx, createParams(func(p *models.CreateParams) {
		p.Name = "Row 3 intake fan"
		p.DeviceKind = models.KindSingleFan
		p.FanCount = 7
	}))
	require.NoError(t, err)
	assert.Equal(t, int32(1), single.FanCount)

	// List returns both, ordered by name; site filter applies.
	devices, err := svc.List(ctx, models.ListFilter{OrgID: testOrgID})
	require.NoError(t, err)
	require.Len(t, devices, 2)
	assert.Equal(t, "Row 3 intake fan", devices[0].Name)
	filtered, err := svc.List(ctx, models.ListFilter{OrgID: testOrgID, SiteIDs: []int64{testSiteID + 999}})
	require.NoError(t, err)
	assert.Empty(t, filtered)

	// Update mutates fields.
	updated, err := svc.Update(ctx, models.UpdateParams{
		OrgID:        testOrgID,
		ID:           created.ID,
		SiteID:       testSiteID,
		BuildingName: "Building 2",
		Name:         "Zone B exhaust fans",
		DeviceKind:   models.KindFanGroup,
		FanCount:     16,
		Enabled:      false,
		DriverType:   "modbus_tcp",
		DriverConfig: validModbusConfig(),
	})
	require.NoError(t, err)
	assert.Equal(t, "Zone B exhaust fans", updated.Name)
	assert.Equal(t, int32(16), updated.FanCount)
	assert.False(t, updated.Enabled)

	// Delete soft-deletes; the row disappears from Get and List.
	require.NoError(t, svc.Delete(ctx, testOrgID, created.ID))
	_, err = svc.Get(ctx, testOrgID, created.ID)
	assert.True(t, fleeterror.IsNotFoundError(err))
	devices, err = svc.List(ctx, models.ListFilter{OrgID: testOrgID})
	require.NoError(t, err)
	assert.Len(t, devices, 1)
	// Deleting again reports NotFound.
	err = svc.Delete(ctx, testOrgID, created.ID)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestService_CreateValidation_DatabaseIntegration(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := t.Context()

	cases := []struct {
		name    string
		mutate  func(*models.CreateParams)
		errText string
	}{
		{"blank name", func(p *models.CreateParams) { p.Name = "  " }, "name is required"},
		{"bad kind", func(p *models.CreateParams) { p.DeviceKind = "pump" }, "device_kind"},
		{"fan group too small", func(p *models.CreateParams) { p.FanCount = 1 }, "at least 2"},
		{"unknown driver", func(p *models.CreateParams) { p.DriverType = "bacnet" }, "unknown infrastructure driver type"},
		{"public endpoint", func(p *models.CreateParams) {
			p.DriverConfig = json.RawMessage(`{"endpoint":"8.8.8.8","port":502,"unit_id":5,"register_address":2001,"write_mode":"coil"}`)
		}, "private, loopback, or link-local"},
		{"bad unit id", func(p *models.CreateParams) {
			p.DriverConfig = json.RawMessage(`{"endpoint":"10.1.2.3","port":502,"unit_id":300,"register_address":2001,"write_mode":"coil"}`)
		}, "unit_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create(ctx, createParams(tc.mutate))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errText)
		})
	}

	// Cross-org site is NotFound, not InvalidArgument.
	_, err := svc.Create(ctx, createParams(func(p *models.CreateParams) { p.SiteID = otherOrgSiteID }))
	assert.True(t, fleeterror.IsNotFoundError(err))

	// Duplicate name within a site is AlreadyExists.
	_, err = svc.Create(ctx, createParams(nil))
	require.NoError(t, err)
	_, err = svc.Create(ctx, createParams(nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestService_OrgIsolation_DatabaseIntegration(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := t.Context()

	created, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)

	// Another org cannot read, update, or delete the device.
	_, err = svc.Get(ctx, otherOrgID, created.ID)
	assert.True(t, fleeterror.IsNotFoundError(err))

	_, err = svc.Update(ctx, models.UpdateParams{
		OrgID: otherOrgID, ID: created.ID, SiteID: otherOrgSiteID,
		Name: "hijack", DeviceKind: models.KindSingleFan, FanCount: 1,
		DriverType: "modbus_tcp", DriverConfig: validModbusConfig(),
	})
	assert.True(t, fleeterror.IsNotFoundError(err))

	err = svc.Delete(ctx, otherOrgID, created.ID)
	assert.True(t, fleeterror.IsNotFoundError(err))

	devices, err := svc.List(ctx, models.ListFilter{OrgID: otherOrgID})
	require.NoError(t, err)
	assert.Empty(t, devices)
}
