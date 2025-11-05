package interfaces

import (
	"context"

	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
)

//go:generate mockgen -source=discovered_device.go -destination=mocks/mock_discovered_device_store.go -package=mocks DiscoveredDeviceStore

// DiscoveredDeviceStore defines the interface for discovered device operations in the store layer
type DiscoveredDeviceStore interface {
	// Save stores or updates a discovered device and returns the saved device
	Save(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier, device *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error)

	// GetDevice retrieves a discovered device by its organization and device identifier
	GetDevice(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) (*discoverymodels.DiscoveredDevice, error)

	// GetDatabaseID retrieves the database ID (primary key) for a discovered device
	GetDatabaseID(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) (int64, error)
}
