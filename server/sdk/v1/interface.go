package sdk

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/block/proto-fleet/server/sdk/v1/errors"
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
	DeviceID        string
	Timestamp       time.Time
	FirmwareVersion string

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

// MinerError represents the standardized classification of device errors
type MinerError = errors.MinerError

// Severity represents the criticality level of an error
type Severity = errors.Severity

// ComponentType represents the type of hardware component
type ComponentType = errors.ComponentType

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

// PerformanceMode represents the power/performance profile for mining operations
type PerformanceMode int

const (
	// PerformanceModeUnspecified represents an unspecified performance mode
	PerformanceModeUnspecified PerformanceMode = iota
	// PerformanceModeMaximumHashrate push miner for peak hashrate output
	PerformanceModeMaximumHashrate
	// PerformanceModeEfficiency limit miner to conserve energy consumption
	PerformanceModeEfficiency
)

// CurtailLevel mirrors curtailment.v1 CurtailmentLevel values.
type CurtailLevel int32

const (
	// CurtailLevelUnspecified is unset.
	CurtailLevelUnspecified CurtailLevel = 0
	// CurtailLevelEfficiency requests the device's lowest-energy supported mining mode.
	CurtailLevelEfficiency CurtailLevel = 1
	// CurtailLevelPartialPercent is reserved for partial-percent curtailment.
	CurtailLevelPartialPercent CurtailLevel = 2
	// CurtailLevelFull is the v1 full-shutdown level.
	CurtailLevelFull CurtailLevel = 3
)

// CurtailRequest describes a curtailment request for a device.
type CurtailRequest struct {
	Level CurtailLevel
}

// UncurtailRequest describes a request to restore a previously curtailed device.
type UncurtailRequest struct{}

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

// MiningPoolConfig represents a mining pool configuration for setting pools on a device
type MiningPoolConfig struct {
	Priority   int32
	URL        string
	WorkerName string
}

// ConfiguredPool represents a pool currently configured on a device
// This is returned by GetMiningPools to show the miner's actual pool configuration
type ConfiguredPool struct {
	Priority int32
	URL      string
	Username string // Worker name / username configured on the miner
}

// NewDeviceResult contains the result of creating a new device
type NewDeviceResult struct {
	Device Device
}

// DeviceInfo represents information about a discovered device
type DeviceInfo struct {
	Host            string // e.g., "192.168.1.100" (maps to proto 'host')
	Port            int32  // e.g., 4028 (maps to proto 'port')
	URLScheme       string // e.g., "http", "https", "ssh" (maps to proto 'url_scheme')
	SerialNumber    string // e.g., "SN123456789" (maps to proto 'serial_number')
	Model           string // e.g., "Antminer S19" (maps to proto 'model')
	Manufacturer    string // e.g., "Bitmain" (maps to proto 'manufacturer')
	MacAddress      string // e.g., "00:1A:2B:3C:4D:5E" (maps to proto 'mac_address')
	FirmwareVersion string // e.g., "1.2.3" (maps to proto 'firmware_version')
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
	// HealthNeedsMiningPool represents device is online but needs mining pool configured
	HealthNeedsMiningPool
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

// DeviceCurtailment is optional and should match reported curtail capabilities.
type DeviceCurtailment interface {
	Curtail(ctx context.Context, req CurtailRequest) error
	Uncurtail(ctx context.Context, req UncurtailRequest) error
}

// DeviceConfiguration represents device configuration operations
type DeviceConfiguration interface {
	// CoreV1 - Configuration methods (required)
	SetCoolingMode(ctx context.Context, mode CoolingMode) error
	// GetCoolingMode returns the current cooling mode configuration from the device
	GetCoolingMode(ctx context.Context) (CoolingMode, error)
	SetPowerTarget(ctx context.Context, performanceMode PerformanceMode) error
	UpdateMiningPools(ctx context.Context, pools []MiningPoolConfig) error
	// UpdateMinerPassword updates the web UI password
	// currentPassword is the existing password for verification (required by some miner APIs)
	UpdateMinerPassword(ctx context.Context, currentPassword string, newPassword string) error
	// GetMiningPools returns the currently configured pools on the device
	GetMiningPools(ctx context.Context) ([]ConfiguredPool, error)
}

// FirmwareFile represents a firmware file to be uploaded to a device.
type FirmwareFile struct {
	Reader   io.Reader
	Filename string
	Size     int64
	FilePath string // On-disk path for gRPC bridge passthrough (plugins share the server's filesystem)
}

// DeviceMaintenance represents device maintenance operations
type DeviceMaintenance interface {
	DownloadLogs(ctx context.Context, since *time.Time, batchLogUUID string) (logData string, moreData bool, err error)
	FirmwareUpdate(ctx context.Context, firmware FirmwareFile) error
	// Unpair clears device credentials during fleet unpairing
	Unpair(ctx context.Context) error
}

// FirmwareUpdateStatus represents the current firmware installation state on a device.
type FirmwareUpdateStatus struct {
	State    string // "current", "downloading", "downloaded", "installing", "installed", "confirming", "success", "error"
	Progress *int
	Error    *string
}

// FirmwareUpdateStatusProvider is an optional interface that devices can implement
// to report firmware installation progress after a file has been uploaded.
// Plugins that do not support install status polling should not implement this.
type FirmwareUpdateStatusProvider interface {
	GetFirmwareUpdateStatus(ctx context.Context) (*FirmwareUpdateStatus, error)
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

// DefaultCredentialsProvider is an optional interface that drivers can implement
// to provide default credentials for auto-authentication during pairing.
// If a driver implements this interface and returns credentials, the server
// will attempt pairing with each credential before requiring manual input.
type DefaultCredentialsProvider interface {
	// GetDefaultCredentials returns credentials to try for the given device.
	// Pass empty strings to get all known credentials (fallback for unknown devices).
	GetDefaultCredentials(ctx context.Context, manufacturer, firmwareVersion string) []UsernamePassword
}

// ModelCapabilitiesProvider is an optional interface that drivers can implement
// to provide model-specific capabilities. This allows plugins to report different
// capabilities for different device models (e.g., Antminer S19 vs S21).
// If a driver implements this interface, the server will call GetCapabilitiesForModel
// with the device model to get model-specific capability overrides.
type ModelCapabilitiesProvider interface {
	// GetCapabilitiesForModel returns capability overrides for a specific device model.
	// The returned capabilities will be merged with the base driver capabilities.
	// Return nil to use only the base driver capabilities for this model.
	GetCapabilitiesForModel(ctx context.Context, model string) Capabilities
}

// DiscoveryPortsProvider is an optional interface that drivers can implement
// to provide canonical discovery scan ports for server-side port derivation.
type DiscoveryPortsProvider interface {
	// GetDiscoveryPorts returns canonical discovery ports in driver-preferred order.
	// Return nil or an empty slice when the driver does not advertise discovery ports.
	GetDiscoveryPorts(ctx context.Context) []string
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

	// Command capabilities
	CapabilityReboot             = "reboot"               // Device reboot support
	CapabilityMiningStart        = "mining_start"         // Start mining support
	CapabilityMiningStop         = "mining_stop"          // Stop mining support
	CapabilityLEDBlink           = "led_blink"            // LED blink support
	CapabilityFactoryReset       = "factory_reset"        // Factory reset support
	CapabilityCoolingModeAir     = "cooling_mode_air"     // Air cooling mode support
	CapabilityCoolingModeImmerse = "cooling_mode_immerse" // Immersion cooling mode support
	CapabilityPoolConfig         = "pool_config"          // Pool configuration support
	CapabilityPoolPriority       = "pool_priority"        // Pool priority support
	CapabilityLogsDownload       = "logs_download"        // Device logs download support
	//#nosec G101 -- Capability constant name, not actual credentials
	CapabilityUpdateMinerPassword = "update_miner_password" // Update miner web UI password support

	// Power mode capabilities
	CapabilityPowerModeEfficiency = "power_mode_efficiency" // Efficiency/low power mode support

	// Curtailment capabilities
	CapabilityCurtailFull       = "curtail_full"       // Curtail/Uncurtail support (FULL level)
	CapabilityCurtailEfficiency = "curtail_efficiency" // Efficiency-mode curtailment support
	CapabilityCurtailPartial    = "curtail_partial"    // reserved: partial-percent curtailment

	// Telemetry capabilities
	CapabilityRealtimeTelemetry = "realtime_telemetry"    // Real-time telemetry support
	CapabilityHistoricalData    = "historical_data"       // Historical data support
	CapabilityHashrateReported  = "hashrate_reported"     // Hashrate reported
	CapabilityPowerUsage        = "power_usage_reported"  // Power usage reported
	CapabilityTemperature       = "temperature_reported"  // Temperature reported
	CapabilityFanSpeed          = "fan_speed_reported"    // Fan speed reported
	CapabilityEfficiency        = "efficiency_reported"   // Efficiency reported
	CapabilityUptime            = "uptime_reported"       // Uptime reported
	CapabilityErrorCount        = "error_count_reported"  // Error count reported
	CapabilityMinerStatus       = "miner_status_reported" // Miner status reported
	CapabilityPoolStats         = "pool_stats_reported"   // Pool stats reported
	CapabilityPerChipStats      = "per_chip_stats"        // Per-chip stats reported
	CapabilityPerBoardStats     = "per_board_stats"       // Per-board stats reported
	CapabilityPSUStats          = "psu_stats_reported"    // PSU stats reported

	// Firmware capabilities
	CapabilityFirmware     = "firmware"      // Firmware update support (generic)
	CapabilityOTAUpdate    = "ota_update"    // OTA update support
	CapabilityManualUpload = "manual_upload" // Manual firmware upload support

	// Authentication capabilities
	CapabilityBasicAuth      = "basic_auth"      // Basic (username/password) authentication
	CapabilityAsymmetricAuth = "asymmetric_auth" // Asymmetric key authentication
)
