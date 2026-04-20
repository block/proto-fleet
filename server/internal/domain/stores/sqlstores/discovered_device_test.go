package sqlstores_test

import (
	"fmt"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLDiscoveredDeviceStore_Save_ShouldInsertNewDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org",
		Name:                "Test Org",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = $1", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	deviceIdentifier := "test-device-123"
	doi := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	}

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "S19 Pro",
			Manufacturer:     "Bitmain",
			DriverName:       "ANTMINER",
			IpAddress:        "192.168.1.100",
			Port:             "4028",
			UrlScheme:        "http",
		},
		OrgID: orgID,
	}

	// Act
	saved, err := store.Save(ctx, doi, device)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, deviceIdentifier, saved.DeviceIdentifier)
	assert.Equal(t, "S19 Pro", saved.Model)
	assert.Equal(t, "Bitmain", saved.Manufacturer)
	assert.Equal(t, "ANTMINER", saved.DriverName)
	assert.Equal(t, "192.168.1.100", saved.IpAddress)
	assert.Equal(t, "4028", saved.Port)
	assert.Equal(t, "http", saved.UrlScheme)
	assert.Equal(t, orgID, saved.OrgID)
	assert.False(t, saved.IsActive, "IsActive should default to false")
	assert.False(t, saved.FirstDiscovered.IsZero())
	assert.False(t, saved.LastSeen.IsZero())
}

func TestSQLDiscoveredDeviceStore_Save_ShouldUpdateExistingDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org",
		Name:                "Test Org",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = $1", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	deviceIdentifier := "test-device-123"
	doi := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	}

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "S19 Pro",
			Manufacturer:     "Bitmain",
			DriverName:       "ANTMINER",
			IpAddress:        "192.168.1.100",
			Port:             "4028",
			UrlScheme:        "http",
		},
		OrgID: orgID,
	}
	_, err = store.Save(ctx, doi, device)
	require.NoError(t, err)

	device.IpAddress = "192.168.1.101"

	// Act
	updated, err := store.Save(ctx, doi, device)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, deviceIdentifier, updated.DeviceIdentifier)
	assert.Equal(t, "192.168.1.101", updated.IpAddress)
	assert.Equal(t, orgID, updated.OrgID)
}

func TestSQLDiscoveredDeviceStore_Save_ShouldRefreshModelAndManufacturerOnRediscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org-refresh-model",
		Name:                "Test Org Refresh Model",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = $1", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	deviceIdentifier := "test-device-refresh"
	doi := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	}

	_, err = store.Save(ctx, doi, &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "Rig",
			Manufacturer:     "Proto",
			DriverName:       "proto",
			IpAddress:        "192.168.1.100",
			Port:             "443",
			UrlScheme:        "https",
		},
		OrgID: orgID,
	})
	require.NoError(t, err)

	updated, err := store.Save(ctx, doi, &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "Proto Rig",
			Manufacturer:     "Proto Labs",
			DriverName:       "proto",
			IpAddress:        "192.168.1.101",
			Port:             "443",
			UrlScheme:        "https",
		},
		OrgID: orgID,
	})
	require.NoError(t, err)

	assert.Equal(t, "Proto Rig", updated.Model)
	assert.Equal(t, "Proto Labs", updated.Manufacturer)
	assert.Equal(t, "192.168.1.101", updated.IpAddress)
}

func TestSQLDiscoveredDeviceStore_Save_ShouldClearFirmwareVersionWhenRediscoveryOmitsIt(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org-clear-firmware",
		Name:                "Test Org Clear Firmware",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = $1", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	deviceIdentifier := "test-device-clear-firmware"
	doi := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	}

	_, err = store.Save(ctx, doi, &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "Proto Rig",
			Manufacturer:     "Proto",
			FirmwareVersion:  "1.2.3",
			DriverName:       "proto",
			IpAddress:        "192.168.1.100",
			Port:             "443",
			UrlScheme:        "https",
		},
		OrgID: orgID,
	})
	require.NoError(t, err)

	updated, err := store.Save(ctx, doi, &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "Proto Rig",
			Manufacturer:     "Proto",
			DriverName:       "proto",
			IpAddress:        "192.168.1.101",
			Port:             "443",
			UrlScheme:        "https",
		},
		OrgID: orgID,
	})
	require.NoError(t, err)

	assert.Empty(t, updated.FirmwareVersion)
	assert.Equal(t, "192.168.1.101", updated.IpAddress)
}

func TestSQLDiscoveredDeviceStore_GetDevice_ShouldReturnExistingDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org-2",
		Name:                "Test Org 2",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = $1", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	deviceIdentifier := "test-device-456"
	doi := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	}

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "S21",
			Manufacturer:     "Bitmain",
			DriverName:       "ANTMINER",
			IpAddress:        "192.168.1.200",
			Port:             "4028",
			UrlScheme:        "http",
		},
		OrgID: orgID,
	}

	saved, err := store.Save(ctx, doi, device)
	require.NoError(t, err)

	// Act
	retrieved, err := store.GetDevice(ctx, doi)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, saved.DeviceIdentifier, retrieved.DeviceIdentifier)
	assert.Equal(t, saved.Model, retrieved.Model)
	assert.Equal(t, saved.Manufacturer, retrieved.Manufacturer)
	assert.Equal(t, saved.DriverName, retrieved.DriverName)
	assert.Equal(t, saved.IpAddress, retrieved.IpAddress)
	assert.Equal(t, saved.Port, retrieved.Port)
	assert.Equal(t, saved.UrlScheme, retrieved.UrlScheme)
	assert.Equal(t, saved.OrgID, retrieved.OrgID)
}

func TestSQLDiscoveredDeviceStore_Save_ShouldAllowSettingIsActiveToTrue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org-active",
		Name:                "Test Org Active",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = $1", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	deviceIdentifier := "test-device-active"
	doi := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	}

	device := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: deviceIdentifier,
			Model:            "S19 Pro",
			Manufacturer:     "Bitmain",
			DriverName:       "ANTMINER",
			IpAddress:        "192.168.1.100",
			Port:             "4028",
			UrlScheme:        "http",
		},
		IsActive: true, // Explicitly set to true
		OrgID:    orgID,
	}

	// Act
	saved, err := store.Save(ctx, doi, device)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.True(t, saved.IsActive, "IsActive should be true when explicitly set")
}

func TestSQLDiscoveredDeviceStore_GetDevice_ShouldReturnErrorForNonExistentDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org-3",
		Name:                "Test Org 3",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	nonExistentDOI := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: "non-existent",
		OrgID:            orgID,
	}

	// Act
	_, err = store.GetDevice(ctx, nonExistentDOI)

	// Assert
	require.Error(t, err)
	assert.Equal(t, minerdiscovery.MinerNotFoundFleetError, err)
}

func TestSQLDiscoveredDeviceStore_GetActiveUnpairedDevices_ShouldReturnUnpairedDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org-unpaired",
		Name:                "Test Org Unpaired",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = $1", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	// Create 3 active discovered devices (all unpaired)
	for i := 1; i <= 3; i++ {
		deviceIdentifier := fmt.Sprintf("device-%d", i)
		doi := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            orgID,
		}

		device := &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: deviceIdentifier,
				Model:            "S19 Pro",
				Manufacturer:     "Bitmain",
				DriverName:       "ANTMINER",
				IpAddress:        fmt.Sprintf("192.168.1.%d", 100+i),
				Port:             "4028",
				UrlScheme:        "http",
			},
			IsActive: true,
			OrgID:    orgID,
		}
		_, err = store.Save(ctx, doi, device)
		require.NoError(t, err)
	}

	// Act
	devices, nextCursor, err := store.GetActiveUnpairedDevices(ctx, orgID, "", 10)

	// Assert
	require.NoError(t, err)
	assert.Len(t, devices, 3, "Should return all 3 unpaired devices")
	assert.Empty(t, nextCursor, "Should not have next cursor since all devices fit in one page")
	for _, device := range devices {
		assert.True(t, device.IsActive)
	}
}

func TestSQLDiscoveredDeviceStore_GetActiveUnpairedDevices_ShouldSupportPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org-pagination",
		Name:                "Test Org Pagination",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = $1", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	// Create 5 unpaired devices
	for i := 1; i <= 5; i++ {
		deviceIdentifier := fmt.Sprintf("page-device-%d", i)
		doi := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            orgID,
		}

		device := &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: deviceIdentifier,
				Model:            "S19 Pro",
				Manufacturer:     "Bitmain",
				DriverName:       "ANTMINER",
				IpAddress:        fmt.Sprintf("192.168.1.%d", 100+i),
				Port:             "4028",
				UrlScheme:        "http",
			},
			IsActive: true,
			OrgID:    orgID,
		}
		_, err = store.Save(ctx, doi, device)
		require.NoError(t, err)
	}

	// Act - Get first page
	firstPage, nextCursor, err := store.GetActiveUnpairedDevices(ctx, orgID, "", 2)
	require.NoError(t, err)
	assert.Len(t, firstPage, 2)
	assert.NotEmpty(t, nextCursor, "Should have next cursor since there are more pages")

	// Act - Get second page using cursor
	secondPage, nextCursor2, err := store.GetActiveUnpairedDevices(ctx, orgID, nextCursor, 2)
	require.NoError(t, err)
	assert.Len(t, secondPage, 2)
	assert.NotEmpty(t, nextCursor2, "Should have next cursor since there are more pages")

	// Act - Get third page
	thirdPage, nextCursor3, err := store.GetActiveUnpairedDevices(ctx, orgID, nextCursor2, 2)
	require.NoError(t, err)
	assert.Len(t, thirdPage, 1, "Last page should have 1 device")
	assert.Empty(t, nextCursor3, "Should not have next cursor on last page")

	// Assert - Pages should have different devices
	assert.NotEqual(t, firstPage[0].DeviceIdentifier, secondPage[0].DeviceIdentifier)
	assert.NotEqual(t, secondPage[0].DeviceIdentifier, thirdPage[0].DeviceIdentifier)
}

func TestSQLDiscoveredDeviceStore_CountActiveUnpairedDevices_ShouldReturnCorrectCount(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "test-org-count",
		Name:                "Test Org Count",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM device WHERE org_id = $1", orgID)
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = $1", orgID)
		_ = queries.DeleteOrganization(ctx, orgID)
	})

	// Create 3 active unpaired devices
	for i := 1; i <= 3; i++ {
		deviceIdentifier := fmt.Sprintf("count-device-%d", i)
		doi := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            orgID,
		}

		device := &discoverymodels.DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: deviceIdentifier,
				Model:            "S19 Pro",
				Manufacturer:     "Bitmain",
				DriverName:       "ANTMINER",
				IpAddress:        fmt.Sprintf("192.168.1.%d", 100+i),
				Port:             "4028",
				UrlScheme:        "http",
			},
			IsActive: true,
			OrgID:    orgID,
		}
		_, err = store.Save(ctx, doi, device)
		require.NoError(t, err)
	}

	// Act
	count, err := store.CountActiveUnpairedDevices(ctx, orgID)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}
