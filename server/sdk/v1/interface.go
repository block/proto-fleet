package sdk

import (
	"context"
	"fmt"
	"time"
)

// DriverIdentifier contains driver identification information
type DriverIdentifier struct {
	DriverName string
	APIVersion string
}

// Capabilities represents feature supported by a driver or device
type Capabilities map[string]bool

// Aggregation represents how a metric value is computed
type Aggregation int

const (
	// AggregationUnspecified represents an unspecified aggregation type
	AggregationUnspecified Aggregation = iota
	// AggregationGauge represents instantaneous best-effort aggregation
	AggregationGauge // instantaneous best-effort
	// AggregationCounter represents monotonically increasing aggregation
	AggregationCounter // monotonically increasing
	// AggregationRate represents rate derived from counter over window
	AggregationRate // rate derived from counter over window
	// AggregationDerived represents function of other metrics
	AggregationDerived // function of other metrics
)

// MetricKind represents the kind of metric
type MetricKind int

const (
	// MetricKindUnspecified represents an unspecified metric kind
	MetricKindUnspecified MetricKind = iota
	// MetricKindGauge represents instantaneous best-effort metric
	MetricKindGauge // instantaneous best-effort
	// MetricKindCounter represents monotonically increasing metric
	MetricKindCounter // monotonically increasing
	// MetricKindRate represents rate derived from counter over window
	MetricKindRate // rate derived from counter over window
	// MetricKindHistogram represents distribution of values over window
	MetricKindHistogram // distribution of values over window
)

// Unit represents the unit of measurement
type Unit int

const (
	// UnitUnspecified represents an unspecified unit
	UnitUnspecified Unit = iota
	// UnitWatt represents watts
	UnitWatt
	// UnitCelsius represents degrees Celsius
	UnitCelsius
	// UnitRPM represents revolutions per minute
	UnitRPM
	// UnitHPS represents hashes per second
	UnitHPS = 10 // hashes per second
	// UnitJoulePerHash represents joules per hash
	UnitJoulePerHash = 11
	// UnitVolt represents volts
	UnitVolt = 20
	// UnitAmpere represents amperes
	UnitAmpere = 21
	// UnitBytes represents bytes
	UnitBytes = 22
	// UnitPercent represents percentage
	UnitPercent = 23
)

// SampleSemantics represents default sampling semantics for metrics
type SampleSemantics struct {
	Aggregation     Aggregation
	AveragingWindow time.Duration
	StartOfWindow   time.Time
}

// MetricDetail provides per-metric override information
type MetricDetail struct {
	Aggregation     Aggregation
	AveragingWindow time.Duration
	Min             *float64
	Max             *float64
	StdDev          *float64
	SensorID        *string
}

// Metric represents additional telemetry data
type Metric struct {
	Name       string
	Value      MetricValue
	Unit       Unit
	Kind       MetricKind
	ObservedAt time.Time
	Window     time.Duration
	Labels     map[string]string
}

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
	// HealthyInactive represents a healthy but inactive device
	HealthyInactive
	// HealthyActive represents a healthy and active device
	HealthyActive
	// Warning represents a device with warnings
	Warning
	// Critical represents a device in critical state
	Critical
	// Unknown represents a device with unknown status
	Unknown
)

// DeviceStatusResponse represents the current state of a device
type DeviceStatusResponse struct {
	DeviceID  string
	Timestamp time.Time
	Summary   string
	Health    HealthStatus // Overall health status

	// Metrics (nil if not supported/available)
	HashrateHS         *float64 // Hashrate in H/s (maps to proto 'hashrate_hs')
	PowerWatts         *float64 // Power consumption in watts (maps to proto 'power_watts')
	TemperatureCelsius *float64 // Temperature in Celsius (maps to proto 'temperature_celsius')
	EfficiencyJPerHash *float64 // Efficiency in J/H (maps to proto 'efficiency_j_per_hash')
	FanRPM             *int32   // Fan speed in RPM (maps to proto 'fan_rpm')

	// Sampling semantics
	Sample *SampleSemantics

	// Per-metric overrides
	MetricDetails map[string]MetricDetail

	// Driver-specific metadata
	Metadata map[string]string

	// Extensibility for additional metrics (Prometheus-style bag)
	ExtraMetrics []Metric
}

// DeviceCore represents the core functionality that all devices must implement
type DeviceCore interface {
	// ID returns the unique device instance identifier
	ID() string

	// DescribeDevice returns device info and capabilities
	DescribeDevice(ctx context.Context) (DeviceInfo, Capabilities, error)

	// Status returns current device status (CoreV1 - required)
	Status(ctx context.Context) (DeviceStatusResponse, error)

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
}

// DeviceOptional represents optional device capabilities
type DeviceOptional interface {
	// Optional capabilities - return (result, false, nil) if unsupported
	TryBatchStatus(ctx context.Context, ids []string) (map[string]DeviceStatusResponse, bool, error)
	TrySubscribe(ctx context.Context, ids []string) (<-chan DeviceStatusResponse, bool, error)
	TryGetWebViewURL(ctx context.Context) (string, bool, error)
	TryGetTimeSeriesData(ctx context.Context, metricNames []string, startTime, endTime time.Time, granularity *time.Duration, maxPoints int32, pageToken string) (series []DeviceStatusResponse, nextPageToken string, supported bool, err error)
}

// Device represents a single device instance managed by a driver
// It composes all the device interfaces to maintain backward compatibility
type Device interface {
	DeviceCore
	DeviceControl
	DeviceConfiguration
	DeviceMaintenance
	DeviceOptional
}

// Driver represents a miner driver that can create and manage device instances
type Driver interface {
	// CoreV1 - Driver Info (required)
	Handshake(ctx context.Context) (DriverIdentifier, error)
	DescribeDriver(ctx context.Context) (DriverIdentifier, Capabilities, error)

	// CoreV1 - Device Pairing (required)
	DiscoverDevice(ctx context.Context, ipAddress, port string) (DeviceInfo, error)
	PairDevice(ctx context.Context, device DeviceInfo, access SecretBundle) (string, error) // returns message

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

// Health status constants
const (
	HealthHealthyInactive = HealthyInactive
	HealthHealthyActive   = HealthyActive
	HealthWarning         = Warning
	HealthCritical        = Critical
	HealthUnknown         = Unknown
)
