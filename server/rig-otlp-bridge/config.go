package main

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"

	miner_rpc "github.com/block/proto-fleet/server/rig-otlp-bridge/internal/rigapi/minertelemetry"
)

const (
	defaultConfigPath              = "config.json"
	defaultTelemetryPort           = 2123
	defaultAPIPort                 = 80
	defaultLogSeverity             = "warn"
	defaultScanIntervalS           = 30.0
	defaultStreamReconnectInitialS = 1.0
	defaultStreamReconnectMaxS     = 30.0
	defaultProbeTimeoutS           = 1.5
	defaultWorkers                 = 64
	defaultMetricQueuePublishS     = 10.0
	defaultMetricQueueWindows      = 3
	defaultMetricExpectedRigsFloor = 16
	estimatedServicesPerRig        = 16
	minMetricQueueCapacity         = 256
	maxMetricQueueCapacity         = 16384
)

// Config drives the bridge's discovery and streaming behavior. All fields
// have JSON tags so the same struct backs the on-disk file; defaults are
// applied after unmarshaling and validation rejects nonsense values.
type Config struct {
	Subnets []string `json:"subnets"`
	Targets []string `json:"targets"`
	Site    string   `json:"site"`

	// Fleet mode: targets and enrichment labels come from proto-fleet's
	// ListMinerStateSnapshots API (service API key); subnets/targets/site
	// are ignored when set.
	FleetAPIURL   string `json:"fleet_api_url"`
	FleetAPIToken string `json:"fleet_api_token"`
	// Allows plain http to a non-loopback fleet API (e.g. a private
	// container network); otherwise plaintext is loopback-only.
	FleetAPIInsecureHTTP bool `json:"fleet_api_insecure_http"`
	// CIDRs fleet-sourced targets must fall within; empty defaults to
	// the private ranges (defaultFleetTargetCIDRs) in fleet mode.
	FleetTargetCIDRs []string `json:"fleet_target_cidrs"`
	// Device models to stream from (fleet's models filter); empty
	// defaults to the proto rig model.
	FleetTargetModels []string `json:"fleet_target_models"`

	fleetTargetPrefixes []netip.Prefix

	// gRPC port `telemetry-service` listens on. Discovery and streaming both
	// use this port. A bare target inherits this value; an explicit host:port
	// target overrides it for that target.
	TelemetryPort int `json:"telemetry_port"`

	// REST API scheme/port used after gRPC discovery to fetch identity and
	// display metadata. Fixed per deployment: proto rigs do not vary them.
	APIScheme string `json:"api_scheme"`
	APIPort   int    `json:"api_port"`

	// OTLP HTTP metrics receiver URL, e.g. a Prometheus with
	// --web.enable-otlp-receiver at <host>:9090/api/v1/otlp/v1/metrics.
	// Required: there is no bundled metrics store to default to.
	MetricsEndpoint string `json:"metrics_endpoint"`

	LogsEndpoint string `json:"logs_endpoint"`
	// Minimum log severity forwarded to the logs endpoint: "info", "warn", or
	// "error". Passed to telemetry-service on the StreamLogs connection so the
	// floor is applied upstream. Defaults to "warn".
	LogSeverity             string  `json:"log_severity"`
	ScanIntervalS           float64 `json:"scan_interval_s"`
	StreamReconnectInitialS float64 `json:"stream_reconnect_initial_s"`
	StreamReconnectMaxS     float64 `json:"stream_reconnect_max_s"`
	ProbeTimeoutS           float64 `json:"probe_timeout_s"`
	Workers                 int     `json:"workers"`

	MetricsGzip                 *bool   `json:"metrics_gzip"`
	LogsGzip                    *bool   `json:"logs_gzip"`
	MetricQueueExpectedRigs     int     `json:"metric_queue_expected_rigs"`
	MetricQueuePublishIntervalS float64 `json:"metric_queue_publish_interval_s"`
	MetricQueueBufferWindows    int     `json:"metric_queue_buffer_windows"`
}

func (c *Config) applyDefaults() {
	if c.TelemetryPort == 0 {
		c.TelemetryPort = defaultTelemetryPort
	}
	if c.APIPort == 0 {
		c.APIPort = defaultAPIPort
	}
	if c.APIScheme == "" {
		c.APIScheme = "http"
	}
	if len(c.FleetTargetModels) == 0 {
		c.FleetTargetModels = []string{defaultFleetTargetModel}
	}
	if c.LogSeverity == "" {
		c.LogSeverity = defaultLogSeverity
	}
	if c.ScanIntervalS == 0 {
		c.ScanIntervalS = defaultScanIntervalS
	}
	if c.StreamReconnectInitialS == 0 {
		c.StreamReconnectInitialS = defaultStreamReconnectInitialS
	}
	if c.StreamReconnectMaxS == 0 {
		c.StreamReconnectMaxS = defaultStreamReconnectMaxS
	}
	if c.ProbeTimeoutS == 0 {
		c.ProbeTimeoutS = defaultProbeTimeoutS
	}
	if c.Workers == 0 {
		c.Workers = defaultWorkers
	}
	if c.MetricQueueExpectedRigs == 0 {
		c.MetricQueueExpectedRigs = len(c.Targets)
		if c.MetricQueueExpectedRigs < defaultMetricExpectedRigsFloor {
			c.MetricQueueExpectedRigs = defaultMetricExpectedRigsFloor
		}
	}
	if c.MetricQueuePublishIntervalS == 0 {
		c.MetricQueuePublishIntervalS = defaultMetricQueuePublishS
	}
	if c.MetricQueueBufferWindows == 0 {
		c.MetricQueueBufferWindows = defaultMetricQueueWindows
	}
}

func (c *Config) validate() error {
	if c.TelemetryPort <= 0 || c.TelemetryPort > 65535 {
		return fmt.Errorf("telemetry_port out of range: %d", c.TelemetryPort)
	}
	if c.APIPort <= 0 || c.APIPort > 65535 {
		return fmt.Errorf("api_port out of range: %d", c.APIPort)
	}
	if c.APIScheme != "http" && c.APIScheme != "https" {
		return fmt.Errorf("api_scheme must be http or https: %q", c.APIScheme)
	}
	if _, err := severityNameToEnum(c.LogSeverity); err != nil {
		return err
	}
	if c.ScanIntervalS <= 0 {
		return fmt.Errorf("scan_interval_s must be > 0: %f", c.ScanIntervalS)
	}
	if c.StreamReconnectInitialS <= 0 || c.StreamReconnectMaxS <= 0 ||
		c.StreamReconnectInitialS > c.StreamReconnectMaxS {
		return fmt.Errorf("invalid reconnect window: initial=%f max=%f",
			c.StreamReconnectInitialS, c.StreamReconnectMaxS)
	}
	if c.ProbeTimeoutS <= 0 {
		return fmt.Errorf("probe_timeout_s must be > 0: %f", c.ProbeTimeoutS)
	}
	if c.Workers <= 0 {
		return fmt.Errorf("workers must be > 0: %d", c.Workers)
	}
	if c.MetricQueueExpectedRigs <= 0 {
		return fmt.Errorf("metric_queue_expected_rigs must be > 0: %d", c.MetricQueueExpectedRigs)
	}
	if c.MetricQueuePublishIntervalS <= 0 {
		return fmt.Errorf("metric_queue_publish_interval_s must be > 0: %f", c.MetricQueuePublishIntervalS)
	}
	if c.MetricQueueBufferWindows <= 0 {
		return fmt.Errorf("metric_queue_buffer_windows must be > 0: %d", c.MetricQueueBufferWindows)
	}
	return nil
}

// validateTargetSource runs after env/flag overrides: fleet mode is
// usually selected via OTLP_BRIDGE_FLEET_API_URL/_TOKEN, not the file.
func (c *Config) validateTargetSource() error {
	if c.FleetAPIURL == "" && len(c.Subnets) == 0 && len(c.Targets) == 0 {
		return fmt.Errorf("config must include a fleet_api_url, or at least one subnet or target")
	}
	// Scan expansion materializes every address; reject subnets that
	// would allocate more than a /16's worth before workers start.
	// Fleet mode ignores subnets, so leftovers from a migrated scan
	// config must not fail startup.
	if c.FleetAPIURL == "" {
		hosts := uint64(0)
		for _, s := range c.Subnets {
			p, err := netip.ParsePrefix(strings.TrimSpace(s))
			if err != nil {
				return fmt.Errorf("invalid subnet %q: %w", s, err)
			}
			hostBits := p.Addr().BitLen() - p.Bits()
			if hostBits > 16 {
				return fmt.Errorf("subnet %q is too large to scan (max /16-equivalent)", s)
			}
			hosts += uint64(1) << hostBits
			if hosts > maxScanHosts {
				return fmt.Errorf("subnets expand to more than %d scan targets", maxScanHosts)
			}
		}
		// A malformed target would otherwise abort every scan at runtime.
		for _, raw := range c.Targets {
			if _, err := parseTarget(raw, c.TelemetryPort); err != nil {
				return err
			}
		}
	}
	if c.FleetAPIURL != "" && c.FleetAPIToken == "" {
		// Fail fast: an empty token would be refused by fleet on every
		// call, leaving a healthy-looking sidecar that streams nothing.
		return fmt.Errorf("fleet_api_url requires fleet_api_token (OTLP_BRIDGE_FLEET_API_TOKEN)")
	}
	// Fail closed: without an explicit allowlist, fleet mode only dials
	// private ranges; public-IP rigs require fleet_target_cidrs.
	if c.FleetAPIURL != "" && len(c.FleetTargetCIDRs) == 0 {
		c.FleetTargetCIDRs = defaultFleetTargetCIDRs
	}
	c.fleetTargetPrefixes = c.fleetTargetPrefixes[:0]
	for _, s := range c.FleetTargetCIDRs {
		p, err := netip.ParsePrefix(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("invalid fleet_target_cidrs entry %q: %w", s, err)
		}
		c.fleetTargetPrefixes = append(c.fleetTargetPrefixes, p.Masked())
	}
	return nil
}

// validateMetricsEndpoint runs after env/flag overrides so either the
// config file or --otlp-endpoint / OTLP_BRIDGE_OTLP_ENDPOINT can supply it.
func (c *Config) validateMetricsEndpoint() error {
	if c.MetricsEndpoint == "" {
		return fmt.Errorf("config must include metrics_endpoint (an OTLP HTTP metrics receiver URL)")
	}
	u, err := url.Parse(c.MetricsEndpoint)
	if err != nil {
		return fmt.Errorf("invalid metrics_endpoint %q: %w", c.MetricsEndpoint, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("metrics_endpoint must be an http(s) URL: %q", c.MetricsEndpoint)
	}
	return nil
}

var defaultFleetTargetCIDRs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}

// maxScanHosts caps total subnet-scan expansion (one /16).
const maxScanHosts = 1 << 16

// defaultFleetTargetModel matches proto rigs in fleet's models filter.
const defaultFleetTargetModel = "Rig"

// fleetTargetAllowed reports whether a fleet-sourced target address falls
// within the configured CIDR allowlist (empty list = any routable address).
func (c *Config) fleetTargetAllowed(ipAddress string) bool {
	if len(c.fleetTargetPrefixes) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(ipAddress)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range c.fleetTargetPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func (c *Config) metricsGzipEnabled() bool {
	return c.MetricsGzip == nil || *c.MetricsGzip
}

func (c *Config) logsGzipEnabled() bool {
	return c.LogsGzip == nil || *c.LogsGzip
}

// severityNameToEnum maps a config/flag severity name to the gRPC enum. We
// only support INFO and above; trace/debug are rejected since services never
// publish them to the telemetry channel.
func severityNameToEnum(name string) (miner_rpc.LogSeverity, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "info":
		return miner_rpc.LogSeverity_LOG_SEVERITY_INFO, nil
	case "warn", "warning":
		return miner_rpc.LogSeverity_LOG_SEVERITY_WARN, nil
	case "error":
		return miner_rpc.LogSeverity_LOG_SEVERITY_ERROR, nil
	default:
		return miner_rpc.LogSeverity_LOG_SEVERITY_UNSPECIFIED,
			fmt.Errorf("invalid log_severity %q (want info, warn, or error)", name)
	}
}

// minLogSeverity resolves the validated LogSeverity name to its gRPC enum.
func (c *Config) minLogSeverity() miner_rpc.LogSeverity {
	sev, _ := severityNameToEnum(c.LogSeverity)
	return sev
}

func (c *Config) metricQueueCapacity() int {
	return clampMetricQueueCapacity(
		c.MetricQueueExpectedRigs * estimatedServicesPerRig * c.MetricQueueBufferWindows,
	)
}

func clampMetricQueueCapacity(capacity int) int {
	if capacity < minMetricQueueCapacity {
		return minMetricQueueCapacity
	}
	if capacity > maxMetricQueueCapacity {
		return maxMetricQueueCapacity
	}
	return capacity
}

// loadConfig reads JSON from path, applies defaults, and validates. It also
// detects removed legacy fields and emits a migration error pointing the
// operator at the v2 names.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := checkLegacyFields(data); err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// checkLegacyFields rejects v1 config field names. We surface a clear
// error rather than silently ignoring so misconfiguration cannot quietly
// stream from the wrong port.
func checkLegacyFields(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		// If it doesn't parse as an object, fall through; the typed
		// unmarshal will produce the real error.
		return nil
	}
	if _, ok := raw["port"]; ok {
		return fmt.Errorf("legacy `port` field is no longer supported; use `telemetry_port`")
	}
	if _, ok := raw["mcdd_port"]; ok {
		return fmt.Errorf("legacy `mcdd_port` field is no longer supported; use `telemetry_port`")
	}
	if _, ok := raw["publish_interval_s"]; ok {
		return fmt.Errorf("legacy `publish_interval_s` field is no longer supported; streaming is push-based")
	}
	return nil
}

func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
