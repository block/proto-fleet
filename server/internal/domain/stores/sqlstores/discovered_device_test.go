package sqlstores_test

import (
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
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
	orgResult, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name: "Test Org",
	})
	require.NoError(t, err)
	orgID, err := orgResult.LastInsertId()
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = ?", orgID)
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
			Type:             "ANTMINER",
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
	assert.Equal(t, "ANTMINER", saved.Type)
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
	orgResult, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name: "Test Org",
	})
	require.NoError(t, err)
	orgID, err := orgResult.LastInsertId()
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = ?", orgID)
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
			Type:             "ANTMINER",
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

func TestSQLDiscoveredDeviceStore_GetDevice_ShouldReturnExistingDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLDiscoveredDeviceStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgResult, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name: "Test Org 2",
	})
	require.NoError(t, err)
	orgID, err := orgResult.LastInsertId()
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = ?", orgID)
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
			Type:             "ANTMINER",
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
	assert.Equal(t, saved.Type, retrieved.Type)
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
	orgResult, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name: "Test Org Active",
	})
	require.NoError(t, err)
	orgID, err := orgResult.LastInsertId()
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM discovered_device WHERE org_id = ?", orgID)
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
			Type:             "ANTMINER",
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
	orgResult, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name: "Test Org 3",
	})
	require.NoError(t, err)
	orgID, err := orgResult.LastInsertId()
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
