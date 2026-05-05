package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const minPasswordLength = 8

// REST API JSON types matching the OpenAPI spec (MDK-API.json)

// MessageResponse is a generic response with a message
type MessageResponse struct {
	Message string `json:"message"`
}

// ErrorResponse is an error response
type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// AuthTokens contains JWT tokens
type AuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// RefreshResponse matches the OpenAPI RefreshResponse schema: refresh rotates
// the access token only — the refresh token stays valid until logout.
type RefreshResponse struct {
	AccessToken string `json:"access_token"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// PasswordRequest matches the OpenAPI PasswordRequest schema (used for login and set-password).
type PasswordRequest struct {
	Password string `json:"password"`
}

// ChangePasswordRequest matches the OpenAPI ChangePasswordRequest schema.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// PoolConfigInner is a single pool configuration (matches OpenAPI PoolConfig_inner)
type PoolConfigInner struct {
	Name     string `json:"name,omitempty"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

// PoolResponse is a single pool response
type PoolResponse struct {
	Pool PoolData `json:"pool"`
}

// PoolsList is the list of pools response
type PoolsList struct {
	Pools []PoolData `json:"pools"`
}

// PoolData is a single pool data
type PoolData struct {
	ID               int    `json:"id"`
	Priority         int    `json:"priority"`
	Name             string `json:"name,omitempty"`
	URL              string `json:"url"`
	User             string `json:"user"`
	Status           string `json:"status"`
	AcceptedShares   int64  `json:"accepted_shares"`
	RejectedShares   int64  `json:"rejected_shares"`
	Difficulty       string `json:"difficulty"`
	Enabled          bool   `json:"enabled"`
	ConnectionStatus string `json:"connection_status"`
}

// SystemInfo contains system information
type SystemInfo struct {
	SystemInfo SystemInfoInner `json:"system-info"`
}

// SystemInfoInner contains the inner system info
type SystemInfoInner struct {
	ProductName       string       `json:"product_name"`
	Manufacturer      string       `json:"manufacturer,omitempty"`
	Model             string       `json:"model,omitempty"`
	Board             string       `json:"board"`
	CBSN              string       `json:"cb_sn"`
	SOC               string       `json:"soc"`
	UptimeSeconds     int64        `json:"uptime_seconds"`
	OS                OSInfo       `json:"os"`
	SWUpdateState     UpdateStatus `json:"sw_update_status"`
	MiningDriverSW    *SWInfo      `json:"mining_driver_sw,omitempty"`
	WebServer         *SWInfo      `json:"web_server,omitempty"`
	WebDashboard      *SWInfo      `json:"web_dashboard,omitempty"`
	PoolInterfaceSW   *SWInfo      `json:"pool_interface_sw,omitempty"`
	HashboardFirmware *SWInfo      `json:"hashboard_firmware,omitempty"`
}

// SWInfo contains software component information
type SWInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// OSInfo contains OS information
type OSInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Variant  string `json:"variant"`
	GitHash  string `json:"git_hash"`
	Hostname string `json:"hostname"`
}

// UpdateStatus contains software update status
type UpdateStatus struct {
	Status          string  `json:"status"`
	PreviousVersion string  `json:"previous_version,omitempty"`
	CurrentVersion  string  `json:"current_version,omitempty"`
	NewVersion      string  `json:"new_version,omitempty"`
	Message         *string `json:"message,omitempty"`
	Progress        *int    `json:"progress,omitempty"`
	Error           *string `json:"error,omitempty"`
	ReleaseNotes    *string `json:"release_notes,omitempty"`
}

// SystemStatuses contains system onboarding status
type SystemStatuses struct {
	Onboarded             bool `json:"onboarded"`
	PasswordSet           bool `json:"password_set"`
	DefaultPasswordActive bool `json:"default_password_active"`
}

// LogsResponse contains the logs response wrapper
type LogsResponse struct {
	Logs LogsData `json:"logs"`
}

// LogsData contains log content and metadata
type LogsData struct {
	Content []string `json:"content"`
	Lines   int      `json:"lines"`
	Source  string   `json:"source"`
}

// MiningStatus contains mining status information
type MiningStatus struct {
	MiningStatus MiningStatusInner `json:"mining-status"`
}

// MiningStatusInner contains the inner mining status
type MiningStatusInner struct {
	Status              string  `json:"status"`
	RebootUptimeS       int64   `json:"reboot_uptime_s"`
	MiningUptimeS       int64   `json:"mining_uptime_s"`
	HashrateGHS         float64 `json:"hashrate_ghs"`
	AverageHashrateGHS  float64 `json:"average_hashrate_ghs"`
	IdealHashrateGHS    float64 `json:"ideal_hashrate_ghs"`
	PowerUsageWatts     float64 `json:"power_usage_watts"`
	PowerTargetWatts    float64 `json:"power_target_watts"`
	PowerEfficiencyJTH  float64 `json:"power_efficiency_jth"`
	AverageHBTempC      float64 `json:"average_hb_temp_c"`
	AverageASICTempC    float64 `json:"average_asic_temp_c"`
	AverageHBEfficiency float64 `json:"average_hb_efficiency_jth"`
	HWErrors            int64   `json:"hw_errors"`
	HashboardsInstalled int     `json:"hashboards_installed"`
	HashboardsMining    int     `json:"hashboards_mining"`
}

// MiningTargetResponse contains mining target configuration (matches OpenAPI MiningTargetResponse)
type MiningTargetResponse struct {
	PowerTargetWatts        int    `json:"power_target_watts"`
	PowerTargetMinWatts     int    `json:"power_target_min_watts"`
	PowerTargetMaxWatts     int    `json:"power_target_max_watts"`
	DefaultPowerTargetWatts int    `json:"default_power_target_watts"`
	PerformanceMode         string `json:"performance_mode"`
	BalanceBays             bool   `json:"balance_bays,omitempty"`
	HashOnDisconnect        bool   `json:"hash_on_disconnect"`
}

// MiningTargetRequest is the request to set mining target
type MiningTargetRequest struct {
	PowerTargetWatts *int   `json:"power_target_watts,omitempty"`
	PerformanceMode  string `json:"performance_mode,omitempty"`
	HashOnDisconnect *bool  `json:"hash_on_disconnect,omitempty"`
}

// MiningTuningConfig is the request/response for the mining tuning endpoint
type MiningTuningConfig struct {
	Algorithm string `json:"algorithm"`
}

// CoolingStatus contains cooling system status
type CoolingStatus struct {
	CoolingStatus CoolingStatusInner `json:"cooling-status"`
}

// CoolingStatusInner contains inner cooling status
type CoolingStatusInner struct {
	FanMode         string      `json:"fan_mode"`
	SpeedPercentage int         `json:"speed_percentage"`
	TargetTempC     *float64    `json:"target_temperature_c,omitempty"`
	Fans            []FanStatus `json:"fans"`
}

// FanStatus is the status of a single fan
type FanStatus struct {
	Slot            int  `json:"slot"`
	RPM             int  `json:"rpm"`
	SpeedPercentage *int `json:"speed_percentage,omitempty"`
}

// CoolingConfig is the cooling configuration request
type CoolingConfig struct {
	Mode            string   `json:"mode"`
	SpeedPercentage *int     `json:"speed_percentage,omitempty"`
	TargetTempC     *float64 `json:"target_temperature_c,omitempty"`
}

// HashboardsResponse contains all hashboard info (matches OpenAPI HashboardsInfo)
type HashboardsResponse struct {
	Hashboards []HashboardInfo `json:"hashboards-info"`
}

// HashboardStats contains stats for a single hashboard
type HashboardStats struct {
	HashboardStats HashboardStatsInner `json:"hashboard-stats"`
}

// HashboardStatsInner contains inner hashboard stats
type HashboardStatsInner struct {
	HBSN             string      `json:"hb_sn"`
	Slot             int         `json:"slot"`
	Status           string      `json:"status"`
	HashrateGHS      float64     `json:"hashrate_ghs"`
	IdealHashrateGHS float64     `json:"ideal_hashrate_ghs"`
	PowerUsageWatts  float64     `json:"power_usage_watts"`
	EfficiencyJTH    float64     `json:"efficiency_jth"`
	InletTempC       float64     `json:"inlet_temp_c"`
	OutletTempC      float64     `json:"outlet_temp_c"`
	AvgASICTempC     float64     `json:"avg_asic_temp_c"`
	MaxASICTempC     float64     `json:"max_asic_temp_c"`
	ASICs            []ASICStats `json:"asics,omitempty"`
}

// ASICStats contains stats for a single ASIC
type ASICStats struct {
	Index            int     `json:"index"`
	ID               string  `json:"id"`
	Row              int     `json:"row"`
	Column           int     `json:"column"`
	HashrateGHS      float64 `json:"hashrate_ghs"`
	IdealHashrateGHS float64 `json:"ideal_hashrate_ghs"`
	TempC            float64 `json:"temp_c"`
	FreqMHz          float64 `json:"freq_mhz"`
	ErrorRate        float64 `json:"error_rate"`
}

// PairingInfoResponse contains pairing information
type PairingInfoResponse struct {
	MAC  string `json:"mac"`
	CBSN string `json:"cb_sn"`
}

// SetAuthKeyRequest is the request to set the auth key
type SetAuthKeyRequest struct {
	PublicKey string `json:"public_key"`
}

// SetAuthKeyResponse is the response after setting the auth key
type SetAuthKeyResponse struct {
	Message string `json:"message"`
}

// HardwareInfo contains hardware information
type HardwareInfo struct {
	HardwareInfo HardwareInfoInner `json:"hardware-info"`
}

// HardwareInfoInner contains inner hardware info (matches OpenAPI HardwareInfo_hardware-info)
type HardwareInfoInner struct {
	ControlBoard ControlBoardInfo `json:"cb-info,omitempty"`
	Hashboards   []HashboardInfo  `json:"hashboards-info,omitempty"`
	PSUs         []PSUInfo        `json:"psus-info,omitempty"`
	Fans         []FanInfo        `json:"fans-info,omitempty"`
}

// ControlBoardInfo contains control board information
type ControlBoardInfo struct {
	MachineName  string `json:"machine_name"`
	BoardID      string `json:"board_id"`
	SerialNumber string `json:"serial_number"`
}

// HashboardInfo contains hashboard hardware info (matches OpenAPI HashboardInfo)
type HashboardInfo struct {
	Slot         int    `json:"slot"`
	Port         int    `json:"port"`
	SerialNumber string `json:"hb_sn,omitempty"`
	ChipID       string `json:"chip_id,omitempty"`
	ASICCount    int    `json:"mining_asic_count,omitempty"`
	MiningASIC   string `json:"mining_asic,omitempty"`
	Board        string `json:"board,omitempty"`
}

// PSUFirmwareInfo contains PSU firmware version info (matches OpenAPI PsuInfo.firmware)
type PSUFirmwareInfo struct {
	AppVersion        string `json:"app_version,omitempty"`
	BootloaderVersion string `json:"bootloader_version,omitempty"`
}

// PSUInfo contains PSU hardware info (matches OpenAPI PsuInfo)
type PSUInfo struct {
	Slot         int              `json:"slot"`
	PSUSN        string           `json:"psu_sn,omitempty"`
	Manufacturer string           `json:"manufacturer,omitempty"`
	HWRevision   string           `json:"hw_revision,omitempty"`
	Model        string           `json:"model,omitempty"`
	Firmware     *PSUFirmwareInfo `json:"firmware,omitempty"`
}

// FanInfo contains fan hardware info
type FanInfo struct {
	Slot   int    `json:"slot"`
	Name   string `json:"name"`
	MinRPM *int   `json:"min_rpm,omitempty"`
	MaxRPM *int   `json:"max_rpm,omitempty"`
}

// PowerSuppliesResponse contains PSU status list
type PowerSuppliesResponse struct {
	PSUs []PSUStatus `json:"psus"`
}

// PSUStatus contains PSU status
type PSUStatus struct {
	Slot           int     `json:"slot"`
	SerialNumber   string  `json:"serial_number"`
	State          string  `json:"state"`
	InputVoltageV  float64 `json:"input_voltage_v"`
	OutputVoltageV float64 `json:"output_voltage_v"`
	InputCurrentA  float64 `json:"input_current_a"`
	OutputCurrentA float64 `json:"output_current_a"`
	InputPowerW    float64 `json:"input_power_w"`
	OutputPowerW   float64 `json:"output_power_w"`
	HotspotTempC   float64 `json:"hotspot_temp_c"`
	AmbientTempC   float64 `json:"ambient_temp_c"`
}

// NetworkInfo contains network configuration
type NetworkInfo struct {
	NetworkInfo NetworkInfoInner `json:"network-info"`
}

// NetworkInfoInner contains inner network info
type NetworkInfoInner struct {
	Hostname   string `json:"hostname"`
	MACAddress string `json:"mac_address"`
	IPAddress  string `json:"ip_address"`
	Netmask    string `json:"netmask"`
	Gateway    string `json:"gateway"`
	DHCP       bool   `json:"dhcp"`
}

// ErrorsResponse contains system errors
type ErrorsResponse []NotificationError

// NotificationError is a system error notification
type NotificationError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
	Timestamp string `json:"timestamp"`
}

// TelemetryResponse contains telemetry data (matches OpenAPI TelemetryData)
type TelemetryResponse struct {
	Timestamp  string               `json:"timestamp"`
	Miner      *MinerTelemetry      `json:"miner,omitempty"`
	Hashboards []HashboardTelemetry `json:"hashboards,omitempty"`
	PSUs       []PSUTelemetry       `json:"psus,omitempty"`
}

// MetricValue represents a metric with value and unit
type MetricValue struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// MinerTelemetry contains miner-level telemetry (matches OpenAPI MinerTelemetry)
type MinerTelemetry struct {
	Hashrate    MetricValue `json:"hashrate"`
	Temperature MetricValue `json:"temperature"`
	Power       MetricValue `json:"power"`
	Efficiency  MetricValue `json:"efficiency"`
}

// HashboardTemperature contains hashboard temperature readings
type HashboardTemperature struct {
	Unit    string  `json:"unit"`
	Inlet   float64 `json:"inlet"`
	Outlet  float64 `json:"outlet"`
	Average float64 `json:"average"`
}

// HashboardTelemetry contains hashboard-level telemetry (matches OpenAPI HashboardTelemetry)
type HashboardTelemetry struct {
	Index        int                  `json:"index"`
	SerialNumber string               `json:"serial_number"`
	Hashrate     MetricValue          `json:"hashrate"`
	Temperature  HashboardTemperature `json:"temperature"`
	Power        MetricValue          `json:"power"`
	Efficiency   MetricValue          `json:"efficiency"`
	Voltage      *MetricValue         `json:"voltage,omitempty"`
	Current      *MetricValue         `json:"current,omitempty"`
	ASICs        *ASICTelemetry       `json:"asics,omitempty"`
}

// ASICTelemetry contains ASIC-level telemetry (matches OpenAPI AsicTelemetry)
type ASICTelemetry struct {
	Hashrate    MetricArray `json:"hashrate"`
	Temperature MetricArray `json:"temperature"`
}

// MetricArray represents an array of metric values with unit
type MetricArray struct {
	Unit   string    `json:"unit"`
	Values []float64 `json:"values"`
}

// PsuInputOutputMetric represents PSU metric with input and output values
type PsuInputOutputMetric struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
	Unit   string  `json:"unit"`
}

// PsuTemperature represents PSU temperature measurements
type PsuTemperature struct {
	Ambient float64 `json:"ambient"`
	Average float64 `json:"average"`
	Hotspot float64 `json:"hotspot"`
	Unit    string  `json:"unit"`
}

// PSUTelemetry contains PSU-level telemetry (matches OpenAPI PsuTelemetry)
type PSUTelemetry struct {
	Index        int                  `json:"index"`
	SerialNumber string               `json:"serial_number,omitempty"`
	Voltage      PsuInputOutputMetric `json:"voltage"`
	Current      PsuInputOutputMetric `json:"current"`
	Power        PsuInputOutputMetric `json:"power"`
	Temperature  PsuTemperature       `json:"temperature"`
}

// RESTApiHandler handles REST API requests
type RESTApiHandler struct {
	state *MinerState
}

// NewRESTApiHandler creates a new REST API handler
func NewRESTApiHandler(state *MinerState) *RESTApiHandler {
	return &RESTApiHandler{state: state}
}

// RegisterRoutes mirrors the Proto firmware auth/default-password contract:
//   - PUBLIC_ROUTES (no auth): PUT /auth/password, POST /auth/login,
//     POST /auth/refresh, GET /system, /system/status, /system/ssh,
//     /system/secure, /system/unlock, /system/tag, /system/telemetry,
//     /network, /pairing/info, POST /pairing/auth-key.
//   - DEFAULT_PASSWORD_EXEMPT_PREFIXES (auth required but not password-gated):
//     /auth/change-password and /pools (all verbs, all sub-paths).
//   - Everything else: auth required AND blocked while default_password_active.
func (h *RESTApiHandler) RegisterRoutes(mux *http.ServeMux) {
	// Pools: auth required, exempt from the default-password gate per firmware.
	// Fleet onboarding configures pools before the operator changes the password.
	mux.HandleFunc("/api/v1/pools", h.requireBearerAuth(h.handlePools))
	mux.HandleFunc("/api/v1/pools/", h.requireBearerAuth(h.handlePoolByID))
	mux.HandleFunc("/api/v1/pools/test-connection", h.requireBearerAuth(h.handleTestPoolConnection))

	// Auth flow. login/refresh/set-password are fully public; change-password
	// requires auth but bypasses the default-password gate (it's the path OUT
	// of that state). logout is authenticated and gated like any other route.
	mux.HandleFunc("/api/v1/auth/login", h.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", h.requireBearerAuth(h.requirePasswordChanged(h.handleLogout)))
	mux.HandleFunc("/api/v1/auth/refresh", h.handleRefresh)
	mux.HandleFunc("/api/v1/auth/password", h.handleSetPassword)
	mux.HandleFunc("/api/v1/auth/change-password", h.requireBearerAuth(h.handleChangePassword))

	// System — GET public for the read-only status endpoints; mutating verbs
	// (PUT, DELETE) require auth and are default-password-gated.
	mux.HandleFunc("/api/v1/system", h.handleSystem)
	mux.HandleFunc("/api/v1/system/status", h.handleSystemStatus)
	mux.HandleFunc("/api/v1/system/secure", h.handleSecureStatus)
	mux.HandleFunc(
		"/api/v1/system/ssh",
		h.requireBearerAuthMethods(h.requirePasswordChangedMethods(h.handleSSH, http.MethodPut), http.MethodPut),
	)
	mux.HandleFunc(
		"/api/v1/system/unlock",
		h.requireBearerAuthMethods(h.requirePasswordChangedMethods(h.handleUnlock, http.MethodPut), http.MethodPut),
	)
	mux.HandleFunc(
		"/api/v1/system/tag",
		h.requireBearerAuthMethods(
			h.requirePasswordChangedMethods(h.handleTag, http.MethodPut, http.MethodDelete),
			http.MethodPut, http.MethodDelete,
		),
	)
	mux.HandleFunc(
		"/api/v1/system/telemetry",
		h.requireBearerAuthMethods(h.requirePasswordChangedMethods(h.handleTelemetryConfig, http.MethodPut), http.MethodPut),
	)
	mux.HandleFunc("/api/v1/system/reboot", h.requireBearerAuth(h.requirePasswordChanged(h.handleReboot)))
	mux.HandleFunc("/api/v1/system/locate", h.requireBearerAuth(h.requirePasswordChanged(h.handleLocate)))
	mux.HandleFunc("/api/v1/system/logs", h.requireBearerAuth(h.requirePasswordChanged(h.handleLogs)))
	mux.HandleFunc("/api/v1/system/update", h.requireBearerAuth(h.requirePasswordChanged(h.handleUpdate)))
	mux.HandleFunc("/api/v1/system/update/check", h.requireBearerAuth(h.requirePasswordChanged(h.handleUpdateCheck)))

	// Mining
	mux.HandleFunc("/api/v1/mining", h.requireBearerAuth(h.requirePasswordChanged(h.handleMining)))
	mux.HandleFunc("/api/v1/mining/target", h.requireBearerAuth(h.requirePasswordChanged(h.handleMiningTarget)))
	mux.HandleFunc("/api/v1/mining/tuning", h.requireBearerAuth(h.requirePasswordChanged(h.handleMiningTuning)))
	mux.HandleFunc("/api/v1/mining/start", h.requireBearerAuth(h.requirePasswordChanged(h.handleMiningStart)))
	mux.HandleFunc("/api/v1/mining/stop", h.requireBearerAuth(h.requirePasswordChanged(h.handleMiningStop)))

	// Hardware — firmware requires auth on /hardware (not in PUBLIC_ROUTES).
	mux.HandleFunc("/api/v1/hardware", h.requireBearerAuth(h.requirePasswordChanged(h.handleHardware)))
	mux.HandleFunc("/api/v1/hardware/psus", h.requireBearerAuth(h.requirePasswordChanged(h.handleHardwarePSUs)))
	mux.HandleFunc("/api/v1/hashboards", h.requireBearerAuth(h.requirePasswordChanged(h.handleHashboards)))
	mux.HandleFunc("/api/v1/hashboards/", h.requireBearerAuth(h.requirePasswordChanged(h.handleHashboardByID)))

	// Telemetry data
	mux.HandleFunc("/api/v1/hashrate", h.requireBearerAuth(h.requirePasswordChanged(h.handleHashrate)))
	mux.HandleFunc("/api/v1/hashrate/", h.requireBearerAuth(h.requirePasswordChanged(h.handleHashrateByID)))
	mux.HandleFunc("/api/v1/temperature", h.requireBearerAuth(h.requirePasswordChanged(h.handleTemperature)))
	mux.HandleFunc("/api/v1/temperature/", h.requireBearerAuth(h.requirePasswordChanged(h.handleTemperatureByID)))
	mux.HandleFunc("/api/v1/power", h.requireBearerAuth(h.requirePasswordChanged(h.handlePower)))
	mux.HandleFunc("/api/v1/power/", h.requireBearerAuth(h.requirePasswordChanged(h.handlePowerByID)))
	mux.HandleFunc("/api/v1/efficiency", h.requireBearerAuth(h.requirePasswordChanged(h.handleEfficiency)))
	mux.HandleFunc("/api/v1/efficiency/", h.requireBearerAuth(h.requirePasswordChanged(h.handleEfficiencyByID)))

	// PSUs
	mux.HandleFunc("/api/v1/power-supplies", h.requireBearerAuth(h.requirePasswordChanged(h.handlePowerSupplies)))
	mux.HandleFunc("/api/v1/power-supplies/update", h.requireBearerAuth(h.requirePasswordChanged(h.handlePowerSuppliesUpdate)))

	// Cooling
	mux.HandleFunc("/api/v1/cooling", h.requireBearerAuth(h.requirePasswordChanged(h.handleCooling)))

	// Network — GET public; PUT requires auth and is default-password-gated.
	mux.HandleFunc(
		"/api/v1/network",
		h.requireBearerAuthMethods(h.requirePasswordChangedMethods(h.handleNetwork, http.MethodPut), http.MethodPut),
	)

	// Errors
	mux.HandleFunc("/api/v1/errors", h.requireBearerAuth(h.requirePasswordChanged(h.handleErrors)))

	// Telemetry
	mux.HandleFunc("/api/v1/telemetry", h.requireBearerAuth(h.requirePasswordChanged(h.handleTelemetry)))
	mux.HandleFunc("/api/v1/timeseries", h.requireBearerAuth(h.requirePasswordChanged(h.handleTimeseries)))

	// Pairing — GET /info and POST /auth-key are public (POST's handler has
	// internal auth logic for key rotation). DELETE requires auth and is
	// default-password-gated, per firmware. Fleet unpair paths must tolerate
	// a 403 from a never-rotated device.
	mux.HandleFunc("/api/v1/pairing/info", h.handlePairingInfo)
	mux.HandleFunc(
		"/api/v1/pairing/auth-key",
		h.requireBearerAuthMethods(
			h.requirePasswordChangedMethods(h.handlePairingAuthKey, http.MethodDelete),
			http.MethodDelete,
		),
	)
}

// Helper functions

func (h *RESTApiHandler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}

func (h *RESTApiHandler) writeError(w http.ResponseWriter, status int, code, message string) {
	resp := ErrorResponse{}
	resp.Error.Code = code
	resp.Error.Message = message
	h.writeJSON(w, status, resp)
}

func protectedMethodsSet(methods ...string) map[string]struct{} {
	protectedMethods := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		protectedMethods[method] = struct{}{}
	}
	return protectedMethods
}

func methodIsProtected(method string, protectedMethods map[string]struct{}) bool {
	if len(protectedMethods) == 0 {
		return true
	}

	_, ok := protectedMethods[method]
	return ok
}

// requireBearerAuthMethods wraps a handler to require a valid bearer token on
// the specified HTTP methods only.
func (h *RESTApiHandler) requireBearerAuthMethods(next http.HandlerFunc, methods ...string) http.HandlerFunc {
	protectedMethods := protectedMethodsSet(methods...)

	return func(w http.ResponseWriter, r *http.Request) {
		h.state.mu.RLock()
		rebooting := h.state.Rebooting
		h.state.mu.RUnlock()
		if rebooting {
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					conn.Close()
					return
				}
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		if !methodIsProtected(r.Method, protectedMethods) {
			next(w, r)
			return
		}

		if !h.isAuthorized(r) {
			h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid bearer token")
			return
		}

		next(w, r)
	}
}

// requireBearerAuth wraps a handler to require a valid bearer token on every request.
func (h *RESTApiHandler) requireBearerAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.state.mu.RLock()
		rebooting := h.state.Rebooting
		h.state.mu.RUnlock()
		if rebooting {
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					conn.Close()
					return
				}
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		if !h.isAuthorized(r) {
			h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid bearer token")
			return
		}

		next(w, r)
	}
}

// requirePasswordChangedMethods returns 403 on the specified HTTP methods when
// the device still has the default password.
func (h *RESTApiHandler) requirePasswordChangedMethods(next http.HandlerFunc, methods ...string) http.HandlerFunc {
	protectedMethods := protectedMethodsSet(methods...)

	return func(w http.ResponseWriter, r *http.Request) {
		if !methodIsProtected(r.Method, protectedMethods) {
			next(w, r)
			return
		}

		if h.state.IsDefaultPasswordActive() {
			h.writeError(w, http.StatusForbidden, "DEFAULT_PASSWORD_ACTIVE", "Default password must be changed before accessing this resource")
			return
		}

		next(w, r)
	}
}

// requirePasswordChanged returns 403 when the device still has the default
// password, matching the Proto firmware's default-password lockout behavior.
func (h *RESTApiHandler) requirePasswordChanged(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.state.IsDefaultPasswordActive() {
			h.writeError(w, http.StatusForbidden, "DEFAULT_PASSWORD_ACTIVE", "Default password must be changed before accessing this resource")
			return
		}

		next(w, r)
	}
}

func (h *RESTApiHandler) isAuthorized(r *http.Request) bool {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		return false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return false
	}

	expectedToken := h.state.GetAccessToken()
	if expectedToken != "" && token == expectedToken {
		return true
	}

	return h.verifyPairedJWT(token)
}

func (h *RESTApiHandler) issueAuthTokens(prefix string) AuthTokens {
	issuedAt := time.Now().UTC().Format(time.RFC3339Nano)
	tokens := AuthTokens{
		AccessToken:  prefix + "-access-token-" + issuedAt,
		RefreshToken: prefix + "-refresh-token-" + issuedAt,
	}
	h.state.SetAccessToken(tokens.AccessToken)
	h.state.SetRefreshToken(tokens.RefreshToken)
	return tokens
}

func (h *RESTApiHandler) verifyPairedJWT(token string) bool {
	publicKey, err := h.getPairedPublicKey()
	if err != nil {
		return false
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}

	var header struct {
		Algorithm string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return false
	}
	if header.Algorithm != "EdDSA" {
		return false
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}

	var claims struct {
		MinerSN string `json:"miner_sn"`
		Exp     int64  `json:"exp"`
		Nbf     int64  `json:"nbf"`
	}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return false
	}
	if claims.MinerSN != h.state.SerialNumber {
		return false
	}

	now := time.Now().Unix()
	if claims.Exp != 0 && now >= claims.Exp {
		return false
	}
	if claims.Nbf != 0 && now < claims.Nbf {
		return false
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}

	return ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature)
}

func (h *RESTApiHandler) getPairedPublicKey() (ed25519.PublicKey, error) {
	authKey := strings.TrimSpace(h.state.GetAuthKey())
	if authKey == "" {
		return nil, fmt.Errorf("no auth key configured")
	}

	derBytes, err := base64.StdEncoding.DecodeString(authKey)
	if err != nil {
		return nil, fmt.Errorf("decode auth key: %w", err)
	}

	publicKey, err := x509.ParsePKIXPublicKey(derBytes)
	if err != nil {
		return nil, fmt.Errorf("parse auth key: %w", err)
	}

	ed25519Key, ok := publicKey.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("unexpected public key type %T", publicKey)
	}

	return ed25519Key, nil
}

func (h *RESTApiHandler) miningStateToString(state MiningState) string {
	if state == "" {
		return string(MiningStateUnknown)
	}
	return string(state)
}

func (h *RESTApiHandler) coolingModeToString(mode CoolingMode) string {
	switch mode {
	case CoolingModeAuto:
		return "Auto"
	case CoolingModeManual:
		return "Manual"
	case CoolingModeOff:
		return "Off"
	default:
		return "Unknown"
	}
}

func (h *RESTApiHandler) stringToCoolingMode(s string) CoolingMode {
	switch strings.ToLower(s) {
	case "auto":
		return CoolingModeAuto
	case "manual":
		return CoolingModeManual
	case "off":
		return CoolingModeOff
	default:
		return CoolingModeUnknown
	}
}

// Pools handlers

func (h *RESTApiHandler) handlePools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getPools(w, r)
	case http.MethodPost:
		h.createPools(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

func (h *RESTApiHandler) getPools(w http.ResponseWriter, r *http.Request) {
	pools := h.state.GetPools()

	h.state.mu.RLock()
	poolsOffline := h.state.ErrorConfig.PoolsOffline
	h.state.mu.RUnlock()

	poolList := make([]PoolData, len(pools))
	for i, p := range pools {
		status := "Active"
		if poolsOffline {
			status = "Dead"
		}

		var acceptedShares, rejectedShares int64
		var difficulty string = "0"
		if p.Statistics != nil {
			acceptedShares = int64(p.Statistics.AcceptedShares)
			rejectedShares = int64(p.Statistics.RejectedShares)
			difficulty = fmt.Sprintf("%.0f", p.Statistics.CurrentDifficulty)
		}

		poolName := h.state.GetPoolName(p.Idx)
		poolList[i] = PoolData{
			ID:               int(p.Idx),
			Priority:         p.Priority,
			Name:             poolName,
			URL:              p.Url,
			User:             p.Username,
			Status:           status,
			AcceptedShares:   acceptedShares,
			RejectedShares:   rejectedShares,
			Difficulty:       difficulty,
			Enabled:          true, // Pool is enabled if it exists
			ConnectionStatus: status,
		}
	}

	h.writeJSON(w, http.StatusOK, PoolsList{Pools: poolList})
}

func (h *RESTApiHandler) createPools(w http.ResponseWriter, r *http.Request) {
	// OpenAPI spec defines PoolConfig as an array of PoolConfigInner
	var pools []PoolConfigInner
	if err := json.NewDecoder(r.Body).Decode(&pools); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	for _, p := range pools {
		if err := validatePoolURL(p.URL); err != nil {
			h.writeError(w, http.StatusBadRequest, "INVALID_POOL_URL", "Invalid pool URL")
			return
		}
	}

	// Clear existing pools
	h.state.ClearPools()

	// Add new pools
	for i, p := range pools {
		priority := i
		if p.Priority != nil {
			priority = *p.Priority
		}

		pool := &Pool{
			Idx:      uint32(i),
			Priority: priority,
			Url:      p.URL,
			Username: p.Username,
			Password: p.Password,
			Statistics: &PoolStatistics{
				AcceptedShares:    defaultPoolAcceptedShares,
				RejectedShares:    defaultPoolRejectedShares,
				CurrentDifficulty: defaultPoolDifficulty,
			},
		}
		h.state.AddPool(pool)
		h.state.SetPoolName(pool.Idx, p.Name)
	}

	// Mark device as onboarded when pools are configured (mimics ensure_onboarded() in real miner)
	if len(pools) > 0 {
		h.state.SetOnboarded(true)
	}

	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Pools configured successfully"})
}

func (h *RESTApiHandler) handlePoolByID(w http.ResponseWriter, r *http.Request) {
	// Extract pool ID from URL
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/pools/")
	if path == "" {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "Pool ID required")
		return
	}

	id, err := strconv.Atoi(path)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid pool ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getPool(w, r, id)
	case http.MethodPut:
		h.updatePool(w, r, id)
	case http.MethodDelete:
		h.deletePool(w, r, id)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

func (h *RESTApiHandler) getPool(w http.ResponseWriter, r *http.Request, id int) {
	pools := h.state.GetPools()
	for _, p := range pools {
		if int(p.Idx) == id {
			var acceptedShares, rejectedShares int64
			var difficulty string = "0"
			if p.Statistics != nil {
				acceptedShares = int64(p.Statistics.AcceptedShares)
				rejectedShares = int64(p.Statistics.RejectedShares)
				difficulty = fmt.Sprintf("%.0f", p.Statistics.CurrentDifficulty)
			}

			h.writeJSON(w, http.StatusOK, PoolResponse{
				Pool: PoolData{
					ID:             int(p.Idx),
					Name:           h.state.GetPoolName(p.Idx),
					Priority:       p.Priority,
					URL:            p.Url,
					User:           p.Username,
					Status:         "Active",
					AcceptedShares: acceptedShares,
					RejectedShares: rejectedShares,
					Difficulty:     difficulty,
					Enabled:        true,
				},
			})
			return
		}
	}
	h.writeError(w, http.StatusNotFound, "NOT_FOUND", "Pool not found")
}

func (h *RESTApiHandler) updatePool(w http.ResponseWriter, r *http.Request, id int) {
	var config PoolConfigInner
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if config.URL != "" {
		if err := validatePoolURL(config.URL); err != nil {
			h.writeError(w, http.StatusBadRequest, "INVALID_POOL_URL", "Invalid pool URL")
			return
		}
	}

	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	for _, p := range h.state.Pools {
		if int(p.Idx) == id {
			if config.Name != "" {
				if h.state.PoolNames == nil {
					h.state.PoolNames = make(map[uint32]string)
				}
				h.state.PoolNames[p.Idx] = config.Name
			}
			if config.URL != "" {
				p.Url = config.URL
			}
			if config.Username != "" {
				p.Username = config.Username
			}
			if config.Password != "" {
				p.Password = config.Password
			}
			if config.Priority != nil {
				p.Priority = *config.Priority
			}
			h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Pool updated successfully"})
			return
		}
	}
	h.writeError(w, http.StatusNotFound, "NOT_FOUND", "Pool not found")
}

func (h *RESTApiHandler) deletePool(w http.ResponseWriter, r *http.Request, id int) {
	h.state.RemovePools([]uint32{uint32(id)})
	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Pool deleted successfully"})
}

func (h *RESTApiHandler) handleTestPoolConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	type testPoolConnectionRequest struct {
		URL      string          `json:"url"`
		Username string          `json:"username"`
		Password json.RawMessage `json:"password"`
	}

	var req testPoolConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if err := validatePoolURL(req.URL); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_POOL_URL", "Invalid pool URL")
		return
	}

	// Optional deterministic failure simulation for tests: any URL containing "fail" triggers a connection error.
	if strings.Contains(strings.ToLower(req.URL), "fail") {
		h.writeError(w, http.StatusBadGateway, "CONNECTION_FAILED", "Connection failed")
		return
	}

	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Connection test passed"})
}

func validatePoolURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty url")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return err
	}

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "stratum+tcp", "stratum+ssl", "stratum+tls", "stratum2+tcp", "stratum2+ssl", "stratum2+tls":
		// ok
	default:
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	if u.Hostname() == "" {
		return fmt.Errorf("missing hostname")
	}

	port := u.Port()
	if port == "" {
		return fmt.Errorf("missing port")
	}

	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("invalid port")
	}

	return nil
}

// Auth handlers

func (h *RESTApiHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	var req PasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if storedPassword := h.state.GetPassword(); storedPassword != "" && req.Password != storedPassword {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid password")
		return
	}

	h.writeJSON(w, http.StatusOK, h.issueAuthTokens("mock"))
}

func (h *RESTApiHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	h.state.SetAccessToken("")
	h.state.SetRefreshToken("")
	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Logged out successfully"})
}

func (h *RESTApiHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if req.RefreshToken == "" || req.RefreshToken != h.state.GetRefreshToken() {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid refresh token")
		return
	}

	// OpenAPI RefreshResponse only declares access_token, so only the access
	// token is rotated. The refresh token remains valid until login/logout —
	// rotating it here would put fake-rig behavior out of sync with the spec
	// clients are generated against.
	issuedAt := time.Now().UTC().Format(time.RFC3339Nano)
	accessToken := "mock-refreshed-access-token-" + issuedAt
	h.state.SetAccessToken(accessToken)
	h.writeJSON(w, http.StatusOK, RefreshResponse{AccessToken: accessToken})
}

func (h *RESTApiHandler) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	if h.state.GetPassword() != "" {
		h.writeError(w, http.StatusForbidden, "PASSWORD_ALREADY_SET", "Password already set; use authenticated change-password instead")
		return
	}

	var req PasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if len(req.Password) < minPasswordLength {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Password must be at least 8 characters long")
		return
	}

	h.state.SetPassword(req.Password)

	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Password set successfully"})
}

func (h *RESTApiHandler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if storedPassword := h.state.GetPassword(); storedPassword != "" && req.CurrentPassword != storedPassword {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Current password is incorrect")
		return
	}

	if len(req.NewPassword) < minPasswordLength {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "New password must be at least 8 characters long")
		return
	}

	if h.state.IsDefaultPassword(req.NewPassword) {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "New password cannot be the same as the default password")
		return
	}

	h.state.SetPassword(req.NewPassword)

	// Revoke existing bearer credentials after password change, matching firmware behavior.
	// Clients must re-authenticate with the new password.
	h.state.SetAuthKey("")
	h.state.SetAccessToken("")
	h.state.SetRefreshToken("")

	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Password changed successfully"})
}

// System handlers

func (h *RESTApiHandler) handleSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	h.state.mu.RLock()
	rebooting := h.state.Rebooting
	h.state.mu.RUnlock()
	if rebooting {
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
				return
			}
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	uptime := int64(time.Since(h.state.StartTime).Seconds())

	h.state.mu.RLock()
	fwStatus := h.state.FWUpdateStatus
	fwCurrentVersion := h.state.FWCurrentVersion
	fwPreviousVersion := h.state.FWPreviousVersion
	h.state.mu.RUnlock()
	if fwStatus == "" {
		fwStatus = "current"
	}
	if fwCurrentVersion == "" {
		fwCurrentVersion = defaultFirmwareVersion
	}

	h.writeJSON(w, http.StatusOK, SystemInfo{
		SystemInfo: SystemInfoInner{
			ProductName:   "Proto Rig",
			Manufacturer:  "Proto",
			Model:         h.state.Model,
			Board:         "C3",
			CBSN:          h.state.SerialNumber,
			SOC:           "STM32MP157F",
			UptimeSeconds: uptime,
			OS: OSInfo{
				Name:     "ProtoOS",
				Version:  fwCurrentVersion,
				Variant:  "release",
				GitHash:  "abc123def456",
				Hostname: h.state.Hostname,
			},
			SWUpdateState: UpdateStatus{
				Status:          fwStatus,
				CurrentVersion:  fwCurrentVersion,
				PreviousVersion: fwPreviousVersion,
			},
			MiningDriverSW: &SWInfo{
				Name:    "mcdd",
				Version: fwCurrentVersion,
			},
			WebServer: &SWInfo{
				Name:    "miner-api-server",
				Version: fwCurrentVersion,
			},
			WebDashboard: &SWInfo{
				Name:    "miner-web",
				Version: fwCurrentVersion,
			},
			PoolInterfaceSW: &SWInfo{
				Name:    "stratum-client",
				Version: fwCurrentVersion,
			},
			HashboardFirmware: &SWInfo{
				Name:    "hashboard-fw",
				Version: fwCurrentVersion,
			},
		},
	})
}

func (h *RESTApiHandler) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	passwordSet := h.state.GetPassword() != ""

	h.writeJSON(w, http.StatusOK, SystemStatuses{
		Onboarded:             h.state.IsOnboarded(),
		PasswordSet:           passwordSet,
		DefaultPasswordActive: h.state.IsDefaultPasswordActive(),
	})
}

func (h *RESTApiHandler) handleReboot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	h.state.mu.Lock()
	if h.state.FWUpdateStatus == "installed" && h.state.FWNewVersion != "" {
		h.state.FWPreviousVersion = h.state.FWCurrentVersion
		h.state.FWCurrentVersion = h.state.FWNewVersion
		h.state.FWNewVersion = ""
	}
	h.state.FWUpdateStatus = "current"
	h.state.Rebooting = true
	h.state.mu.Unlock()

	log.Printf("[FAKE-RIG] Reboot initiated, going offline for 10 seconds")

	// Simulate offline period during reboot
	go func() {
		time.Sleep(10 * time.Second)
		h.state.mu.Lock()
		h.state.Rebooting = false
		h.state.StartTime = time.Now()
		h.state.mu.Unlock()
		log.Printf("[FAKE-RIG] Reboot complete, back online")
	}()

	h.writeJSON(w, http.StatusAccepted, MessageResponse{Message: "Reboot initiated"})
}

func (h *RESTApiHandler) handleLocate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	// `led_on_time` is part of the MDK contract. The simulator does not model the
	// duration, but it accepts and validates the query parameter for compatibility.
	if ledOnTime := r.URL.Query().Get("led_on_time"); ledOnTime != "" {
		if _, err := strconv.Atoi(ledOnTime); err != nil {
			h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "led_on_time must be an integer")
			return
		}
	}

	h.state.SetLocateActive(true)

	h.writeJSON(w, http.StatusAccepted, MessageResponse{Message: "Locate sequence activated"})
}

func (h *RESTApiHandler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	linesStr := query.Get("lines")
	source := query.Get("source")
	if source == "" {
		source = "miner_sw"
	}

	lines := 100 // default
	if linesStr != "" {
		if parsed, err := strconv.Atoi(linesStr); err == nil && parsed > 0 {
			lines = parsed
		}
	}

	// Generate simulated log content
	logContent := []string{
		"[INFO] Proto Miner Simulator started",
		"[INFO] Mining driver initialized",
		"[INFO] Connected to pool: stratum+tcp://pool.example.com:3333",
		"[INFO] Hashboard 0 online - 35.2 TH/s",
		"[INFO] Hashboard 1 online - 35.1 TH/s",
		"[INFO] Hashboard 2 online - 34.9 TH/s",
		"[INFO] Hashboard 3 online - 35.0 TH/s",
		"[INFO] Total hashrate: 140.2 TH/s",
		"[INFO] System temperature: 55°C",
		"[INFO] Fan speed: 4500 RPM (60%)",
	}

	// Limit to requested number of lines
	if lines < len(logContent) {
		logContent = logContent[:lines]
	}

	h.writeJSON(w, http.StatusOK, LogsResponse{
		Logs: LogsData{
			Content: logContent,
			Lines:   len(logContent),
			Source:  source,
		},
	})
}

func (h *RESTApiHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// OTA update (no file upload)
		h.writeJSON(w, http.StatusAccepted, MessageResponse{Message: "Update started"})
	case http.MethodPut:
		// File-based firmware upload (multipart/form-data)
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "multipart/form-data") {
			h.writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Content-Type must be multipart/form-data")
			return
		}

		h.state.mu.Lock()
		// Reject re-uploads whenever an update is pending (downloaded/installing)
		// OR has already completed install and is awaiting reboot. Treating
		// "installed" as in-progress prevents a second upload from clobbering
		// FWUpdateStatus/FWNewVersion and causing handleReboot to skip promotion.
		switch h.state.FWUpdateStatus {
		case "downloaded", "installing", "installed":
			h.state.mu.Unlock()
			h.writeJSON(w, http.StatusConflict, MessageResponse{Message: "System update is already in progress."})
			return
		}
		h.state.FWUpdateStatus = "downloaded"
		h.state.FWNewVersion = defaultNextFirmwareVersion
		h.state.mu.Unlock()

		file, header, err := r.FormFile("file")
		if err != nil {
			h.state.mu.Lock()
			h.state.FWUpdateStatus = ""
			h.state.FWNewVersion = ""
			h.state.mu.Unlock()
			h.writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Missing or invalid 'file' field in multipart form")
			return
		}
		defer file.Close()

		log.Printf("Firmware upload received: filename=%s, size=%d", header.Filename, header.Size)

		// Simulate async install lifecycle: installing → installed
		// Timings are longer than Fleet's 10s poll interval so each phase is observable.
		// Each step checks Rebooting to abort if a reboot arrived mid-install.
		go func() {
			time.Sleep(5 * time.Second)

			h.state.mu.Lock()
			if h.state.Rebooting {
				h.state.mu.Unlock()
				return
			}
			h.state.FWUpdateStatus = "installing"
			h.state.mu.Unlock()
			log.Printf("[FAKE-RIG] Firmware update status: installing (will take ~55s)")

			time.Sleep(55 * time.Second)

			h.state.mu.Lock()
			if h.state.Rebooting {
				h.state.mu.Unlock()
				return
			}
			h.state.FWUpdateStatus = "installed"
			h.state.mu.Unlock()
			log.Printf("[FAKE-RIG] Firmware update status: installed (reboot required)")
		}()

		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Firmware uploaded successfully"})
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

func (h *RESTApiHandler) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	h.state.mu.RLock()
	currentVersion := h.state.FWCurrentVersion
	h.state.mu.RUnlock()
	if currentVersion == "" {
		currentVersion = defaultFirmwareVersion
	}

	h.writeJSON(w, http.StatusOK, UpdateStatus{
		Status:         "current",
		CurrentVersion: currentVersion,
	})
}

func (h *RESTApiHandler) handleSSH(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
	case http.MethodPut:
		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "SSH configuration updated"})
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

func (h *RESTApiHandler) handleSecureStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]bool{"secure": false})
}

func (h *RESTApiHandler) handleUnlock(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.writeJSON(w, http.StatusOK, map[string]string{"lock-status": "UNLOCKED"})
	case http.MethodPut:
		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "System unlock status updated"})
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

func (h *RESTApiHandler) handleTag(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.writeJSON(w, http.StatusOK, map[string]string{"tag": ""})
	case http.MethodPut:
		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Tag updated"})
	case http.MethodDelete:
		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Tag deleted"})
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

func (h *RESTApiHandler) handleTelemetryConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
	case http.MethodPut:
		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Telemetry configuration updated"})
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

// Mining handlers

func (h *RESTApiHandler) handleMining(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	miningState := h.state.GetMiningState()
	hashrate, temperature, power, efficiency := h.state.GetMinerTelemetry()
	uptime := int64(time.Since(h.state.StartTime).Seconds())

	h.state.mu.RLock()
	powerTarget := h.state.PowerTargetW
	h.state.mu.RUnlock()

	hashboardsInstalled := h.state.GetHashboardCount()
	hashboardsMining := 0
	if miningState == MiningStateMining ||
		miningState == MiningStateDegraded {
		for i := range defaultHashboardCount {
			if !h.state.IsHashboardMissing(i) && !h.state.IsHashboardInError(i) {
				hashboardsMining++
			}
		}
	}

	h.writeJSON(w, http.StatusOK, MiningStatus{
		MiningStatus: MiningStatusInner{
			Status:              h.miningStateToString(miningState),
			RebootUptimeS:       uptime,
			MiningUptimeS:       uptime,
			HashrateGHS:         hashrate * 1000, // TH/s to GH/s
			AverageHashrateGHS:  hashrate * 1000,
			IdealHashrateGHS:    defaultIdealHashrate * 1000,
			PowerUsageWatts:     power,
			PowerTargetWatts:    float64(powerTarget),
			PowerEfficiencyJTH:  efficiency,
			AverageHBTempC:      temperature,
			AverageASICTempC:    applyVariation(defaultASICTemperature, telemetryVariation),
			AverageHBEfficiency: efficiency,
			HWErrors:            0,
			HashboardsInstalled: hashboardsInstalled,
			HashboardsMining:    hashboardsMining,
		},
	})
}

// parsePerformanceMode maps an OpenAPI performance mode string to its protobuf enum.
func parsePerformanceMode(s string) (PerformanceMode, bool) {
	switch s {
	case "MaximumHashrate":
		return PerformanceModeMaxHashrate, true
	case "Efficiency":
		return PerformanceModeEfficiency, true
	default:
		return "", false
	}
}

func performanceModeToString(mode PerformanceMode) string {
	if mode == PerformanceModeEfficiency {
		return "Efficiency"
	}
	return "MaximumHashrate"
}

func (h *RESTApiHandler) handleMiningTarget(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.state.mu.RLock()
		powerTarget := h.state.PowerTargetW
		perfMode := h.state.PerformanceModeVal
		hashOnDisconnect := h.state.HashOnDisconnect
		h.state.mu.RUnlock()

		h.writeJSON(w, http.StatusOK, MiningTargetResponse{
			PowerTargetWatts:        int(powerTarget),
			PowerTargetMinWatts:     defaultPowerTargetMin,
			PowerTargetMaxWatts:     defaultPowerTargetMax,
			DefaultPowerTargetWatts: defaultPowerTargetW,
			PerformanceMode:         performanceModeToString(perfMode),
			HashOnDisconnect:        hashOnDisconnect,
		})

	case http.MethodPut:
		var req MiningTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
			return
		}

		// Validate power target when provided
		if req.PowerTargetWatts != nil {
			pw := *req.PowerTargetWatts
			if pw <= 0 {
				h.writeError(w, http.StatusUnprocessableEntity, "OUT_OF_RANGE", "power_target_watts must be positive")
				return
			}
			if pw < defaultPowerTargetMin || pw > defaultPowerTargetMax {
				h.writeError(w, http.StatusUnprocessableEntity, "OUT_OF_RANGE",
					fmt.Sprintf("power_target_watts must be between %d and %d", defaultPowerTargetMin, defaultPowerTargetMax))
				return
			}
		}

		// Validate and parse performance mode when provided
		var perfMode PerformanceMode
		if req.PerformanceMode != "" {
			var ok bool
			perfMode, ok = parsePerformanceMode(req.PerformanceMode)
			if !ok {
				h.writeError(w, http.StatusUnprocessableEntity, "INVALID_PERFORMANCE_MODE",
					fmt.Sprintf("Invalid performance_mode %q; expected MaximumHashrate or Efficiency", req.PerformanceMode))
				return
			}
		}

		// Read current values, apply only the fields that were provided, then persist
		h.state.mu.RLock()
		powerW := h.state.PowerTargetW
		mode := h.state.PerformanceModeVal
		h.state.mu.RUnlock()

		if req.PowerTargetWatts != nil {
			powerW = uint32(*req.PowerTargetWatts)
		}
		if req.PerformanceMode != "" {
			mode = perfMode
		}

		h.state.SetPowerTarget(powerW, mode, req.HashOnDisconnect)

		// Read back the updated values
		h.state.mu.RLock()
		updatedPowerTarget := h.state.PowerTargetW
		updatedPerfMode := h.state.PerformanceModeVal
		updatedHashOnDisconnect := h.state.HashOnDisconnect
		h.state.mu.RUnlock()

		h.writeJSON(w, http.StatusOK, MiningTargetResponse{
			PowerTargetWatts:        int(updatedPowerTarget),
			PowerTargetMinWatts:     defaultPowerTargetMin,
			PowerTargetMaxWatts:     defaultPowerTargetMax,
			DefaultPowerTargetWatts: defaultPowerTargetW,
			PerformanceMode:         performanceModeToString(updatedPerfMode),
			HashOnDisconnect:        updatedHashOnDisconnect,
		})

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

func (h *RESTApiHandler) handleMiningStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	h.state.SetMiningState(MiningStateMining)
	h.writeJSON(w, http.StatusAccepted, MessageResponse{Message: "Mining started"})
}

func (h *RESTApiHandler) handleMiningStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	h.state.SetMiningState(MiningStateStopped)
	h.writeJSON(w, http.StatusAccepted, MessageResponse{Message: "Mining stopped"})
}

func (h *RESTApiHandler) handleMiningTuning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	var req MiningTuningConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	algorithmMap := map[string]TuningAlgorithm{
		"None":                         TuningAlgorithmNone,
		"VoltageImbalanceCompensation": TuningAlgorithmVoltageImbalanceCompensation,
		"Fuzzing":                      TuningAlgorithmFuzzing,
	}
	algo, ok := algorithmMap[req.Algorithm]
	if !ok {
		h.writeError(w, http.StatusUnprocessableEntity, "INVALID_ALGORITHM",
			fmt.Sprintf("Invalid tuning algorithm %q; expected None, VoltageImbalanceCompensation, or Fuzzing", req.Algorithm))
		return
	}

	h.state.SetTuningAlgorithm(algo)
	log.Printf("Mining tuning set: %s (SN: %s)", req.Algorithm, h.state.SerialNumber)
	h.writeJSON(w, http.StatusOK, MiningTuningConfig{Algorithm: req.Algorithm})
}

// Hardware handlers

func (h *RESTApiHandler) handleHardware(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	// Generate hashboards
	hashboards := make([]HashboardInfo, 0, defaultHashboardCount)
	for i := range defaultHashboardCount {
		if h.state.IsHashboardMissing(i) {
			continue
		}
		hashboards = append(hashboards, HashboardInfo{
			Slot:         i + 1, // 1-based slot number
			Port:         i,     // 0-based USB port number
			SerialNumber: fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, i),
			ChipID:       "BM1370",
			ASICCount:    defaultASICCount,
			MiningASIC:   "BZM",
			Board:        "B4",
		})
	}

	// Generate PSUs
	psus := make([]PSUInfo, 0, defaultPSUCount)
	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			continue
		}
		psus = append(psus, PSUInfo{
			Slot:         i + 1, // 1-based slot number
			PSUSN:        fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			Manufacturer: "Proto",
			Model:        "PSU-3600W",
			HWRevision:   "v1.0",
			Firmware: &PSUFirmwareInfo{
				AppVersion:        "1.2.0",
				BootloaderVersion: "1.0.0",
			},
		})
	}

	// Generate fans
	fans := make([]FanInfo, 4)
	for i := range 4 {
		minRPM := 1000
		maxRPM := 6000
		fans[i] = FanInfo{
			Slot:   i + 1, // 1-based slot number
			Name:   fmt.Sprintf("Fan %d", i+1),
			MinRPM: &minRPM,
			MaxRPM: &maxRPM,
		}
	}

	h.writeJSON(w, http.StatusOK, HardwareInfo{
		HardwareInfo: HardwareInfoInner{
			ControlBoard: ControlBoardInfo{
				MachineName:  h.state.Model,
				BoardID:      "CB-001",
				SerialNumber: h.state.SerialNumber,
			},
			Hashboards: hashboards,
			PSUs:       psus,
			Fans:       fans,
		},
	})
}

func (h *RESTApiHandler) handleHardwarePSUs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	psus := make([]PSUInfo, 0, defaultPSUCount)
	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			continue
		}
		psus = append(psus, PSUInfo{
			Slot:         i + 1, // 1-based slot number
			PSUSN:        fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			Manufacturer: "Proto",
			Model:        "PSU-3600W",
			HWRevision:   "v1.0",
			Firmware: &PSUFirmwareInfo{
				AppVersion:        "1.2.0",
				BootloaderVersion: "1.0.0",
			},
		})
	}

	h.writeJSON(w, http.StatusOK, map[string][]PSUInfo{"psus-info": psus})
}

func (h *RESTApiHandler) handleHashboards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	// /api/v1/hashboards returns hardware info (HashboardInfo), not stats
	hashboards := make([]HashboardInfo, 0, defaultHashboardCount)

	for i := range defaultHashboardCount {
		if h.state.IsHashboardMissing(i) {
			continue
		}

		hashboards = append(hashboards, HashboardInfo{
			Slot:         i + 1, // 1-based slot number
			Port:         i,     // 0-based USB port number
			SerialNumber: fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, i),
			ChipID:       "BM1370",
			ASICCount:    defaultASICCount,
			MiningASIC:   "BZM",
			Board:        "B4",
		})
	}

	h.writeJSON(w, http.StatusOK, HashboardsResponse{Hashboards: hashboards})
}

func (h *RESTApiHandler) handleHashboardByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	// Parse path: /api/v1/hashboards/{hb_sn} or /api/v1/hashboards/{hb_sn}/{asic_id}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/hashboards/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid hashboard ID")
		return
	}

	hbSN := parts[0]

	// Extract index from serial number
	var idx int
	_, err := fmt.Sscanf(hbSN, "HB-"+h.state.SerialNumber+"-%d", &idx)
	if err != nil || idx < 0 || idx >= defaultHashboardCount || h.state.IsHashboardMissing(idx) {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "Hashboard not found")
		return
	}

	miningState := h.state.GetMiningState()
	state := "Running"
	if h.state.IsHashboardInError(idx) {
		state = "Error"
	} else if miningState != MiningStateMining {
		state = "Stopped"
	}

	hbHashrate := applyVariation(defaultHashboardHashrate, telemetryVariation)
	if state != "Running" {
		hbHashrate = 0
	}

	hbStats := HashboardStatsInner{
		HBSN:             hbSN,
		Slot:             idx + 1, // 1-based slot number
		Status:           state,
		HashrateGHS:      hbHashrate * 1000,
		IdealHashrateGHS: defaultHashboardHashrate * 1000,
		PowerUsageWatts:  applyVariation(defaultHashboardPower, telemetryVariation),
		EfficiencyJTH:    applyVariation(defaultEfficiencyJTH, telemetryVariation),
		InletTempC:       applyVariation(defaultHashboardInletTemp, telemetryVariation),
		OutletTempC:      applyVariation(defaultHashboardOutletTemp, telemetryVariation),
		AvgASICTempC:     applyVariation(defaultASICTemperature, telemetryVariation),
		MaxASICTempC:     applyVariation(defaultASICTemperature+5, telemetryVariation),
	}

	// If ASIC ID is specified
	if len(parts) > 1 {
		asicID, err := strconv.Atoi(parts[1])
		if err != nil || asicID < 0 || asicID >= defaultASICCount {
			h.writeError(w, http.StatusNotFound, "NOT_FOUND", "ASIC not found")
			return
		}

		asicHashrate := applyVariation(defaultASICHashrate, telemetryVariation)
		if state != "Mining" {
			asicHashrate = 0
		}

		row := asicID / 10
		col := asicID % 10
		asic := ASICStats{
			Index:            asicID,
			ID:               fmt.Sprintf("%c%d", 'A'+row, col),
			Row:              row,
			Column:           col,
			HashrateGHS:      asicHashrate * 1000,
			IdealHashrateGHS: defaultASICHashrate * 1000,
			TempC:            applyVariation(defaultASICTemperature, telemetryVariation),
			FreqMHz:          applyVariation(600, telemetryVariation),
			ErrorRate:        applyVariation(0.01, 1.0),
		}

		h.writeJSON(w, http.StatusOK, map[string]ASICStats{"asic-stats": asic})
		return
	}

	h.writeJSON(w, http.StatusOK, HashboardStats{HashboardStats: hbStats})
}

// Telemetry data handlers (hashrate, temperature, power, efficiency)

func (h *RESTApiHandler) handleHashrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	hashrate, _, _, _ := h.state.GetMinerTelemetry()
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"hashrate-data": map[string]interface{}{
			"aggregates": map[string]float64{
				"avg": hashrate * 1000, // TH/s to GH/s
				"min": hashrate * 1000 * 0.95,
				"max": hashrate * 1000 * 1.05,
			},
			"data":     []interface{}{},
			"duration": "1h",
		},
	})
}

func (h *RESTApiHandler) handleHashrateByID(w http.ResponseWriter, r *http.Request) {
	h.handleHashrate(w, r)
}

func (h *RESTApiHandler) handleTemperature(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	_, temperature, _, _ := h.state.GetMinerTelemetry()
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"temperature-data": map[string]interface{}{
			"aggregates": map[string]float64{
				"avg": temperature,
				"min": temperature - 5,
				"max": temperature + 5,
			},
			"data":     []interface{}{},
			"duration": "1h",
		},
	})
}

func (h *RESTApiHandler) handleTemperatureByID(w http.ResponseWriter, r *http.Request) {
	h.handleTemperature(w, r)
}

func (h *RESTApiHandler) handlePower(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	_, _, power, _ := h.state.GetMinerTelemetry()
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"power-data": map[string]interface{}{
			"aggregates": map[string]float64{
				"avg": power,
				"min": power * 0.95,
				"max": power * 1.05,
			},
			"data":     []interface{}{},
			"duration": "1h",
		},
	})
}

func (h *RESTApiHandler) handlePowerByID(w http.ResponseWriter, r *http.Request) {
	h.handlePower(w, r)
}

func (h *RESTApiHandler) handleEfficiency(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	_, _, _, efficiency := h.state.GetMinerTelemetry()
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"efficiency-data": map[string]interface{}{
			"aggregates": map[string]float64{
				"avg": efficiency,
				"min": efficiency * 0.95,
				"max": efficiency * 1.05,
			},
			"data":     []interface{}{},
			"duration": "1h",
		},
	})
}

func (h *RESTApiHandler) handleEfficiencyByID(w http.ResponseWriter, r *http.Request) {
	h.handleEfficiency(w, r)
}

// PSU handlers

func (h *RESTApiHandler) handlePowerSupplies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	psus := make([]PSUStatus, 0, defaultPSUCount)
	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			continue
		}

		state := "Ready"
		if h.state.IsPSUInError(i) {
			state = "Error"
		}

		psus = append(psus, PSUStatus{
			Slot:           i,
			SerialNumber:   fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			State:          state,
			InputVoltageV:  applyVariation(defaultPSUInputVoltage, telemetryVariation),
			OutputVoltageV: applyVariation(defaultPSUOutputVoltage, telemetryVariation),
			InputCurrentA:  applyVariation(defaultPSUInputCurrent, telemetryVariation),
			OutputCurrentA: applyVariation(defaultPSUOutputCurrent, telemetryVariation),
			InputPowerW:    applyVariation(defaultPSUInputPower, telemetryVariation),
			OutputPowerW:   applyVariation(defaultPSUOutputPower, telemetryVariation),
			HotspotTempC:   applyVariation(defaultPSUHotspotTemp, telemetryVariation),
			AmbientTempC:   applyVariation(defaultPSUAmbientTemp, telemetryVariation),
		})
	}

	h.writeJSON(w, http.StatusOK, PowerSuppliesResponse{PSUs: psus})
}

func (h *RESTApiHandler) handlePowerSuppliesUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	h.writeJSON(w, http.StatusAccepted, MessageResponse{Message: "PSU firmware update started"})
}

// Cooling handlers

func (h *RESTApiHandler) handleCooling(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.state.mu.RLock()
		mode := h.state.CoolingModeVal
		speedPct := h.state.FanSpeedPct
		targetTempC := h.state.TargetTempC
		h.state.mu.RUnlock()

		fans := make([]FanStatus, 4)
		for i := range 4 {
			rpm := int(applyVariation(float64(defaultFanSpeedRPM), telemetryVariation))
			pct := int(speedPct)
			fans[i] = FanStatus{
				Slot:            i + 1, // 1-based slot number
				RPM:             rpm,
				SpeedPercentage: &pct,
			}
		}

		status := CoolingStatusInner{
			FanMode:         h.coolingModeToString(mode),
			SpeedPercentage: int(speedPct),
			Fans:            fans,
		}
		if mode == CoolingModeAuto {
			status.TargetTempC = &targetTempC
		}

		h.writeJSON(w, http.StatusOK, CoolingStatus{CoolingStatus: status})

	case http.MethodPut:
		var config CoolingConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
			return
		}

		mode := h.stringToCoolingMode(config.Mode)
		var speedPct *uint32
		if config.SpeedPercentage != nil {
			sp := uint32(*config.SpeedPercentage)
			speedPct = &sp
		}
		targetTempC := config.TargetTempC
		if mode != CoolingModeAuto {
			targetTempC = nil
		}
		h.state.SetCoolingMode(mode, speedPct, targetTempC)

		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Cooling configuration updated"})

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

// Network handlers

func (h *RESTApiHandler) handleNetwork(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.state.mu.RLock()
		defer h.state.mu.RUnlock()

		h.writeJSON(w, http.StatusOK, NetworkInfo{
			NetworkInfo: NetworkInfoInner{
				Hostname:   h.state.Hostname,
				MACAddress: h.state.MacAddress,
				IPAddress:  h.state.IPAddress,
				Netmask:    h.state.NetMask,
				Gateway:    h.state.Gateway,
				DHCP:       h.state.DHCP,
			},
		})

	case http.MethodPut:
		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Network configuration updated"})

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

// Error handlers

func (h *RESTApiHandler) handleErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	h.writeJSON(w, http.StatusOK, []NotificationError{})
}

// Telemetry handlers

func (h *RESTApiHandler) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	// Parse level query parameter
	levels := r.URL.Query()["level"]
	if len(levels) == 0 {
		levels = []string{"miner"}
	}

	hashrate, temperature, power, efficiency := h.state.GetMinerTelemetry()
	miningState := h.state.GetMiningState()

	response := TelemetryResponse{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Parse levels - handle both comma-separated and multiple params
	var parsedLevels []string
	for _, level := range levels {
		for _, l := range strings.Split(level, ",") {
			parsedLevels = append(parsedLevels, strings.TrimSpace(l))
		}
	}

	for _, level := range parsedLevels {
		switch strings.ToLower(level) {
		case "miner":
			response.Miner = &MinerTelemetry{
				Hashrate:    MetricValue{Value: hashrate, Unit: "TH/s"},
				Temperature: MetricValue{Value: temperature, Unit: "°C"},
				Power:       MetricValue{Value: power, Unit: "W"},
				Efficiency:  MetricValue{Value: efficiency, Unit: "J/TH"},
			}

		case "hashboard":
			response.Hashboards = h.generateHashboardsTelemetry(miningState, false)

		case "asic":
			response.Hashboards = h.generateHashboardsTelemetry(miningState, true)

		case "psu":
			response.PSUs = h.generatePSUsTelemetry()
		}
	}

	h.writeJSON(w, http.StatusOK, response)
}

func (h *RESTApiHandler) generateHashboardsTelemetry(miningState MiningState, includeASICs bool) []HashboardTelemetry {
	hashboards := make([]HashboardTelemetry, 0, defaultHashboardCount)

	for i := range defaultHashboardCount {
		if h.state.IsHashboardMissing(i) {
			continue
		}

		hbHashrate := applyVariation(defaultHashboardHashrate, telemetryVariation)
		if miningState != MiningStateMining || h.state.IsHashboardInError(i) {
			hbHashrate = 0
		}

		hb := HashboardTelemetry{
			Index:        i,
			SerialNumber: fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, i),
			Hashrate:     MetricValue{Value: hbHashrate, Unit: "TH/s"},
			Temperature: HashboardTemperature{
				Unit:    "°C",
				Inlet:   applyVariation(defaultHashboardInletTemp, telemetryVariation),
				Outlet:  applyVariation(defaultHashboardOutletTemp, telemetryVariation),
				Average: applyVariation(defaultHashboardAvgTemp, telemetryVariation),
			},
			Power:      MetricValue{Value: applyVariation(defaultHashboardPower, telemetryVariation), Unit: "W"},
			Efficiency: MetricValue{Value: applyVariation(defaultEfficiencyJTH, telemetryVariation), Unit: "J/TH"},
			Voltage:    &MetricValue{Value: applyVariation(defaultHashboardVoltage, telemetryVariation), Unit: "V"},
			Current:    &MetricValue{Value: applyVariation(defaultHashboardCurrent, telemetryVariation), Unit: "A"},
		}

		if includeASICs {
			hb.ASICs = h.generateASICsTelemetry(miningState, i)
		}

		hashboards = append(hashboards, hb)
	}

	return hashboards
}

func (h *RESTApiHandler) generateASICsTelemetry(miningState MiningState, hashboardIdx int) *ASICTelemetry {
	hashrates := make([]float64, defaultASICCount)
	temps := make([]float64, defaultASICCount)

	for i := range defaultASICCount {
		asicHashrate := applyVariation(defaultASICHashrate, telemetryVariation)
		if miningState != MiningStateMining || h.state.IsHashboardInError(hashboardIdx) {
			asicHashrate = 0
		}
		hashrates[i] = asicHashrate
		temps[i] = applyVariation(defaultASICTemperature, telemetryVariation)
	}

	return &ASICTelemetry{
		Hashrate:    MetricArray{Unit: "TH/s", Values: hashrates},
		Temperature: MetricArray{Unit: "°C", Values: temps},
	}
}

func (h *RESTApiHandler) generatePSUsTelemetry() []PSUTelemetry {
	psus := make([]PSUTelemetry, 0, defaultPSUCount)

	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			continue
		}

		hotspotTemp := applyVariation(defaultPSUHotspotTemp, telemetryVariation)
		ambientTemp := applyVariation(defaultPSUAmbientTemp, telemetryVariation)
		avgTemp := (hotspotTemp + ambientTemp) / 2

		psus = append(psus, PSUTelemetry{
			Index:        i,
			SerialNumber: fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			Voltage: PsuInputOutputMetric{
				Input:  applyVariation(defaultPSUInputVoltage, telemetryVariation),
				Output: applyVariation(defaultPSUOutputVoltage, telemetryVariation),
				Unit:   "V",
			},
			Current: PsuInputOutputMetric{
				Input:  applyVariation(defaultPSUInputCurrent, telemetryVariation),
				Output: applyVariation(defaultPSUOutputCurrent, telemetryVariation),
				Unit:   "A",
			},
			Power: PsuInputOutputMetric{
				Input:  applyVariation(defaultPSUInputPower, telemetryVariation),
				Output: applyVariation(defaultPSUOutputPower, telemetryVariation),
				Unit:   "W",
			},
			Temperature: PsuTemperature{
				Hotspot: hotspotTemp,
				Ambient: ambientTemp,
				Average: avgTemp,
				Unit:    "°C",
			},
		})
	}

	return psus
}

// Pairing handlers

func (h *RESTApiHandler) handlePairingInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	h.writeJSON(w, http.StatusOK, PairingInfoResponse{
		MAC:  h.state.MacAddress,
		CBSN: h.state.SerialNumber,
	})
}

func (h *RESTApiHandler) handlePairingAuthKey(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// Key rotation requires auth when already paired
		if existing := h.state.GetAuthKey(); existing != "" {
			if !h.isAuthorized(r) {
				h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required for key rotation")
				return
			}
			if h.state.IsDefaultPasswordActive() {
				h.writeError(w, http.StatusForbidden, "DEFAULT_PASSWORD_ACTIVE", "Default password must be changed before accessing this resource")
				return
			}
		}

		var req SetAuthKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
			return
		}

		if req.PublicKey == "" {
			h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "public_key is required")
			return
		}

		h.state.SetAuthKey(req.PublicKey)
		h.writeJSON(w, http.StatusOK, SetAuthKeyResponse{Message: "Auth key set successfully"})

	case http.MethodDelete:
		h.state.ClearAuthKey()
		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Auth key cleared successfully"})

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	}
}

func (h *RESTApiHandler) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	// Parse request body
	var req TimeSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	// Validate required fields per OpenAPI spec
	if req.StartTime == "" {
		h.writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "start_time is required")
		return
	}
	if len(req.Levels) == 0 {
		h.writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "levels is required and must have at least one item")
		return
	}

	// Generate mock time series response
	response := h.generateTimeSeriesResponse(req)
	h.writeJSON(w, http.StatusOK, response)
}

// TimeSeriesRequest is the request for time series data
type TimeSeriesRequest struct {
	StartTime   string                  `json:"start_time"`
	EndTime     string                  `json:"end_time,omitempty"`
	Duration    string                  `json:"duration,omitempty"`
	Interval    string                  `json:"interval,omitempty"`
	Aggregation string                  `json:"aggregation,omitempty"`
	Levels      []TimeSeriesLevelConfig `json:"levels"`
}

// TimeSeriesLevelConfig is the configuration for a level in time series query
type TimeSeriesLevelConfig struct {
	Type    string   `json:"type"`
	Fields  []string `json:"fields"`
	Indexes []int    `json:"indexes,omitempty"`
}

// TimeSeriesResponse is the response for time series data
type TimeSeriesResponse struct {
	Data *TimeSeriesData `json:"data,omitempty"`
	Meta *TimeSeriesMeta `json:"meta,omitempty"`
}

// TimeSeriesData contains hierarchical data organized by level
type TimeSeriesData struct {
	Miner      map[string]*TimeSeriesMetricData `json:"miner,omitempty"`
	Hashboards []TimeSeriesHashboardData        `json:"hashboards,omitempty"`
	ASICs      []TimeSeriesASICData             `json:"asics,omitempty"`
	PSUs       []TimeSeriesPSUData              `json:"psus,omitempty"`
}

// TimeSeriesHashboardData contains hashboard-level time series data
type TimeSeriesHashboardData struct {
	Index        int                              `json:"index"`
	SerialNumber string                           `json:"serial_number,omitempty"`
	Metrics      map[string]*TimeSeriesMetricData `json:"-"`
}

// MarshalJSON implements custom JSON marshaling for TimeSeriesHashboardData
func (d TimeSeriesHashboardData) MarshalJSON() ([]byte, error) {
	result := map[string]any{
		"index":         d.Index,
		"serial_number": d.SerialNumber,
	}
	for k, v := range d.Metrics {
		result[k] = v
	}
	return json.Marshal(result)
}

// TimeSeriesASICData contains ASIC-level time series data
type TimeSeriesASICData struct {
	HashboardIndex int                              `json:"hashboard_index"`
	ASICIndex      int                              `json:"asic_index"`
	Metrics        map[string]*TimeSeriesMetricData `json:"-"`
}

// MarshalJSON implements custom JSON marshaling for TimeSeriesASICData
func (d TimeSeriesASICData) MarshalJSON() ([]byte, error) {
	result := map[string]any{
		"hashboard_index": d.HashboardIndex,
		"asic_index":      d.ASICIndex,
	}
	for k, v := range d.Metrics {
		result[k] = v
	}
	return json.Marshal(result)
}

// TimeSeriesPSUData contains PSU-level time series data
type TimeSeriesPSUData struct {
	Index        int                              `json:"index"`
	SerialNumber string                           `json:"serial_number,omitempty"`
	Metrics      map[string]*TimeSeriesMetricData `json:"-"`
}

// MarshalJSON implements custom JSON marshaling for TimeSeriesPSUData
func (d TimeSeriesPSUData) MarshalJSON() ([]byte, error) {
	result := map[string]any{
		"index":         d.Index,
		"serial_number": d.SerialNumber,
	}
	for k, v := range d.Metrics {
		result[k] = v
	}
	return json.Marshal(result)
}

// TimeSeriesMetricData contains data series for a specific metric
type TimeSeriesMetricData struct {
	Unit       string                `json:"unit,omitempty"`
	Values     []float64             `json:"values,omitempty"`
	Aggregates *TimeSeriesAggregates `json:"aggregates,omitempty"`
}

// TimeSeriesAggregates contains statistical aggregates
type TimeSeriesAggregates struct {
	Avg float64 `json:"avg,omitempty"`
	Max float64 `json:"max,omitempty"`
	Min float64 `json:"min,omitempty"`
}

// TimeSeriesMeta contains metadata about the time series response
type TimeSeriesMeta struct {
	StartTime   string                  `json:"start_time,omitempty"`
	EndTime     string                  `json:"end_time,omitempty"`
	Interval    string                  `json:"interval,omitempty"`
	Aggregation string                  `json:"aggregation,omitempty"`
	Levels      []TimeSeriesLevelConfig `json:"levels,omitempty"`
}

func (h *RESTApiHandler) generateTimeSeriesResponse(req TimeSeriesRequest) *TimeSeriesResponse {
	now := time.Now()

	// Default values
	aggregation := "mean"
	if req.Aggregation != "" {
		aggregation = req.Aggregation
	}

	interval := "PT5M"
	if req.Interval != "" {
		interval = req.Interval
	}

	// Generate 12 data points (1 hour of 5-minute intervals)
	const numPoints = 12

	response := &TimeSeriesResponse{
		Meta: &TimeSeriesMeta{
			StartTime:   now.Add(-time.Hour).Format(time.RFC3339),
			EndTime:     now.Format(time.RFC3339),
			Interval:    interval,
			Aggregation: aggregation,
			Levels:      req.Levels,
		},
		Data: &TimeSeriesData{},
	}

	// Process each level
	for _, level := range req.Levels {
		switch level.Type {
		case "miner":
			response.Data.Miner = h.generateMinerTimeSeries(level.Fields, numPoints)
		case "hashboard":
			response.Data.Hashboards = h.generateHashboardTimeSeries(level.Fields, level.Indexes, numPoints)
		case "asic":
			response.Data.ASICs = h.generateASICTimeSeries(level.Fields, level.Indexes, numPoints)
		case "psu":
			response.Data.PSUs = h.generatePSUTimeSeries(level.Fields, level.Indexes, numPoints)
		}
	}

	return response
}

func (h *RESTApiHandler) generateMinerTimeSeries(fields []string, numPoints int) map[string]*TimeSeriesMetricData {
	result := make(map[string]*TimeSeriesMetricData)

	fieldUnits := map[string]string{
		"hashrate":    "TH/s",
		"temperature": "°C",
		"power":       "W",
		"efficiency":  "J/TH",
	}

	fieldBaseValues := map[string]float64{
		"hashrate":    defaultHashrateTHS,
		"temperature": defaultTemperatureC,
		"power":       defaultPowerW,
		"efficiency":  defaultEfficiencyJTH,
	}

	for _, field := range fields {
		unit, ok := fieldUnits[field]
		if !ok {
			continue
		}
		baseValue := fieldBaseValues[field]

		values := make([]float64, numPoints)
		var sum, min, max float64
		min = baseValue * 2 // Start high for min

		for i := range numPoints {
			values[i] = applyVariation(baseValue, telemetryVariation)
			sum += values[i]
			if values[i] < min {
				min = values[i]
			}
			if values[i] > max {
				max = values[i]
			}
		}

		result[field] = &TimeSeriesMetricData{
			Unit:   unit,
			Values: values,
			Aggregates: &TimeSeriesAggregates{
				Avg: sum / float64(numPoints),
				Min: min,
				Max: max,
			},
		}
	}

	return result
}

func (h *RESTApiHandler) generateHashboardTimeSeries(fields []string, indexes []int, numPoints int) []TimeSeriesHashboardData {
	var result []TimeSeriesHashboardData

	// If no indexes specified, include all hashboards
	if len(indexes) == 0 {
		for i := range defaultHashboardCount {
			indexes = append(indexes, i)
		}
	}

	fieldUnits := map[string]string{
		"hashrate":    "TH/s",
		"inletTemp":   "°C",
		"outletTemp":  "°C",
		"temperature": "°C",
		"power":       "W",
		"efficiency":  "J/TH",
	}

	for _, idx := range indexes {
		if h.state.IsHashboardMissing(idx) {
			continue
		}

		hbData := TimeSeriesHashboardData{
			Index:        idx,
			SerialNumber: fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, idx),
			Metrics:      make(map[string]*TimeSeriesMetricData),
		}

		for _, field := range fields {
			unit, ok := fieldUnits[field]
			if !ok {
				continue
			}

			var baseValue float64
			switch field {
			case "hashrate":
				baseValue = defaultHashboardHashrate
			case "inletTemp":
				baseValue = defaultHashboardInletTemp
			case "outletTemp":
				baseValue = defaultHashboardOutletTemp
			case "temperature":
				baseValue = defaultHashboardAvgTemp
			case "power":
				baseValue = defaultHashboardPower
			case "efficiency":
				baseValue = defaultEfficiencyJTH
			}

			values := make([]float64, numPoints)
			var sum, min, max float64
			min = baseValue * 2

			for i := range numPoints {
				values[i] = applyVariation(baseValue, telemetryVariation)
				sum += values[i]
				if values[i] < min {
					min = values[i]
				}
				if values[i] > max {
					max = values[i]
				}
			}

			hbData.Metrics[field] = &TimeSeriesMetricData{
				Unit:   unit,
				Values: values,
				Aggregates: &TimeSeriesAggregates{
					Avg: sum / float64(numPoints),
					Min: min,
					Max: max,
				},
			}
		}

		result = append(result, hbData)
	}

	return result
}

func (h *RESTApiHandler) generateASICTimeSeries(fields []string, indexes []int, numPoints int) []TimeSeriesASICData {
	var result []TimeSeriesASICData

	// ASIC-level data: for each hashboard, for each ASIC
	// If no indexes specified, include all ASICs from all hashboards
	fieldUnits := map[string]string{
		"hashrate":    "TH/s",
		"temperature": "°C",
	}

	fieldBaseValues := map[string]float64{
		"hashrate":    defaultASICHashrate,
		"temperature": defaultASICTemperature,
	}

	for hbIdx := range defaultHashboardCount {
		if h.state.IsHashboardMissing(hbIdx) {
			continue
		}

		// For each ASIC on the hashboard
		for asicIdx := range defaultASICCount {
			// If indexes specified, filter by ASIC index
			if len(indexes) > 0 {
				found := false
				for _, idx := range indexes {
					if idx == asicIdx {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			asicData := TimeSeriesASICData{
				HashboardIndex: hbIdx,
				ASICIndex:      asicIdx,
				Metrics:        make(map[string]*TimeSeriesMetricData),
			}

			for _, field := range fields {
				unit, ok := fieldUnits[field]
				if !ok {
					continue
				}
				baseValue := fieldBaseValues[field]

				values := make([]float64, numPoints)
				var sum, min, max float64
				min = baseValue * 2

				for i := range numPoints {
					values[i] = applyVariation(baseValue, telemetryVariation)
					sum += values[i]
					if values[i] < min {
						min = values[i]
					}
					if values[i] > max {
						max = values[i]
					}
				}

				asicData.Metrics[field] = &TimeSeriesMetricData{
					Unit:   unit,
					Values: values,
					Aggregates: &TimeSeriesAggregates{
						Avg: sum / float64(numPoints),
						Min: min,
						Max: max,
					},
				}
			}

			result = append(result, asicData)
		}
	}

	return result
}

func (h *RESTApiHandler) generatePSUTimeSeries(fields []string, indexes []int, numPoints int) []TimeSeriesPSUData {
	var result []TimeSeriesPSUData

	// If no indexes specified, include all PSUs
	if len(indexes) == 0 {
		for i := range defaultPSUCount {
			indexes = append(indexes, i)
		}
	}

	fieldUnits := map[string]string{
		"outputVoltage": "V",
		"outputCurrent": "A",
		"outputPower":   "W",
		"inputVoltage":  "V",
		"inputCurrent":  "A",
		"inputPower":    "W",
		"hotspotTemp":   "°C",
		"ambientTemp":   "°C",
		"averageTemp":   "°C",
	}

	fieldBaseValues := map[string]float64{
		"outputVoltage": defaultPSUOutputVoltage,
		"outputCurrent": defaultPSUOutputCurrent,
		"outputPower":   defaultPSUOutputPower,
		"inputVoltage":  defaultPSUInputVoltage,
		"inputCurrent":  defaultPSUInputCurrent,
		"inputPower":    defaultPSUInputPower,
		"hotspotTemp":   defaultPSUHotspotTemp,
		"ambientTemp":   defaultPSUAmbientTemp,
		"averageTemp":   (defaultPSUHotspotTemp + defaultPSUAmbientTemp) / 2,
	}

	for _, idx := range indexes {
		if h.state.IsPSUMissing(idx) {
			continue
		}

		psuData := TimeSeriesPSUData{
			Index:        idx,
			SerialNumber: fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, idx),
			Metrics:      make(map[string]*TimeSeriesMetricData),
		}

		for _, field := range fields {
			unit, ok := fieldUnits[field]
			if !ok {
				continue
			}
			baseValue := fieldBaseValues[field]

			values := make([]float64, numPoints)
			var sum, min, max float64
			min = baseValue * 2

			for i := range numPoints {
				values[i] = applyVariation(baseValue, telemetryVariation)
				sum += values[i]
				if values[i] < min {
					min = values[i]
				}
				if values[i] > max {
					max = values[i]
				}
			}

			psuData.Metrics[field] = &TimeSeriesMetricData{
				Unit:   unit,
				Values: values,
				Aggregates: &TimeSeriesAggregates{
					Avg: sum / float64(numPoints),
					Min: min,
					Max: max,
				},
			}
		}

		result = append(result, psuData)
	}

	return result
}
