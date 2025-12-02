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
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/networking"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/web"
	"github.com/btc-mining/proto-fleet/server/sdk/v1"
)

const (
	// GHSToHS converts GH/s to H/s
	GHSToHS = 1e9
	// THSToGHS converts TH/s to GH/s
	THSToGHS       = 1000
	defaultTimeOut = 30 * time.Second
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
	HashrateHS         *float64
	PowerWatts         *float64
	TemperatureCelsius *float64
	EfficiencyJPerHash *float64
	FanRPM             *float64
	UptimeSeconds      *int64
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

	// Determine state based on hashrate and errors
	var state sdk.HealthStatus
	if summary.GHS5s > 0 {
		state = sdk.HealthHealthyActive
	} else {
		state = sdk.HealthHealthyInactive
	}

	// Check for errors
	errorMessage := ""
	if summary.HardwareErrors > 0 {
		state = sdk.HealthCritical
		errorMessage = fmt.Sprintf("Hardware errors: %d", summary.HardwareErrors)
	}

	// Get firmware version
	versionResp, err := c.GetVersion(ctx)
	firmwareVersion := ""
	if err == nil && len(versionResp.Version) > 0 {
		firmwareVersion = versionResp.Version[0].BMMiner
	}

	return &Status{
		State:           state,
		ErrorMessage:    errorMessage,
		FirmwareVersion: firmwareVersion,
	}, nil
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

	// Get device information for temperature
	devsResp, err := c.GetDevs(ctx)
	var avgTemp float64
	var fanRPM float64
	if err == nil && len(devsResp.Devs) > 0 {
		tempSum := 0.0
		tempCount := 0
		for _, dev := range devsResp.Devs {
			temp := dev.GetTemperature()
			if temp > 0 {
				tempSum += temp
				tempCount++
			}
		}
		if tempCount > 0 {
			avgTemp = tempSum / float64(tempCount)
		}
	}

	// Try to get more detailed information from web API if available
	if c.credentials != nil {
		connInfo := c.getWebConnectionInfo()
		if webSummary, err := c.webClient.GetMinerSummary(ctx, connInfo); err == nil && len(webSummary.Summary) > 0 {
			// Use web API data if available
			webSum := webSummary.Summary[0]
			if webSum.Rate5s > 0 {
				// Web API typically provides TH/s, convert to GH/s
				summary.GHS5s = webSum.Rate5s * THSToGHS
			}
		}
	}

	// Convert GH/s to H/s
	hashrateHS := summary.GHS5s * GHSToHS

	return &Telemetry{
		HashrateHS:         &hashrateHS,
		TemperatureCelsius: &avgTemp,
		FanRPM:             &fanRPM,
		UptimeSeconds:      &summary.Elapsed,
	}, nil
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
	err := c.webClient.SetMinerConfig(ctx, c.getWebConnectionInfo(), &web.MinerConfig{
		BitmainWorkMode: web.BitmainWorkModeStart,
	})
	return err
}

// StopMining stops mining operations
func (c *Client) StopMining(ctx context.Context) error {
	err := c.webClient.SetMinerConfig(ctx, c.getWebConnectionInfo(), &web.MinerConfig{
		BitmainWorkMode: web.BitmainWorkModeSleep,
	})
	return err
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

// GetLogs retrieves device logs
func (c *Client) GetLogs(ctx context.Context, _ *time.Time, _ int) (string, bool, error) {
	// This would typically involve web API calls to retrieve logs
	return "", false, fmt.Errorf("log retrieval not implemented for Antminer")
}

// Reboot reboots the device
func (c *Client) Reboot(ctx context.Context) error {
	if c.credentials == nil {
		return fmt.Errorf("credentials required for reboot")
	}

	connInfo := c.getWebConnectionInfo()
	return c.webClient.Reboot(ctx, connInfo)
}

// UpdateFirmware initiates firmware update
func (c *Client) UpdateFirmware(ctx context.Context) error {
	// This would typically involve web API calls for firmware management
	return fmt.Errorf("firmware update not implemented for Antminer")
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
