package interfaces

import (
	"context"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=discovered_device.go -destination=mocks/mock_discovered_device_store.go -package=mocks DiscoveredDeviceStore

// DiscoveredDeviceStore defines the interface for discovered device operations in the store layer
//
//nolint:interfacebloat // Discovery reads, writes, and pairing guards share one transactional store boundary.
type DiscoveredDeviceStore interface {
	// LockPairingSnapshot requires a transaction. It locks the saved inventory
	// and rejects pairing writes if its group, routes, or owner changed after verification.
	LockPairingSnapshot(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier, expected *pairingpb.Device, expectedFleetNodeID *int64) error

	// Save stores device metadata. Existing endpoint groups retain their current addresses.
	Save(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier, device *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error)

	// SaveDirect rejects updates to FleetNode-owned records during local discovery.
	SaveDirect(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier, device *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error)

	// ActivateDirect marks a server-managed discovery active without rewriting its addresses.
	ActivateDirect(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) error

	// GetDevice retrieves a discovered device by its organization and device identifier
	GetDevice(ctx context.Context, doi discoverymodels.DeviceOrgIdentifier) (*discoverymodels.DiscoveredDevice, error)

	// GetByIPAndPort retrieves a discovered device by its IP address and port for a given organization
	GetByIPAndPort(ctx context.Context, orgID int64, ipAddress string, port string) (*discoverymodels.DiscoveredDevice, error)

	// GetDirectContainerByIP matches a server-managed container independently of its ports.
	// Ambiguous matches fail; FleetNode-owned inventory is excluded.
	GetDirectContainerByIP(ctx context.Context, orgID int64, ipAddress, driverName, model, manufacturer string) (*discoverymodels.DiscoveredDevice, error)

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
