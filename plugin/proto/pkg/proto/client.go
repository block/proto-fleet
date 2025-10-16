// Package proto provides a client for communicating with Proto miners.
//
// This package demonstrates:
//   - HTTP/HTTPS client management
//   - Connect-RPC integration
//   - Protocol negotiation and fallback
//   - Structured API communication
//   - Error handling and retry logic
//
// The client abstracts the Proto miner API and provides
// a clean interface for the plugin to use.
package proto

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api/miner_data_apiconnect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
	"github.com/btc-mining/proto-fleet/server/sdk/v1"
	"golang.org/x/net/http2"
)

var (
	httpClient      *http.Client
	httpsClient     *http.Client
	httpClientOnce  = &sync.Once{}
	httpsClientOnce = &sync.Once{}
)

// Client provides communication with a Proto miner.
//
// It manages HTTP connections, authentication, and API calls.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	bearerToken sdk.BearerToken

	// Connect-RPC clients for different API services
	dataClient    miner_data_apiconnect.MinerDataApiClient
	commandClient miner_command_apiconnect.MinerCommandApiClient
	systemClient  miner_system_apiconnect.MinerSystemApiClient
	pairingClient miner_system_apiconnect.MinerPairingApiClient
}

// DeviceInfo represents basic device information.
type DeviceInfo struct {
	SerialNumber string
	MacAddress   string
	Model        string
	Manufacturer string
}

// Status represents the current status of a miner.
type Status struct {
	State           sdk.HealthStatus
	ErrorMessage    string
	FirmwareVersion string
}

// Telemetry represents telemetry data from a miner.
type Telemetry struct {
	HashrateHS         float64
	PowerWatts         float64
	TemperatureCelsius float64
	EfficiencyJPerHash float64
	FanRPM             int32
	UptimeSeconds      int64
	TimeInterval       time.Duration
}

// Pool represents a mining pool configuration.
type Pool struct {
	Priority   int
	URL        string
	WorkerName string
}

// AuthTokenContextKey is the key used to store auth tokens in context
type contextKey string

const AuthTokenContextKey contextKey = "auth_token"

// authInterceptor handles Bearer token injection for the plugin client
type authInterceptor struct{}

// newAuthInterceptor creates a new auth interceptor
func newAuthInterceptor() connect.Interceptor {
	return &authInterceptor{}
}

// WrapUnary implements the connect.Interceptor interface
func (i *authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		// Extract auth token from context
		if token := getAuthTokenFromContext(ctx); token != "" {
			req.Header().Set("Authorization", "Bearer "+token)
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient implements the connect.Interceptor interface
func (i *authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements the connect.Interceptor interface
func (i *authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// getAuthTokenFromContext extracts the auth token from context
func getAuthTokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(AuthTokenContextKey).(string); ok {
		return token
	}
	return ""
}

// withAuthToken adds an auth token to the context
func withAuthToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, AuthTokenContextKey, token)
}

// NewClient creates a new Proto miner client.
//
// This function demonstrates:
//   - HTTP client configuration for different protocols
//   - Connect-RPC client setup
//   - TLS configuration and security settings
func NewClient(host string, port int32, scheme string) (*Client, error) {
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, host, port)

	// Create HTTP client based on scheme
	var httpClient *http.Client
	if scheme == "https" {
		httpClient = createHTTPSClient()
	} else {
		httpClient = createHTTPClient()
	}

	// Create Connect-RPC clients with auth interceptor
	clientOptions := []connect.ClientOption{
		connect.WithGRPC(),
		connect.WithInterceptors(newAuthInterceptor()),
	}

	client := &Client{
		baseURL:       baseURL,
		httpClient:    httpClient,
		dataClient:    miner_data_apiconnect.NewMinerDataApiClient(httpClient, baseURL, clientOptions...),
		commandClient: miner_command_apiconnect.NewMinerCommandApiClient(httpClient, baseURL, clientOptions...),
		systemClient:  miner_system_apiconnect.NewMinerSystemApiClient(httpClient, baseURL, clientOptions...),
		pairingClient: miner_system_apiconnect.NewMinerPairingApiClient(httpClient, baseURL, clientOptions...),
	}

	return client, nil
}

// createHTTPSClient creates an HTTPS client with proper TLS configuration.
func createHTTPSClient() *http.Client {
	httpsClientOnce.Do(func() {
		transport := &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: shouldSkipTLSVerification(), // #nosec G402 -- Configurable via environment for development/testing
				MinVersion:         tls.VersionTLS12,
			},
			ForceAttemptHTTP2: true,
		}

		httpsClient = &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}
	})
	return httpsClient
}

// createHTTPClient creates an HTTP client for cleartext HTTP/2 connections.
func createHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		transport := &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				dialer := &net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
					DualStack: true,
				}
				return dialer.DialContext(ctx, network, addr)
			},
			ReadIdleTimeout:  30 * time.Second,
			PingTimeout:      15 * time.Second,
			WriteByteTimeout: 10 * time.Second,
		}

		httpClient = &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		}

	})
	return httpClient
}

// shouldSkipTLSVerification checks environment variables for TLS verification settings.
func shouldSkipTLSVerification() bool {
	skipVerify := strings.ToLower(os.Getenv("SKIP_TLS_VERIFY"))
	insecureTLS := strings.ToLower(os.Getenv("INSECURE_TLS"))
	return skipVerify == "true" || insecureTLS == "true"
}

// SetCredentials sets authentication credentials for API calls.
func (c *Client) SetCredentials(bearerToken sdk.BearerToken) error {
	c.bearerToken = bearerToken
	return nil
}

// Close closes the client and cleans up resources.
func (c *Client) Close() error {
	// HTTP clients don't need explicit cleanup
	return nil
}

// GetDeviceInfo retrieves basic device information.
//
// This method demonstrates:
//   - API call patterns
//   - Error handling and conversion
//   - Data structure mapping
func (c *Client) GetDeviceInfo(ctx context.Context) (*DeviceInfo, error) {
	// Add authentication if available
	ctx = c.withAuth(ctx)

	// Get pairing info which contains device identification
	resp, err := c.pairingClient.GetPairingInfo(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return nil, fmt.Errorf("failed to get pairing info: %w", err)
	}

	return &DeviceInfo{
		SerialNumber: resp.Msg.CbSn,
		MacAddress:   resp.Msg.Mac,
		Model:        "Rig", // TODO(DASH-782): Get actual model from API when available
		Manufacturer: "Proto",
	}, nil
}

// GetStatus retrieves the current miner status.
func (c *Client) GetStatus(ctx context.Context) (*Status, error) {
	ctx = c.withAuth(ctx)

	resp, err := c.dataClient.GetMiningStatus(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return nil, fmt.Errorf("failed to get mining status: %w", err)
	}

	// Convert miner state to string
	var state sdk.HealthStatus
	switch resp.Msg.State {
	case miner_data_api.MiningState_MINING_STATE_MINING:
		state = sdk.HealthHealthyActive
	case miner_data_api.MiningState_MINING_STATE_DEGRADED_MINING:
		state = sdk.Warning
	case miner_data_api.MiningState_MINING_STATE_STOPPED:
		state = sdk.HealthHealthyInactive
	case miner_data_api.MiningState_MINING_STATE_UNKNOWN:
		state = sdk.Unknown
	case miner_data_api.MiningState_MINING_STATE_UNINITIALIZED:
		state = sdk.Unknown
	case miner_data_api.MiningState_MINING_STATE_POWERING_ON:
		state = sdk.HealthyInactive
	case miner_data_api.MiningState_MINING_STATE_POWERING_OFF:
		state = sdk.Unknown
	case miner_data_api.MiningState_MINING_STATE_NO_POOLS:
		state = sdk.HealthUnknown
	case miner_data_api.MiningState_MINING_STATE_ERROR:
		state = sdk.Critical
	default:
		state = sdk.Unknown
	}

	return &Status{
		State:           state,
		ErrorMessage:    "", // TODO: Extract from API when available
		FirmwareVersion: "", // TODO: Get from API when available
	}, nil
}

func timeToAPITimestamp(t time.Time) *miner_common_api.Timestamp {
	if t.IsZero() {
		return nil
	}
	return &miner_common_api.Timestamp{
		Seconds: func() uint64 {
			s := t.Unix()
			if s < 0 {
				return 0
			}
			return uint64(s)
		}(),
		Nanos: func() uint32 {
			n := t.Nanosecond()
			if n < 0 || n > math.MaxUint32 {
				return 0
			}
			return uint32(n)
		}(),
	}
}

// GetTelemetry retrieves telemetry data from the miner.
func (c *Client) GetTelemetry(ctx context.Context) (*Telemetry, error) {
	ctx = c.withAuth(ctx)

	// Create time interval for recent data (last 5 minutes)
	now := time.Now()
	start := now.Add(-5 * time.Minute)

	timeInterval := &miner_common_api.Interval{
		StartTime: timeToAPITimestamp(start),
		EndTime:   timeToAPITimestamp(now),
	}

	errorCount := 0

	telemetry := &Telemetry{}

	// Get hashrate data
	hashrateResp, err := c.dataClient.GetTimeSeriesData(ctx, connect.NewRequest(&miner_data_api.TimeSeriesDataRequest{
		TimeInterval: timeInterval,
		DataType:     miner_data_api.DataType_DATA_TYPE_MINER_HASHRATE_MH_S,
		ComponentId: &miner_data_api.ComponentId{
			Id: &miner_data_api.ComponentId_ComponentId{ComponentId: 0},
		},
		HashrateType: miner_data_api.HashrateType_HASHRATE_TYPE_NORMAL,
	}))
	if err != nil {
		errorCount++
		slog.Error("Failed to get hashrate data", "error", err)
	}
	if err == nil && len(hashrateResp.Msg.DataPoints) > 0 {
		latest := hashrateResp.Msg.DataPoints[len(hashrateResp.Msg.DataPoints)-1]
		telemetry.HashrateHS = latest.Value * 1000000 // Convert MH/s to H/s
		telemetry.TimeInterval = time.Duration(hashrateResp.Msg.TimeInterval.Granularity) * time.Second
	}

	// Get power data
	powerResp, err := c.dataClient.GetTimeSeriesData(ctx, connect.NewRequest(&miner_data_api.TimeSeriesDataRequest{
		TimeInterval: timeInterval,
		DataType:     miner_data_api.DataType_DATA_TYPE_MINER_POWER_W,
		ComponentId: &miner_data_api.ComponentId{
			Id: &miner_data_api.ComponentId_ComponentId{ComponentId: 0},
		},
	}))
	if err != nil {
		slog.Error("Failed to get power data", "error", err)
		errorCount++
	}
	if err == nil && len(powerResp.Msg.DataPoints) > 0 {
		latest := powerResp.Msg.DataPoints[len(powerResp.Msg.DataPoints)-1]
		telemetry.PowerWatts = latest.Value
		telemetry.TimeInterval = time.Duration(powerResp.Msg.TimeInterval.Granularity) * time.Second
	}

	// Get temperature data
	tempResp, err := c.dataClient.GetTimeSeriesData(ctx, connect.NewRequest(&miner_data_api.TimeSeriesDataRequest{
		TimeInterval: timeInterval,
		DataType:     miner_data_api.DataType_DATA_TYPE_MINER_TEMPERATURE_C,
		ComponentId: &miner_data_api.ComponentId{
			Id: &miner_data_api.ComponentId_ComponentId{ComponentId: 0},
		},
	}))
	if err != nil {
		slog.Error("Failed to get temperature data", "error", err)
		errorCount++
	}
	if err == nil && len(tempResp.Msg.DataPoints) > 0 {
		latest := tempResp.Msg.DataPoints[len(tempResp.Msg.DataPoints)-1]
		telemetry.TemperatureCelsius = latest.Value
		telemetry.TimeInterval = time.Duration(tempResp.Msg.TimeInterval.Granularity) * time.Second
	}

	if errorCount == 3 {
		return nil, fmt.Errorf("failed to get telemetry data")
	}

	return telemetry, nil
}

// Pair performs device pairing with the provided credentials.
func (c *Client) Pair(ctx context.Context, key sdk.APIKey) error {
	_, err := c.pairingClient.SetAuthKey(ctx, connect.NewRequest(&miner_system_api.SetAuthKeyRequest{
		PublicKey: key.Key,
	}))
	if err != nil {
		return fmt.Errorf("failed to set auth key: %w", err)
	}

	return nil
}

// StartMining starts mining operations.
func (c *Client) StartMining(ctx context.Context) error {
	ctx = c.withAuth(ctx)

	resp, err := c.commandClient.StartMining(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return fmt.Errorf("failed to start mining: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fmt.Errorf("start mining failed: %s", resp.Msg.Message)
	}

	return nil
}

// StopMining stops mining operations.
func (c *Client) StopMining(ctx context.Context) error {
	ctx = c.withAuth(ctx)

	resp, err := c.commandClient.StopMining(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return fmt.Errorf("failed to stop mining: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fmt.Errorf("stop mining failed: %s", resp.Msg.Message)
	}

	return nil
}

// SetCoolingMode configures the cooling system.
func (c *Client) SetCoolingMode(ctx context.Context, mode sdk.CoolingMode) error {
	ctx = c.withAuth(ctx)

	// Convert SDK cooling mode to API enum
	var apiMode miner_data_api.CoolingMode
	switch mode {
	case sdk.CoolingModeAirCooled:
		apiMode = miner_data_api.CoolingMode_COOLING_MODE_AUTO
	case sdk.CoolingModeManual:
		apiMode = miner_data_api.CoolingMode_COOLING_MODE_MANUAL
	case sdk.CoolingModeImmersionCooled:
		apiMode = miner_data_api.CoolingMode_COOLING_MODE_OFF
	case sdk.CoolingModeUnspecified:
		apiMode = miner_data_api.CoolingMode_COOLING_MODE_AUTO
	default:
		apiMode = miner_data_api.CoolingMode_COOLING_MODE_UNKNOWN
	}

	resp, err := c.commandClient.SetCoolingMode(ctx, connect.NewRequest(&miner_command_api.CoolingModeRequest{
		Mode: apiMode,
	}))
	if err != nil {
		return fmt.Errorf("failed to set cooling mode: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fmt.Errorf("set cooling mode failed: %s", resp.Msg.String())
	}

	return nil
}

// UpdatePools configures mining pools.
func (c *Client) UpdatePools(ctx context.Context, pools []Pool) error {
	ctx = c.withAuth(ctx)

	// Convert to API format
	apiPools := make([]*miner_data_api.Pool, len(pools))
	for i, pool := range pools {
		var priority = uint32(0)
		if pool.Priority > 0 && pool.Priority <= math.MaxUint32 {
			priority = uint32(pool.Priority) // #nosec G701 -- Range checked above
		}
		apiPools[i] = &miner_data_api.Pool{
			Priority: priority,
			Url:      pool.URL,
			Username: pool.WorkerName,
			Password: "", // The pool options for the proto miner do not use passwords, but field is still required for the api.
		}
	}

	// Remove existing pools first
	if poolsResp, err := c.dataClient.GetPools(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{})); err == nil {
		if len(poolsResp.Msg.Pools) > 0 {
			_, err := c.commandClient.RemovePools(ctx, connect.NewRequest(&miner_command_api.PoolsRequest{
				Pools: poolsResp.Msg.Pools,
			}))
			if err != nil {
				return fmt.Errorf("failed to remove existing pools: %w", err)
			}
		}
	}

	// Add new pools
	resp, err := c.commandClient.AddPools(ctx, connect.NewRequest(&miner_command_api.PoolsRequest{
		Pools: apiPools,
	}))
	if err != nil {
		return fmt.Errorf("failed to add pools: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fmt.Errorf("add pools failed: %s", resp.Msg.String())
	}

	return nil
}

// BlinkLED triggers LED identification.
func (c *Client) BlinkLED(ctx context.Context) error {
	ctx = c.withAuth(ctx)

	resp, err := c.commandClient.PlayLocateSequence(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return fmt.Errorf("failed to blink LED: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fmt.Errorf("blink LED failed: %s", resp.Msg.Result)
	}

	return nil
}

// GetLogs retrieves log data from the miner.
func (c *Client) GetLogs(ctx context.Context, _ *time.Time, maxLines int) (string, bool, error) {
	ctx = c.withAuth(ctx)

	var lines uint32
	if maxLines > 0 && maxLines <= math.MaxUint32 {
		lines = uint32(maxLines)
	}
	resp, err := c.systemClient.GetLogs(ctx, connect.NewRequest(&miner_system_api.GetLogsRequest{
		Lines:  &lines,
		Source: miner_system_api.LogSource_LOG_SOURCE_MINER_SW,
	}))
	if err != nil {
		return "", false, fmt.Errorf("failed to get logs: %w", err)
	}

	// Join log lines
	var logContent string
	if len(resp.Msg.Content) > 0 {
		logContent = strings.Join(resp.Msg.Content, "\n")
	}

	// We don't implement pagination, because the miner client does not support it.
	return logContent, false, nil
}

// Reboot reboots the miner.
func (c *Client) Reboot(ctx context.Context) error {
	ctx = c.withAuth(ctx)

	resp, err := c.systemClient.Reboot(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return fmt.Errorf("failed to reboot: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fmt.Errorf("reboot failed: %s", resp.Msg.Result)
	}

	return nil
}

// UpdateFirmware initiates a firmware update.
func (c *Client) UpdateFirmware(ctx context.Context) error {
	ctx = c.withAuth(ctx)

	_, err := c.systemClient.Update(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return fmt.Errorf("failed to update firmware: %w", err)
	}

	return nil
}

// withAuth adds authentication to the context if credentials are available.
func (c *Client) withAuth(ctx context.Context) context.Context {
	if c.bearerToken.Token != "" {
		return withAuthToken(ctx, c.bearerToken.Token)
	}
	return ctx
}
