package interfaces

import (
	"context"

	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"

	"github.com/btc-mining/proto-fleet/server/generated/sqlc"

	fm "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	tm "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
	diagnosticsmodels "github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics/models"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

//go:generate mockgen -source=device.go -destination=mocks/mock_device_store.go -package=mocks DeviceStore

type MinerFilter struct {
	PairingStatuses     []fm.PairingStatus // Changed from single value to slice
	DeviceStatusFilter  []mm.MinerStatus
	MinerType           []mm.Type
	ErrorComponentTypes []diagnosticsmodels.ComponentType // Filter devices by component types that have errors
}

// DeviceStatusUpdate represents a status update for batch operations.
type DeviceStatusUpdate struct {
	DeviceIdentifier models.DeviceIdentifier
	Status           mm.MinerStatus
}

// OfflineDeviceInfo contains information about an offline device needed for IP scanning
type OfflineDeviceInfo struct {
	DeviceID                   int64
	DeviceIdentifier           string
	MacAddress                 string
	DeviceType                 string
	LastKnownIP                string
	LastKnownPort              string
	LastKnownURLScheme         string
	OrgID                      int64
	DiscoveredDeviceIdentifier string
}

//nolint:interfacebloat // DeviceStore defines the interface for device-related operations in the store layer. We are okay with bloat at this time.
type DeviceStore interface {
	InsertDevice(ctx context.Context, device *pb.Device, orgID int64, discoveredDeviceIdentifier string) error
	UpsertMinerCredentials(ctx context.Context, device *pb.Device, orgID int64, usernameEnc string, passwordEnc *secrets.Text) error
	UpsertDevicePairing(ctx context.Context, device *pb.Device, orgID int64, pairingStatus string) error
	UpdateDevicePairingStatusByIdentifier(ctx context.Context, deviceIdentifier string, pairingStatus string) error
	GetMinerCredentials(ctx context.Context, device *pb.Device, orgID int64) (*pb.Credentials, error)
	GetDeviceByDeviceIdentifier(ctx context.Context, identifier string, orgID int64) (*pb.Device, error)
	UpdateDeviceInfo(ctx context.Context, device *pb.Device, orgID int64) error
	GetDeviceWithIPAssignment(ctx context.Context, deviceIdentifier string, orgID int64) (*discoverymodels.DiscoveredDevice, error)
	GetTotalPairedDevices(ctx context.Context, orgID int64, filter *MinerFilter) (int64, error)
	GetTotalDevicesPendingAuth(ctx context.Context, orgID int64) (int64, error)
	GetAllPairedDeviceIdentifiers(ctx context.Context) ([]models.DeviceIdentifier, error)
	GetMinerStateCounts(ctx context.Context, orgID int64, filter *MinerFilter) (*tm.MinerStateCounts, error)
	GetAvailableMinerTypes(ctx context.Context, orgID int64) ([]mm.Type, error)
	UpsertDeviceStatus(ctx context.Context, deviceIdentifier models.DeviceIdentifier, status mm.MinerStatus, details string) error
	UpsertDeviceStatuses(ctx context.Context, updates []DeviceStatusUpdate) error
	GetDeviceStatusForDeviceIdentifiers(ctx context.Context, deviceIdentifiers []models.DeviceIdentifier) (map[models.DeviceIdentifier]mm.MinerStatus, error)
	GetOfflineDevices(ctx context.Context, limit int) ([]OfflineDeviceInfo, error)
	ListMinerStateSnapshots(ctx context.Context, orgID int64, cursor string, pageSize int32, filter *MinerFilter, sortConfig *SortConfig) ([]sqlc.ListMinerStateSnapshotsRow, string, int64, error)
	AllDevicesBelongToOrg(ctx context.Context, deviceIdentifiers []string, orgID int64) (bool, error)
}
