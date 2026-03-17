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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_command_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_command_api/miner_command_apiconnect"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_data_api/miner_data_apiconnect"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_system_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_system_api/miner_system_apiconnect"
	sdk "github.com/proto-at-block/proto-fleet/server/sdk/v1"
	"golang.org/x/net/http2"
)

const (
	// HTTP client configuration
	httpMaxIdleConnections        = 100
	httpMaxIdleConnectionsPerHost = 10
	httpIdleConnectionTimeout     = 90 * time.Second
	httpTLSHandshakeTimeout       = 10 * time.Second
	httpResponseHeaderTimeout     = 30 * time.Second
	httpClientTimeout             = 30 * time.Second

	// HTTP dialer configuration
	httpDialerTimeout   = 10 * time.Second
	httpDialerKeepAlive = 30 * time.Second

	// HTTP/2 transport configuration
	http2ReadIdleTimeout  = 30 * time.Second
	http2PingTimeout      = 15 * time.Second
	http2WriteByteTimeout = 10 * time.Second

	// Firmware upload can transfer hundreds of megabytes over slow links.
	firmwareUploadTimeout = 30 * time.Minute
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
	baseURL      string
	webUIBaseURL string // Standard HTTP(S) port — used for web UI endpoints that enforce password auth
	httpClient   *http.Client
	webUIClient  *http.Client // HTTP/1.1 client for web UI port (80/443)
	bearerToken  sdk.BearerToken

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

// Pool represents a mining pool configuration.
type Pool struct {
	Priority   int
	URL        string
	WorkerName string
}

// TelemetryValues represents comprehensive telemetry data from a miner.
type TelemetryValues struct {
	Miner      *MinerTelemetry
	Hashboards []*HashboardTelemetry
	PSUs       []*PSUTelemetry
}

// MinerTelemetry represents device-level telemetry aggregates.
type MinerTelemetry struct {
	HashrateThS   float64
	TemperatureC  float64
	PowerW        float64
	EfficiencyJTh float64
}

// HashboardTelemetry represents per-hashboard metrics.
type HashboardTelemetry struct {
	Index               uint32
	SerialNumber        string
	HashrateThS         float64
	AverageTemperatureC float64
	InletTemperatureC   float64
	OutletTemperatureC  float64
	VoltageV            *float64 // optional
	CurrentA            *float64 // optional
	ASICs               *ASICTelemetry
}

// ASICTelemetry represents per-ASIC metrics (array-based).
type ASICTelemetry struct {
	HashrateThS  []float64
	TemperatureC []float64
}

// PSUTelemetry represents per-PSU metrics.
type PSUTelemetry struct {
	Index               uint32
	InputVoltageV       float64
	OutputVoltageV      float64
	InputCurrentA       float64
	OutputCurrentA      float64
	InputPowerW         float64
	OutputPowerW        float64
	HotspotTemperatureC float64
}

// PowerTargetInfo represents power target configuration and bounds from the miner.
type PowerTargetInfo struct {
	CurrentW uint32
	MinW     uint32
	MaxW     uint32
	DefaultW uint32
	Mode     sdk.PerformanceMode
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
	// Web UI is served on the standard port (80 for http, 443 for https), not the gRPC port.
	// The gRPC port authenticates via Bearer token and does not enforce password validation.
	webUIBaseURL := fmt.Sprintf("%s://%s", scheme, host)

	// Create HTTP client based on scheme
	var httpClient *http.Client
	if scheme == "https" {
		httpClient = createHTTPSClient()
	} else {
		httpClient = createHTTPClient()
	}

	// Web UI client uses HTTP/1.1 (not h2c) since the web UI port speaks HTTP/1.1
	var webUIClient *http.Client
	if scheme == "https" {
		webUIClient = createHTTPSClient()
	} else {
		webUIClient = &http.Client{Timeout: httpClientTimeout}
	}

	// Create Connect-RPC clients with auth interceptor
	clientOptions := []connect.ClientOption{
		connect.WithGRPC(),
		connect.WithInterceptors(newAuthInterceptor()),
	}

	client := &Client{
		baseURL:       baseURL,
		webUIBaseURL:  webUIBaseURL,
		httpClient:    httpClient,
		webUIClient:   webUIClient,
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
			MaxIdleConns:          httpMaxIdleConnections,
			MaxIdleConnsPerHost:   httpMaxIdleConnectionsPerHost,
			IdleConnTimeout:       httpIdleConnectionTimeout,
			TLSHandshakeTimeout:   httpTLSHandshakeTimeout,
			ResponseHeaderTimeout: httpResponseHeaderTimeout,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: shouldSkipTLSVerification(), // #nosec G402 -- Configurable via environment for development/testing
				MinVersion:         tls.VersionTLS12,
			},
			ForceAttemptHTTP2: true,
		}

		httpsClient = &http.Client{
			Transport: transport,
			Timeout:   httpClientTimeout,
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
					Timeout:   httpDialerTimeout,
					KeepAlive: httpDialerKeepAlive,
					DualStack: true,
				}
				return dialer.DialContext(ctx, network, addr)
			},
			ReadIdleTimeout:  http2ReadIdleTimeout,
			PingTimeout:      http2PingTimeout,
			WriteByteTimeout: http2WriteByteTimeout,
		}

		httpClient = &http.Client{
			Transport: transport,
			Timeout:   httpClientTimeout,
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

// GetSoftwareInfo retrieves software/firmware version information.
func (c *Client) GetSoftwareInfo(ctx context.Context) (*connect.Response[miner_data_api.SoftwareInfoResponse], error) {
	ctx = c.withAuth(ctx)
	return c.dataClient.GetSoftwareInfo(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
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
		Model:        "Rig", // TODO: Get actual model from API when available
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
		state = sdk.HealthWarning
	case miner_data_api.MiningState_MINING_STATE_STOPPED:
		state = sdk.HealthHealthyInactive
	case miner_data_api.MiningState_MINING_STATE_UNKNOWN:
		state = sdk.HealthUnknown
	case miner_data_api.MiningState_MINING_STATE_UNINITIALIZED:
		state = sdk.HealthUnknown
	case miner_data_api.MiningState_MINING_STATE_POWERING_ON:
		state = sdk.HealthHealthyInactive
	case miner_data_api.MiningState_MINING_STATE_POWERING_OFF:
		state = sdk.HealthUnknown
	case miner_data_api.MiningState_MINING_STATE_NO_POOLS:
		state = sdk.HealthNeedsMiningPool
	case miner_data_api.MiningState_MINING_STATE_ERROR:
		state = sdk.HealthCritical
	default:
		state = sdk.HealthUnknown
	}

	// The actual pool list is the source of truth, not MiningState (which can be stale).
	needsPool, err := c.checkNeedsMiningPool(ctx)
	if err != nil {
		slog.Warn("failed to check pool configuration", "error", err)
	} else if needsPool {
		state = sdk.HealthNeedsMiningPool
	} else if state == sdk.HealthNeedsMiningPool {
		// Firmware says NO_POOLS but pools are configured - use actual pool data
		state = sdk.HealthHealthyInactive
	}

	// Get firmware version from software info API
	firmwareVersion := ""
	swInfoResp, err := c.dataClient.GetSoftwareInfo(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		slog.Debug("failed to get software info", "error", err)
	} else if swInfoResp.Msg.SwInfo != nil {
		firmwareVersion = swInfoResp.Msg.SwInfo.Version
	}

	return &Status{
		State:           state,
		ErrorMessage:    "", // TODO: Extract from API when available
		FirmwareVersion: firmwareVersion,
	}, nil
}

// checkNeedsMiningPool checks if the miner has no active pools configured.
// Returns true if no pools are configured or all pools are dead/inactive.
func (c *Client) checkNeedsMiningPool(ctx context.Context) (bool, error) {
	poolsResp, err := c.dataClient.GetPools(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return false, fmt.Errorf("failed to get pools: %w", err)
	}

	// No pools configured at all
	if len(poolsResp.Msg.Pools) == 0 {
		return true, nil
	}

	// Check if any pool has a URL configured
	for _, pool := range poolsResp.Msg.Pools {
		if pool.Url != "" {
			return false, nil
		}
	}

	// All pools have empty URLs - effectively no pools configured
	return true, nil
}

// GetPools retrieves the currently configured pools from the miner.
func (c *Client) GetPools(ctx context.Context) ([]sdk.ConfiguredPool, error) {
	ctx = c.withAuth(ctx)

	poolsResp, err := c.dataClient.GetPools(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return nil, fmt.Errorf("failed to get pools: %w", err)
	}

	pools := make([]sdk.ConfiguredPool, 0, len(poolsResp.Msg.Pools))
	for _, pool := range poolsResp.Msg.Pools {
		// Only include pools that have a URL configured
		if pool.Url != "" {
			pools = append(pools, sdk.ConfiguredPool{
				// #nosec G115 -- Pool priorities are protocol-bounded (0-2 for default/backup1/backup2)
				Priority: int32(pool.Priority),
				URL:      pool.Url,
				Username: pool.Username,
			})
		}
	}

	return pools, nil
}

// GetTelemetryValues retrieves comprehensive telemetry data from the miner.
//
// This method uses the GetTelemetryValues API which provides hierarchical
// telemetry data in a single call, including:
//   - Miner-level aggregates (hashrate, temp, power, efficiency)
//   - Per-hashboard metrics with optional per-ASIC details
//   - Per-PSU metrics
func (c *Client) GetTelemetryValues(ctx context.Context) (*TelemetryValues, error) {
	ctx = c.withAuth(ctx)

	resp, err := c.dataClient.GetTelemetryValues(ctx, connect.NewRequest(&miner_data_api.GetTelemetryValuesRequest{
		Levels: []miner_data_api.TelemetryLevel{
			miner_data_api.TelemetryLevel_TELEMETRY_LEVEL_MINER,
			miner_data_api.TelemetryLevel_TELEMETRY_LEVEL_HASHBOARD,
			miner_data_api.TelemetryLevel_TELEMETRY_LEVEL_PSU,
			miner_data_api.TelemetryLevel_TELEMETRY_LEVEL_ASIC,
		},
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to get telemetry values: %w", err)
	}

	return c.convertTelemetryValues(resp.Msg), nil
}

// convertTelemetryValues converts miner API telemetry response to client types.
func (c *Client) convertTelemetryValues(resp *miner_data_api.GetTelemetryValuesResponse) *TelemetryValues {
	result := &TelemetryValues{}

	// Convert miner-level telemetry
	if resp.Miner != nil {
		result.Miner = &MinerTelemetry{
			HashrateThS:   resp.Miner.HashrateThS,
			TemperatureC:  resp.Miner.TemperatureC,
			PowerW:        resp.Miner.PowerW,
			EfficiencyJTh: resp.Miner.EfficiencyJTh,
		}
	}

	// Convert hashboard telemetry
	if len(resp.Hashboards) > 0 {
		result.Hashboards = make([]*HashboardTelemetry, len(resp.Hashboards))
		for i, hb := range resp.Hashboards {
			result.Hashboards[i] = &HashboardTelemetry{
				Index:               hb.Index,
				SerialNumber:        hb.SerialNumber,
				HashrateThS:         hb.HashrateThS,
				AverageTemperatureC: hb.AverageTemperatureC,
				InletTemperatureC:   hb.InletTemperatureC,
				OutletTemperatureC:  hb.OutletTemperatureC,
				VoltageV:            hb.VoltageV,
				CurrentA:            hb.CurrentA,
			}

			// Convert ASIC telemetry
			if hb.Asics != nil {
				result.Hashboards[i].ASICs = &ASICTelemetry{
					HashrateThS:  hb.Asics.HashrateThS,
					TemperatureC: hb.Asics.TemperatureC,
				}
			}
		}
	}

	// Convert PSU telemetry
	if len(resp.Psus) > 0 {
		result.PSUs = make([]*PSUTelemetry, len(resp.Psus))
		for i, psu := range resp.Psus {
			result.PSUs[i] = &PSUTelemetry{
				Index:               psu.Index,
				InputVoltageV:       psu.InputVoltageV,
				OutputVoltageV:      psu.OutputVoltageV,
				InputCurrentA:       psu.InputCurrentA,
				OutputCurrentA:      psu.OutputCurrentA,
				InputPowerW:         psu.InputPowerW,
				OutputPowerW:        psu.OutputPowerW,
				HotspotTemperatureC: psu.HotspotTemperatureC,
			}
		}
	}

	return result
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

// ClearAuthKey clears the authentication key from the device during unpairing.
func (c *Client) ClearAuthKey(ctx context.Context) error {
	ctx = c.withAuth(ctx)
	resp, err := c.pairingClient.ClearAuthKey(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return fmt.Errorf("failed to clear auth key: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fmt.Errorf("clear auth key failed with result: %v", resp.Msg.Result)
	}

	return nil
}

// updatePasswordRequest represents the JSON request body for password change operations.
type updatePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// loginRequest represents the JSON request body for the miner login endpoint.
type loginRequest struct {
	Password string `json:"password"`
}

// authTokensResponse represents the JSON response from the miner login endpoint.
type authTokensResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// loginWithPassword authenticates via the miner's web UI port (80/443) and returns an
// access token. The gRPC port (2121) accepts any Bearer token without password validation,
// so all password-sensitive operations must go through the web UI port.
func (c *Client) loginWithPassword(ctx context.Context, password string) (string, error) {
	loginURL := fmt.Sprintf("%s/api/v1/auth/login", c.webUIBaseURL)

	bodyBytes, err := json.Marshal(loginRequest{Password: password})
	if err != nil {
		return "", fmt.Errorf("failed to marshal login request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.webUIClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("incorrect current password")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed with status %d", resp.StatusCode)
	}

	var tokens authTokensResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return "", fmt.Errorf("failed to decode login response: %w", err)
	}

	return tokens.AccessToken, nil
}

// ChangePassword updates the miner web UI password via the web UI port (80/443),
// mirroring the flow of the miner's own web UI. The fleet Bearer token is deliberately
// not used: the gRPC port (2121) returns 200 without actually applying the change when
// called with the fleet token, treating it as a privileged no-op.
func (c *Client) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	accessToken, err := c.loginWithPassword(ctx, currentPassword)
	if err != nil {
		return err
	}

	changeURL := fmt.Sprintf("%s/api/v1/auth/change-password", c.webUIBaseURL)

	bodyBytes, err := json.Marshal(updatePasswordRequest{
		CurrentPassword: currentPassword,
		NewPassword:     newPassword,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, changeURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.webUIClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("change password failed with status %d", resp.StatusCode)
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

// GetCoolingMode retrieves the current cooling mode configuration from the miner.
func (c *Client) GetCoolingMode(ctx context.Context) (sdk.CoolingMode, error) {
	ctx = c.withAuth(ctx)

	resp, err := c.dataClient.GetCoolingMode(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return sdk.CoolingModeUnspecified, fmt.Errorf("failed to get cooling mode: %w", err)
	}

	// Convert API cooling mode to SDK enum (reverse of SetCoolingMode mapping)
	switch resp.Msg.Mode {
	case miner_data_api.CoolingMode_COOLING_MODE_AUTO:
		return sdk.CoolingModeAirCooled, nil
	case miner_data_api.CoolingMode_COOLING_MODE_OFF:
		return sdk.CoolingModeImmersionCooled, nil
	case miner_data_api.CoolingMode_COOLING_MODE_MANUAL:
		return sdk.CoolingModeManual, nil
	case miner_data_api.CoolingMode_COOLING_MODE_UNKNOWN:
		return sdk.CoolingModeUnspecified, nil
	default:
		return sdk.CoolingModeUnspecified, nil
	}
}

// SetPowerTarget configures the power target and performance mode.
func (c *Client) SetPowerTarget(ctx context.Context, powerTargetW uint32, performanceMode sdk.PerformanceMode) error {
	ctx = c.withAuth(ctx)

	// Convert SDK performance mode to API enum
	var apiMode miner_data_api.PerformanceMode
	switch performanceMode {
	case sdk.PerformanceModeMaximumHashrate:
		apiMode = miner_data_api.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE
	case sdk.PerformanceModeEfficiency:
		apiMode = miner_data_api.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY
	case sdk.PerformanceModeUnspecified:
		apiMode = miner_data_api.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE
	default:
		apiMode = miner_data_api.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE
	}

	resp, err := c.commandClient.SetPowerTarget(ctx, connect.NewRequest(&miner_command_api.PowerTargetRequest{
		PowerTargetW:    powerTargetW,
		PerformanceMode: apiMode,
	}))
	if err != nil {
		return fmt.Errorf("failed to set power target: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return fmt.Errorf("set power target failed: %s", resp.Msg.String())
	}

	return nil
}

// GetPowerTarget retrieves the current power target configuration and bounds from the miner.
func (c *Client) GetPowerTarget(ctx context.Context) (*PowerTargetInfo, error) {
	ctx = c.withAuth(ctx)

	resp, err := c.dataClient.GetPowerTarget(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return nil, fmt.Errorf("failed to get power target: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return nil, fmt.Errorf("get power target failed: %s", resp.Msg.String())
	}

	// Convert API performance mode to SDK performance mode
	var mode sdk.PerformanceMode
	switch resp.Msg.PerformanceMode {
	case miner_data_api.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE:
		mode = sdk.PerformanceModeMaximumHashrate
	case miner_data_api.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY:
		mode = sdk.PerformanceModeEfficiency
	default:
		mode = sdk.PerformanceModeUnspecified
	}

	return &PowerTargetInfo{
		CurrentW: resp.Msg.PowerTargetW,
		MinW:     resp.Msg.PowerTargetMinW,
		MaxW:     resp.Msg.PowerTargetMaxW,
		DefaultW: resp.Msg.DefaultPowerTargetW,
		Mode:     mode,
	}, nil
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

// GetErrors retrieves error data from the miner.
func (c *Client) GetErrors(ctx context.Context) (*miner_data_api.ErrorsResponse, error) {
	ctx = c.withAuth(ctx)

	resp, err := c.dataClient.GetErrors(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return nil, fmt.Errorf("failed to get errors: %w", err)
	}

	if resp.Msg.Result != miner_common_api.ApiResult_RESULT_SUCCESS {
		return nil, fmt.Errorf("get errors failed: %s", resp.Msg.Result)
	}

	return resp.Msg, nil
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

// UpdateFirmware initiates an OTA firmware update (no file upload).
func (c *Client) UpdateFirmware(ctx context.Context) error {
	ctx = c.withAuth(ctx)

	_, err := c.systemClient.Update(ctx, connect.NewRequest(&miner_common_api.EmptyRequest{}))
	if err != nil {
		return fmt.Errorf("failed to update firmware: %w", err)
	}

	return nil
}

// UploadFirmware uploads a firmware file to the miner via the MDK REST API
// (PUT /api/v1/system/update, multipart/form-data). The file is streamed
// from firmware.Reader without buffering the entire payload in memory.
func (c *Client) UploadFirmware(ctx context.Context, firmware sdk.FirmwareFile) error {
	if firmware.Reader == nil {
		return fmt.Errorf("firmware reader is required")
	}

	uploadURL := fmt.Sprintf("%s/api/v1/system/update", c.webUIBaseURL)

	ctx, cancel := context.WithTimeout(ctx, firmwareUploadTimeout)
	defer cancel()

	pr, pw := io.Pipe()
	defer pr.Close()

	mw := multipart.NewWriter(pw)

	// Channel captures the goroutine's outcome so we can confirm the entire
	// multipart body was written before reporting success.
	writerDone := make(chan error, 1)

	go func() {
		defer pw.Close()

		part, err := mw.CreateFormFile("file", firmware.Filename)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to create multipart form file: %w", err))
			writerDone <- err
			return
		}

		if _, err := io.Copy(part, firmware.Reader); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to write firmware data: %w", err))
			writerDone <- err
			return
		}

		if err := mw.Close(); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to close multipart writer: %w", err))
			writerDone <- err
			return
		}

		writerDone <- nil
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, pr)
	if err != nil {
		return fmt.Errorf("failed to create firmware upload request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.bearerToken.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken.Token)
	}

	// Use a client without the default 30s timeout — firmware uploads can take
	// much longer. The context timeout above controls the overall deadline.
	transport := c.webUIClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	uploadClient := &http.Client{Transport: transport}

	resp, err := uploadClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload firmware: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	detail := strings.TrimSpace(string(respBody))

	switch resp.StatusCode {
	case http.StatusOK:
		if err := <-writerDone; err != nil {
			return fmt.Errorf("firmware upload: multipart writer failed: %w", err)
		}
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("firmware upload unauthorized: %s", withDetail("check bearer token", detail))
	case http.StatusConflict:
		return fmt.Errorf("firmware update already in progress: %s", withDetail("try again later", detail))
	case http.StatusBadRequest:
		return fmt.Errorf("firmware upload rejected by device: %s", withDetail("bad request", detail))
	default:
		return fmt.Errorf("firmware upload failed with status %d: %s", resp.StatusCode, withDetail("unknown error", detail))
	}
}

// withDetail returns detail if non-empty, otherwise falls back to fallback.
func withDetail(fallback, detail string) string {
	if detail != "" {
		return detail
	}
	return fallback
}

// withAuth adds authentication to the context if credentials are available.
func (c *Client) withAuth(ctx context.Context) context.Context {
	if c.bearerToken.Token != "" {
		return withAuthToken(ctx, c.bearerToken.Token)
	}
	return ctx
}
