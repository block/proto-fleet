package interfaces

import (
	"context"

	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=discovered_device.go -destination=mocks/mock_discovered_device_store.go -package=mocks DiscoveredDeviceStore

// DiscoveredDeviceStore defines the interface for discovered device operations in the store layer
type DiscoveredDeviceStore interface {
	// Save stores or updates a discovered device and returns the saved device
	Save(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier, device *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error)

	// GetDevice retrieves a discovered device by its organization and device identifier
	GetDevice(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) (*discoverymodels.DiscoveredDevice, error)

	// GetByIPAndPort retrieves a discovered device by its IP address and port for a given organization
	GetByIPAndPort(ctx context.Context, orgID int64, ipAddress string, port string) (*discoverymodels.DiscoveredDevice, error)

	// GetDatabaseID retrieves the database ID (primary key) for a discovered device
	GetDatabaseID(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) (int64, error)

	// GetActiveUnpairedDevices retrieves active discovered devices that haven't been paired yet
	// cursor is an opaque pagination token (empty string for first page)
	// Returns devices, nextCursor (empty if no more pages), and error
	GetActiveUnpairedDevices(ctx context.Context, orgID int64, cursor string, limit int32) ([]*discoverymodels.DiscoveredDevice, string, error)

	// CountActiveUnpairedDevices returns the total count of active unpaired devices for an organization
	CountActiveUnpairedDevices(ctx context.Context, orgID int64) (int64, error)

	// SoftDelete soft-deletes a discovered device record
	SoftDelete(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) error
}
