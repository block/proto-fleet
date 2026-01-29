package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
)

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

// PoolConfig is the pool configuration for creating/updating pools
type PoolConfig struct {
	Pools []PoolConfigInner `json:"pools"`
}

// PoolConfigInner is a single pool configuration
type PoolConfigInner struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
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
	ProductName   string       `json:"product_name"`
	Board         string       `json:"board"`
	CBSN          string       `json:"cb_sn"`
	SOC           string       `json:"soc"`
	UptimeSeconds int64        `json:"uptime_seconds"`
	OS            OSInfo       `json:"os"`
	SWUpdateState UpdateStatus `json:"sw_update_status"`
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

// MiningStatus contains mining status information
type MiningStatus struct {
	MiningStatus MiningStatusInner `json:"mining-status"`
}

// MiningStatusInner contains the inner mining status
type MiningStatusInner struct {
	State               string  `json:"state"`
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

// MiningTarget contains mining target configuration
type MiningTarget struct {
	MiningTarget MiningTargetInner `json:"mining-target"`
}

// MiningTargetInner contains inner mining target
type MiningTargetInner struct {
	PowerTargetWatts    int    `json:"power_target_watts"`
	PowerTargetMinWatts int    `json:"power_target_min_watts"`
	PowerTargetMaxWatts int    `json:"power_target_max_watts"`
	PerformanceMode     string `json:"performance_mode"`
}

// MiningTargetRequest is the request to set mining target
type MiningTargetRequest struct {
	PowerTargetWatts int    `json:"power_target_watts,omitempty"`
	PerformanceMode  string `json:"performance_mode,omitempty"`
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

// HashboardsResponse contains all hashboard stats
type HashboardsResponse struct {
	Hashboards []HashboardStatsInner `json:"hashboards"`
}

// HashboardStats contains stats for a single hashboard
type HashboardStats struct {
	HashboardStats HashboardStatsInner `json:"hashboard-stats"`
}

// HashboardStatsInner contains inner hashboard stats
type HashboardStatsInner struct {
	HBSN             string      `json:"hb_sn"`
	Slot             int         `json:"slot"`
	State            string      `json:"state"`
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

// HardwareInfoInner contains inner hardware info
type HardwareInfoInner struct {
	ControlBoard ControlBoardInfo `json:"control_board"`
	Hashboards   []HashboardInfo  `json:"hashboards"`
	PSUs         []PSUInfo        `json:"psus"`
	Fans         []FanInfo        `json:"fans"`
}

// ControlBoardInfo contains control board information
type ControlBoardInfo struct {
	MachineName  string `json:"machine_name"`
	BoardID      string `json:"board_id"`
	SerialNumber string `json:"serial_number"`
}

// HashboardInfo contains hashboard hardware info
type HashboardInfo struct {
	Slot            int    `json:"slot"`
	SerialNumber    string `json:"serial_number"`
	ChipID          string `json:"chip_id"`
	ASICCount       int    `json:"asic_count"`
	FirmwareVersion string `json:"firmware_version"`
}

// PSUInfo contains PSU hardware info
type PSUInfo struct {
	Slot            int    `json:"slot"`
	SerialNumber    string `json:"serial_number"`
	Vendor          string `json:"vendor"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmware_version"`
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

// TelemetryResponse contains telemetry data
type TelemetryResponse struct {
	Timestamp  string               `json:"timestamp"`
	Miner      *MinerTelemetry      `json:"miner,omitempty"`
	Hashboards []HashboardTelemetry `json:"hashboards,omitempty"`
	PSUs       []PSUTelemetry       `json:"psus,omitempty"`
}

// MinerTelemetry contains miner-level telemetry
type MinerTelemetry struct {
	HashrateTHS   float64 `json:"hashrate_ths"`
	TemperatureC  float64 `json:"temperature_c"`
	PowerW        float64 `json:"power_w"`
	EfficiencyJTH float64 `json:"efficiency_jth"`
}

// HashboardTelemetry contains hashboard-level telemetry
type HashboardTelemetry struct {
	Index               int            `json:"index"`
	SerialNumber        string         `json:"serial_number"`
	HashrateTHS         float64        `json:"hashrate_ths"`
	InletTemperatureC   float64        `json:"inlet_temperature_c"`
	OutletTemperatureC  float64        `json:"outlet_temperature_c"`
	AverageTemperatureC float64        `json:"average_temperature_c"`
	PowerW              float64        `json:"power_w"`
	EfficiencyJTH       float64        `json:"efficiency_jth"`
	ASICs               *ASICTelemetry `json:"asics,omitempty"`
}

// ASICTelemetry contains ASIC-level telemetry
type ASICTelemetry struct {
	HashrateTHS  []float64 `json:"hashrate_ths"`
	TemperatureC []float64 `json:"temperature_c"`
}

// PSUTelemetry contains PSU-level telemetry
type PSUTelemetry struct {
	Index               int     `json:"index"`
	SerialNumber        string  `json:"serial_number"`
	InputVoltageV       float64 `json:"input_voltage_v"`
	OutputVoltageV      float64 `json:"output_voltage_v"`
	InputCurrentA       float64 `json:"input_current_a"`
	OutputCurrentA      float64 `json:"output_current_a"`
	InputPowerW         float64 `json:"input_power_w"`
	OutputPowerW        float64 `json:"output_power_w"`
	HotspotTemperatureC float64 `json:"hotspot_temperature_c"`
	AmbientTemperatureC float64 `json:"ambient_temperature_c"`
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
		return "Starting"
	case miner_data_api.MiningState_MINING_STATE_DEGRADED_MINING:
		return "DegradedMining"
	case miner_data_api.MiningState_MINING_STATE_POWERING_OFF:
		return "PoweringOff"
	case miner_data_api.MiningState_MINING_STATE_ERROR:
		return "Error"
	default:
		return "Unknown"
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
	var config PoolConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	// Clear existing pools
	h.state.mu.Lock()
	h.state.Pools = make([]*miner_data_api.Pool, 0)
	h.state.mu.Unlock()

	// Add new pools
	for i, p := range config.Pools {
		pool := &miner_data_api.Pool{
			Idx:      uint32(i),
			Url:      p.URL,
			Username: p.User,
			Password: p.Password,
			Statistics: &miner_data_api.PoolStatistics{
				AcceptedShares:    defaultPoolAcceptedShares,
				RejectedShares:    defaultPoolRejectedShares,
				CurrentDifficulty: defaultPoolDifficulty,
			},
		}
		h.state.AddPool(pool)
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

	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	for _, p := range h.state.Pools {
		if int(p.Idx) == id {
			if config.URL != "" {
				p.Url = config.URL
			}
			if config.User != "" {
				p.Username = config.User
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
	// Simulate successful connection test
	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Connection test passed"})
}

// Auth handlers

func (h *RESTApiHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	// Return mock JWT tokens
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
	h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Password set successfully"})
}

func (h *RESTApiHandler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
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
		Onboarded:   len(h.state.GetPools()) > 0,
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
	// Return empty logs
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Proto Miner Simulator Logs\n[INFO] System running normally\n"))
}

func (h *RESTApiHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost, http.MethodPut:
		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Update started"})
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
			State:               h.miningStateToString(miningState),
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

func (h *RESTApiHandler) handleMiningTarget(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.state.mu.RLock()
		powerTarget := h.state.PowerTargetW
		perfMode := h.state.PerformanceMode
		h.state.mu.RUnlock()

		modeStr := "MaximumHashrate"
		if perfMode == miner_data_api.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY {
			modeStr = "MaximumEfficiency"
		}

		h.writeJSON(w, http.StatusOK, MiningTarget{
			MiningTarget: MiningTargetInner{
				PowerTargetWatts:    int(powerTarget),
				PowerTargetMinWatts: defaultPowerTargetMin,
				PowerTargetMaxWatts: defaultPowerTargetMax,
				PerformanceMode:     modeStr,
			},
		})

	case http.MethodPut:
		var req MiningTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
			return
		}

		perfMode := miner_data_api.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE
		if strings.ToLower(req.PerformanceMode) == "maximumefficiency" {
			perfMode = miner_data_api.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY
		}

		if req.PowerTargetWatts > 0 {
			h.state.SetPowerTarget(uint32(req.PowerTargetWatts), perfMode)
		}

		h.writeJSON(w, http.StatusOK, MessageResponse{Message: "Mining target updated"})

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
			Slot:            i,
			SerialNumber:    fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, i),
			ChipID:          "BM1370",
			ASICCount:       defaultASICCount,
			FirmwareVersion: defaultFirmwareVersion,
		})
	}

	// Generate PSUs
	psus := make([]PSUInfo, 0, defaultPSUCount)
	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			continue
		}
		psus = append(psus, PSUInfo{
			Slot:            i,
			SerialNumber:    fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			Vendor:          "Proto",
			Model:           "PSU-3600W",
			FirmwareVersion: "1.2.0",
		})
	}

	// Generate fans
	fans := make([]FanInfo, 4)
	for i := range 4 {
		minRPM := 1000
		maxRPM := 6000
		fans[i] = FanInfo{
			Slot:   i,
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
			Slot:            i,
			SerialNumber:    fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			Vendor:          "Proto",
			Model:           "PSU-3600W",
			FirmwareVersion: "1.2.0",
		})
	}

	h.writeJSON(w, http.StatusOK, map[string][]PSUInfo{"psus": psus})
}

func (h *RESTApiHandler) handleHashboards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	miningState := h.state.GetMiningState()
	hashboards := make([]HashboardStatsInner, 0, defaultHashboardCount)

	for i := range defaultHashboardCount {
		if h.state.IsHashboardMissing(i) {
			continue
		}

		state := "Mining"
		if h.state.IsHashboardInError(i) {
			state = "Error"
		} else if miningState != miner_data_api.MiningState_MINING_STATE_MINING {
			state = "Off"
		}

		hbHashrate := applyVariation(defaultHashboardHashrate, telemetryVariation)
		if state != "Mining" {
			hbHashrate = 0
		}

		hashboards = append(hashboards, HashboardStatsInner{
			HBSN:             fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, i),
			Slot:             i,
			State:            state,
			HashrateGHS:      hbHashrate * 1000, // TH/s to GH/s
			IdealHashrateGHS: defaultHashboardHashrate * 1000,
			PowerUsageWatts:  applyVariation(defaultHashboardPower, telemetryVariation),
			EfficiencyJTH:    applyVariation(defaultEfficiencyJTH, telemetryVariation),
			InletTempC:       applyVariation(defaultHashboardInletTemp, telemetryVariation),
			OutletTempC:      applyVariation(defaultHashboardOutletTemp, telemetryVariation),
			AvgASICTempC:     applyVariation(defaultASICTemperature, telemetryVariation),
			MaxASICTempC:     applyVariation(defaultASICTemperature+5, telemetryVariation),
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
	state := "Mining"
	if h.state.IsHashboardInError(idx) {
		state = "Error"
	} else if miningState != miner_data_api.MiningState_MINING_STATE_MINING {
		state = "Off"
	}

	hbHashrate := applyVariation(defaultHashboardHashrate, telemetryVariation)
	if state != "Mining" {
		hbHashrate = 0
	}

	hbStats := HashboardStatsInner{
		HBSN:             hbSN,
		Slot:             idx,
		State:            state,
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
				Slot:            i,
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

	for _, level := range levels {
		switch strings.ToLower(level) {
		case "miner":
			response.Miner = &MinerTelemetry{
				HashrateTHS:   hashrate,
				TemperatureC:  temperature,
				PowerW:        power,
				EfficiencyJTH: efficiency,
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
			Index:               i,
			SerialNumber:        fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, i),
			HashrateTHS:         hbHashrate,
			InletTemperatureC:   applyVariation(defaultHashboardInletTemp, telemetryVariation),
			OutletTemperatureC:  applyVariation(defaultHashboardOutletTemp, telemetryVariation),
			AverageTemperatureC: applyVariation(defaultHashboardAvgTemp, telemetryVariation),
			PowerW:              applyVariation(defaultHashboardPower, telemetryVariation),
			EfficiencyJTH:       applyVariation(defaultEfficiencyJTH, telemetryVariation),
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
		HashrateTHS:  hashrates,
		TemperatureC: temps,
	}
}

func (h *RESTApiHandler) generatePSUsTelemetry() []PSUTelemetry {
	psus := make([]PSUTelemetry, 0, defaultPSUCount)

	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			continue
		}

		psus = append(psus, PSUTelemetry{
			Index:               i,
			SerialNumber:        fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			InputVoltageV:       applyVariation(defaultPSUInputVoltage, telemetryVariation),
			OutputVoltageV:      applyVariation(defaultPSUOutputVoltage, telemetryVariation),
			InputCurrentA:       applyVariation(defaultPSUInputCurrent, telemetryVariation),
			OutputCurrentA:      applyVariation(defaultPSUOutputCurrent, telemetryVariation),
			InputPowerW:         applyVariation(defaultPSUInputPower, telemetryVariation),
			OutputPowerW:        applyVariation(defaultPSUOutputPower, telemetryVariation),
			HotspotTemperatureC: applyVariation(defaultPSUHotspotTemp, telemetryVariation),
			AmbientTemperatureC: applyVariation(defaultPSUAmbientTemp, telemetryVariation),
		})
	}

	return psus
}

func (h *RESTApiHandler) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}

	// Return empty time series data
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":     []interface{}{},
		"duration": "1h",
	})
}
