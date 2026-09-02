package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RESTApiHandler serves the MDK v2 REST surface a fleet client hits against a
// CT1 container. JSON field names follow the grpc-gateway camelCase encoding
// of the mdk.v2 protos so the simulator can't silently drift from the client.
type RESTApiHandler struct {
	state *ContainerState
}

// NewRESTApiHandler creates a handler backed by the given container state.
func NewRESTApiHandler(state *ContainerState) *RESTApiHandler {
	return &RESTApiHandler{state: state}
}

// RegisterRoutes wires the implemented v2 endpoints onto mux.
//
// Routes are method-qualified and use Go 1.22+ path wildcards ({moduleId},
// {identifier}), so ServeMux handles method dispatch (405 for a wrong method on
// a known path) and path-parameter extraction natively — no hand-rolled
// prefix parsing.
func (h *RESTApiHandler) RegisterRoutes(mux *http.ServeMux) {
	// System.
	mux.HandleFunc("GET /api/v2/system/info", h.handleSystemInfo)
	mux.HandleFunc("GET /api/v2/system/status", h.handleSystemStatus)

	// Container topology and telemetry.
	mux.HandleFunc("GET /api/v2/container/map", h.handleContainerMap)
	mux.HandleFunc("GET /api/v2/telemetry/container", h.handleContainerTelemetry)

	// Mining status and global lifecycle.
	mux.HandleFunc("GET /api/v2/mining/status", h.handleMiningStatus)
	mux.HandleFunc("POST /api/v2/mining/start", h.handleMiningStart)
	mux.HandleFunc("POST /api/v2/mining/stop", h.handleMiningStop)

	// Scoped mining writes.
	mux.HandleFunc("POST /api/v2/mining/scoped/power", h.handleScopedPower)
	mux.HandleFunc("POST /api/v2/mining/scoped/power/clear", h.handleScopedPowerClear)
	mux.HandleFunc("POST /api/v2/mining/scoped/start", h.handleScopedStart)
	mux.HandleFunc("POST /api/v2/mining/scoped/stop", h.handleScopedStop)

	// Per-module writes and per-hashboard reads (path parameters).
	mux.HandleFunc("PUT /api/v2/mining/modules/{moduleId}/voltage/fixed", h.handleModuleFixedVoltage)
	mux.HandleFunc("DELETE /api/v2/mining/modules/{moduleId}/voltage/fixed", h.handleModuleFixedVoltage)
	mux.HandleFunc("GET /api/v2/hashboards/{identifier}/asics", h.handleHashboardAsics)

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
	ActiveBoards          int32  `json:"activeBoards"`
	TotalBoards           int32  `json:"totalBoards"`
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
	ID      int32       `json:"id"`
	Modules []moduleDTO `json:"modules"`
}

type moduleDTO struct {
	ID    int32     `json:"id"`
	Slots []slotDTO `json:"slots"`
}

type slotDTO struct {
	BreakerID   string        `json:"breakerId"`
	Hashboard   *hashboardDTO `json:"hashboard,omitempty"`
	Occupied    bool          `json:"occupied"`
	PowerFeedID string        `json:"powerFeedId"`
}

type hashboardDTO struct {
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
	Tank        int32    `json:"tank"`
	Module      int32    `json:"module"`
	BreakerID   string   `json:"breakerId"`
	PowerFeedID string   `json:"powerFeedId"`
	Slot        int64str `json:"slot"`
}

type hashboardHealth struct {
	Hashrate                float64   `json:"hashrate"`
	HashrateIdeal           float64   `json:"hashrateIdeal"`
	Temperatures            []float64 `json:"temperatures"`
	Voltage                 float64   `json:"voltage"`
	Current                 float64   `json:"current"`
	Power                   float64   `json:"power"`
	Uptime                  uint64str `json:"uptime"`
	HashrailOK              bool      `json:"hashrailOk"`
	RefclksOK               bool      `json:"refclksOk"`
	Timestamp               time.Time `json:"timestamp"`
	AverageAsicTemperatureC float64   `json:"averageAsicTemperatureC"`
}

type miningStatusResponse struct {
	Hashrate                     float64          `json:"hashrate"`
	HashrateIdeal                float64          `json:"hashrateIdeal"`
	Power                        float64          `json:"power"`
	ActiveBoards                 int32            `json:"activeBoards"`
	TotalBoards                  int32            `json:"totalBoards"`
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

type containerTelemetryResponse struct {
	Tank      tankTelemetry  `json:"tank"`
	Phases    []phaseTel     `json:"phases"`
	Timestamp time.Time      `json:"timestamp"`
	Plant     plantTelemetry `json:"plant"`
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
	OperatingState      string  `json:"operatingState"`
	Powered             bool    `json:"powered"`
	InletTemperatureC   float64 `json:"inletTemperatureC"`
	OutletTemperatureC  float64 `json:"outletTemperatureC"`
	AmbientTemperatureC float64 `json:"ambientTemperatureC"`
	CoolantPumpActive   bool    `json:"coolantPumpActive"`
	HeatRejectionActive bool    `json:"heatRejectionActive"`
	TotalPowerW         float64 `json:"totalPowerW"`
	HeatRejectionPowerW float64 `json:"heatRejectionPowerW"`
	FacilityPowerW      float64 `json:"facilityPowerW"`
	PowerFactor         float64 `json:"powerFactor"`
	PUE                 float64 `json:"pue"`
	FlowRateLpm         float64 `json:"flowRateLpm"`
}

type asicsResponse struct {
	Asics  []asicDTO  `json:"asics"`
	Layout asicLayout `json:"layout"`
}

type asicDTO struct {
	Index         int32 `json:"index"`
	VoltageMv     int32 `json:"voltageMv"`
	TemperatureDc int32 `json:"temperatureDc"`
	FrequencyMhz  int32 `json:"frequencyMhz"`
	PassRateBp    int32 `json:"passRateBp"`
	HashrateGhs10 int32 `json:"hashrateGhs10"`
}

type asicLayout struct {
	StackCount    int32           `json:"stackCount"`
	AsicsPerStack int32           `json:"asicsPerStack"`
	TotalAsics    int32           `json:"totalAsics"`
	Entries       []asicLayoutEnt `json:"entries"`
}

type asicLayoutEnt struct {
	Index      int32 `json:"index"`
	StackID    int32 `json:"stackId"`
	StackIndex int32 `json:"stackIndex"`
	X          int32 `json:"x"`
	Y          int32 `json:"y"`
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
	BoardsUpdated int32         `json:"boardsUpdated"`
	Results       []boardResult `json:"results"`
}

type clearPowerScopedResponse struct {
	BoardsUpdated int32         `json:"boardsUpdated"`
	Results       []boardResult `json:"results"`
}

type startScopedResponse struct {
	BoardsStarted int32         `json:"boardsStarted"`
	Results       []boardResult `json:"results"`
}

type stopScopedResponse struct {
	BoardsStopped int32         `json:"boardsStopped"`
	Results       []boardResult `json:"results"`
}

type setModuleVoltageRequest struct {
	Millivolts int64str `json:"millivolts"`
}

type moduleVoltageResponse struct {
	Setting       moduleVoltageSetting `json:"setting"`
	BoardsUpdated int32                `json:"boardsUpdated"`
	Results       []boardResult        `json:"results"`
}

type moduleVoltageSetting struct {
	ModuleID                   string    `json:"moduleId"`
	FixedMillivolts            int64str  `json:"fixedMillivolts"`
	SupportedMinimumMillivolts int64str  `json:"supportedMinimumMillivolts"`
	SupportedMaximumMillivolts int64str  `json:"supportedMaximumMillivolts"`
	BoardCount                 int32     `json:"boardCount"`
	Readiness                  string    `json:"readiness"`
	ReadinessMessage           string    `json:"readinessMessage"`
	UpdatedBy                  string    `json:"updatedBy"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (h *RESTApiHandler) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	s := h.state
	s.mu.RLock()
	resp := systemInfoResponse{Info: systemInfo{
		ID:            s.ServerID,
		Version:       s.Version,
		Product:       product,
		Hostname:      s.Hostname,
		UptimeSeconds: int64str(time.Since(s.StartTime).Seconds()),
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
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
}

func (h *RESTApiHandler) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	s := h.state
	s.mu.RLock()
	active, total := s.boardCounts()
	resp := systemStatusResponse{Status: systemStatus{
		Health:                s.healthStatus(active, total),
		Mining:                active > 0,
		ActiveBoards:          int32(active),
		TotalBoards:           int32(total),
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
	s := h.state
	s.mu.RLock()
	cm := containerMap{}
	for _, tank := range s.Tanks {
		td := tankDTO{ID: int32(tank.ID)}
		for _, module := range tank.Modules {
			md := moduleDTO{ID: int32(module.ID)}
			for _, slot := range module.Slots {
				sd := slotDTO{
					BreakerID:   slot.BreakerID,
					Occupied:    slot.Occupied,
					PowerFeedID: slot.PowerFeedID,
				}
				if slot.Board != nil {
					sd.Hashboard = s.hashboardDTO(slot.Board)
				}
				md.Slots = append(md.Slots, sd)
			}
			td.Modules = append(td.Modules, md)
		}
		cm.Tanks = append(cm.Tanks, td)
	}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, containerMapResponse{ContainerMap: cm})
}

func (h *RESTApiHandler) handleMiningStatus(w http.ResponseWriter, r *http.Request) {
	s := h.state
	s.mu.RLock()
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
			hashrate += vary(boardIdealHashrateTHS * 0.97)
			power += vary(b.targetWatts())
			asicTempSum += asicTempC
			boardTempSum += boardTempC
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
		ActiveBoards:                 int32(active),
		TotalBoards:                  int32(total),
		ActivePool:                   defaultActivePool,
		UptimeSeconds:                int64str(time.Since(s.StartTime).Seconds()),
		Efficiency:                   efficiency,
		Mining:                       active > 0,
		BoardsByState:                byState,
		ShareAcceptanceRate:          0.995,
		OperatingState:               operatingState(active, total),
		AverageAsicTemperatureC:      avgAsic,
		AverageHashboardTemperatureC: avgBoard,
	}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
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

func (h *RESTApiHandler) handleContainerTelemetry(w http.ResponseWriter, r *http.Request) {
	s := h.state
	s.mu.RLock()
	var totalPower float64
	active := 0
	for _, b := range s.eachBoard() {
		if b.isMining() {
			totalPower += b.targetWatts()
			active++
		}
	}
	mining := active > 0
	pue := vary(basePUE)
	facility := totalPower * pue
	heatRejection := facility - totalPower
	now := time.Now()

	// Split IT load across three phases.
	phaseW := facility / 3
	phaseV := 415.0
	phases := make([]phaseTel, 0, 3)
	for _, p := range []string{"PHASE_A", "PHASE_B", "PHASE_C"} {
		w := vary(phaseW)
		phases = append(phases, phaseTel{
			Phase:    p,
			PowerW:   w,
			CurrentA: w / phaseV,
			VoltageV: vary(phaseV),
		})
	}

	resp := containerTelemetryResponse{
		Tank: tankTelemetry{
			State:              tankState(mining),
			Powered:            true,
			InletTemperature:   vary(plantInletTempC),
			OutletTemperature:  vary(plantOutletTempC),
			AmbientTemperature: vary(plantAmbientTempC),
			PumpActive:         true,
			CoolerActive:       true,
			TotalWattage:       totalPower,
			EmergencyStop:      false,
		},
		Phases:    phases,
		Timestamp: now,
		Plant: plantTelemetry{
			OperatingState:      plantState(mining),
			Powered:             true,
			InletTemperatureC:   vary(plantInletTempC),
			OutletTemperatureC:  vary(plantOutletTempC),
			AmbientTemperatureC: vary(plantAmbientTempC),
			CoolantPumpActive:   true,
			HeatRejectionActive: true,
			TotalPowerW:         totalPower,
			HeatRejectionPowerW: heatRejection,
			FacilityPowerW:      facility,
			PowerFactor:         0.99,
			PUE:                 pue,
			FlowRateLpm:         vary(1200),
		},
	}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
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
	writeJSON(w, http.StatusOK, setPowerScopedResponse{BoardsUpdated: int32(countApplied(results)), Results: results})
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
	writeJSON(w, http.StatusOK, clearPowerScopedResponse{BoardsUpdated: int32(countApplied(results)), Results: results})
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
	writeJSON(w, http.StatusOK, startScopedResponse{BoardsStarted: int32(countApplied(results)), Results: results})
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
	writeJSON(w, http.StatusOK, stopScopedResponse{BoardsStopped: int32(countApplied(results)), Results: results})
}

// ---------------------------------------------------------------------------
// State -> DTO helpers (caller must hold the appropriate lock)
// ---------------------------------------------------------------------------

func (s *ContainerState) hashboardDTO(b *Hashboard) *hashboardDTO {
	mining := b.isMining()
	hr := 0.0
	power := 0.0
	if mining {
		hr = vary(boardIdealHashrateTHS * 0.97)
		power = vary(b.targetWatts())
	}
	return &hashboardDTO{
		MACAddress:  b.MAC,
		Serial:      b.Serial,
		HWRevision:  b.HWRevision,
		MiningState: b.State,
		Location: hashboardLoc{
			Tank:        int32(b.TankID),
			Module:      int32(b.ModuleNum),
			BreakerID:   b.BreakerID,
			PowerFeedID: b.PowerFeedID,
			Slot:        int64str(b.SlotIndex),
		},
		Health: hashboardHealth{
			Hashrate:                hr,
			HashrateIdeal:           boardIdealHashrateTHS,
			Temperatures:            []float64{vary(boardTempC), vary(boardTempC)},
			Voltage:                 vary(boardVoltage),
			Current:                 power / boardVoltage,
			Power:                   power,
			Uptime:                  uint64str(time.Since(b.StartTime).Seconds()),
			HashrailOK:              true,
			RefclksOK:               true,
			Timestamp:               time.Now(),
			AverageAsicTemperatureC: vary(asicTempC),
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

func (s *ContainerState) asicsResponse(b *Hashboard) asicsResponse {
	total := s.Config.AsicsPerBoard
	// Guard the layout math: a shrunk topology (or a board with fewer ASICs
	// than stacks) must not divide by zero or produce out-of-range stack ids.
	perStack := total / asicStackCount
	if perStack < 1 {
		perStack = 1
	}
	mining := b.isMining()

	asics := make([]asicDTO, 0, total)
	entries := make([]asicLayoutEnt, 0, total)
	for i := 0; i < total; i++ {
		stackID := i / perStack
		if stackID >= asicStackCount {
			stackID = asicStackCount - 1
		}
		stackIndex := i % perStack
		ghs10 := int32(0)
		if mining {
			ghs10 = int32(vary(boardIdealHashrateTHS*1000/float64(total)) * 10)
		}
		asics = append(asics, asicDTO{
			Index:         int32(i),
			VoltageMv:     int32(vary(asicVoltageMv)),
			TemperatureDc: int32(vary(asicTempC) * 10),
			FrequencyMhz:  asicFrequencyMHz,
			PassRateBp:    asicPassRateBp,
			HashrateGhs10: ghs10,
		})
		entries = append(entries, asicLayoutEnt{
			Index:      int32(i),
			StackID:    int32(stackID),
			StackIndex: int32(stackIndex),
			X:          int32(stackIndex),
			Y:          int32(stackID),
		})
	}
	return asicsResponse{
		Asics: asics,
		Layout: asicLayout{
			StackCount:    asicStackCount,
			AsicsPerStack: int32(perStack),
			TotalAsics:    int32(total),
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
			SupportedMinimumMillivolts: 12000,
			SupportedMaximumMillivolts: 16000,
			BoardCount:                 int32(len(results)),
			Readiness:                  readiness,
			UpdatedBy:                  "fake-ct1",
			UpdatedAt:                  time.Now(),
		},
		BoardsUpdated: int32(len(results)),
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

const defaultActivePool = "stratum+tcp://btc.example.com:3333"

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
		_ = json.NewEncoder(jw.ResponseWriter).Encode(map[string]any{"code": code, "message": msg})
		return
	}
	jw.ResponseWriter.WriteHeader(code)
}

func (jw *jsonErrorWriter) Write(b []byte) (int, error) {
	if jw.rewrote {
		// Swallow ServeMux's plain-text body; we already wrote the JSON one.
		return len(b), nil
	}
	return jw.ResponseWriter.Write(b)
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
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    status,
		"message": message,
	})
}
