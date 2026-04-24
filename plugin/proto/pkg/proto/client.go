// Package proto provides a client for communicating with Proto miners via REST/HTTP.
//
// The client communicates with the miner's REST API (MDK-API) over HTTP/HTTPS
// and provides a clean interface for the plugin to use.
package proto

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	// HTTP client configuration
	httpMaxIdleConnections        = 100
	httpMaxIdleConnectionsPerHost = 10
	httpIdleConnectionTimeout     = 90 * time.Second
	httpTLSHandshakeTimeout       = 10 * time.Second
	httpResponseHeaderTimeout     = 30 * time.Second
	httpClientTimeout             = 30 * time.Second

	// Firmware upload can transfer hundreds of megabytes over slow links.
	firmwareUploadTimeout = 30 * time.Minute
)

var (
	sharedHTTPClient  *http.Client
	sharedHTTPSClient *http.Client
	httpClientOnce    = &sync.Once{}
	httpsClientOnce   = &sync.Once{}
)

// Client provides communication with a Proto miner via the MDK REST API.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	bearerToken sdk.BearerToken
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
	State        sdk.HealthStatus
	ErrorMessage string
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

// NotificationError represents a single error from the REST /api/v1/errors endpoint.
type NotificationError struct {
	Source    string `json:"source"`
	Slot      int    `json:"slot"`
	ErrorCode string `json:"error_code"`
	Timestamp int64  `json:"timestamp"`
	Message   string `json:"message"`
}

// ErrorsResponse represents the response from GET /api/v1/errors.
// The MDK OpenAPI spec describes a top-level JSON array; some firmware versions
// return a wrapped object {"errors": [...]}. UnmarshalJSON accepts both shapes.
type ErrorsResponse struct {
	Errors []NotificationError `json:"errors"`
}

// UnmarshalJSON decodes either a bare array or an object with an "errors" field.
func (e *ErrorsResponse) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		e.Errors = nil
		return nil
	}
	if data[0] == '[' {
		if err := json.Unmarshal(data, &e.Errors); err != nil {
			return fmt.Errorf("unmarshal errors array: %w", err)
		}
		return nil
	}
	type wrapped struct {
		Errors []NotificationError `json:"errors"`
	}
	var w wrapped
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("unmarshal errors object: %w", err)
	}
	e.Errors = w.Errors
	return nil
}

// --- REST API response types for JSON deserialization ---

type pairingInfoResponse struct {
	Mac  string `json:"mac"`
	CbSn string `json:"cb_sn"`
}

type setAuthKeyRequest struct {
	PublicKey string `json:"public_key"`
}

type messageResponse struct {
	Message string `json:"message"`
}

type errorResponse struct {
	Error *errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type systemInfoResponse struct {
	SystemInfo systemInfoInner `json:"system-info"`
}

type systemInfoInner struct {
	ProductName     string          `json:"product_name"`
	Manufacturer    string          `json:"manufacturer"`
	Model           string          `json:"model"`
	CBSN            string          `json:"cb_sn"`
	OS              *swInfo         `json:"os,omitempty"`
	MiningDriverSW  *swInfo         `json:"mining_driver_sw,omitempty"`
	WebServer       *swInfo         `json:"web_server,omitempty"`
	WebDashboard    *swInfo         `json:"web_dashboard,omitempty"`
	PoolInterfaceSW *swInfo         `json:"pool_interface_sw,omitempty"`
	SwUpdateStatus  *swUpdateStatus `json:"sw_update_status,omitempty"`
}

type swUpdateStatus struct {
	Status   string  `json:"status"`
	Progress *int    `json:"progress,omitempty"`
	Error    *string `json:"error,omitempty"`
}

type swInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type miningStatusResponse struct {
	MiningStatus miningStatusInner `json:"mining-status"`
}

type miningStatusInner struct {
	Status string `json:"status"`
}

type poolsList struct {
	Pools []poolData `json:"pools"`
}

type poolData struct {
	ID       int    `json:"id"`
	URL      string `json:"url"`
	User     string `json:"user"`
	Priority *int   `json:"priority,omitempty"`
}

type poolConfigRequest struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

type miningTargetResponse struct {
	PowerTargetWatts        int    `json:"power_target_watts"`
	PowerTargetMinWatts     int    `json:"power_target_min_watts"`
	PowerTargetMaxWatts     int    `json:"power_target_max_watts"`
	DefaultPowerTargetWatts int    `json:"default_power_target_watts"`
	PerformanceMode         string `json:"performance_mode"`
}

type miningTargetRequest struct {
	PowerTargetWatts *int   `json:"power_target_watts,omitempty"`
	PerformanceMode  string `json:"performance_mode,omitempty"`
}

type coolingStatusResponse struct {
	CoolingStatus coolingStatusInner `json:"cooling-status"`
}

type coolingStatusInner struct {
	FanMode string `json:"fan_mode"`
}

type coolingConfigRequest struct {
	Mode string `json:"mode"`
}

type logsResponse struct {
	Logs logsData `json:"logs"`
}

type logsData struct {
	Content []string `json:"content"`
}

// REST telemetry response types
type telemetryResponse struct {
	Miner      *telemetryMiner      `json:"miner,omitempty"`
	Hashboards []telemetryHashboard `json:"hashboards,omitempty"`
	PSUs       []telemetryPSU       `json:"psus,omitempty"`
}

type telemetryMiner struct {
	Hashrate    *telemetryValue `json:"hashrate,omitempty"`
	Temperature *telemetryValue `json:"temperature,omitempty"`
	Power       *telemetryValue `json:"power,omitempty"`
	Efficiency  *telemetryValue `json:"efficiency,omitempty"`
}

type telemetryValue struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type telemetryHashboard struct {
	Index        uint32                  `json:"index"`
	SerialNumber string                  `json:"serial_number"`
	Hashrate     *telemetryValue         `json:"hashrate,omitempty"`
	Temperature  *telemetryHashboardTemp `json:"temperature,omitempty"`
	Voltage      *telemetryValue         `json:"voltage,omitempty"`
	Current      *telemetryValue         `json:"current,omitempty"`
	ASICs        *telemetryASICs         `json:"asics,omitempty"`
}

type telemetryHashboardTemp struct {
	Unit    string  `json:"unit"`
	Inlet   float64 `json:"inlet"`
	Outlet  float64 `json:"outlet"`
	Average float64 `json:"average"`
}

type telemetryASICs struct {
	Hashrate    *telemetryArrayValue `json:"hashrate,omitempty"`
	Temperature *telemetryArrayValue `json:"temperature,omitempty"`
}

type telemetryArrayValue struct {
	Unit   string    `json:"unit"`
	Values []float64 `json:"values"`
}

type telemetryPSU struct {
	Index       uint32               `json:"index"`
	Voltage     *telemetryPSUVoltage `json:"voltage,omitempty"`
	Current     *telemetryPSUCurrent `json:"current,omitempty"`
	Power       *telemetryPSUPower   `json:"power,omitempty"`
	Temperature *telemetryPSUTemp    `json:"temperature,omitempty"`
}

type telemetryPSUVoltage struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

type telemetryPSUCurrent struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

type telemetryPSUPower struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

type telemetryPSUTemp struct {
	Hotspot float64 `json:"hotspot"`
}

// --- Password change types ---

type updatePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type loginRequest struct {
	Password string `json:"password"`
}

type authTokensResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// NewClient creates a new Proto miner REST client.
func NewClient(host string, port int32, scheme string) (*Client, error) {
	baseURL := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, fmt.Sprintf("%d", port)))

	var httpClient *http.Client
	if scheme == "https" {
		httpClient = createHTTPSClient()
	} else {
		httpClient = createHTTPClient()
	}

	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}, nil
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
				// Proto rigs commonly present self-signed or otherwise unverifiable certs.
				// This keeps transport encryption while intentionally not authenticating
				// the remote endpoint, so it does not protect against active LAN MITM.
				InsecureSkipVerify: true, // #nosec G402 -- Intentional for Proto rig HTTPS
				MinVersion:         tls.VersionTLS12,
			},
			ForceAttemptHTTP2: true,
		}

		sharedHTTPSClient = &http.Client{
			Transport: transport,
			Timeout:   httpClientTimeout,
		}
	})
	return sharedHTTPSClient
}

// createHTTPClient creates a standard HTTP/1.1 client.
func createHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		transport := &http.Transport{
			MaxIdleConns:          httpMaxIdleConnections,
			MaxIdleConnsPerHost:   httpMaxIdleConnectionsPerHost,
			IdleConnTimeout:       httpIdleConnectionTimeout,
			ResponseHeaderTimeout: httpResponseHeaderTimeout,
		}

		sharedHTTPClient = &http.Client{
			Transport: transport,
			Timeout:   httpClientTimeout,
		}
	})
	return sharedHTTPClient
}

// SetCredentials sets authentication credentials for API calls.
func (c *Client) SetCredentials(bearerToken sdk.BearerToken) error {
	c.bearerToken = bearerToken
	return nil
}

// Close closes the client and cleans up resources.
func (c *Client) Close() error {
	return nil
}

// doRequest executes an HTTP request with authentication and returns the response.
func (c *Client) doRequest(ctx context.Context, method, path string, body any) (*http.Response, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.bearerToken.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}

// doGet performs a GET request and decodes the JSON response.
func (c *Client) doGet(ctx context.Context, path string, result any) error {
	_, err := c.doGetWithStatus(ctx, path, result)
	return err
}

// doGetWithStatus performs a GET request, decodes JSON when present, and returns the HTTP status.
func (c *Client) doGetWithStatus(ctx context.Context, path string, result any) (int, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, fmt.Errorf("unauthenticated: missing or invalid credentials")
	}

	if resp.StatusCode == http.StatusForbidden {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return resp.StatusCode, fmt.Errorf("failed to read forbidden response: %w", readErr)
		}
		return resp.StatusCode, classifyForbiddenResponse(body)
	}

	if resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return resp.StatusCode, fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return resp.StatusCode, nil
}

// doPost performs a POST request and checks the response.
func (c *Client) doPost(ctx context.Context, path string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return checkResponse(resp, "request failed", "unauthenticated: missing or invalid credentials", http.StatusOK, http.StatusAccepted)
}

// defaultPasswordMessageMarker is the Proto firmware's free-text 403 substring
// for the default-password lockout. It's Proto-firmware-specific, not part of
// the shared SDK contract.
const defaultPasswordMessageMarker = "default password must be changed"

// defaultPasswordCodeMarker is the lowercased ErrCodeDefaultPasswordActive —
// firmware sometimes surfaces the code as the body of a plain-text 403.
var defaultPasswordCodeMarker = strings.ToLower(ErrCodeDefaultPasswordActive)

// isDefaultPasswordMessage reports whether msg contains a Proto firmware
// default-password marker. Only this package should call it — the shared SDK
// must not bake in firmware-specific text.
func isDefaultPasswordMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, defaultPasswordMessageMarker) ||
		strings.Contains(lower, defaultPasswordCodeMarker)
}

func classifyForbiddenResponse(body []byte) error {
	var payload errorResponse
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil {
		if strings.EqualFold(payload.Error.Code, ErrCodeDefaultPasswordActive) || isDefaultPasswordMessage(payload.Error.Message) {
			return fmt.Errorf("forbidden: %s", defaultPasswordMessageMarker)
		}
		if payload.Error.Message != "" {
			return fmt.Errorf("forbidden: %s", payload.Error.Message)
		}
	}

	rawMessage := strings.TrimSpace(string(body))
	if isDefaultPasswordMessage(rawMessage) {
		return fmt.Errorf("forbidden: %s", defaultPasswordMessageMarker)
	}
	if rawMessage != "" {
		return fmt.Errorf("forbidden: %s", rawMessage)
	}
	return fmt.Errorf("forbidden: access denied")
}

func checkResponse(resp *http.Response, failurePrefix, unauthorizedMessage string, okStatuses ...int) error {
	for _, okStatus := range okStatuses {
		if resp.StatusCode == okStatus {
			_, _ = io.Copy(io.Discard, resp.Body)
			return nil
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, resp.Body)
		return errors.New(unauthorizedMessage)
	}

	if resp.StatusCode == http.StatusForbidden {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("failed to read forbidden response: %w", readErr)
		}
		return classifyForbiddenResponse(body)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return fmt.Errorf("%s with status %d", failurePrefix, resp.StatusCode)
}

// GetSoftwareInfo retrieves the firmware (OS) version string from the miner.
func (c *Client) GetSoftwareInfo(ctx context.Context) (string, error) {
	var resp systemInfoResponse
	if err := c.doGet(ctx, "/api/v1/system", &resp); err != nil {
		return "", fmt.Errorf("failed to get system info: %w", err)
	}

	if resp.SystemInfo.OS != nil {
		return resp.SystemInfo.OS.Version, nil
	}

	return "", nil
}

// GetFirmwareVersion retrieves the firmware (OS) version string from the miner.
func (c *Client) GetFirmwareVersion(ctx context.Context) (string, error) {
	return c.GetSoftwareInfo(ctx)
}

// StratumV2SupportProbe is the tri-state answer the proto plugin returns
// after probing ProtoOS for SV2 support. Mirrors sdk.StratumV2SupportStatus
// on the fleet side but kept local here so the proto client doesn't
// depend on the fleet SDK's enum values.
type StratumV2SupportProbe int

const (
	StratumV2ProbeUnknown     StratumV2SupportProbe = iota // ProtoOS didn't tell us
	StratumV2ProbeUnsupported                              // Firmware is SV1-only
	StratumV2ProbeSupported                                // Firmware natively speaks SV2
)

// SystemSnapshot bundles the firmware version string and the SV2-support
// probe result from a single /api/v1/system fetch. The plugin's Status
// path calls this once per throttle window instead of round-tripping
// twice for two fields that come from the same payload.
type SystemSnapshot struct {
	FirmwareVersion  string
	StratumV2Support StratumV2SupportProbe
}

// GetSystemSnapshot fetches /api/v1/system once and extracts both the
// firmware version and the SV2-support verdict from the response. See
// interpretStratumV2Support for the matching rules.
func (c *Client) GetSystemSnapshot(ctx context.Context) (SystemSnapshot, error) {
	var resp systemInfoResponse
	if err := c.doGet(ctx, "/api/v1/system", &resp); err != nil {
		return SystemSnapshot{}, fmt.Errorf("failed to get system info: %w", err)
	}
	var fw string
	if resp.SystemInfo.OS != nil {
		fw = resp.SystemInfo.OS.Version
	}
	return SystemSnapshot{
		FirmwareVersion:  fw,
		StratumV2Support: interpretStratumV2Support(resp.SystemInfo.PoolInterfaceSW),
	}, nil
}

// interpretStratumV2Support maps ProtoOS's pool_interface_sw.name label
// onto the tri-state probe result. Absent / empty pool interface is
// Unknown (lets the server fall back to static caps); a recognised
// "stratum v2"-ish label is Supported; any answered label that doesn't
// match Supported is Unsupported — an unrecognised-but-answered name
// is not the same as "no answer" and must not silently defer to the
// static view.
//
// Matching is case-insensitive and tolerant of separator variations
// (space, hyphen, underscore) so ProtoOS can evolve the label without
// forcing a plugin release for every format change.
func interpretStratumV2Support(sw *swInfo) StratumV2SupportProbe {
	if sw == nil {
		return StratumV2ProbeUnknown
	}
	name := strings.ToLower(sw.Name)
	if name == "" {
		return StratumV2ProbeUnknown
	}
	if containsSV2Token(name) {
		return StratumV2ProbeSupported
	}
	return StratumV2ProbeUnsupported
}

// containsSV2Token returns true when the pool-interface-software label
// indicates SV2. Accepts "sv2", "stratum-v2", "stratum v2", "stratum_v2",
// and "stratumv2" — the separator is load-bearing enough elsewhere that
// any reasonable rendering should match.
func containsSV2Token(label string) bool {
	if strings.Contains(label, "sv2") {
		return true
	}
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(label)
	return strings.Contains(normalized, "stratumv2")
}

// GetUpdateStatus retrieves the firmware update installation status from the miner.
// The status field tracks the swupdate lifecycle: "current", "downloading",
// "downloaded", "installing", "installed", "confirming", "success".
// On failure, the rig sets status back to "current" with the error field populated.
func (c *Client) GetUpdateStatus(ctx context.Context) (*swUpdateStatus, error) {
	var resp systemInfoResponse
	if err := c.doGet(ctx, "/api/v1/system", &resp); err != nil {
		return nil, fmt.Errorf("failed to get system info: %w", err)
	}
	return resp.SystemInfo.SwUpdateStatus, nil
}

// GetDeviceInfo retrieves basic device information via the pairing info endpoint.
func (c *Client) GetDeviceInfo(ctx context.Context) (*DeviceInfo, error) {
	var resp pairingInfoResponse
	if err := c.doGet(ctx, "/api/v1/pairing/info", &resp); err != nil {
		return nil, fmt.Errorf("failed to get pairing info: %w", err)
	}

	model := "Rig"
	manufacturer := "Proto"
	var sysResp systemInfoResponse
	if err := c.doGet(ctx, "/api/v1/system", &sysResp); err == nil {
		if sysResp.SystemInfo.Model != "" {
			model = sysResp.SystemInfo.Model
		} else if sysResp.SystemInfo.ProductName != "" {
			model = sysResp.SystemInfo.ProductName
		}
		if sysResp.SystemInfo.Manufacturer != "" {
			manufacturer = sysResp.SystemInfo.Manufacturer
		}
	}

	return &DeviceInfo{
		SerialNumber: resp.CbSn,
		MacAddress:   resp.Mac,
		Model:        model,
		Manufacturer: manufacturer,
	}, nil
}

// GetStatus retrieves the current miner status.
func (c *Client) GetStatus(ctx context.Context) (*Status, error) {
	var resp miningStatusResponse
	statusCode, err := c.doGetWithStatus(ctx, "/api/v1/mining", &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to get mining status: %w", err)
	}

	state := sdk.HealthHealthyInactive
	if statusCode != http.StatusNoContent {
		state = mapMiningState(resp.MiningStatus.Status)
	}

	// The actual pool list is the source of truth, not MiningState (which can be stale).
	needsPool, err := c.checkNeedsMiningPool(ctx)
	if err != nil {
		slog.Warn("failed to check pool configuration", "error", err)
	} else if needsPool {
		state = sdk.HealthNeedsMiningPool
	} else if state == sdk.HealthNeedsMiningPool {
		state = sdk.HealthHealthyInactive
	}

	return &Status{
		State:        state,
		ErrorMessage: "", // TODO: Extract from API when available
	}, nil
}

// mapMiningState converts a REST API mining status string to SDK HealthStatus.
// The MDK API returns CamelCase values (e.g. "DegradedMining", "NoPools").
func mapMiningState(status string) sdk.HealthStatus {
	switch strings.ToLower(status) {
	case "mining":
		return sdk.HealthHealthyActive
	case "degradedmining", "degraded_mining", "degraded":
		return sdk.HealthWarning
	case "stopped":
		return sdk.HealthHealthyInactive
	case "poweringon", "powering_on":
		return sdk.HealthHealthyInactive
	case "poweringoff", "powering_off":
		return sdk.HealthUnknown
	case "nopools", "no_pools":
		return sdk.HealthNeedsMiningPool
	case "error":
		return sdk.HealthCritical
	default:
		return sdk.HealthUnknown
	}
}

// checkNeedsMiningPool checks if the miner has no active pools configured.
func (c *Client) checkNeedsMiningPool(ctx context.Context) (bool, error) {
	var resp poolsList
	if err := c.doGet(ctx, "/api/v1/pools", &resp); err != nil {
		return false, fmt.Errorf("failed to get pools: %w", err)
	}

	if len(resp.Pools) == 0 {
		return true, nil
	}

	for _, pool := range resp.Pools {
		if pool.URL != "" {
			return false, nil
		}
	}

	return true, nil
}

// GetPools retrieves the currently configured pools from the miner.
func (c *Client) GetPools(ctx context.Context) ([]sdk.ConfiguredPool, error) {
	var resp poolsList
	if err := c.doGet(ctx, "/api/v1/pools", &resp); err != nil {
		return nil, fmt.Errorf("failed to get pools: %w", err)
	}

	pools := make([]sdk.ConfiguredPool, 0, len(resp.Pools))
	for _, pool := range resp.Pools {
		if pool.URL != "" {
			priority := int32(pool.ID) // #nosec G115 -- pool ID is a small integer from the miner API
			if pool.Priority != nil {
				priority = int32(*pool.Priority) // #nosec G115 -- pool priority is a small integer (typically 0-3)
			}
			pools = append(pools, sdk.ConfiguredPool{
				Priority: priority,
				URL:      pool.URL,
				Username: pool.User,
			})
		}
	}

	return pools, nil
}

// GetTelemetryValues retrieves comprehensive telemetry data from the miner.
func (c *Client) GetTelemetryValues(ctx context.Context) (*TelemetryValues, error) {
	var resp telemetryResponse
	if err := c.doGet(ctx, "/api/v1/telemetry?level=miner,hashboard,psu,asic", &resp); err != nil {
		return nil, fmt.Errorf("failed to get telemetry values: %w", err)
	}

	return convertTelemetryResponse(&resp), nil
}

// convertTelemetryResponse converts the REST telemetry response to client types.
func convertTelemetryResponse(resp *telemetryResponse) *TelemetryValues {
	result := &TelemetryValues{}

	if resp.Miner != nil {
		miner := &MinerTelemetry{}
		if resp.Miner.Hashrate != nil {
			miner.HashrateThS = resp.Miner.Hashrate.Value
		}
		if resp.Miner.Temperature != nil {
			miner.TemperatureC = resp.Miner.Temperature.Value
		}
		if resp.Miner.Power != nil {
			miner.PowerW = resp.Miner.Power.Value
		}
		if resp.Miner.Efficiency != nil {
			miner.EfficiencyJTh = resp.Miner.Efficiency.Value
		}
		result.Miner = miner
	}

	if len(resp.Hashboards) > 0 {
		result.Hashboards = make([]*HashboardTelemetry, len(resp.Hashboards))
		for i, hb := range resp.Hashboards {
			ht := &HashboardTelemetry{
				Index:        hb.Index,
				SerialNumber: hb.SerialNumber,
			}
			if hb.Hashrate != nil {
				ht.HashrateThS = hb.Hashrate.Value
			}
			if hb.Temperature != nil {
				ht.AverageTemperatureC = hb.Temperature.Average
				ht.InletTemperatureC = hb.Temperature.Inlet
				ht.OutletTemperatureC = hb.Temperature.Outlet
			}
			if hb.Voltage != nil {
				v := hb.Voltage.Value
				ht.VoltageV = &v
			}
			if hb.Current != nil {
				a := hb.Current.Value
				ht.CurrentA = &a
			}
			if hb.ASICs != nil {
				asics := &ASICTelemetry{}
				if hb.ASICs.Hashrate != nil {
					asics.HashrateThS = hb.ASICs.Hashrate.Values
				}
				if hb.ASICs.Temperature != nil {
					asics.TemperatureC = hb.ASICs.Temperature.Values
				}
				ht.ASICs = asics
			}
			result.Hashboards[i] = ht
		}
	}

	if len(resp.PSUs) > 0 {
		result.PSUs = make([]*PSUTelemetry, len(resp.PSUs))
		for i, psu := range resp.PSUs {
			pt := &PSUTelemetry{Index: psu.Index}
			if psu.Voltage != nil {
				pt.InputVoltageV = psu.Voltage.Input
				pt.OutputVoltageV = psu.Voltage.Output
			}
			if psu.Current != nil {
				pt.InputCurrentA = psu.Current.Input
				pt.OutputCurrentA = psu.Current.Output
			}
			if psu.Power != nil {
				pt.InputPowerW = psu.Power.Input
				pt.OutputPowerW = psu.Power.Output
			}
			if psu.Temperature != nil {
				pt.HotspotTemperatureC = psu.Temperature.Hotspot
			}
			result.PSUs[i] = pt
		}
	}

	return result
}

// Pair performs device pairing by setting the authentication public key.
// If the device is already paired, the request includes the bearer token
// for authentication as required by the API for key rotation.
func (c *Client) Pair(ctx context.Context, key sdk.APIKey) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/pairing/auth-key", setAuthKeyRequest{
		PublicKey: key.Key,
	})
	if err != nil {
		return fmt.Errorf("failed to set auth key: %w", err)
	}
	defer resp.Body.Close()
	return checkResponse(
		resp,
		"set auth key failed",
		"unauthenticated: device is already paired and requires valid credentials for key rotation",
		http.StatusOK,
	)
}

// ClearAuthKey clears the authentication key from the device during unpairing.
func (c *Client) ClearAuthKey(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, "/api/v1/pairing/auth-key", nil)
	if err != nil {
		return fmt.Errorf("failed to clear auth key: %w", err)
	}
	defer resp.Body.Close()
	return checkResponse(resp, "clear auth key failed", "unauthenticated: missing or invalid credentials", http.StatusOK)
}

// loginWithPassword authenticates via the miner's login endpoint and returns an access token.
// This deliberately bypasses doRequest to avoid sending the fleet bearer token.
func (c *Client) loginWithPassword(ctx context.Context, password string) (string, error) {
	bodyBytes, err := json.Marshal(loginRequest{Password: password})
	if err != nil {
		return "", fmt.Errorf("failed to marshal login request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth/login", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
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

// ChangePassword updates the miner web UI password.
func (c *Client) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	accessToken, err := c.loginWithPassword(ctx, currentPassword)
	if err != nil {
		return err
	}

	url := c.baseURL + "/api/v1/auth/change-password"

	bodyBytes, err := json.Marshal(updatePasswordRequest{
		CurrentPassword: currentPassword,
		NewPassword:     newPassword,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
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
	return c.doPost(ctx, "/api/v1/mining/start")
}

// StopMining stops mining operations.
func (c *Client) StopMining(ctx context.Context) error {
	return c.doPost(ctx, "/api/v1/mining/stop")
}

// SetCoolingMode configures the cooling system.
func (c *Client) SetCoolingMode(ctx context.Context, mode sdk.CoolingMode) error {
	var apiMode string
	switch mode {
	case sdk.CoolingModeAirCooled, sdk.CoolingModeUnspecified:
		apiMode = "Auto"
	case sdk.CoolingModeManual:
		apiMode = "Manual"
	case sdk.CoolingModeImmersionCooled:
		apiMode = "Off"
	default:
		apiMode = "Auto"
	}

	resp, err := c.doRequest(ctx, http.MethodPut, "/api/v1/cooling", coolingConfigRequest{Mode: apiMode})
	if err != nil {
		return fmt.Errorf("failed to set cooling mode: %w", err)
	}
	defer resp.Body.Close()
	return checkResponse(resp, "set cooling mode failed", "unauthenticated: missing or invalid credentials", http.StatusOK)
}

// GetCoolingMode retrieves the current cooling mode configuration from the miner.
func (c *Client) GetCoolingMode(ctx context.Context) (sdk.CoolingMode, error) {
	var resp coolingStatusResponse
	if err := c.doGet(ctx, "/api/v1/cooling", &resp); err != nil {
		return sdk.CoolingModeUnspecified, fmt.Errorf("failed to get cooling mode: %w", err)
	}

	switch strings.ToLower(resp.CoolingStatus.FanMode) {
	case "auto":
		return sdk.CoolingModeAirCooled, nil
	case "off":
		return sdk.CoolingModeImmersionCooled, nil
	case "manual":
		return sdk.CoolingModeManual, nil
	default:
		return sdk.CoolingModeUnspecified, nil
	}
}

// SetPowerTarget configures the power target and performance mode.
func (c *Client) SetPowerTarget(ctx context.Context, powerTargetW uint32, performanceMode sdk.PerformanceMode) error {
	var apiMode string
	switch performanceMode {
	case sdk.PerformanceModeMaximumHashrate, sdk.PerformanceModeUnspecified:
		apiMode = "MaximumHashrate"
	case sdk.PerformanceModeEfficiency:
		apiMode = "Efficiency"
	default:
		apiMode = "MaximumHashrate"
	}

	targetW := int(powerTargetW)
	body := miningTargetRequest{
		PowerTargetWatts: &targetW,
		PerformanceMode:  apiMode,
	}

	resp, err := c.doRequest(ctx, http.MethodPut, "/api/v1/mining/target", body)
	if err != nil {
		return fmt.Errorf("failed to set power target: %w", err)
	}
	defer resp.Body.Close()
	return checkResponse(resp, "set power target failed", "unauthenticated: missing or invalid credentials", http.StatusOK)
}

// GetPowerTarget retrieves the current power target configuration and bounds from the miner.
func (c *Client) GetPowerTarget(ctx context.Context) (*PowerTargetInfo, error) {
	var resp miningTargetResponse
	statusCode, err := c.doGetWithStatus(ctx, "/api/v1/mining/target", &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to get power target: %w", err)
	}

	if statusCode == http.StatusNoContent {
		return nil, nil
	}

	var mode sdk.PerformanceMode
	switch strings.ToLower(resp.PerformanceMode) {
	case "maximumhashrate", "maximum_hashrate":
		mode = sdk.PerformanceModeMaximumHashrate
	case "efficiency":
		mode = sdk.PerformanceModeEfficiency
	default:
		mode = sdk.PerformanceModeUnspecified
	}

	return &PowerTargetInfo{
		CurrentW: safeIntToUint32(resp.PowerTargetWatts),
		MinW:     safeIntToUint32(resp.PowerTargetMinWatts),
		MaxW:     safeIntToUint32(resp.PowerTargetMaxWatts),
		DefaultW: safeIntToUint32(resp.DefaultPowerTargetWatts),
		Mode:     mode,
	}, nil
}

func safeIntToUint32(v int) uint32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v) // #nosec G115 -- Range checked above
}

// UpdatePools configures mining pools atomically via POST /api/v1/pools.
func (c *Client) UpdatePools(ctx context.Context, pools []Pool) error {
	apiPools := make([]poolConfigRequest, len(pools))
	for i, pool := range pools {
		priority := pool.Priority
		apiPools[i] = poolConfigRequest{
			URL:      pool.URL,
			Username: pool.WorkerName,
			Priority: &priority,
		}
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/api/v1/pools", apiPools)
	if err != nil {
		return fmt.Errorf("failed to update pools: %w", err)
	}
	defer resp.Body.Close()
	return checkResponse(resp, "update pools failed", "unauthenticated: missing or invalid credentials", http.StatusOK, http.StatusCreated)
}

// BlinkLED triggers LED identification.
func (c *Client) BlinkLED(ctx context.Context) error {
	return c.doPost(ctx, "/api/v1/system/locate")
}

// GetLogs retrieves log data from the miner.
func (c *Client) GetLogs(ctx context.Context, _ *time.Time, maxLines int) (string, bool, error) {
	path := "/api/v1/system/logs"
	if maxLines > 0 {
		path = fmt.Sprintf("%s?lines=%d&source=miner_sw", path, maxLines)
	}

	var resp logsResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return "", false, fmt.Errorf("failed to get logs: %w", err)
	}

	var logContent string
	if len(resp.Logs.Content) > 0 {
		logContent = strings.Join(resp.Logs.Content, "\n")
	}

	return logContent, false, nil
}

// GetErrors retrieves error data from the miner.
func (c *Client) GetErrors(ctx context.Context) (*ErrorsResponse, error) {
	var resp ErrorsResponse
	if err := c.doGet(ctx, "/api/v1/errors", &resp); err != nil {
		return nil, fmt.Errorf("failed to get errors: %w", err)
	}

	return &resp, nil
}

// Reboot reboots the miner.
func (c *Client) Reboot(ctx context.Context) error {
	return c.doPost(ctx, "/api/v1/system/reboot")
}

// UpdateFirmware initiates an OTA firmware update (no file upload).
func (c *Client) UpdateFirmware(ctx context.Context) error {
	return c.doPost(ctx, "/api/v1/system/update")
}

// UploadFirmware uploads a firmware file to the miner via the MDK REST API
// (PUT /api/v1/system/update, multipart/form-data). The file is streamed
// from firmware.Reader without buffering the entire payload in memory.
func (c *Client) UploadFirmware(ctx context.Context, firmware sdk.FirmwareFile) error {
	if firmware.Reader == nil {
		return fmt.Errorf("firmware reader is required")
	}

	uploadURL := fmt.Sprintf("%s/api/v1/system/update", c.baseURL)

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
	transport := c.httpClient.Transport
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
	case http.StatusRequestEntityTooLarge:
		return grpcstatus.Errorf(codes.FailedPrecondition,
			"firmware upload rejected: payload too large (%d bytes, HTTP 413). %s",
			firmware.Size, withDetail("rig reverse-proxy body limit is smaller than this firmware file", detail))
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
