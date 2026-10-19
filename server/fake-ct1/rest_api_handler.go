package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Live telemetry and history expose the same simulated fan inventory.
var dryCoolerFanIDs = []string{
	"dry-cooler-fan-1", "dry-cooler-fan-2", "dry-cooler-fan-3", "dry-cooler-fan-4", "dry-cooler-fan-5",
	"dry-cooler-fan-6", "dry-cooler-fan-7", "dry-cooler-fan-8", "dry-cooler-fan-9", "dry-cooler-fan-10",
}

// RESTApiHandler serves the MDK v2 REST surface a fleet client hits against a
// CT1 container. JSON field names follow the grpc-gateway camelCase encoding
// of the mdk.v2 protos so the simulator can't silently drift from the client.
type RESTApiHandler struct {
	state    *ContainerState
	requests sync.Mutex
}

// NewRESTApiHandler creates a handler backed by the given container state.
func NewRESTApiHandler(state *ContainerState) *RESTApiHandler {
	state.initHistory()
	return &RESTApiHandler{state: state}
}

// RegisterRoutes wires the implemented v2 endpoints onto mux.
//
// Routes are method-qualified and use Go 1.22+ path wildcards ({moduleId},
// {identifier}), so ServeMux handles method dispatch (405 for a wrong method on
// a known path) and path-parameter extraction natively — no hand-rolled
// prefix parsing.
func (h *RESTApiHandler) RegisterRoutes(mux *http.ServeMux) {
	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			h.requests.Lock()
			defer h.requests.Unlock()
			h.state.mu.Lock()
			now := h.state.now()
			h.state.recordUntil(now)
			h.state.advance(now)
			h.state.mu.Unlock()
			handler(w, r)
			if r.Method != http.MethodGet {
				h.state.mu.Lock()
				h.state.advance(now)
				h.state.recordTransition()
				h.state.mu.Unlock()
			}
		})
	}

	// System.
	register("GET /api/v2/system/info", h.handleSystemInfo)
	register("GET /api/v2/system/status", h.handleSystemStatus)

	// Container topology and telemetry.
	register("GET /api/v2/container/map", h.handleContainerMap)
	register("GET /api/v2/telemetry/container", h.handleContainerTelemetry)
	register("GET /api/v2/telemetry/timeseries", h.handleTimeSeries)
	register("GET /api/v2/power/container/controls", h.handleContainerControls)
	register("POST /api/v2/power/container/controls/{name}", h.handleContainerControl)
	register("POST /api/v2/power/container/setpoints/{name}", h.handleContainerSetpoint)

	// Mining status and global lifecycle.
	register("GET /api/v2/mining/status", h.handleMiningStatus)
	register("POST /api/v2/mining/start", h.handleMiningStart)
	register("POST /api/v2/mining/stop", h.handleMiningStop)
	register("GET /api/v2/mining/target-power", h.handleTargetPower)
	register("PUT /api/v2/mining/target-power", h.handleTargetPower)

	// Pool configuration and runtime diagnostics.
	register("GET /api/v2/pools", h.handlePools)
	register("GET /api/v2/pools/diagnostics", h.handlePoolDiagnostics)
	register("GET /api/v2/telemetry/shares/history", h.handlePoolShareHistory)

	// Scoped mining writes.
	register("POST /api/v2/mining/scoped/power", h.handleScopedPower)
	register("POST /api/v2/mining/scoped/power/clear", h.handleScopedPowerClear)
	register("POST /api/v2/mining/scoped/start", h.handleScopedStart)
	register("POST /api/v2/mining/scoped/stop", h.handleScopedStop)

	// Per-module writes and per-hashboard reads (path parameters).
	register("PUT /api/v2/mining/modules/{moduleId}/voltage/fixed", h.handleModuleFixedVoltage)
	register("DELETE /api/v2/mining/modules/{moduleId}/voltage/fixed", h.handleModuleFixedVoltage)
	register("GET /api/v2/hashboards/{identifier}/asics", h.handleHashboardAsics)
	register("POST /api/v2/modules/{moduleId}/run-in", h.handleModuleRunIn)

	// Unmatched paths (404) and wrong-method requests to matched paths (405)
	// fall through to ServeMux's defaults; jsonErrorMiddleware rewrites those
	// text/plain responses into the JSON error envelope every endpoint uses.
}

// ---------------------------------------------------------------------------
// protojson-compatible 64-bit integers
// ---------------------------------------------------------------------------

// int64str / uint64str match protojson's encoding of 64-bit integers: emitted
// as a JSON string, and accepted as either a JSON string or number on input.
// grpc-gateway uses protojson, so a correctly-built MDK v2 client sends and
// expects e.g. `{"millivolts":"14000"}` — plain int64 fields would reject it.
type int64str int64

func (v int64str) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strconv.FormatInt(int64(v), 10))), nil
}

func (v *int64str) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid int64 %q: %w", string(b), err)
	}
	*v = int64str(n)
	return nil
}

type uint64str uint64

func (v uint64str) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strconv.FormatUint(uint64(v), 10))), nil
}

func (v *uint64str) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid uint64 %q: %w", string(b), err)
	}
	*v = uint64str(n)
	return nil
}

// ---------------------------------------------------------------------------
// JSON DTOs (mdk.v2 grpc-gateway shapes)
// ---------------------------------------------------------------------------

type systemInfoResponse struct {
	Info systemInfo `json:"info"`
}

type systemInfo struct {
	ID            string    `json:"id"`
	Version       string    `json:"version"`
	Product       string    `json:"product"`
	Hostname      string    `json:"hostname"`
	UptimeSeconds int64str  `json:"uptimeSeconds"`
	IPAddresses   []string  `json:"ipAddresses"`
	Manufacturer  string    `json:"manufacturer"`
	Model         string    `json:"model"`
	OSBuild       osBuild   `json:"osBuild"`
	ControlBoard  ctrlBoard `json:"controlBoard"`
}

type osBuild struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	GitHash string `json:"gitHash"`
	Variant string `json:"variant"`
}

type ctrlBoard struct {
	Manufacturer string `json:"manufacturer"`
	BoardModel   string `json:"boardModel"`
	SerialNumber string `json:"serialNumber"`
	MachineName  string `json:"machineName"`
}

type systemStatusResponse struct {
	Status systemStatus `json:"status"`
}

type systemStatus struct {
	Health                string `json:"health"`
	Mining                bool   `json:"mining"`
	ActiveBoards          int    `json:"activeBoards"`
	TotalBoards           int    `json:"totalBoards"`
	ActivePool            string `json:"activePool"`
	DatabaseConnected     bool   `json:"databaseConnected"`
	ServerID              string `json:"serverId"`
	HARole                string `json:"haRole"`
	Version               string `json:"version"`
	ContainerWritePrimary bool   `json:"containerWritePrimary"`
}

type containerMapResponse struct {
	ContainerMap containerMap `json:"containerMap"`
}

type containerMap struct {
	Tanks []tankDTO `json:"tanks"`
}

type tankDTO struct {
	ID      int         `json:"id"`
	Modules []moduleDTO `json:"modules"`
}

type moduleDTO struct {
	ID    int       `json:"id"`
	Slots []slotDTO `json:"slots"`
}

type slotDTO struct {
	BreakerID   string        `json:"breakerId"`
	Hashboard   *hashboardDTO `json:"hashboard,omitempty"`
	Occupied    bool          `json:"occupied"`
	PowerFeedID string        `json:"powerFeedId"`
}

type hashboardDTO struct {
	PsuIdentity         psuIdentity     `json:"psuIdentity"`
	PsuType             string          `json:"psuType"`
	MACAddress          string          `json:"macAddress"`
	Serial              string          `json:"serial"`
	HWRevision          string          `json:"hwRevision"`
	MiningState         string          `json:"miningState"`
	Location            hashboardLoc    `json:"location"`
	Health              hashboardHealth `json:"health"`
	OwnerServerID       string          `json:"ownerServerId"`
	Source              string          `json:"source"`
	ModuleID            string          `json:"moduleId"`
	Isolated            bool            `json:"isolated"`
	FirmwareVersion     string          `json:"firmwareVersion"`
	FirmwareGitHash     string          `json:"firmwareGitHash"`
	PowerTargetMinWatts float64         `json:"powerTargetMinWatts"`
	PowerTargetMaxWatts float64         `json:"powerTargetMaxWatts"`
	ChipID              string          `json:"chipId"`
	AsicModel           string          `json:"asicModel"`
	ExpectedAsicCount   int64str        `json:"expectedAsicCount"`
	ObservedAsicCount   int64str        `json:"observedAsicCount"`
}

type hashboardLoc struct {
	Tank              int      `json:"tank"`
	Module            int      `json:"module"`
	BreakerID         string   `json:"breakerId"`
	PowerFeedID       string   `json:"powerFeedId"`
	HashboardPosition int64str `json:"hashboardPosition,omitempty"`
}

type hashboardHealth struct {
	PsuElectrical             psuElectricalHealth    `json:"psuElectrical"`
	PsuThermal                psuThermalHealth       `json:"psuThermal"`
	Errors                    []hashboardHealthError `json:"errors,omitempty"`
	Hashrate                  float64                `json:"hashrate"`
	HashrateIdeal             float64                `json:"hashrateIdeal"`
	Temperatures              []float64              `json:"temperatures"`
	Voltage                   float64                `json:"voltage"`
	Current                   float64                `json:"current"`
	Power                     float64                `json:"power"`
	Uptime                    uint64str              `json:"uptime"`
	HashrailOK                bool                   `json:"hashrailOk"`
	RefclksOK                 bool                   `json:"refclksOk"`
	Timestamp                 time.Time              `json:"timestamp"`
	AverageAsicTemperatureC   float64                `json:"averageAsicTemperatureC"`
	MaximumAsicTemperatureC   *float64               `json:"maximumAsicTemperatureC,omitempty"`
	AsicTemperatureObservedAt *time.Time             `json:"asicTemperatureObservedAt,omitempty"`
}

type hashboardHealthError struct {
	Cause string `json:"cause"`
}

type miningStatusResponse struct {
	Hashrate                     float64          `json:"hashrate"`
	HashrateIdeal                float64          `json:"hashrateIdeal"`
	Power                        float64          `json:"power"`
	ActiveBoards                 int              `json:"activeBoards"`
	TotalBoards                  int              `json:"totalBoards"`
	ActivePool                   string           `json:"activePool"`
	UptimeSeconds                int64str         `json:"uptimeSeconds"`
	Efficiency                   float64          `json:"efficiency"`
	Mining                       bool             `json:"mining"`
	BoardsByState                map[string]int32 `json:"boardsByState"`
	ShareAcceptanceRate          float64          `json:"shareAcceptanceRate"`
	OperatingState               string           `json:"operatingState"`
	AverageAsicTemperatureC      float64          `json:"averageAsicTemperatureC"`
	AverageHashboardTemperatureC float64          `json:"averageHashboardTemperatureC"`
}

type poolDTO struct {
	ID       int64str `json:"id"`
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Username string   `json:"username"`
	Priority int32    `json:"priority"`
	Enabled  bool     `json:"enabled"`
}

type listPoolsResponse struct {
	Pools []poolDTO `json:"pools"`
}

type poolDiagnostics struct {
	PoolID     int64str `json:"poolId"`
	PoolName   string   `json:"poolName"`
	ConnState  string   `json:"connState"`
	IsActive   bool     `json:"isActive"`
	IsReady    bool     `json:"isReady"`
	Difficulty float64  `json:"currentDifficulty"`
}

type poolDiagnosticsResponse struct {
	ActivePoolID *int64str         `json:"activePoolId,omitempty"`
	Pools        []poolDiagnostics `json:"pools"`
	WeightedMode bool              `json:"weightedMode"`
}

type containerTelemetryResponse struct {
	Tank               tankTelemetry       `json:"tank"`
	Phases             []phaseTel          `json:"phases"`
	Timestamp          time.Time           `json:"timestamp"`
	Plant              plantTelemetry      `json:"plant"`
	Controls           []containerControl  `json:"controls"`
	Setpoints          []containerSetpoint `json:"setpoints"`
	CoolingFans        []plantCoolingFan   `json:"coolingFans"`
	PowerDistributions []powerDistribution `json:"powerDistributions"`
	PowerFeeds         []powerFeed         `json:"powerFeeds"`
}

type containerControl struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	StateKnown bool   `json:"stateKnown"`
}

type containerControlsResponse struct {
	Supported                   bool                `json:"supported"`
	Controls                    []containerControl  `json:"controls"`
	Setpoints                   []containerSetpoint `json:"setpoints"`
	MainCircuitBreakerSupported bool                `json:"mainCircuitBreakerSupported"`
}

type containerSetpoint struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type setContainerControlRequest struct {
	Enabled bool `json:"enabled"`
}

type setContainerControlResponse struct {
	Control containerControl `json:"control"`
}

type setContainerSetpointRequest struct {
	Value float64 `json:"value"`
}

type setContainerSetpointResponse struct {
	Setpoint containerSetpoint `json:"setpoint"`
}

type plantCoolingFan struct {
	ComponentID string  `json:"componentId"`
	PowerW      float64 `json:"powerW"`
	SpeedPct    float64 `json:"speedPercent"`
	AlarmActive bool    `json:"alarmActive"`
}

type powerDistribution struct {
	ComponentID     string  `json:"componentId"`
	ActiveFeedCount int     `json:"activeFeedCount"`
	ActivePowerW    float64 `json:"activePowerW"`
}

type powerFeed struct {
	PowerFeedID string  `json:"powerFeedId"`
	Active      bool    `json:"active"`
	Faulted     bool    `json:"faulted"`
	PowerW      float64 `json:"powerW"`
	VoltageV    float64 `json:"voltageV"`
	CurrentA    float64 `json:"currentA,omitempty"`
}

type targetPowerResponse struct {
	Watts            float64 `json:"watts"`
	DefaultWatts     float64 `json:"defaultWatts,omitempty"`
	MinimumWatts     float64 `json:"minimumWatts,omitempty"`
	MaximumWatts     float64 `json:"maximumWatts,omitempty"`
	PerformanceMode  string  `json:"performanceMode"`
	BalanceBays      bool    `json:"balanceBays"`
	HashOnDisconnect bool    `json:"hashOnDisconnect"`
}

type targetPowerRequest struct {
	Watts            float64 `json:"watts"`
	PerformanceMode  string  `json:"performanceMode"`
	BalanceBays      bool    `json:"balanceBays"`
	HashOnDisconnect bool    `json:"hashOnDisconnect"`
}

type timeSeriesBucket struct {
	Time  time.Time `json:"time"`
	Avg   float64   `json:"avg"`
	Min   float64   `json:"min"`
	Max   float64   `json:"max"`
	Count int64str  `json:"count"`
}

type timeSeriesResponse struct {
	Metric  string             `json:"metric"`
	Unit    string             `json:"unit"`
	Buckets []timeSeriesBucket `json:"buckets"`
}

type moduleRunInConfig struct {
	Duration              string            `json:"duration"`
	SampleInterval        string            `json:"sampleInterval"`
	CooldownDuration      string            `json:"cooldownDuration"`
	BoardPowerTargetWatts float64           `json:"boardPowerTargetWatts"`
	StopMiningOnFinish    bool              `json:"stopMiningOnFinish"`
	TuningSweep           *moduleRunInSweep `json:"tuningSweep,omitempty"`
}

type moduleRunInSweep struct {
	DwellDuration string `json:"dwellDuration"`
	PowerTarget   struct {
		StartPowerTargetWatts float64 `json:"startPowerTargetWatts"`
		EndPowerTargetWatts   float64 `json:"endPowerTargetWatts"`
		StepPowerTargetWatts  float64 `json:"stepPowerTargetWatts"`
	} `json:"powerTarget"`
}

type moduleRunInResponse struct {
	TaskID           string             `json:"taskId"`
	ModuleID         string             `json:"moduleId"`
	Status           string             `json:"status"`
	VoltageLeaderMAC string             `json:"voltageLeaderMac"`
	Boards           []moduleRunInBoard `json:"boards"`
}

type moduleRunInBoard struct {
	MACAddress string `json:"macAddress"`
	Serial     string `json:"serial"`
	Status     string `json:"status"`
}

type tankTelemetry struct {
	State              string  `json:"state"`
	Powered            bool    `json:"powered"`
	InletTemperature   float64 `json:"inletTemperature"`
	OutletTemperature  float64 `json:"outletTemperature"`
	AmbientTemperature float64 `json:"ambientTemperature"`
	PumpActive         bool    `json:"pumpActive"`
	CoolerActive       bool    `json:"coolerActive"`
	TotalWattage       float64 `json:"totalWattage"`
	EmergencyStop      bool    `json:"emergencyStop"`
}

type phaseTel struct {
	Phase    string  `json:"phase"`
	PowerW   float64 `json:"powerW"`
	CurrentA float64 `json:"currentA"`
	VoltageV float64 `json:"voltageV"`
}

type plantTelemetry struct {
	DryCoolerInletTemperatureC    float64 `json:"dryCoolerInletTemperatureC"`
	OperatingState                string  `json:"operatingState"`
	Powered                       bool    `json:"powered"`
	InletTemperatureC             float64 `json:"inletTemperatureC"`
	OutletTemperatureC            float64 `json:"outletTemperatureC"`
	OutletATemperatureC           float64 `json:"outletATemperatureC"`
	OutletBTemperatureC           float64 `json:"outletBTemperatureC"`
	AmbientTemperatureC           float64 `json:"ambientTemperatureC"`
	InsideAmbientTemperatureC     float64 `json:"insideAmbientTemperatureC"`
	OutsideAmbientTemperatureC    float64 `json:"outsideAmbientTemperatureC"`
	CoolantPumpActive             bool    `json:"coolantPumpActive"`
	CoolantPumpSpeedHz            float64 `json:"coolantPumpSpeedHz"`
	PumpInletPressurePa           float64 `json:"pumpInletPressurePa"`
	PumpOutletPressurePa          float64 `json:"pumpOutletPressurePa"`
	PumpPressureDiffPa            float64 `json:"pumpPressureDiffPa"`
	HeatRejectionActive           bool    `json:"heatRejectionActive"`
	HeatRejectionInletPressurePa  float64 `json:"heatRejectionInletPressurePa"`
	HeatRejectionOutletPressurePa float64 `json:"heatRejectionOutletPressurePa"`
	HeatRejectionPressureDiffPa   float64 `json:"heatRejectionPressureDiffPa"`
	TankALiquidLevelMm            float64 `json:"tankALiquidLevelMm"`
	TankBLiquidLevelMm            float64 `json:"tankBLiquidLevelMm"`
	AlarmActive                   bool    `json:"alarmActive"`
	TotalPowerW                   float64 `json:"totalPowerW"`
	HeatRejectionPowerW           float64 `json:"heatRejectionPowerW"`
	FacilityPowerW                float64 `json:"facilityPowerW"`
	PowerFactor                   float64 `json:"powerFactor"`
	PUE                           float64 `json:"pue"`
	FlowRateLpm                   float64 `json:"flowRateLpm"`
}

type asicsResponse struct {
	Asics  []asicDTO  `json:"asics"`
	Layout asicLayout `json:"layout"`
}

type asicDTO struct {
	Index         int `json:"index"`
	VoltageMv     int `json:"voltageMv"`
	TemperatureDc int `json:"temperatureDc"`
	FrequencyMhz  int `json:"frequencyMhz"`
	PassRateBp    int `json:"passRateBp"`
	HashrateGhs10 int `json:"hashrateGhs10"`
}

type asicLayout struct {
	StackCount    int             `json:"stackCount"`
	AsicsPerStack int             `json:"asicsPerStack"`
	TotalAsics    int             `json:"totalAsics"`
	Entries       []asicLayoutEnt `json:"entries"`
}

type asicLayoutEnt struct {
	Index      int `json:"index"`
	StackID    int `json:"stackId"`
	StackIndex int `json:"stackIndex"`
	X          int `json:"x"`
	Y          int `json:"y"`
}

// Scope selects boards by identifier (MAC/serial) and/or by module coordinate.
type scope struct {
	Identifiers []string      `json:"identifiers"`
	Modules     []moduleCoord `json:"modules"`
}

type moduleCoord struct {
	Tank   int32 `json:"tank"`
	Module int32 `json:"module"`
}

type scopedPowerRequest struct {
	Scope        scope   `json:"scope"`
	Watts        float64 `json:"watts"`
	PLLAlgorithm string  `json:"pllAlgorithm"`
}

type scopeOnlyRequest struct {
	Scope scope `json:"scope"`
}

type boardResult struct {
	MACAddress string `json:"macAddress"`
	Applied    bool   `json:"applied"`
	Message    string `json:"message"`
}

// Per-operation scoped-write responses. mdk.v2 gives each op its own message
// (a stop response carries boardsStopped, not boardsUpdated), so they are split
// rather than sharing one struct with omitempty — which would drop a legitimate
// zero count and let every response carry all three keys.
type setPowerScopedResponse struct {
	BoardsUpdated int           `json:"boardsUpdated"`
	Results       []boardResult `json:"results"`
}

type clearPowerScopedResponse struct {
	BoardsUpdated int           `json:"boardsUpdated"`
	Results       []boardResult `json:"results"`
}

type startScopedResponse struct {
	BoardsStarted int           `json:"boardsStarted"`
	Results       []boardResult `json:"results"`
}

type stopScopedResponse struct {
	BoardsStopped int           `json:"boardsStopped"`
	Results       []boardResult `json:"results"`
}

type errorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type setModuleVoltageRequest struct {
	Millivolts int64str `json:"millivolts"`
}

type moduleVoltageResponse struct {
	Setting       moduleVoltageSetting `json:"setting"`
	BoardsUpdated int                  `json:"boardsUpdated"`
	Results       []boardResult        `json:"results"`
}

type moduleVoltageSetting struct {
	ModuleID                   string    `json:"moduleId"`
	FixedMillivolts            int64str  `json:"fixedMillivolts"`
	SupportedMinimumMillivolts int64str  `json:"supportedMinimumMillivolts"`
	SupportedMaximumMillivolts int64str  `json:"supportedMaximumMillivolts"`
	BoardCount                 int       `json:"boardCount"`
	Readiness                  string    `json:"readiness"`
	ReadinessMessage           string    `json:"readinessMessage"`
	UpdatedBy                  string    `json:"updatedBy"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (h *RESTApiHandler) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	h.state.mu.RLock()
	resp := h.state.systemInfoDTO()
	h.state.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
}

func (s *ContainerState) systemInfoDTO() systemInfoResponse {
	resp := systemInfoResponse{Info: systemInfo{
		ID:            s.ServerID,
		Version:       s.Version,
		Product:       product,
		Hostname:      s.Hostname,
		UptimeSeconds: int64str(s.sampleTime.Sub(s.StartTime).Seconds()),
		IPAddresses:   []string{s.IPAddress},
		Manufacturer:  manufacturer,
		Model:         model,
		OSBuild: osBuild{
			Name:    "proto-os",
			Version: s.Version,
			GitHash: "e87a5197",
			Variant: "OS_BUILD_VARIANT_DEVELOPMENT",
		},
		ControlBoard: ctrlBoard{
			Manufacturer: manufacturer,
			BoardModel:   "cb-ct1",
			SerialNumber: s.ServerID,
			MachineName:  s.Hostname,
		},
	}}

	return resp
}

func (h *RESTApiHandler) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	s := h.state
	s.mu.RLock()
	active, total := s.boardCounts()
	resp := systemStatusResponse{Status: systemStatus{
		Health:                s.healthStatus(active, total),
		Mining:                active > 0,
		ActiveBoards:          active,
		TotalBoards:           total,
		ActivePool:            defaultActivePool,
		DatabaseConnected:     true,
		ServerID:              s.ServerID,
		HARole:                "HA_ROLE_PRIMARY",
		Version:               s.Version,
		ContainerWritePrimary: true,
	}}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
}

func (h *RESTApiHandler) handleContainerMap(w http.ResponseWriter, r *http.Request) {
	h.state.mu.RLock()
	resp := h.state.containerMapDTO()
	h.state.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
}

func (s *ContainerState) containerMapDTO() containerMapResponse {
	cm := containerMap{}
	unlocated := []slotDTO{}
	remaining := s.Config.UnlocatedBoards
	if remaining < 0 {
		remaining = s.BoardCount()
	}
	for _, tank := range s.Tanks {
		td := tankDTO{ID: tank.ID}
		for _, module := range tank.Modules {
			md := moduleDTO{ID: module.ID}
			for _, slot := range module.Slots {
				sd := slotDTO{
					BreakerID:   slot.BreakerID,
					Occupied:    slot.Occupied,
					PowerFeedID: slot.PowerFeedID,
				}
				if slot.Board != nil {
					sd.Hashboard = s.hashboardDTO(slot.Board)
					if remaining > 0 {
						// Project unknown wiring without changing simulated hardware,
						// identity, or telemetry. No physical slot is claimed twice.
						sd.Hashboard.Location = hashboardLoc{}
						sd.Hashboard.ModuleID = ""
						sd.Hashboard.Source = "HASHBOARD_SOURCE_INVENTORY"
						sd.BreakerID, sd.PowerFeedID = "", ""
						unlocated = append(unlocated, sd)
						remaining--
						continue
					}
				}
				md.Slots = append(md.Slots, sd)
			}
			if len(md.Slots) > 0 {
				td.Modules = append(td.Modules, md)
			}
		}
		if len(td.Modules) > 0 {
			cm.Tanks = append(cm.Tanks, td)
		}
	}
	if len(unlocated) > 0 {
		cm.Tanks = append([]tankDTO{{ID: 0, Modules: []moduleDTO{{ID: 0, Slots: unlocated}}}}, cm.Tanks...)
	}

	return containerMapResponse{ContainerMap: cm}
}

func (h *RESTApiHandler) handleMiningStatus(w http.ResponseWriter, r *http.Request) {
	h.state.mu.RLock()
	resp := h.state.miningStatusDTO()
	h.state.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
}

func (s *ContainerState) miningStatusDTO() miningStatusResponse {
	var (
		hashrate, ideal, power, asicTempSum, boardTempSum float64
		active, total                                     int
	)
	byState := map[string]int32{}
	for _, b := range s.eachBoard() {
		total++
		byState[b.State]++
		ideal += boardIdealHashrateTHS
		if b.isMining() {
			active++
			sample := s.boardSample(b)
			hashrate += sample.Hashrate
			power += sample.Power
			asicTempSum += sample.AsicTemp
			boardTempSum += sample.BoardTemp
		}
	}
	efficiency := 0.0
	if hashrate > 0 {
		efficiency = power / hashrate // J/TH
	}
	avgAsic, avgBoard := 0.0, 0.0
	if active > 0 {
		avgAsic = asicTempSum / float64(active)
		avgBoard = boardTempSum / float64(active)
	}
	resp := miningStatusResponse{
		Hashrate:                     hashrate,
		HashrateIdeal:                ideal,
		Power:                        power,
		ActiveBoards:                 active,
		TotalBoards:                  total,
		ActivePool:                   defaultActivePool,
		UptimeSeconds:                int64str(s.sampleTime.Sub(s.StartTime).Seconds()),
		Efficiency:                   efficiency,
		Mining:                       active > 0,
		BoardsByState:                byState,
		ShareAcceptanceRate:          s.shareAcceptanceRate(),
		OperatingState:               operatingState(active, total),
		AverageAsicTemperatureC:      avgAsic,
		AverageHashboardTemperatureC: avgBoard,
	}

	return resp
}

func (h *RESTApiHandler) handleMiningStart(w http.ResponseWriter, r *http.Request) {
	s := h.state
	s.mu.Lock()
	for _, b := range s.eachBoard() {
		if !b.Isolated {
			b.State = miningStateMining
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, struct{}{})
}

func (h *RESTApiHandler) handleMiningStop(w http.ResponseWriter, r *http.Request) {
	s := h.state
	s.mu.Lock()
	for _, b := range s.eachBoard() {
		b.State = miningStateIdle
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, struct{}{})
}

func demoPools() []poolDTO {
	return []poolDTO{{
		ID:       1,
		Name:     defaultPoolName,
		URL:      defaultActivePool,
		Username: defaultPoolUsername,
		Priority: 0,
		Enabled:  true,
	}, {
		ID: 2, Name: "Backup 1", URL: "stratum+tcp://backup1.example.com:3333",
		Username: defaultPoolUsername, Priority: 1, Enabled: true,
	}, {
		ID: 3, Name: "Backup 2", URL: "stratum+tcp://backup2.example.com:3333",
		Username: defaultPoolUsername, Priority: 2, Enabled: true,
	}}
}

func (h *RESTApiHandler) handlePools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, listPoolsResponse{Pools: demoPools()})
}

func (h *RESTApiHandler) handlePoolDiagnostics(w http.ResponseWriter, r *http.Request) {
	h.state.mu.RLock()
	activeBoards, _ := h.state.boardCounts()
	h.state.mu.RUnlock()
	active := activeBoards > 0
	state := "POOL_CONN_STATE_IDLE"
	var activePoolID *int64str
	if active {
		state = "POOL_CONN_STATE_ACTIVE"
		id := int64str(1)
		activePoolID = &id
	}
	var diagnostics []poolDiagnostics
	for _, pool := range demoPools() {
		connectionState := "POOL_CONN_STATE_IDLE"
		if pool.ID == 1 {
			connectionState = state
		}
		diagnostics = append(diagnostics, poolDiagnostics{
			PoolID:     pool.ID,
			PoolName:   pool.Name,
			ConnState:  connectionState,
			IsActive:   pool.ID == 1 && active,
			IsReady:    true,
			Difficulty: 2048,
		})
	}
	writeJSON(w, http.StatusOK, poolDiagnosticsResponse{
		ActivePoolID: activePoolID,
		Pools:        diagnostics,
	})
}

func (h *RESTApiHandler) handleTargetPower(w http.ResponseWriter, r *http.Request) {
	var request targetPowerRequest
	if r.Method == http.MethodPut && !decodeJSON(w, r, &request) {
		return
	}

	s := h.state
	s.mu.Lock()
	boards := s.eachBoard()
	if len(boards) == 0 {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "container has no hashboards")
		return
	}
	if r.Method == http.MethodPut {
		if request.Watts <= 0 {
			s.mu.Unlock()
			writeError(w, http.StatusBadRequest, "watts must be greater than zero")
			return
		}
		for _, board := range boards {
			board.PowerTargetW = math.Max(board.PowerTargetMinW, math.Min(board.PowerTargetMaxW, request.Watts))
		}
		s.PerformanceMode = request.PerformanceMode
		s.BalanceBays = request.BalanceBays
		s.HashOnDisconnect = request.HashOnDisconnect
	}
	current := boards[0].PowerTargetW
	response := targetPowerResponse{
		Watts:            current,
		DefaultWatts:     boardPowerTargetW,
		MinimumWatts:     boardPowerTargetMinW,
		MaximumWatts:     boardPowerTargetMaxW,
		PerformanceMode:  s.PerformanceMode,
		BalanceBays:      s.BalanceBays,
		HashOnDisconnect: s.HashOnDisconnect,
	}
	if response.PerformanceMode == "" {
		response.PerformanceMode = "PERFORMANCE_MODE_MAXIMUM_HASHRATE"
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, response)
}

func (h *RESTApiHandler) handleContainerTelemetry(w http.ResponseWriter, r *http.Request) {
	s := h.state
	s.mu.RLock()
	resp := s.containerTelemetry()
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
}

func (h *RESTApiHandler) handleContainerControls(w http.ResponseWriter, r *http.Request) {
	h.state.mu.RLock()
	telemetry := h.state.containerTelemetry()
	h.state.mu.RUnlock()
	writeJSON(w, http.StatusOK, containerControlsResponse{
		Supported: true,
		Controls:  telemetry.Controls,
		Setpoints: telemetry.Setpoints,
		// The simulator has no dedicated breaker operation or physical readback.
		MainCircuitBreakerSupported: false,
	})
}

func (h *RESTApiHandler) handleContainerControl(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	request := setContainerControlRequest{}
	if !decodeJSON(w, r, &request) {
		return
	}
	h.state.mu.Lock()
	if _, supported := h.state.ContainerControls[name]; !supported {
		h.state.mu.Unlock()
		writeError(w, http.StatusNotFound, "unknown container control")
		return
	}
	h.state.ContainerControls[name] = request.Enabled
	h.state.mu.Unlock()
	writeJSON(w, http.StatusOK, setContainerControlResponse{Control: containerControl{
		Name: name, Enabled: request.Enabled, StateKnown: true,
	}})
}

func (h *RESTApiHandler) handleContainerSetpoint(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	request := setContainerSetpointRequest{}
	if !decodeJSON(w, r, &request) {
		return
	}
	if math.IsNaN(request.Value) || math.IsInf(request.Value, 0) || request.Value < 20 || request.Value > 55 {
		writeError(w, http.StatusBadRequest, "temperature target must be between 20 and 55°C (simulator bounds)")
		return
	}
	h.state.mu.Lock()
	if _, supported := h.state.ContainerSetpoints[name]; !supported {
		h.state.mu.Unlock()
		writeError(w, http.StatusNotFound, "unknown container setpoint")
		return
	}
	h.state.ContainerSetpoints[name] = request.Value
	h.state.mu.Unlock()
	writeJSON(w, http.StatusOK, setContainerSetpointResponse{Setpoint: containerSetpoint{
		Name: name, Value: request.Value, Unit: "°C",
	}})
}

func (h *RESTApiHandler) handleTimeSeries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	metric := query.Get("metric")
	start, err := time.Parse(time.RFC3339Nano, query.Get("range.start"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "range.start must be an RFC3339 timestamp")
		return
	}
	end, err := time.Parse(time.RFC3339Nano, query.Get("range.end"))
	if err != nil || !end.After(start) {
		writeError(w, http.StatusBadRequest, "range.end must be after range.start")
		return
	}
	bucketDurations := map[string]time.Duration{
		"BUCKET_SIZE_1M":  time.Minute,
		"BUCKET_SIZE_5M":  5 * time.Minute,
		"BUCKET_SIZE_15M": 15 * time.Minute,
		"BUCKET_SIZE_1H":  time.Hour,
		"BUCKET_SIZE_1D":  24 * time.Hour,
	}
	bucketDuration := bucketDurations[query.Get("bucketSize")]
	if bucketDuration == 0 {
		writeError(w, http.StatusBadRequest, "unsupported bucketSize")
		return
	}
	s := h.state
	s.mu.Lock()
	defer s.mu.Unlock()
	identifier := query.Get("identifier")
	unit, boardIndex, err := s.historyMetric(metric, identifier)
	if err != nil {
		status := http.StatusBadRequest
		if strings.HasPrefix(err.Error(), "unknown") {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	if strings.Contains(metric, "_FAN_") || strings.Contains(metric, "_DISTRIBUTION_") || strings.Contains(metric, "_PHASE_") || strings.HasPrefix(metric, "TIME_SERIES_METRIC_POWER_FEED_") {
		if _, ok := s.recordSample().metric(metric, identifier, boardIndex); !ok {
			writeError(w, http.StatusNotFound, "unknown component identifier")
			return
		}
	}
	buckets := make([]timeSeriesBucket, 0, 300)
	for at := start; at.Before(end) && len(buckets) < 300; at = at.Add(bucketDuration) {
		bucket := timeSeriesBucket{Time: at}
		sum := 0.0
		addSample := func(sample recordedSample) {
			value, ok := sample.metric(metric, identifier, boardIndex)
			if !ok {
				return
			}
			if bucket.Count == 0 {
				bucket.Min = value
				bucket.Max = value
			}
			bucket.Min = math.Min(bucket.Min, value)
			bucket.Max = math.Max(bucket.Max, value)
			sum += value
			bucket.Count++
		}
		bucketEnd := minTime(at.Add(bucketDuration), end)
		for tick := at; tick.Before(bucketEnd); tick = tick.Add(time.Minute) {
			if sample, ok := s.sampleAt(tick); ok {
				addSample(sample)
			}
		}
		first := sort.Search(len(s.history.Samples), func(i int) bool { return s.history.Samples[i].At.After(at) })
		for _, sample := range s.history.Samples[first:] {
			if !sample.At.Before(bucketEnd) {
				break
			}
			if sample.At.Sub(at)%time.Minute != 0 && !sample.At.Before(s.sampleTime.Add(-historyRetention)) {
				addSample(sample)
			}
		}
		if bucket.Count > 0 {
			bucket.Avg = sum / float64(bucket.Count)
		}
		buckets = append(buckets, bucket)
	}
	writeJSON(w, http.StatusOK, timeSeriesResponse{Metric: metric, Unit: unit, Buckets: buckets})
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (h *RESTApiHandler) handleModuleRunIn(w http.ResponseWriter, r *http.Request) {
	var config moduleRunInConfig
	if !decodeJSON(w, r, &config) {
		return
	}
	moduleID := r.PathValue("moduleId")
	s := h.state
	s.mu.RLock()
	module := s.modulesByID[moduleID]
	if module == nil {
		s.mu.RUnlock()
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown module %q", moduleID))
		return
	}
	boards := make([]moduleRunInBoard, 0, len(module.Slots))
	leader := ""
	for _, slot := range module.Slots {
		if slot.Board == nil {
			continue
		}
		if leader == "" {
			leader = slot.Board.MAC
		}
		boards = append(boards, moduleRunInBoard{
			MACAddress: slot.Board.MAC,
			Serial:     slot.Board.Serial,
			Status:     "RUN_IN_BOARD_STATUS_QUEUED",
		})
	}
	s.mu.RUnlock()
	taskID := r.URL.Query().Get("taskId")
	if taskID == "" {
		taskID = fmt.Sprintf("run-in-%d", time.Now().UnixNano())
	}
	writeJSON(w, http.StatusOK, moduleRunInResponse{
		TaskID:           taskID,
		ModuleID:         moduleID,
		Status:           "HASHBOARD_RUN_IN_STATUS_QUEUED",
		VoltageLeaderMAC: leader,
		Boards:           boards,
	})
}

// handleModuleFixedVoltage serves PUT/DELETE
// /api/v2/mining/modules/{moduleId}/voltage/fixed. ServeMux extracts moduleId
// and rejects other methods with a 405.
func (h *RESTApiHandler) handleModuleFixedVoltage(w http.ResponseWriter, r *http.Request) {
	moduleID := r.PathValue("moduleId")

	// Decode the PUT body before taking the write lock so a slow or malformed
	// body can't pin the state mutex.
	var millivolts int64
	if r.Method == http.MethodPut {
		var req setModuleVoltageRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		millivolts = int64(req.Millivolts)
	}

	s := h.state
	s.mu.Lock()
	module := s.modulesByID[moduleID]
	if module == nil {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown module %q", moduleID))
		return
	}
	if r.Method == http.MethodPut {
		if millivolts < 26000 || millivolts > 30000 {
			s.mu.Unlock()
			writeError(w, http.StatusBadRequest, "fixed voltage must be between 26000 and 30000 mV (simulator bounds)")
			return
		}
		module.FixedMillivolts = millivolts
		module.FixedVoltageSet = true
	} else {
		module.FixedMillivolts = 0
		module.FixedVoltageSet = false
	}
	resp := s.moduleVoltageResponse(module)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

// handleHashboardAsics serves GET /api/v2/hashboards/{identifier}/asics, where
// identifier is a MAC address (CT1) or serial (R2).
func (h *RESTApiHandler) handleHashboardAsics(w http.ResponseWriter, r *http.Request) {
	identifier := r.PathValue("identifier")

	s := h.state
	s.mu.RLock()
	board := s.findBoard(identifier)
	if board == nil {
		s.mu.RUnlock()
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown hashboard %q", identifier))
		return
	}
	resp := s.asicsResponse(board)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Scoped writes
// ---------------------------------------------------------------------------

func (h *RESTApiHandler) handleScopedPower(w http.ResponseWriter, r *http.Request) {
	var req scopedPowerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	s := h.state
	s.mu.Lock()
	results := s.applyToScope(req.Scope, func(b *Hashboard) (bool, string) {
		clamped := math.Max(b.PowerTargetMinW, math.Min(b.PowerTargetMaxW, req.Watts))
		b.ScopedWatts = clamped
		b.ScopedWattsSet = true
		if clamped != req.Watts {
			return true, fmt.Sprintf("clamped to %.0fW", clamped)
		}
		return true, ""
	})
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, setPowerScopedResponse{BoardsUpdated: countApplied(results), Results: results})
}

func (h *RESTApiHandler) handleScopedPowerClear(w http.ResponseWriter, r *http.Request) {
	var req scopeOnlyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	s := h.state
	s.mu.Lock()
	results := s.applyToScope(req.Scope, func(b *Hashboard) (bool, string) {
		b.ScopedWatts = 0
		b.ScopedWattsSet = false
		return true, ""
	})
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, clearPowerScopedResponse{BoardsUpdated: countApplied(results), Results: results})
}

func (h *RESTApiHandler) handleScopedStart(w http.ResponseWriter, r *http.Request) {
	var req scopeOnlyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	s := h.state
	s.mu.Lock()
	results := s.applyToScope(req.Scope, func(b *Hashboard) (bool, string) {
		if b.Isolated {
			return false, "board isolated"
		}
		b.State = miningStateMining
		return true, ""
	})
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, startScopedResponse{BoardsStarted: countApplied(results), Results: results})
}

func (h *RESTApiHandler) handleScopedStop(w http.ResponseWriter, r *http.Request) {
	var req scopeOnlyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	s := h.state
	s.mu.Lock()
	results := s.applyToScope(req.Scope, func(b *Hashboard) (bool, string) {
		b.State = miningStateIdle
		return true, ""
	})
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, stopScopedResponse{BoardsStopped: countApplied(results), Results: results})
}

// ---------------------------------------------------------------------------
// State -> DTO helpers (caller must hold the appropriate lock)
// ---------------------------------------------------------------------------

func (s *ContainerState) hashboardDTO(b *Hashboard) *hashboardDTO {
	identity, electrical, thermal := s.modulePsu(b)
	mining := b.isMining()
	sample := s.boardSample(b)
	var maximumAsicTemperature *float64
	var asicTemperatureObservedAt *time.Time
	if s.Config.AsicsPerBoard > 0 {
		maximum := asicTemperatureDeciC(sample.AsicTemp, 0, s.Config.AsicsPerBoard)
		for i := 1; i < s.Config.AsicsPerBoard; i++ {
			maximum = max(maximum, asicTemperatureDeciC(sample.AsicTemp, i, s.Config.AsicsPerBoard))
		}
		maximumAsicTemperature = new(float64(maximum) / 10)
		asicTemperatureObservedAt = &sample.Timestamp
	}
	var errors []hashboardHealthError
	if s.Config.DemoModuleStates && b.TankID == demoFaultTank && b.ModuleNum == demoAttentionModule && b.State == miningStateError {
		errors = []hashboardHealthError{
			{Cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE"},
			{Cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT"},
			{Cause: "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY"},
		}
		errors = errors[:min(b.SlotIndex+1, len(errors))]
	}
	if s.Config.DemoModuleStates && b.TankID == demoLightFaultTank && b.ModuleNum == demoLightFaultModule && b.SlotIndex == 0 && b.State == miningStateError {
		errors = []hashboardHealthError{{Cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT"}}
	}

	return &hashboardDTO{
		PsuIdentity: identity,
		PsuType:     identity.PsuType,
		MACAddress:  b.MAC,
		Serial:      b.Serial,
		HWRevision:  b.HWRevision,
		MiningState: b.State,
		Location: hashboardLoc{
			Tank:              b.TankID,
			Module:            b.ModuleNum,
			BreakerID:         b.BreakerID,
			PowerFeedID:       b.PowerFeedID,
			HashboardPosition: int64str(b.SlotIndex + 1),
		},
		Health: hashboardHealth{
			PsuElectrical:             electrical,
			PsuThermal:                thermal,
			Errors:                    errors,
			Hashrate:                  sample.Hashrate,
			HashrateIdeal:             boardIdealHashrateTHS,
			Temperatures:              []float64{sample.BoardTemp - .2, sample.BoardTemp + .2},
			Voltage:                   sample.Voltage,
			Current:                   sample.Power / sample.Voltage,
			Power:                     sample.Power,
			Uptime:                    uint64str(math.Max(0, s.sampleTime.Sub(b.StartTime).Seconds())),
			HashrailOK:                mining,
			RefclksOK:                 mining,
			Timestamp:                 sample.Timestamp,
			AverageAsicTemperatureC:   sample.AsicTemp,
			MaximumAsicTemperatureC:   maximumAsicTemperature,
			AsicTemperatureObservedAt: asicTemperatureObservedAt,
		},
		OwnerServerID:       s.ServerID,
		Source:              "HASHBOARD_SOURCE_LOCAL",
		ModuleID:            b.ModuleID,
		Isolated:            b.Isolated,
		FirmwareVersion:     b.FirmwareVersion,
		FirmwareGitHash:     b.FirmwareGitHash,
		PowerTargetMinWatts: b.PowerTargetMinW,
		PowerTargetMaxWatts: b.PowerTargetMaxW,
		ChipID:              b.ChipID,
		AsicModel:           asicModel,
		ExpectedAsicCount:   int64str(s.Config.AsicsPerBoard),
		ObservedAsicCount:   int64str(s.Config.AsicsPerBoard),
	}
}

// Keep the summary and ASIC detail feed on the same simulated readings and precision.
func asicTemperatureDeciC(average float64, index, count int) int {
	return int(math.Round((average + 8*math.Sin(float64(index)*2*math.Pi/float64(max(1, count)))) * 10))
}

func (s *ContainerState) asicsResponse(b *Hashboard) asicsResponse {
	total := s.Config.AsicsPerBoard
	// Guard the layout math: a shrunk topology (or a board with fewer ASICs
	// than stacks) must not divide by zero or produce out-of-range stack ids.
	perStack := total / asicStackCount
	if perStack < 1 {
		perStack = 1
	}
	mining := b.isMining()

	sample := s.boardSample(b)
	weights := make([]float64, total)
	weightSum := 0.0
	for i := range total {
		weights[i] = 1 + .06*math.Sin(float64(i)*2.4) + .03*math.Sin(float64(i)*.17)
		if i%79 == 0 {
			weights[i] *= .8
		} // stable low-performing outliers
		weightSum += weights[i]
	}
	cumulative, allocated := 0.0, 0
	asics := make([]asicDTO, 0, total)
	entries := make([]asicLayoutEnt, 0, total)
	for i := range total {
		stackID := i / perStack
		if stackID >= asicStackCount {
			stackID = asicStackCount - 1
		}
		stackIndex := i % perStack
		ghs10 := 0
		if mining {
			cumulative += weights[i]
			next := int(math.Round(sample.Hashrate * 10000 * cumulative / weightSum))
			ghs10 = next - allocated
			allocated = next
		}
		frequency, passRate := 0, 0
		if mining {
			frequency = asicFrequencyMHz
			passRate = asicPassRateBp + int(30*math.Sin(float64(i)*1.7))
		}
		asics = append(asics, asicDTO{
			Index:         i,
			VoltageMv:     asicVoltageMv,
			TemperatureDc: asicTemperatureDeciC(sample.AsicTemp, i, total),
			FrequencyMhz:  frequency,
			PassRateBp:    passRate,
			HashrateGhs10: ghs10,
		})
		entries = append(entries, asicLayoutEnt{
			Index:      i,
			StackID:    stackID,
			StackIndex: stackIndex,
			X:          stackIndex,
			Y:          stackID,
		})
	}
	return asicsResponse{
		Asics: asics,
		Layout: asicLayout{
			StackCount:    asicStackCount,
			AsicsPerStack: perStack,
			TotalAsics:    total,
			Entries:       entries,
		},
	}
}

func (s *ContainerState) moduleVoltageResponse(m *Module) moduleVoltageResponse {
	results := make([]boardResult, 0, len(m.Slots))
	for _, slot := range m.Slots {
		if slot.Board == nil {
			continue
		}
		results = append(results, boardResult{MACAddress: slot.Board.MAC, Applied: true})
	}
	readiness := "MODULE_VOLTAGE_READINESS_READY"
	if !m.FixedVoltageSet {
		readiness = "MODULE_VOLTAGE_READINESS_UNSPECIFIED"
	}
	return moduleVoltageResponse{
		Setting: moduleVoltageSetting{
			ModuleID:                   m.ModuleID,
			FixedMillivolts:            int64str(m.FixedMillivolts),
			SupportedMinimumMillivolts: 26000,
			SupportedMaximumMillivolts: 30000,
			BoardCount:                 len(results),
			Readiness:                  readiness,
			UpdatedBy:                  "fake-ct1",
			UpdatedAt:                  time.Now(),
		},
		BoardsUpdated: len(results),
		Results:       results,
	}
}

// ---------------------------------------------------------------------------
// State query / mutation helpers (caller must hold the appropriate lock)
// ---------------------------------------------------------------------------

// eachBoard returns all occupied hashboards in stable tree order.
func (s *ContainerState) eachBoard() []*Hashboard {
	boards := make([]*Hashboard, 0, len(s.boardsByMAC))
	for _, m := range s.Modules {
		for _, slot := range m.Slots {
			if slot.Board != nil {
				boards = append(boards, slot.Board)
			}
		}
	}
	return boards
}

func (s *ContainerState) boardCounts() (active, total int) {
	for _, b := range s.eachBoard() {
		total++
		if b.isMining() {
			active++
		}
	}
	return active, total
}

func (s *ContainerState) healthStatus(active, total int) string {
	switch {
	case active == 0:
		return "HEALTH_STATUS_UNHEALTHY"
	case active < total:
		return "HEALTH_STATUS_DEGRADED"
	default:
		return "HEALTH_STATUS_HEALTHY"
	}
}

// operatingState derives the container-level mining state from board counts.
// Container "mining" is not a separate intent flag, so a fully-stopped
// container reports STOPPED and a partially-active one DEGRADED_MINING.
func operatingState(active, total int) string {
	switch {
	case active == 0:
		return "MINING_OPERATING_STATE_STOPPED"
	case active < total:
		return "MINING_OPERATING_STATE_DEGRADED_MINING"
	default:
		return "MINING_OPERATING_STATE_MINING"
	}
}

// applyToScope resolves the scope to a deduplicated set of boards, applies fn
// to each, and returns a per-board result. fn reports (applied, message) so a
// board that was matched but not mutated (e.g. isolated) is distinguishable
// from one that was. An empty scope matches every board. Identifiers that
// resolve to no board are surfaced as explicit unapplied results so a caller
// can tell a genuine no-op from a typo'd MAC/serial.
func (s *ContainerState) applyToScope(sc scope, fn func(*Hashboard) (bool, string)) []boardResult {
	seen := map[string]*Hashboard{}

	empty := len(sc.Identifiers) == 0 && len(sc.Modules) == 0
	if empty {
		for _, b := range s.eachBoard() {
			seen[b.MAC] = b
		}
	}
	var unresolved []string
	for _, id := range sc.Identifiers {
		if b := s.findBoard(id); b != nil {
			seen[b.MAC] = b
		} else {
			unresolved = append(unresolved, id)
		}
	}
	for _, mc := range sc.Modules {
		for _, b := range s.eachBoard() {
			if b.TankID == int(mc.Tank) && b.ModuleNum == int(mc.Module) {
				seen[b.MAC] = b
			}
		}
	}

	macs := make([]string, 0, len(seen))
	for mac := range seen {
		macs = append(macs, mac)
	}
	sort.Strings(macs)

	results := make([]boardResult, 0, len(macs)+len(unresolved))
	for _, mac := range macs {
		applied, msg := fn(seen[mac])
		results = append(results, boardResult{MACAddress: mac, Applied: applied, Message: msg})
	}
	sort.Strings(unresolved)
	for _, id := range unresolved {
		results = append(results, boardResult{MACAddress: id, Applied: false, Message: "unknown identifier"})
	}
	return results
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

const (
	defaultActivePool           = "stratum+tcp://btc.example.com:3333"
	defaultPoolName             = "Primary"
	defaultPoolUsername         = "fake-ct1"
	timeSeriesMetricHashrate    = "TIME_SERIES_METRIC_HASHRATE"
	timeSeriesMetricPower       = "TIME_SERIES_METRIC_POWER"
	timeSeriesMetricTemperature = "TIME_SERIES_METRIC_TEMPERATURE"
)

func countApplied(results []boardResult) int {
	n := 0
	for _, r := range results {
		if r.Applied {
			n++
		}
	}
	return n
}

func tankState(mining bool) string {
	if mining {
		return "TANK_STATE_RUNNING"
	}
	return "TANK_STATE_STANDBY"
}

func plantState(mining bool) string {
	if mining {
		return "PLANT_STATE_RUNNING"
	}
	return "PLANT_STATE_STANDBY"
}

// jsonErrorMiddleware rewrites ServeMux's built-in text/plain 404 (no route)
// and 405 (route exists, wrong method) responses into the JSON error envelope
// every implemented endpoint returns, so a client with only a JSON parser sees
// a consistent shape. Handler-issued errors already set Content-Type
// application/json and pass through untouched.
func jsonErrorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&jsonErrorWriter{ResponseWriter: w}, r)
	})
}

type jsonErrorWriter struct {
	http.ResponseWriter
	rewrote bool
}

func (jw *jsonErrorWriter) WriteHeader(code int) {
	// Only intercept the default framework errors; anything a handler emits as
	// JSON (including its own 404s) already has the header set and is left alone.
	if (code == http.StatusNotFound || code == http.StatusMethodNotAllowed) &&
		jw.Header().Get("Content-Type") != "application/json" {
		jw.rewrote = true
		jw.Header().Set("Content-Type", "application/json")
		jw.ResponseWriter.WriteHeader(code)
		msg := "not found"
		if code == http.StatusMethodNotAllowed {
			msg = "method not allowed"
		}
		if err := json.NewEncoder(jw.ResponseWriter).Encode(errorResponse{Code: code, Message: msg}); err != nil {
			return
		}
		return
	}
	jw.ResponseWriter.WriteHeader(code)
}

func (jw *jsonErrorWriter) Write(b []byte) (int, error) {
	if jw.rewrote {
		// Swallow ServeMux's plain-text body; we already wrote the JSON one.
		return len(b), nil
	}
	n, err := jw.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("write response body: %w", err)
	}
	return n, nil
}

// decodeJSON reads a JSON request body into v, capping it at 1 MiB so a
// malicious or buggy client can't exhaust memory. Unknown fields are rejected:
// a misspelled scope (e.g. `{"scoope":...}`) would otherwise decode to a
// zero-value scope and silently fan a destructive write out to every board. It
// writes a 400 and returns false on failure; callers should return immediately.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorResponse{Code: status, Message: message}); err != nil {
		return
	}
}
