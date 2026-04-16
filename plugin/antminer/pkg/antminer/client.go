// Package antminer provides client functionality for communicating with Bitmain Antminer devices.
//
// This package implements both RPC and Web API clients for comprehensive device management:
//   - RPC API for mining status and control
//   - Web API for configuration and authentication
//   - Unified interface for device operations
//   - Proper error handling and timeouts
package antminer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/networking"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	"github.com/block/proto-fleet/server/sdk/v1"
)

const (
	// GHSToHS converts GH/s to H/s
	GHSToHS = 1e9
	// THSToGHS converts TH/s to GH/s
	THSToGHS       = 1000
	defaultTimeOut = 30 * time.Second

	// Set to absolute zero in Celsius as any reading below this is invalid
	minValidTemperature = -273.15

	// Temperature sensor positions in TempChip array [inlet_1, inlet_2, outlet_1, outlet_2]
	// Based on Antminer hardware sensor layout
	tempSensorCount      = 4
	inletTempStartIndex  = 0
	inletTempEndIndex    = 2
	outletTempStartIndex = 2
	outletTempEndIndex   = 4
)

// Client constants
const (
	DefaultDialTimeout = 10 * time.Second
	DefaultReadTimeout = 30 * time.Second
	MaxResponseSize    = 1 << 20 // 1MB
	DefaultRPCPort     = "4028"
	DefaultWebPort     = "80"
)

// Client provides a unified interface for communicating with Antminer devices
type Client struct {
	connectInfo *web.AntminerConnectionInfo
	host        string
	rpcPort     int32
	webPort     int32
	urlScheme   string
	credentials *sdk.UsernamePassword

	// HTTP client for web API
	httpClient *http.Client

	// Web API client
	webClient web.WebAPIClient

	// RPC client
	rpcClient rpc.RPCClient

	stopBlinkMux sync.Mutex
}

// Credentials holds authentication information
type Credentials struct {
	Username string
	Password string
}

// DeviceInfo represents basic device information
type DeviceInfo struct {
	SerialNumber string
	Model        string
	Manufacturer string
	MacAddress   string
}

// Status represents the current mining status
type Status struct {
	State           sdk.HealthStatus
	ErrorMessage    string
	FirmwareVersion string
}

// Telemetry represents device telemetry data
type Telemetry struct {
	// Device-level aggregates
	HashrateHS         *float64
	TemperatureCelsius *float64
	FanRPM             *float64
	UptimeSeconds      *int64

	// Power and efficiency (from RPC stats command, not all firmware versions report these)
	PowerWatts         *float64
	EfficiencyJPerHash *float64

	// Component-level metrics
	HashBoards        []HashBoardTelemetry
	Fans              []FanTelemetry
	HardwareErrorRate *float64
}

// HashBoardTelemetry represents per-hashboard (chain) telemetry
type HashBoardTelemetry struct {
	Index            int
	SerialNumber     string
	HashrateHS       *float64
	Temperature      *float64
	InletTemp        *float64
	OutletTemp       *float64
	ChipCount        int
	ChipFrequencyMHz int
	HardwareErrors   int
}

// FanTelemetry represents per-fan telemetry
type FanTelemetry struct {
	Index int
	RPM   int
}

// Pool represents a mining pool configuration
type Pool struct {
	Priority   int
	URL        string
	WorkerName string
}

// NewClient creates a new Antminer client
func NewClient(host string, rpcPort, webPort int32, urlScheme string) (*Client, error) {
	dialTimeout := DefaultDialTimeout
	readTimeout := DefaultReadTimeout

	// Create protocol from scheme
	protocol, err := networking.ProtocolFromString(urlScheme)
	if err != nil {
		return nil, fmt.Errorf("invalid URL scheme: %w", err)
	}

	// Create connection info for web API
	connInfo, err := networking.NewConnectionInfo(host, fmt.Sprintf("%d", webPort), protocol)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection info: %w", err)
	}

	client := &Client{
		connectInfo: &web.AntminerConnectionInfo{
			ConnectionInfo: *connInfo,
			Creds:          sdk.UsernamePassword{}, // Empty credentials initially
		},
		host:      host,
		rpcPort:   rpcPort,
		webPort:   webPort,
		urlScheme: urlScheme,
		httpClient: &http.Client{
			Timeout: defaultTimeOut,
		},
		webClient: web.NewService(),
		rpcClient: rpc.NewService(rpc.WithDialTimeout(dialTimeout), rpc.WithReadTimeout(readTimeout)),
	}

	return client, nil
}

// getWebConnectionInfo creates connection info for web API calls
func (c *Client) getWebConnectionInfo() *web.AntminerConnectionInfo {
	var creds sdk.UsernamePassword
	if c.credentials != nil {
		creds = *c.credentials
	}

	return web.NewAntminerConnectionInfo(
		c.connectInfo.ConnectionInfo,
		creds,
	)
}

// getRPCConnectionInfo creates connection info for RPC calls (uses TCP protocol)
func (c *Client) getRPCConnectionInfo() (*networking.ConnectionInfo, error) {
	connInfo, err := networking.NewConnectionInfo(c.host, fmt.Sprintf("%d", c.rpcPort), networking.ProtocolTCP)
	if err != nil {
		return nil, fmt.Errorf("failed to create RPC connection info: %w", err)
	}
	return connInfo, nil
}

// Close closes the client and cleans up resources
func (c *Client) Close() {
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
}

// GetDeviceInfo retrieves basic device information
func (c *Client) GetDeviceInfo(ctx context.Context) (*DeviceInfo, error) {
	// Get version info via RPC
	versionResp, err := c.GetVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get version info: %w", err)
	}

	if len(versionResp.Version) == 0 {
		return nil, fmt.Errorf("no version information available")
	}

	version := versionResp.Version[0]

	// Try to get system info via web API if credentials are available
	var serialNumber, macAddress string
	if c.credentials != nil {
		connInfo := c.getWebConnectionInfo()
		if systemInfo, err := c.webClient.GetSystemInfo(ctx, connInfo); err == nil {
			serialNumber = systemInfo.SerialNumber
			macAddress = systemInfo.MacAddr
		}
	}

	return &DeviceInfo{
		SerialNumber: serialNumber,
		Model:        version.Type,
		Manufacturer: "Bitmain",
		MacAddress:   macAddress,
	}, nil
}

// GetStatus retrieves the current mining status
func (c *Client) GetStatus(ctx context.Context) (*Status, error) {
	summaryResp, err := c.GetSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get summary: %w", err)
	}

	if len(summaryResp.Summary) == 0 {
		return nil, fmt.Errorf("no summary information available")
	}

	summary := summaryResp.Summary[0]

	// Determine state based on hashrate (not cumulative HardwareErrors counter).
	// When hashrate is zero and credentials are available, also check the work mode
	// to distinguish intentional sleep from an unexpected stop (matching ASIC-RS
	// parse_is_mining behaviour: check work-mode first, fall back to hashrate).
	var state sdk.HealthStatus
	if summary.GHS5s > 0 {
		state = sdk.HealthHealthyActive
	} else if c.credentials != nil {
		connInfo := c.getWebConnectionInfo()
		config, err := c.webClient.GetMinerConfig(ctx, connInfo)
		if err == nil {
			workMode := config.BitmainWorkMode
			if config.MinerMode != "" {
				workMode = web.BitmainWorkMode(config.MinerMode)
			}
			if workMode == web.BitmainWorkModeSleep {
				state = sdk.HealthHealthyInactive
			} else {
				// Zero hashrate while not in sleep mode — device should be mining but isn't.
				state = sdk.HealthWarning
			}
		} else {
			state = sdk.HealthHealthyInactive
		}
	} else {
		state = sdk.HealthHealthyInactive
	}

	// Get firmware version
	versionResp, err := c.GetVersion(ctx)
	firmwareVersion := ""
	if err == nil && len(versionResp.Version) > 0 {
		firmwareVersion = versionResp.Version[0].BMMiner
	}

	return &Status{
		State:           state,
		ErrorMessage:    "",
		FirmwareVersion: firmwareVersion,
	}, nil
}

// extractTelemetryFromStats extracts comprehensive telemetry data from stats.cgi response
func extractTelemetryFromStats(stats *web.StatsInfo) (*Telemetry, error) {
	if len(stats.STATS) == 0 {
		return nil, fmt.Errorf("stats response contains no data")
	}

	statsData := stats.STATS[0]

	// Validate critical fields
	if statsData.ChainNum == 0 {
		return nil, fmt.Errorf("invalid chain count: %d", statsData.ChainNum)
	}

	if len(statsData.Chain) == 0 {
		return nil, fmt.Errorf("stats response contains no chain data")
	}

	// Calculate device-level aggregates
	maxTemp, err := calculateMaxTemperature(statsData.Chain)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate max temperature: %w", err)
	}

	maxFanRPM := calculateMaxFanSpeed(statsData.Fan)
	hwErrorRate := statsData.HWPTotal

	// Extract per-chain (hashboard) telemetry
	hashBoards := make([]HashBoardTelemetry, 0, len(statsData.Chain))
	for _, chain := range statsData.Chain {
		// Validate hashrate
		if chain.RateReal < 0 {
			return nil, fmt.Errorf("invalid hashrate for chain %d: %f GH/s", chain.Index, chain.RateReal)
		}

		// Validate temperature array length before slicing
		if len(chain.TempChip) < tempSensorCount {
			return nil, fmt.Errorf("chain %d has insufficient temperature sensors: got %d, expected %d",
				chain.Index, len(chain.TempChip), tempSensorCount)
		}

		hashrate := chain.RateReal * GHSToHS
		temp, err := calculateMaxTemperatureFromArray(chain.TempChip)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate temperature for chain %d: %w", chain.Index, err)
		}

		inletTemp, err := calculateAverageTemperatureFromArray(chain.TempChip[inletTempStartIndex:inletTempEndIndex])
		if err != nil {
			return nil, fmt.Errorf("failed to calculate inlet temperature for chain %d: %w", chain.Index, err)
		}

		outletTemp, err := calculateAverageTemperatureFromArray(chain.TempChip[outletTempStartIndex:outletTempEndIndex])
		if err != nil {
			return nil, fmt.Errorf("failed to calculate outlet temperature for chain %d: %w", chain.Index, err)
		}

		hashBoards = append(hashBoards, HashBoardTelemetry{
			Index:            chain.Index,
			SerialNumber:     chain.SN,
			HashrateHS:       &hashrate,
			Temperature:      &temp,
			InletTemp:        &inletTemp,
			OutletTemp:       &outletTemp,
			ChipCount:        chain.ASICNum,
			ChipFrequencyMHz: chain.FreqAvg,
			HardwareErrors:   chain.HW,
		})
	}

	// Extract per-fan telemetry
	fans := make([]FanTelemetry, len(statsData.Fan))
	for i, rpm := range statsData.Fan {
		fans[i] = FanTelemetry{
			Index: i,
			RPM:   rpm,
		}
	}

	uptime := int64(statsData.Elapsed)

	return &Telemetry{
		TemperatureCelsius: &maxTemp,
		FanRPM:             &maxFanRPM,
		UptimeSeconds:      &uptime,
		HashBoards:         hashBoards,
		Fans:               fans,
		HardwareErrorRate:  &hwErrorRate,
		// HashrateHS will be set from summary.cgi by GetTelemetry()
	}, nil
}

// calculateMaxTemperature calculates the maximum temperature across all chains
// Returns an error if no valid temperature readings are found
func calculateMaxTemperature(chains []web.ChainStats) (float64, error) {
	if len(chains) == 0 {
		return 0, fmt.Errorf("no chains provided")
	}

	maxTemp := minValidTemperature
	validTempFound := false

	for _, chain := range chains {
		chainMax, err := calculateMaxTemperatureFromArray(chain.TempChip)
		if err != nil {
			// Skip chains with no valid temperatures rather than failing completely
			// but log the issue for debugging
			slog.Warn("Skipping chain due to invalid temperatures", "chain_index", chain.Index, "error", err)
			continue
		}
		validTempFound = true
		if chainMax > maxTemp {
			maxTemp = chainMax
		}
	}

	if !validTempFound {
		return 0, fmt.Errorf("no valid temperature readings found across %d chains", len(chains))
	}

	return maxTemp, nil
}

// calculateMaxTemperatureFromArray returns the maximum temperature from an array
// Returns an error if no valid temperatures are found in the array
func calculateMaxTemperatureFromArray(temps []float64) (float64, error) {
	if len(temps) == 0 {
		return 0, fmt.Errorf("empty temperature array")
	}

	maxTemp := minValidTemperature
	validTempFound := false

	for _, temp := range temps {
		if temp > minValidTemperature {
			validTempFound = true
			if temp > maxTemp {
				maxTemp = temp
			}
		}
	}

	if !validTempFound {
		return 0, fmt.Errorf("no valid temperature readings in array (all temps <= %.2f°C)", minValidTemperature)
	}

	return maxTemp, nil
}

// calculateAverageTemperatureFromArray calculates average temperature from an array
// Returns an error if no valid temperatures are found in the array
func calculateAverageTemperatureFromArray(temps []float64) (float64, error) {
	if len(temps) == 0 {
		return 0, fmt.Errorf("empty temperature array")
	}

	totalTemp := 0.0
	tempCount := 0

	for _, temp := range temps {
		if temp > minValidTemperature {
			totalTemp += temp
			tempCount++
		}
	}

	if tempCount == 0 {
		return 0, fmt.Errorf("no valid temperature readings in array (all temps <= %.2f°C)", minValidTemperature)
	}

	return totalTemp / float64(tempCount), nil
}

// calculateMaxFanSpeed returns the maximum fan speed from the fan array
func calculateMaxFanSpeed(fans []int) float64 {
	maxRPM := 0
	for _, rpm := range fans {
		if rpm > maxRPM {
			maxRPM = rpm
		}
	}
	return float64(maxRPM)
}

// GetTelemetry retrieves device telemetry data
func (c *Client) GetTelemetry(ctx context.Context) (*Telemetry, error) {
	summaryResp, err := c.GetSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get summary: %w", err)
	}

	if len(summaryResp.Summary) == 0 {
		return nil, fmt.Errorf("no summary information available")
	}

	summary := summaryResp.Summary[0]

	// Get temperature, fan, and component-level metrics from stats.cgi
	if c.credentials == nil {
		return nil, fmt.Errorf("credentials required for telemetry collection")
	}

	connInfo := c.getWebConnectionInfo()
	statsInfo, err := c.webClient.GetStatsInfo(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats info: %w", err)
	}

	telemetry, err := extractTelemetryFromStats(statsInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to extract telemetry from stats: %w", err)
	}

	// Convert GH/s to H/s for device-level hashrate from RPC summary
	hashrateHS := summary.GHS5s * GHSToHS
	telemetry.HashrateHS = &hashrateHS

	// Try to get power from RPC stats (best-effort; not all firmware versions report it)
	rpcStatsResp, err := c.GetStats(ctx)
	if err != nil {
		slog.Debug("failed to get RPC stats for power data", "error", err)
	} else {
		telemetry.PowerWatts = parseWattageFromRPCStats(rpcStatsResp)
	}

	// Compute efficiency (J/H) if we have both power and hashrate
	if telemetry.PowerWatts != nil && hashrateHS > 0 {
		efficiency := *telemetry.PowerWatts / hashrateHS
		telemetry.EfficiencyJPerHash = &efficiency
	}

	return telemetry, nil
}

// parseWattageFromRPCStats extracts power (watts) from the RPC stats response.
// The STATS array's second element (index 1) contains the mining stats, which
// may include power data as "chain_power" (string like "3250 W") or "power"/"Power" (numeric).
// Returns nil if power data is not available (older firmware).
func parseWattageFromRPCStats(resp *rpc.StatsResponse) *float64 {
	if resp == nil || len(resp.Stats) < 2 {
		return nil
	}

	var statsData map[string]json.RawMessage
	if err := json.Unmarshal(resp.Stats[1], &statsData); err != nil {
		return nil
	}

	// Try chain_power first (string "3250 W" or "3250.00" format)
	if raw, ok := statsData["chain_power"]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			parts := strings.Fields(s)
			if len(parts) > 0 {
				if watts, err := strconv.ParseFloat(parts[0], 64); err == nil && watts > 0 {
					return &watts
				}
			}
		}
		// Also try as a bare number
		var watts float64
		if json.Unmarshal(raw, &watts) == nil && watts > 0 {
			return &watts
		}
	}

	// Fallback to numeric "power" or "Power" field
	for _, key := range []string{"power", "Power"} {
		if raw, ok := statsData[key]; ok {
			var watts float64
			if json.Unmarshal(raw, &watts) == nil && watts > 0 {
				return &watts
			}
		}
	}

	return nil
}

// Pair performs device pairing (authentication setup)
func (c *Client) Pair(ctx context.Context, credentials sdk.UsernamePassword) error {
	err := c.SetCredentials(credentials)
	if err != nil {
		return fmt.Errorf("failed to set credentials: %w", err)
	}

	_, err = c.webClient.GetSystemInfo(ctx, c.getWebConnectionInfo()) // Test if credentials are valid
	if err != nil {
		return fmt.Errorf("failed to pair with device: %w", err)
	}

	return nil
}

// StartMining starts mining operations
func (c *Client) StartMining(ctx context.Context) error {
	return c.setWorkMode(ctx, web.BitmainWorkModeStart)
}

// StopMining stops mining operations by putting the device into sleep mode.
func (c *Client) StopMining(ctx context.Context) error {
	return c.setWorkMode(ctx, web.BitmainWorkModeSleep)
}

// setWorkMode fetches the current miner config, updates the work mode, and applies it.
//
// Older Antminer firmware uses a "miner-mode" field; newer firmware uses
// "bitmain-work-mode". Both encode the same values ("0" = normal, "1" = sleep).
// We detect which field the device uses at runtime by checking which one is
// non-empty in the GET response, matching the ASIC-RS approach.
func (c *Client) setWorkMode(ctx context.Context, mode web.BitmainWorkMode) error {
	connInfo := c.getWebConnectionInfo()
	config, err := c.webClient.GetMinerConfig(ctx, connInfo)
	if err != nil {
		return fmt.Errorf("failed to get current miner config: %w", err)
	}
	// Legacy devices return "miner-mode"; modern devices return "bitmain-work-mode".
	// Update whichever field the device reported so we don't clobber the wrong one.
	if config.MinerMode != "" {
		config.MinerMode = string(mode)
	} else {
		config.BitmainWorkMode = mode
	}
	return c.webClient.SetMinerConfig(ctx, connInfo, config)
}

// SetCoolingMode sets the cooling mode
func (c *Client) SetCoolingMode(_ context.Context, _ web.CoolingMode) error {
	return fmt.Errorf("cooling mode control is not supported for antminer devices")
}

// UpdatePools updates mining pool configuration
func (c *Client) UpdatePools(ctx context.Context, pools []Pool) error {
	if c.credentials == nil {
		return fmt.Errorf("credentials required for pool configuration")
	}

	connInfo := c.getWebConnectionInfo()

	// Get current config
	config, err := c.webClient.GetMinerConfig(ctx, connInfo)
	if err != nil {
		return fmt.Errorf("failed to get current config: %w", err)
	}

	// Convert pools to web API format
	webPools := make([]web.Pool, len(pools))
	for i, pool := range pools {
		webPools[i] = web.Pool{
			URL:      pool.URL,
			Username: pool.WorkerName,
		}
	}

	// Update pools in config
	config.Pools = webPools

	// Set the updated config
	return c.webClient.SetMinerConfig(ctx, connInfo, config)
}

// GetMinerConfig retrieves the current miner configuration
func (c *Client) GetMinerConfig(ctx context.Context) (*web.MinerConfig, error) {
	if c.credentials == nil {
		return nil, fmt.Errorf("credentials required for miner configuration")
	}

	connInfo := c.getWebConnectionInfo()
	return c.webClient.GetMinerConfig(ctx, connInfo)
}

// SetMinerConfig updates the miner configuration
func (c *Client) SetMinerConfig(ctx context.Context, config *web.MinerConfig) error {
	if c.credentials == nil {
		return fmt.Errorf("credentials required for miner configuration")
	}

	connInfo := c.getWebConnectionInfo()
	return c.webClient.SetMinerConfig(ctx, connInfo, config)
}

// BlinkLED triggers LED identification
func (c *Client) BlinkLED(ctx context.Context, duration time.Duration) error {
	if c.credentials == nil {
		return fmt.Errorf("credentials required for LED blink")
	}

	connInfo := c.getWebConnectionInfo()

	// Start blinking
	if err := c.webClient.StartBlink(ctx, connInfo); err != nil {
		return fmt.Errorf("failed to start LED blink: %w", err)
	}

	if !c.stopBlinkMux.TryLock() {
		return fmt.Errorf("LED is already blinking")
	}
	time.AfterFunc(duration, func() {
		// Stop blinking after duration
		_ = c.webClient.StopBlink(context.Background(), connInfo)
		c.stopBlinkMux.Unlock()
	})

	return nil
}

// GetLogs retrieves device logs from the kernel log endpoint.
// The since parameter is not used as Antminer doesn't support time-based filtering.
// The maxLines parameter is not used as Antminer returns the full log.
// Returns the log content, a boolean indicating if there are more logs (always false for Antminer),
// and any error encountered.
func (c *Client) GetLogs(ctx context.Context, _ *time.Time, _ int) (string, bool, error) {
	if c.credentials == nil {
		return "", false, fmt.Errorf("credentials required for log download")
	}

	connInfo := c.getWebConnectionInfo()
	logs, err := c.webClient.GetKernelLog(ctx, connInfo)
	if err != nil {
		return "", false, fmt.Errorf("failed to get kernel log: %w", err)
	}

	return logs, false, nil
}

// Reboot reboots the device
func (c *Client) Reboot(ctx context.Context) error {
	if c.credentials == nil {
		return fmt.Errorf("credentials required for reboot")
	}

	connInfo := c.getWebConnectionInfo()
	return c.webClient.Reboot(ctx, connInfo)
}

// ChangePassword updates the miner web UI password
func (c *Client) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	if c.credentials == nil {
		return fmt.Errorf("credentials required for password change")
	}

	connInfo := c.getWebConnectionInfo()
	if err := c.webClient.ChangePassword(ctx, connInfo, currentPassword, newPassword); err != nil {
		return fmt.Errorf("failed to change password: %w", err)
	}

	// Update stored credentials with new password
	c.credentials.Password = newPassword
	c.connectInfo.Creds.Password = newPassword

	return nil
}

// Deprecated: use UploadFirmware instead.
func (c *Client) UpdateFirmware(ctx context.Context) error {
	return fmt.Errorf("firmware update not implemented for Antminer")
}

// UploadFirmware uploads a firmware file to the Antminer via the CGI upgrade endpoint.
func (c *Client) UploadFirmware(ctx context.Context, firmware sdk.FirmwareFile) error {
	if c.credentials == nil {
		return fmt.Errorf("credentials required for firmware upload")
	}

	connInfo := c.getWebConnectionInfo()
	return c.webClient.UploadFirmware(ctx, connInfo, firmware)
}

func (c *Client) SetCredentials(creds sdk.UsernamePassword) error {
	c.credentials = &creds
	// Update the connectInfo credentials as well
	if c.connectInfo != nil {
		c.connectInfo.Creds = creds
	}
	return nil
}

// RPC methods - delegate to the RPC client

// GetVersion gets version information via RPC
func (c *Client) GetVersion(ctx context.Context) (*rpc.VersionResponse, error) {
	connInfo, err := c.getRPCConnectionInfo()
	if err != nil {
		return nil, err
	}
	return c.rpcClient.GetVersion(ctx, connInfo)
}

// GetSummary gets mining summary information via RPC
func (c *Client) GetSummary(ctx context.Context) (*rpc.SummaryResponse, error) {
	connInfo, err := c.getRPCConnectionInfo()
	if err != nil {
		return nil, err
	}
	resp, err := c.rpcClient.GetSummary(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// GetDevs gets device (ASIC) information via RPC
func (c *Client) GetDevs(ctx context.Context) (*rpc.DevsResponse, error) {
	connInfo, err := c.getRPCConnectionInfo()
	if err != nil {
		return nil, err
	}
	resp, err := c.rpcClient.GetDevs(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// GetPools gets pool information via RPC
func (c *Client) GetPools(ctx context.Context) (*rpc.PoolsResponse, error) {
	connInfo, err := c.getRPCConnectionInfo()
	if err != nil {
		return nil, err
	}
	return c.rpcClient.GetPools(ctx, connInfo)
}

// GetStats gets mining stats via RPC (includes power data on supported firmware)
func (c *Client) GetStats(ctx context.Context) (*rpc.StatsResponse, error) {
	connInfo, err := c.getRPCConnectionInfo()
	if err != nil {
		return nil, err
	}
	return c.rpcClient.GetStats(ctx, connInfo)
}

// GetStatsInfo gets comprehensive stats via Web API
func (c *Client) GetStatsInfo(ctx context.Context) (*web.StatsInfo, error) {
	if c.credentials == nil {
		return nil, fmt.Errorf("credentials required for stats info")
	}

	connInfo := c.getWebConnectionInfo()
	return c.webClient.GetStatsInfo(ctx, connInfo)
}
