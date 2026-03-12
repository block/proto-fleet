package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_command_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
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
	State            string `json:"state"`
	AvailableVersion string `json:"available_version,omitempty"`
	CurrentVersion   string `json:"current_version"`
}

// SystemStatuses contains system onboarding status
type SystemStatuses struct {
	Onboarded   bool `json:"onboarded"`
	PasswordSet bool `json:"password_set"`
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
	Mode            string `json:"mode"`
	SpeedPercentage *int   `json:"speed_percentage,omitempty"`
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
	Row              int     `json:"row"`
	Column           int     `json:"column"`
	HashrateGHS      float64 `json:"hashrate_ghs"`
	IdealHashrateGHS float64 `json:"ideal_hashrate_ghs"`
	TempC            float64 `json:"temp_c"`
	FreqMHz          float64 `json:"freq_mhz"`
	ErrorRate        float64 `json:"error_rate"`
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

// RegisterRoutes registers all REST API routes
func (h *RESTApiHandler) RegisterRoutes(mux *http.ServeMux) {
	// Pools
	mux.HandleFunc("/api/v1/pools", h.handlePools)
	mux.HandleFunc("/api/v1/pools/", h.handlePoolByID)
	mux.HandleFunc("/api/v1/pools/test-connection", h.handleTestPoolConnection)

	// Auth
	mux.HandleFunc("/api/v1/auth/login", h.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", h.handleLogout)
	mux.HandleFunc("/api/v1/auth/refresh", h.handleRefresh)
	mux.HandleFunc("/api/v1/auth/password", h.handleSetPassword)
	mux.HandleFunc("/api/v1/auth/change-password", h.handleChangePassword)

	// System
	mux.HandleFunc("/api/v1/system", h.handleSystem)
	mux.HandleFunc("/api/v1/system/status", h.handleSystemStatus)
	mux.HandleFunc("/api/v1/system/reboot", h.handleReboot)
	mux.HandleFunc("/api/v1/system/locate", h.handleLocate)
	mux.HandleFunc("/api/v1/system/logs", h.handleLogs)
	mux.HandleFunc("/api/v1/system/update", h.handleUpdate)
	mux.HandleFunc("/api/v1/system/update/check", h.handleUpdateCheck)
	mux.HandleFunc("/api/v1/system/ssh", h.handleSSH)
	mux.HandleFunc("/api/v1/system/unlock", h.handleUnlock)
	mux.HandleFunc("/api/v1/system/tag", h.handleTag)
	mux.HandleFunc("/api/v1/system/telemetry", h.handleTelemetryConfig)

	// Mining
	mux.HandleFunc("/api/v1/mining", h.handleMining)
	mux.HandleFunc("/api/v1/mining/target", h.handleMiningTarget)
	mux.HandleFunc("/api/v1/mining/tuning", h.handleMiningTuning)
	mux.HandleFunc("/api/v1/mining/start", h.handleMiningStart)
	mux.HandleFunc("/api/v1/mining/stop", h.handleMiningStop)

	// Hardware
	mux.HandleFunc("/api/v1/hardware", h.handleHardware)
	mux.HandleFunc("/api/v1/hardware/psus", h.handleHardwarePSUs)
	mux.HandleFunc("/api/v1/hashboards", h.handleHashboards)
	mux.HandleFunc("/api/v1/hashboards/", h.handleHashboardByID)

	// Telemetry data
	mux.HandleFunc("/api/v1/hashrate", h.handleHashrate)
	mux.HandleFunc("/api/v1/hashrate/", h.handleHashrateByID)
	mux.HandleFunc("/api/v1/temperature", h.handleTemperature)
	mux.HandleFunc("/api/v1/temperature/", h.handleTemperatureByID)
	mux.HandleFunc("/api/v1/power", h.handlePower)
	mux.HandleFunc("/api/v1/power/", h.handlePowerByID)
	mux.HandleFunc("/api/v1/efficiency", h.handleEfficiency)
	mux.HandleFunc("/api/v1/efficiency/", h.handleEfficiencyByID)

	// PSUs
	mux.HandleFunc("/api/v1/power-supplies", h.handlePowerSupplies)
	mux.HandleFunc("/api/v1/power-supplies/update", h.handlePowerSuppliesUpdate)

	// Cooling
	mux.HandleFunc("/api/v1/cooling", h.handleCooling)

	// Network
	mux.HandleFunc("/api/v1/network", h.handleNetwork)

	// Errors
	mux.HandleFunc("/api/v1/errors", h.handleErrors)

	// Telemetry
	mux.HandleFunc("/api/v1/telemetry", h.handleTelemetry)
	mux.HandleFunc("/api/v1/timeseries", h.handleTimeseries)
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

func (h *RESTApiHandler) miningStateToString(state miner_data_api.MiningState) string {
	switch state {
	case miner_data_api.MiningState_MINING_STATE_MINING:
		return "Mining"
	case miner_data_api.MiningState_MINING_STATE_STOPPED:
		return "Stopped"
	case miner_data_api.MiningState_MINING_STATE_NO_POOLS:
		return "NoPools"
	case miner_data_api.MiningState_MINING_STATE_POWERING_ON:
		return "PoweringOn"
	case miner_data_api.MiningState_MINING_STATE_DEGRADED_MINING:
		return "DegradedMining"
	case miner_data_api.MiningState_MINING_STATE_POWERING_OFF:
		return "PoweringOff"
	case miner_data_api.MiningState_MINING_STATE_ERROR:
		return "Error"
	case miner_data_api.MiningState_MINING_STATE_UNINITIALIZED:
		return "Uninitialized"
	case miner_data_api.MiningState_MINING_STATE_UNKNOWN:
		return "Uninitialized"
	default:
		return "Uninitialized"
	}
}

func (h *RESTApiHandler) coolingModeToString(mode miner_data_api.CoolingMode) string {
	switch mode {
	case miner_data_api.CoolingMode_COOLING_MODE_AUTO:
		return "Auto"
	case miner_data_api.CoolingMode_COOLING_MODE_MANUAL:
		return "Manual"
	case miner_data_api.CoolingMode_COOLING_MODE_OFF:
		return "Off"
	default:
		return "Unknown"
	}
}

func (h *RESTApiHandler) stringToCoolingMode(s string) miner_data_api.CoolingMode {
	switch strings.ToLower(s) {
	case "auto":
		return miner_data_api.CoolingMode_COOLING_MODE_AUTO
	case "manual":
		return miner_data_api.CoolingMode_COOLING_MODE_MANUAL
	case "off":
		return miner_data_api.CoolingMode_COOLING_MODE_OFF
	default:
		return miner_data_api.CoolingMode_COOLING_MODE_UNKNOWN
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

		poolList[i] = PoolData{
			ID:               int(p.Idx),
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
	h.state.mu.Lock()
	h.state.Pools = make([]*miner_data_api.Pool, 0)
	h.state.mu.Unlock()

	// Add new pools
	for i, p := range pools {
		pool := &miner_data_api.Pool{
			Idx:      uint32(i),
			Url:      p.URL,
			Username: p.Username,
			Password: p.Password,
			Statistics: &miner_data_api.PoolStatistics{
				AcceptedShares:    defaultPoolAcceptedShares,
				RejectedShares:    defaultPoolRejectedShares,
				CurrentDifficulty: defaultPoolDifficulty,
			},
		}
		h.state.AddPool(pool)
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
			if config.URL != "" {
				p.Url = config.URL
			}
			if config.Username != "" {
				p.Username = config.Username
			}
			if config.Password != "" {
				p.Password = config.Password
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

	h.writeJSON(w, http.StatusOK, AuthTokens{
		AccessToken:  "mock-access-token-" + time.Now().Format(time.RFC3339),
		RefreshToken: "mock-refresh-token-" + time.Now().Format(time.RFC3339),
	})
}

func (h *RESTApiHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Logged out successfully"})
}

func (h *RESTApiHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	h.writeJSON(w, http.StatusOK, AuthTokens{
		AccessToken:  "mock-access-token-refreshed-" + time.Now().Format(time.RFC3339),
		RefreshToken: "mock-refresh-token-refreshed-" + time.Now().Format(time.RFC3339),
	})
}

func (h *RESTApiHandler) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
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
	h.state.SetAuthKey("mock-password-hash")

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

	h.state.SetPassword(req.NewPassword)

	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Password changed successfully"})
}

// System handlers

func (h *RESTApiHandler) handleSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	uptime := int64(time.Since(h.state.StartTime).Seconds())

	h.writeJSON(w, http.StatusOK, SystemInfo{
		SystemInfo: SystemInfoInner{
			ProductName:   h.state.Model,
			Board:         "C3",
			CBSN:          h.state.SerialNumber,
			SOC:           "STM32MP157F",
			UptimeSeconds: uptime,
			OS: OSInfo{
				Name:     "ProtoOS",
				Version:  defaultFirmwareVersion,
				Variant:  "production",
				GitHash:  "abc123def456",
				Hostname: h.state.Hostname,
			},
			SWUpdateState: UpdateStatus{
				State:          "idle",
				CurrentVersion: defaultFirmwareVersion,
			},
			MiningDriverSW: &SWInfo{
				Name:    "mcdd",
				Version: defaultFirmwareVersion,
			},
			WebServer: &SWInfo{
				Name:    "miner-api-server",
				Version: defaultFirmwareVersion,
			},
			WebDashboard: &SWInfo{
				Name:    "miner-web",
				Version: defaultFirmwareVersion,
			},
			PoolInterfaceSW: &SWInfo{
				Name:    "stratum-client",
				Version: defaultFirmwareVersion,
			},
			HashboardFirmware: &SWInfo{
				Name:    "hashboard-fw",
				Version: defaultFirmwareVersion,
			},
		},
	})
}

func (h *RESTApiHandler) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	// Check if password is set (auth key configured)
	passwordSet := h.state.GetAuthKey() != ""

	h.writeJSON(w, http.StatusOK, SystemStatuses{
		Onboarded:   h.state.IsOnboarded(),
		PasswordSet: passwordSet,
	})
}

func (h *RESTApiHandler) handleReboot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Reboot initiated"})
}

func (h *RESTApiHandler) handleLocate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	var req struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Default to toggle
		h.state.mu.Lock()
		h.state.LocateActive = !h.state.LocateActive
		h.state.mu.Unlock()
	} else {
		h.state.SetLocateActive(req.Active)
	}

	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Locate sequence activated"})
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

		file, header, err := r.FormFile("file")
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Missing or invalid 'file' field in multipart form")
			return
		}
		defer file.Close()

		log.Printf("Firmware upload received: filename=%s, size=%d", header.Filename, header.Size)
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
	h.writeJSON(w, http.StatusOK, UpdateStatus{
		State:          "idle",
		CurrentVersion: defaultFirmwareVersion,
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

func (h *RESTApiHandler) handleUnlock(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.writeJSON(w, http.StatusOK, map[string]bool{"unlocked": true})
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
		},
	})
}

// parsePerformanceMode maps an OpenAPI performance mode string to its protobuf enum.
func parsePerformanceMode(s string) (miner_data_api.PerformanceMode, bool) {
	switch s {
	case "MaximumHashrate":
		return miner_data_api.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE, true
	case "Efficiency":
		return miner_data_api.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY, true
	default:
		return 0, false
	}
}

func performanceModeToString(mode miner_data_api.PerformanceMode) string {
	if mode == miner_data_api.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY {
		return "Efficiency"
	}
	return "MaximumHashrate"
}

func (h *RESTApiHandler) handleMiningTarget(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.state.mu.RLock()
		powerTarget := h.state.PowerTargetW
		perfMode := h.state.PerformanceMode
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
		var perfMode miner_data_api.PerformanceMode
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
		mode := h.state.PerformanceMode
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
		updatedPerfMode := h.state.PerformanceMode
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
	h.state.SetMiningState(miner_data_api.MiningState_MINING_STATE_MINING)
	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Mining started"})
}

func (h *RESTApiHandler) handleMiningStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	h.state.SetMiningState(miner_data_api.MiningState_MINING_STATE_STOPPED)
	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Mining stopped"})
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

	algorithmMap := map[string]miner_command_api.TuningAlgorithm{
		"None":                         miner_command_api.TuningAlgorithm_None,
		"VoltageImbalanceCompensation": miner_command_api.TuningAlgorithm_VoltageImbalanceCompensation,
		"Fuzzing":                      miner_command_api.TuningAlgorithm_Fuzzing,
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
	} else if miningState != miner_data_api.MiningState_MINING_STATE_MINING {
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

		asic := ASICStats{
			Index:            asicID,
			Row:              asicID / 10,
			Column:           asicID % 10,
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
	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "PSU firmware update started"})
}

// Cooling handlers

func (h *RESTApiHandler) handleCooling(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.state.mu.RLock()
		mode := h.state.CoolingMode
		speedPct := h.state.FanSpeedPct
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

		h.writeJSON(w, http.StatusOK, CoolingStatus{
			CoolingStatus: CoolingStatusInner{
				FanMode:         h.coolingModeToString(mode),
				SpeedPercentage: int(speedPct),
				Fans:            fans,
			},
		})

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
		h.state.SetCoolingMode(mode, speedPct)

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

	// Return empty error list for healthy miner
	h.writeJSON(w, http.StatusOK, ErrorsResponse{})
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

func (h *RESTApiHandler) generateHashboardsTelemetry(miningState miner_data_api.MiningState, includeASICs bool) []HashboardTelemetry {
	hashboards := make([]HashboardTelemetry, 0, defaultHashboardCount)

	for i := range defaultHashboardCount {
		if h.state.IsHashboardMissing(i) {
			continue
		}

		hbHashrate := applyVariation(defaultHashboardHashrate, telemetryVariation)
		if miningState != miner_data_api.MiningState_MINING_STATE_MINING || h.state.IsHashboardInError(i) {
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

func (h *RESTApiHandler) generateASICsTelemetry(miningState miner_data_api.MiningState, hashboardIdx int) *ASICTelemetry {
	hashrates := make([]float64, defaultASICCount)
	temps := make([]float64, defaultASICCount)

	for i := range defaultASICCount {
		asicHashrate := applyVariation(defaultASICHashrate, telemetryVariation)
		if miningState != miner_data_api.MiningState_MINING_STATE_MINING || h.state.IsHashboardInError(hashboardIdx) {
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
