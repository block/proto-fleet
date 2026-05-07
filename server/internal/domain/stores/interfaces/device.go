package interfaces

import (
	"context"
	"net/netip"

	mm "github.com/block/proto-fleet/server/internal/domain/miner/models"

	"github.com/block/proto-fleet/server/generated/sqlc"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	tm "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	diagnosticsmodels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
)

// NumericFilterField selects which device_metrics column a NumericRange targets.
// Mirrors fleetmanagement.v1.NumericField on the wire.
type NumericFilterField int

const (
	NumericFilterFieldUnspecified NumericFilterField = iota
	NumericFilterFieldHashrateTHs
	NumericFilterFieldEfficiencyJTH
	NumericFilterFieldPowerKW
	NumericFilterFieldTemperatureC
	NumericFilterFieldVoltageV
	NumericFilterFieldCurrentA
)

// NumericRange is a half-open or closed range predicate on a single telemetry
// field. Min and Max are pointers so either bound can be omitted; at least one
// must be non-nil after parseFilter validation.
type NumericRange struct {
	Field        NumericFilterField
	Min          *float64
	Max          *float64
	MinInclusive bool
	MaxInclusive bool
}

//go:generate go run go.uber.org/mock/mockgen -source=device.go -destination=mocks/mock_device_store.go -package=mocks DeviceStore

type MinerFilter struct {
	PairingStatuses     []fm.PairingStatus // Changed from single value to slice
	DeviceStatusFilter  []mm.MinerStatus
	ModelNames          []string                          // Filter by device model names (e.g., "S21 XP", "M60")
	ErrorComponentTypes []diagnosticsmodels.ComponentType // Filter devices by component types that have errors
	GroupIDs            []int64                           // Filter by group membership (OR logic: match any group)
	RackIDs             []int64                           // Filter by rack membership (OR logic: match any rack)
	DeviceIdentifiers   []string                          // Filter by specific device identifiers (e.g., for group-scoped queries)
	FirmwareVersions    []string                          // Filter by firmware version strings (OR logic)
	Zones               []string                          // Filter by rack zones (OR logic). Excludes miners not assigned to any rack.
	NumericRanges       []NumericRange                    // Range predicates on telemetry. Multiple entries AND'd; presence triggers an INNER JOIN to latest_metrics and excludes OFFLINE miners.
	IPCIDRs             []netip.Prefix                    // CIDR membership filter (OR logic across entries). Already normalized via Prefix.Masked().
}

// MinerStateCounts holds fleet health state counts for a collection.
type MinerStateCounts struct {
	HashingCount  int32
	BrokenCount   int32
	OfflineCount  int32
	SleepingCount int32
}

// ComponentErrorCount holds error counts by component type for a collection.
type ComponentErrorCount struct {
	CollectionID  int64
	ComponentType int32
	DeviceCount   int32
}

// MinerModelGroupResult holds model group data with count.
type MinerModelGroupResult struct {
	Model        string
	Manufacturer string
	Count        int32
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
	DriverName                 string
	LastKnownIP                string
	LastKnownPort              string
	LastKnownURLScheme         string
	OrgID                      int64
	DiscoveredDeviceIdentifier string
}

// DeviceRenameProperties holds the device attributes needed for name generation.
type DeviceRenameProperties struct {
	DeviceIdentifier         string
	DiscoveredDeviceID       int64
	CustomName               string
	MacAddress               string
	SerialNumber             string
	Model                    string
	ModelSortValue           *string
	Manufacturer             string
	IPAddress                string
	FirmwareVersion          string
	FirmwareSortValue        *string
	WorkerName               string
	WorkerNamePoolSyncStatus string
	RackLabel                string
	RackPosition             string
	Hashrate                 *float64
	Temperature              *float64
	Power                    *float64
	Efficiency               *float64
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
	GetAvailableModels(ctx context.Context, orgID int64) ([]string, error)
	GetAvailableFirmwareVersions(ctx context.Context, orgID int64) ([]string, error)
	GetMinerModelGroups(ctx context.Context, orgID int64, filter *MinerFilter) ([]MinerModelGroupResult, error)
	UpsertDeviceStatus(ctx context.Context, deviceIdentifier models.DeviceIdentifier, status mm.MinerStatus, details string) error
	UpsertDeviceStatuses(ctx context.Context, updates []DeviceStatusUpdate) error
	GetDeviceStatusForDeviceIdentifiers(ctx context.Context, deviceIdentifiers []models.DeviceIdentifier) (map[models.DeviceIdentifier]mm.MinerStatus, error)
	GetOfflineDevices(ctx context.Context, limit int) ([]OfflineDeviceInfo, error)
	GetKnownSubnets(ctx context.Context, orgID int64, maskBits int, isIPv4 bool) ([]string, error)
	ListMinerStateSnapshots(ctx context.Context, orgID int64, cursor string, pageSize int32, filter *MinerFilter, sortConfig *SortConfig) ([]sqlc.ListMinerStateSnapshotsRow, string, int64, error)
	AllDevicesBelongToOrg(ctx context.Context, deviceIdentifiers []string, orgID int64) (bool, error)
	SoftDeleteDevices(ctx context.Context, deviceIdentifiers []string, orgID int64) (int64, error)
	GetDeviceIdentifiersByOrgWithFilter(ctx context.Context, orgID int64, filter *MinerFilter) ([]string, error)
	GetMinerStateCountsByCollections(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]MinerStateCounts, error)
	UpdateFirmwareVersion(ctx context.Context, deviceIdentifier models.DeviceIdentifier, firmwareVersion string) error
	UpdateWorkerName(ctx context.Context, deviceIdentifier models.DeviceIdentifier, workerName string) error
	GetDevicePropertiesForRename(ctx context.Context, orgID int64, deviceIdentifiers []string, includeTelemetry bool) ([]DeviceRenameProperties, error)
	UpdateDeviceCustomNames(ctx context.Context, orgID int64, names map[string]string) error
	GetPairedDeviceByMACAddress(ctx context.Context, macAddress string, orgID int64) (*PairedDeviceInfo, error)
	GetPairedDevicesByMACAddresses(ctx context.Context, macAddresses []string, orgID int64) (map[string]*PairedDeviceInfo, error)
	GetPairedDeviceBySerialNumber(ctx context.Context, serialNumber string, orgID int64) (*PairedDeviceInfo, error)
}

// PairedDeviceInfo contains information about an existing paired device found during reconciliation.
// Used during discovery/pairing reconciliation to detect devices that moved to a new IP/subnet.
type PairedDeviceInfo struct {
	DeviceIdentifier           string
	MacAddress                 string
	SerialNumber               string
	DiscoveredDeviceIdentifier string
	DiscoveredDeviceID         int64
}
