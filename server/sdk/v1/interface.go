package sdk

import (
	"context"
	"fmt"
	"time"

	"github.com/btc-mining/proto-fleet/server/sdk/v1/errors"
)

// DriverIdentifier contains driver identification information
type DriverIdentifier struct {
	DriverName string
	APIVersion string
}

// Capabilities represents feature supported by a driver or device
type Capabilities map[string]bool

// ============================================================================
// V2 Telemetry Model - Go Types
// ============================================================================

// MetricKind represents the kind of metric
type MetricKind int

const (
	// MetricKindUnspecified represents an unspecified metric kind
	MetricKindUnspecified MetricKind = iota
	// MetricKindGauge represents instantaneous best-effort metric (point-in-time measurement)
	MetricKindGauge
	// MetricKindRate represents rate derived from counter over window (rate of change per second)
	MetricKindRate
	// MetricKindCounter represents monotonically increasing metric
	MetricKindCounter
)

// MetricValue represents a single telemetry measurement with optional statistical metadata
type MetricValue struct {
	Value    float64
	Kind     MetricKind
	MetaData *MetricValueMetaData
}

// MetricValueMetaData provides statistical context for a metric value
type MetricValueMetaData struct {
	Window    *time.Duration
	Min       *float64
	Max       *float64
	Avg       *float64
	StdDev    *float64
	Timestamp *time.Time
}

// ComponentStatus represents the health and operational state of an individual component
type ComponentStatus int

const (
	// ComponentStatusUnspecified represents an unspecified component status
	ComponentStatusUnspecified ComponentStatus = iota
	// ComponentStatusUnknown represents unknown status (no telemetry data)
	ComponentStatusUnknown
	// ComponentStatusHealthy represents operating normally within acceptable parameters
	ComponentStatusHealthy
	// ComponentStatusWarning represents degraded performance but still functional
	ComponentStatusWarning
	// ComponentStatusCritical represents failed, malfunctioning, or out of safe operating range
	ComponentStatusCritical
	// ComponentStatusOffline represents not responding or unreachable
	ComponentStatusOffline
	// ComponentStatusDisabled represents intentionally disabled by operator or firmware
	ComponentStatusDisabled
)

// ComponentInfo contains common metadata for all hardware components
type ComponentInfo struct {
	Index        int32
	Name         string
	Status       ComponentStatus
	StatusReason *string
	Timestamp    *time.Time
}

// HashBoardMetrics represents telemetry from an ASIC hashboard
type HashBoardMetrics struct {
	ComponentInfo
	SerialNumber *string

	// Performance metrics
	HashRateHS *MetricValue
	TempC      *MetricValue

	// Electrical metrics
	VoltageV *MetricValue
	CurrentA *MetricValue

	// Temperature sensors
	InletTempC   *MetricValue
	OutletTempC  *MetricValue
	AmbientTempC *MetricValue

	// Chip information
	ChipCount        *int32
	ChipFrequencyMHz *MetricValue

	// Sub-components
	ASICs      []ASICMetrics
	FanMetrics []FanMetrics
}

// ASICMetrics represents telemetry from an individual ASIC chip
type ASICMetrics struct {
	ComponentInfo

	TempC        *MetricValue
	FrequencyMHz *MetricValue
	VoltageV     *MetricValue
	HashrateHS   *MetricValue
}

// PSUMetrics represents telemetry from a power supply unit
type PSUMetrics struct {
	ComponentInfo

	// Output measurements
	OutputPowerW   *MetricValue
	OutputVoltageV *MetricValue
	OutputCurrentA *MetricValue

	// Input measurements
	InputPowerW   *MetricValue
	InputVoltageV *MetricValue
	InputCurrentA *MetricValue

	// Additional metrics
	HotSpotTempC      *MetricValue
	EfficiencyPercent *MetricValue

	// Sub-components
	FanMetrics []FanMetrics
}

// FanMetrics represents telemetry from a cooling fan
type FanMetrics struct {
	ComponentInfo

	RPM     *MetricValue
	TempC   *MetricValue
	Percent *MetricValue
}

// ControlBoardMetrics represents telemetry from the device control board
type ControlBoardMetrics struct {
	ComponentInfo
}

// SensorMetrics represents miscellaneous sensors on the device
type SensorMetrics struct {
	ComponentInfo

	Type  string
	Unit  string
	Value *MetricValue
}

// DeviceMetrics represents the complete telemetry snapshot for a mining device
type DeviceMetrics struct {
	// Identity
	DeviceID  string
	Timestamp time.Time

	// Device-level health
	Health       HealthStatus
	HealthReason *string

	// Device-level aggregated metrics
	HashrateHS   *MetricValue
	TempC        *MetricValue
	FanRPM       *MetricValue
	PowerW       *MetricValue
	EfficiencyJH *MetricValue

	// Component-level metrics
	HashBoards          []HashBoardMetrics
	PSUMetrics          []PSUMetrics
	ControlBoardMetrics []ControlBoardMetrics
	FanMetrics          []FanMetrics
	SensorMetrics       []SensorMetrics
}

// ============================================================================
// Error Reporting Types
// ============================================================================

// DeviceError represents an error reported by a plugin for a device.
// This is the plugin-facing error type without the fleet-managed ErrorID field.
// Plugins populate this type and return it from GetErrors().
type DeviceError = errors.DeviceError

// DeviceErrors contains all plugin-reported errors for a specific device.
// This is returned by plugin GetErrors() calls.
type DeviceErrors = errors.DeviceErrors

// ErrorMessage represents a fleet-tracked miner error.
// This type includes fleet-managed fields (ErrorID, DeviceID).
type ErrorMessage = errors.ErrorMessage

// MinerError represents the standardized classification of device errors
type MinerError = errors.MinerError

// Severity represents the criticality level of an error
type Severity = errors.Severity

// ============================================================================
// Other SDK Types
// ============================================================================

// CoolingMode represents the cooling mode of a device
type CoolingMode int

const (
	// CoolingModeUnspecified represents an unspecified cooling mode
	CoolingModeUnspecified CoolingMode = iota
	// CoolingModeAirCooled represents air cooling
	CoolingModeAirCooled
	// CoolingModeImmersionCooled represents immersion cooling
	CoolingModeImmersionCooled
	// CoolingModeManual represents manual cooling mode (e.g., user sets fan speed manually)
	CoolingModeManual
)

// APIKey represents API key authentication
type APIKey struct {
	Key string
}

func (a APIKey) String() string {
	return "APIKey(*****)"
}

// UsernamePassword represents username/password authentication
type UsernamePassword struct {
	Username string
	Password string
}

func (u UsernamePassword) String() string {
	return fmt.Sprintf("UsernamePassword(%s/*****)", u.Username)
}

// BearerToken represents bearer token authentication
type BearerToken struct {
	Token string
}

func (b BearerToken) String() string {
	return "BearerToken(*****)"
}

// TLSClientCert represents TLS client certificate authentication
type TLSClientCert struct {
	ClientCertPEM []byte
	KeyPEM        []byte
	CACertPEM     []byte
}

func (t TLSClientCert) String() string {
	return "TLSClientCert(*****)"
}

// SecretBundle represents authentication credentials
type SecretBundle struct {
	Version string
	Kind    interface{} // can be APIKey, UsernamePassword, BearerToken, or TLSClientCert
	TTL     *time.Duration
}

// MiningPoolConfig represents a mining pool configuration
type MiningPoolConfig struct {
	Priority   int32
	URL        string
	WorkerName string
}

// NewDeviceResult contains the result of creating a new device
type NewDeviceResult struct {
	Device Device
}

// DeviceType represents the type of mining device
type DeviceType int

const (
	// DeviceTypeUnspecified represents an unspecified device type
	DeviceTypeUnspecified DeviceType = iota
	// DeviceTypeASIC represents an ASIC mining device
	DeviceTypeASIC
	// DeviceTypeGPU represents a GPU mining device
	DeviceTypeGPU
	// DeviceTypeFPGA represents an FPGA mining device
	DeviceTypeFPGA
)

// DeviceInfo represents information about a discovered device
type DeviceInfo struct {
	Host         string     // e.g., "192.168.1.100" (maps to proto 'host')
	Port         int32      // e.g., 4028 (maps to proto 'port')
	URLScheme    string     // e.g., "http", "https", "ssh" (maps to proto 'url_scheme')
	SerialNumber string     // e.g., "SN123456789" (maps to proto 'serial_number')
	Model        string     // e.g., "Antminer S19" (maps to proto 'model')
	Manufacturer string     // e.g., "Bitmain" (maps to proto 'manufacturer')
	Type         DeviceType // Device type enum (maps to proto 'type')
	MacAddress   string     // e.g., "00:1A:2B:3C:4D:5E" (maps to proto 'mac_address')
}

// HealthStatus represents the health status of a device
type HealthStatus int

const (
	// HealthStatusUnspecified represents an unspecified health status
	HealthStatusUnspecified HealthStatus = iota
	// HealthUnknown represents unknown health state (device unreachable)
	HealthUnknown
	// HealthHealthyActive represents mining and all systems healthy
	HealthHealthyActive
	// HealthHealthyInactive represents all systems healthy but not actively mining
	HealthHealthyInactive
	// HealthWarning represents degraded performance but still operational
	HealthWarning
	// HealthCritical represents failed, non-functional, or requires immediate attention
	HealthCritical
)

// DeviceCore represents the core functionality that all devices must implement
type DeviceCore interface {
	// ID returns the unique device instance identifier
	ID() string

	// DescribeDevice returns device info and capabilities
	DescribeDevice(ctx context.Context) (DeviceInfo, Capabilities, error)

	// Status returns current device status (CoreV1 - required)
	Status(ctx context.Context) (DeviceMetrics, error)

	// Close releases device resources
	Close(ctx context.Context) error
}

// DeviceControl represents mining control operations
type DeviceControl interface {
	// CoreV1 - Control methods (required)
	StartMining(ctx context.Context) error
	StopMining(ctx context.Context) error
	BlinkLED(ctx context.Context) error
	Reboot(ctx context.Context) error
}

// DeviceConfiguration represents device configuration operations
type DeviceConfiguration interface {
	// CoreV1 - Configuration methods (required)
	SetCoolingMode(ctx context.Context, mode CoolingMode) error
	UpdateMiningPools(ctx context.Context, pools []MiningPoolConfig) error
}

// DeviceMaintenance represents device maintenance operations
type DeviceMaintenance interface {
	DownloadLogs(ctx context.Context, since *time.Time, batchLogUUID string) (logData string, moreData bool, err error)
	FirmwareUpdate(ctx context.Context) error
	// Unpair clears device credentials during fleet unpairing
	Unpair(ctx context.Context) error
}

// DeviceErrorReporting represents device error reporting operations
type DeviceErrorReporting interface {
	// CoreV1 - Error System (required)
	// GetErrors returns all active and historical errors for the device
	GetErrors(ctx context.Context) (DeviceErrors, error)
}

// DeviceOptional represents optional device capabilities
type DeviceOptional interface {
	// Optional capabilities - return (result, false, nil) if unsupported
	TryBatchStatus(ctx context.Context, ids []string) (map[string]DeviceMetrics, bool, error)
	TrySubscribe(ctx context.Context, ids []string) (<-chan DeviceMetrics, bool, error)
	TryGetWebViewURL(ctx context.Context) (string, bool, error)
	TryGetTimeSeriesData(ctx context.Context, metricNames []string, startTime, endTime time.Time, granularity *time.Duration, maxPoints int32, pageToken string) (series []DeviceMetrics, nextPageToken string, supported bool, err error)
}

// Device represents a single device instance managed by a driver
// It composes all the device interfaces to maintain backward compatibility
type Device interface {
	DeviceCore
	DeviceControl
	DeviceConfiguration
	DeviceMaintenance
	DeviceErrorReporting
	DeviceOptional
}

// Driver represents a miner driver that can create and manage device instances
type Driver interface {
	// CoreV1 - Driver Info (required)
	Handshake(ctx context.Context) (DriverIdentifier, error)
	DescribeDriver(ctx context.Context) (DriverIdentifier, Capabilities, error)

	// CoreV1 - Device Pairing (required)
	DiscoverDevice(ctx context.Context, ipAddress, port string) (DeviceInfo, error)
	PairDevice(ctx context.Context, device DeviceInfo, access SecretBundle) (DeviceInfo, error) // returns updated device info after pairing

	// CoreV1 - Device Management (required)
	NewDevice(ctx context.Context, deviceID string, deviceInfo DeviceInfo, secret SecretBundle) (NewDeviceResult, error)
}

// Standard capability flags
const (
	// CoreV1 capabilities
	CapabilityPollingHost = "polling_host" // Host-side polling supported

	// Optional capabilities
	CapabilityPollingPlugin = "polling_plugin" // Plugin-side polling with Subscribe()
	CapabilityBatchStatus   = "batch_status"   // BatchStatus() support
	CapabilityStreaming     = "streaming"      // Stream-based updates

	// Discovery and pairing capabilities
	CapabilityDiscovery = "discovery" // Device discovery support
	CapabilityPairing   = "pairing"   // Device pairing support

	// Admin capabilities
	CapabilityReboot     = "reboot"      // Device reboot support
	CapabilityFirmware   = "firmware"    // Firmware update support
	CapabilityPoolConfig = "pool_config" // Pool configuration support
)
