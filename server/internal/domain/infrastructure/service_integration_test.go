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
	secondSiteID   = int64(11)
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
		fmt.Sprintf(`INSERT INTO site (id, org_id, name, slug) VALUES (%d, %d, 'Austin', 'austin')`, secondSiteID, testOrgID),
		fmt.Sprintf(`INSERT INTO site (id, org_id, name, slug) VALUES (%d, %d, 'Miami', 'miami')`, otherOrgSiteID, otherOrgID),
	}
	for _, stmt := range seed {
		_, err := conn.Exec(stmt)
		require.NoError(t, err)
	}
	store := sqlstores.NewSQLInfrastructureDeviceStore(conn)
	siteStore := sqlstores.NewSQLSiteStore(conn)
	transactor := sqlstores.NewSQLTransactor(conn)
	return infrastructure.NewService(store, siteStore, infrastructure.NewDefaultDriverRegistry(), transactor, nil), conn
}

func validModbusConfig() json.RawMessage {
	return json.RawMessage(`{"endpoint":"10.1.2.3","port":502,"unit_id":5,"register_address":2001,"write_mode":"holding_register"}`)
}

func boolPtr(b bool) *bool { return &b }

func stringPtr(s string) *string { return &s }

func createParams(mutate func(*models.CreateParams)) models.CreateParams {
	params := models.CreateParams{
		OrgID:        testOrgID,
		SiteID:       testSiteID,
		BuildingName: "Building 1",
		RackName:     "Rack A1",
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
	svc, conn := newTestService(t)
	ctx := t.Context()

	created, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)
	assert.Equal(t, "Zone A exhaust fans", created.Name)
	assert.Equal(t, "Denton", created.SiteLabel)
	assert.Equal(t, "Rack A1", created.RackName)
	assert.Equal(t, int32(12), created.FanCount)
	assert.True(t, created.Enabled)
	assert.JSONEq(t, string(validModbusConfig()), string(created.DriverConfig))

	got, err := svc.Get(ctx, testOrgID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)

	// Single-fan devices normalize fan_count to 1, and a padded
	// driver_type is trimmed before persisting so later registry
	// lookups resolve the stored key.
	single, err := svc.Create(ctx, createParams(func(p *models.CreateParams) {
		p.Name = "Row 3 intake fan"
		p.DeviceKind = models.KindSingleFan
		p.FanCount = 7
		p.DriverType = "  modbus_tcp  "
	}))
	require.NoError(t, err)
	assert.Equal(t, int32(1), single.FanCount)
	assert.Equal(t, "modbus_tcp", single.DriverType)

	// List returns both, ordered by name.
	devices, err := svc.List(ctx, models.ListFilter{OrgID: testOrgID})
	require.NoError(t, err)
	require.Len(t, devices, 2)
	assert.Equal(t, "Row 3 intake fan", devices[0].Name)

	// Site filter discriminates between real sites: a device on the
	// second site is included/excluded by the filter, not just by
	// nonexistent-site emptiness.
	austinDevice, err := svc.Create(ctx, createParams(func(p *models.CreateParams) {
		p.Name = "Austin roof exhaust"
		p.SiteID = secondSiteID
	}))
	require.NoError(t, err)
	dentonOnly, err := svc.List(ctx, models.ListFilter{OrgID: testOrgID, SiteIDs: []int64{testSiteID}})
	require.NoError(t, err)
	require.Len(t, dentonOnly, 2)
	austinOnly, err := svc.List(ctx, models.ListFilter{OrgID: testOrgID, SiteIDs: []int64{secondSiteID}})
	require.NoError(t, err)
	require.Len(t, austinOnly, 1)
	assert.Equal(t, austinDevice.ID, austinOnly[0].ID)
	bothSites, err := svc.List(ctx, models.ListFilter{OrgID: testOrgID, SiteIDs: []int64{testSiteID, secondSiteID}})
	require.NoError(t, err)
	assert.Len(t, bothSites, 3)
	filtered, err := svc.List(ctx, models.ListFilter{OrgID: testOrgID, SiteIDs: []int64{testSiteID + 999}})
	require.NoError(t, err)
	assert.Empty(t, filtered)

	// ExcludedSiteIDs removes sites regardless of SiteIDs — the
	// handler pushes the caller's narrowed-away sites through it.
	withoutDenton, err := svc.List(ctx, models.ListFilter{OrgID: testOrgID, ExcludedSiteIDs: []int64{testSiteID}})
	require.NoError(t, err)
	require.Len(t, withoutDenton, 1)
	assert.Equal(t, austinDevice.ID, withoutDenton[0].ID)
	excludedWins, err := svc.List(ctx, models.ListFilter{
		OrgID:           testOrgID,
		SiteIDs:         []int64{testSiteID, secondSiteID},
		ExcludedSiteIDs: []int64{secondSiteID},
	})
	require.NoError(t, err)
	require.Len(t, excludedWins, 2)
	for _, d := range excludedWins {
		assert.Equal(t, testSiteID, d.SiteID)
	}

	// Update mutates fields; explicit enabled=false disables.
	updated, err := svc.Update(ctx, models.UpdateParams{
		OrgID:          testOrgID,
		ID:             created.ID,
		ExpectedSiteID: testSiteID,
		SiteID:         testSiteID,
		BuildingName:   "Building 2",
		RackName:       stringPtr("Rack B1"),
		Name:           "Zone B exhaust fans",
		DeviceKind:     models.KindFanGroup,
		FanCount:       16,
		Enabled:        boolPtr(false),
		DriverType:     "modbus_tcp",
		DriverConfig:   validModbusConfig(),
	})
	require.NoError(t, err)
	assert.Equal(t, "Zone B exhaust fans", updated.Name)
	assert.Equal(t, "Rack B1", updated.RackName)
	assert.Equal(t, int32(16), updated.FanCount)
	assert.False(t, updated.Enabled)

	// Nil Enabled and RackName preserve their current values in the
	// UPDATE itself (COALESCE against the stored columns), so an older
	// client performing an unrelated rename cannot clear either field.
	preserved, err := svc.Update(ctx, models.UpdateParams{
		OrgID:          testOrgID,
		ID:             created.ID,
		ExpectedSiteID: testSiteID,
		SiteID:         testSiteID,
		BuildingName:   "Building 2",
		RackName:       nil,
		Name:           "Zone B exhaust fans (renamed)",
		DeviceKind:     models.KindFanGroup,
		FanCount:       16,
		Enabled:        nil,
		DriverType:     "modbus_tcp",
		DriverConfig:   validModbusConfig(),
	})
	require.NoError(t, err)
	assert.Equal(t, "Zone B exhaust fans (renamed)", preserved.Name)
	assert.False(t, preserved.Enabled, "nil Enabled must preserve the stored value, not reset it")
	assert.Equal(t, "Rack B1", preserved.RackName, "nil RackName must preserve the stored value, not clear it")

	// A handler-authorized update is predicated on the rack value observed
	// before entering the service. If placement changes concurrently, the
	// stale request fails closed instead of clearing or replacing the new rack.
	_, err = conn.ExecContext(ctx, `UPDATE infrastructure_device SET rack_name = 'Rack C1' WHERE id = $1`, created.ID)
	require.NoError(t, err)
	expectedRackName := "Rack B1"
	emptyRackName := ""
	_, err = svc.Update(ctx, models.UpdateParams{
		OrgID:            testOrgID,
		ID:               created.ID,
		ExpectedSiteID:   testSiteID,
		ExpectedRackName: &expectedRackName,
		SiteID:           testSiteID,
		BuildingName:     "Building 2",
		RackName:         &emptyRackName,
		Name:             "Zone B exhaust fans (renamed)",
		DeviceKind:       models.KindFanGroup,
		FanCount:         16,
		Enabled:          nil,
		DriverType:       "modbus_tcp",
		DriverConfig:     validModbusConfig(),
	})
	assert.True(t, fleeterror.IsNotFoundError(err))
	concurrentlyPlaced, err := svc.Get(ctx, testOrgID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Rack C1", concurrentlyPlaced.RackName)

	// Delete soft-deletes; the row disappears from Get and List.
	require.NoError(t, svc.Delete(ctx, testOrgID, created.ID, testSiteID))
	_, err = svc.Get(ctx, testOrgID, created.ID)
	assert.True(t, fleeterror.IsNotFoundError(err))
	devices, err = svc.List(ctx, models.ListFilter{OrgID: testOrgID})
	require.NoError(t, err)
	assert.Len(t, devices, 2)
	// Deleting again reports NotFound.
	err = svc.Delete(ctx, testOrgID, created.ID, testSiteID)
	assert.True(t, fleeterror.IsNotFoundError(err))

	// A soft-deleted device's name is reusable in the same site — the
	// unique index is partial on deleted_at IS NULL.
	reused, err := svc.Create(ctx, createParams(func(p *models.CreateParams) {
		p.Name = "Zone B exhaust fans (renamed)" // the deleted device's final name
	}))
	require.NoError(t, err)
	assert.Equal(t, "Zone B exhaust fans (renamed)", reused.Name)
}

func TestRackAssignmentFollowsCatalogMutations_DatabaseIntegration(t *testing.T) {
	svc, conn := newTestService(t)
	ctx := t.Context()

	_, err := conn.ExecContext(ctx, `
		INSERT INTO building (id, org_id, site_id, name)
		VALUES (100, $1, $2, 'Building 1'), (101, $1, $2, 'Building 2')
	`, testOrgID, testSiteID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO device_set (id, org_id, type, label)
		VALUES (1000, $1, 'rack', 'Rack A1')
	`, testOrgID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO device_set_rack (device_set_id, org_id, site_id, building_id, rows, columns)
		VALUES (1000, $1, $2, 100, 4, 4)
	`, testOrgID, testSiteID)
	require.NoError(t, err)

	device, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)

	_, err = conn.ExecContext(ctx, `UPDATE device_set SET label = 'Rack A2' WHERE id = 1000`)
	require.NoError(t, err)
	device, err = svc.Get(ctx, testOrgID, device.ID)
	require.NoError(t, err)
	assert.Equal(t, "Rack A2", device.RackName, "rack rename must update assigned infrastructure devices")

	_, err = conn.ExecContext(ctx, `UPDATE device_set_rack SET building_id = 101 WHERE device_set_id = 1000`)
	require.NoError(t, err)
	device, err = svc.Get(ctx, testOrgID, device.ID)
	require.NoError(t, err)
	assert.Empty(t, device.RackName, "rack move must clear assignments at the old location")

	device, err = svc.Create(ctx, createParams(func(params *models.CreateParams) {
		params.BuildingName = "Building 2"
		params.RackName = "Rack A2"
		params.Name = "Building 2 exhaust fans"
	}))
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE device_set SET deleted_at = CURRENT_TIMESTAMP WHERE id = 1000`)
	require.NoError(t, err)
	device, err = svc.Get(ctx, testOrgID, device.ID)
	require.NoError(t, err)
	assert.Empty(t, device.RackName, "rack delete must clear assigned infrastructure devices")
}

func TestService_DeleteRejectsResponseProfileReference_DatabaseIntegration(t *testing.T) {
	svc, conn := newTestService(t)
	ctx := t.Context()

	device, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)
	_, err = conn.ExecContext(
		ctx,
		`INSERT INTO curtailment_response_profile (
			org_id,
			profile_name,
			site_id,
			mode,
			scope_json,
			facility_fan_device_ids
		) VALUES (
			$1,
			'Fan-coordinated shed',
			$2,
			'FULL_FLEET',
			jsonb_build_object('site_ids', jsonb_build_array($2::bigint)),
			ARRAY[$3::bigint]
		)`,
		testOrgID,
		testSiteID,
		device.ID,
	)
	require.NoError(t, err)

	err = svc.Delete(ctx, testOrgID, device.ID, testSiteID)

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "update those profiles first")
	_, err = svc.Get(ctx, testOrgID, device.ID)
	require.NoError(t, err, "guarded device must remain live")
}

func TestService_UpdateRejectsMovingReferencedDevice_DatabaseIntegration(t *testing.T) {
	svc, conn := newTestService(t)
	ctx := t.Context()

	device, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)
	_, err = conn.ExecContext(
		ctx,
		`INSERT INTO curtailment_response_profile (
			org_id, profile_name, mode, scope_json, facility_fan_device_ids
		) VALUES ($1, 'Whole-fleet fans', 'FULL_FLEET', '{"whole_org":true}', ARRAY[$2::bigint])`,
		testOrgID,
		device.ID,
	)
	require.NoError(t, err)

	_, err = svc.Update(ctx, models.UpdateParams{
		OrgID:          testOrgID,
		ID:             device.ID,
		ExpectedSiteID: testSiteID,
		SiteID:         secondSiteID,
		BuildingName:   device.BuildingName,
		Name:           device.Name,
		DeviceKind:     device.DeviceKind,
		FanCount:       device.FanCount,
		Enabled:        boolPtr(device.Enabled),
		DriverType:     device.DriverType,
		DriverConfig:   device.DriverConfig,
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	stillAtSource, err := svc.Get(ctx, testOrgID, device.ID)
	require.NoError(t, err)
	assert.Equal(t, testSiteID, stillAtSource.SiteID)
}

func TestService_UpdateRenameCollision_DatabaseIntegration(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := t.Context()

	first, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)
	second, err := svc.Create(ctx, createParams(func(p *models.CreateParams) {
		p.Name = "Row 3 intake fan"
		p.DeviceKind = models.KindSingleFan
		p.FanCount = 1
	}))
	require.NoError(t, err)

	// Renaming the second device to the first's name trips the partial
	// unique index and maps to AlreadyExists.
	_, err = svc.Update(ctx, models.UpdateParams{
		OrgID:          testOrgID,
		ID:             second.ID,
		ExpectedSiteID: testSiteID,
		SiteID:         testSiteID,
		BuildingName:   second.BuildingName,
		Name:           first.Name,
		DeviceKind:     second.DeviceKind,
		FanCount:       second.FanCount,
		Enabled:        &second.Enabled,
		DriverType:     second.DriverType,
		DriverConfig:   second.DriverConfig,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestService_StaleSiteAuthorizationFailsClosed_DatabaseIntegration(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := t.Context()

	created, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)

	// Simulate a concurrent move: the device is now at secondSiteID,
	// but a caller authorized against testSiteID (the site it was read
	// at) attempts an update/delete. Both must fail closed (NotFound),
	// not mutate the device now living in a site the caller may not
	// manage.
	moved, err := svc.Update(ctx, models.UpdateParams{
		OrgID: testOrgID, ID: created.ID, ExpectedSiteID: testSiteID, SiteID: secondSiteID,
		BuildingName: created.BuildingName, Name: created.Name, DeviceKind: created.DeviceKind,
		FanCount: created.FanCount, Enabled: &created.Enabled, DriverType: created.DriverType,
		DriverConfig: created.DriverConfig,
	})
	require.NoError(t, err)
	require.Equal(t, secondSiteID, moved.SiteID)

	// Update predicated on the stale site is rejected.
	_, err = svc.Update(ctx, models.UpdateParams{
		OrgID: testOrgID, ID: created.ID, ExpectedSiteID: testSiteID, SiteID: testSiteID,
		BuildingName: created.BuildingName, Name: "renamed", DeviceKind: created.DeviceKind,
		FanCount: created.FanCount, Enabled: &created.Enabled, DriverType: created.DriverType,
		DriverConfig: created.DriverConfig,
	})
	assert.True(t, fleeterror.IsNotFoundError(err), "update against stale site must fail closed")

	// Delete predicated on the stale site is rejected; the device
	// survives at its new site.
	err = svc.Delete(ctx, testOrgID, created.ID, testSiteID)
	assert.True(t, fleeterror.IsNotFoundError(err), "delete against stale site must fail closed")
	still, err := svc.Get(ctx, testOrgID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, secondSiteID, still.SiteID)

	// Delete predicated on the correct (current) site succeeds.
	require.NoError(t, svc.Delete(ctx, testOrgID, created.ID, secondSiteID))
}

func TestService_MoveOutOfDeletedSiteFailsClosed_DatabaseIntegration(t *testing.T) {
	svc, conn := newTestService(t)
	ctx := t.Context()

	created, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)

	// Simulate the DeleteSite race window: the source site row is
	// soft-deleted but the device row is still live (as it would be
	// between DeleteSite's site lock and its device cascade). Update
	// locks the source site too, so LockSiteForWrite must surface
	// NotFound and the move out of the dying site must fail closed
	// instead of slipping the device past the cascade.
	_, err = conn.ExecContext(ctx,
		`UPDATE site SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, testSiteID)
	require.NoError(t, err)

	_, err = svc.Update(ctx, models.UpdateParams{
		OrgID: testOrgID, ID: created.ID, ExpectedSiteID: testSiteID, SiteID: secondSiteID,
		BuildingName: created.BuildingName, Name: created.Name, DeviceKind: created.DeviceKind,
		FanCount: created.FanCount, Enabled: &created.Enabled, DriverType: created.DriverType,
		DriverConfig: created.DriverConfig,
	})
	assert.True(t, fleeterror.IsNotFoundError(err), "move out of a deleted site must fail closed")
}

func TestService_SiteCascade_DatabaseIntegration(t *testing.T) {
	svc, conn := newTestService(t)
	ctx := t.Context()

	created, err := svc.Create(ctx, createParams(nil))
	require.NoError(t, err)

	// Soft-deleting the parent site cascades to its infrastructure
	// devices (mirrors SoftDeleteBuildingsBySite in DeleteSite).
	siteStore := sqlstores.NewSQLSiteStore(conn)
	affected, err := siteStore.SoftDeleteInfrastructureDevicesBySite(ctx, testOrgID, testSiteID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)
	_, err = svc.Get(ctx, testOrgID, created.ID)
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
		}, "must be a private"},
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
		OrgID: otherOrgID, ID: created.ID, ExpectedSiteID: otherOrgSiteID, SiteID: otherOrgSiteID,
		Name: "hijack", DeviceKind: models.KindSingleFan, FanCount: 1,
		DriverType: "modbus_tcp", DriverConfig: validModbusConfig(),
	})
	assert.True(t, fleeterror.IsNotFoundError(err))

	err = svc.Delete(ctx, otherOrgID, created.ID, otherOrgSiteID)
	assert.True(t, fleeterror.IsNotFoundError(err))

	devices, err := svc.List(ctx, models.ListFilter{OrgID: otherOrgID})
	require.NoError(t, err)
	assert.Empty(t, devices)
}
