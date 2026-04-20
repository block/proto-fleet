package main

import (
	"math/rand/v2"
	"sync"
	"time"
)

// Default telemetry values for a simulated Proto miner
const (
	// Miner-level defaults
	defaultHashrateTHS    = 140.0  // TH/s
	defaultTemperatureC   = 55.0   // Celsius
	defaultPowerW         = 3400.0 // Watts
	defaultEfficiencyJTH  = 24.3   // J/TH
	defaultIdealHashrate  = 145.0  // TH/s
	defaultFanSpeedRPM    = 4500
	defaultFanSpeedPct    = 60
	defaultTargetTempC    = 50.0
	defaultPowerTargetW   = 3400
	defaultPowerTargetMin = 2000
	defaultPowerTargetMax = 4000

	// Hashboard defaults (per board)
	defaultHashboardCount      = 4
	defaultHashboardHashrate   = 35.0  // TH/s per board
	defaultHashboardInletTemp  = 40.0  // Celsius
	defaultHashboardOutletTemp = 55.0  // Celsius
	defaultHashboardAvgTemp    = 47.5  // Celsius
	defaultHashboardVoltage    = 14.5  // Volts
	defaultHashboardCurrent    = 58.0  // Amps
	defaultHashboardPower      = 850.0 // Watts per board

	// ASIC defaults (per ASIC)
	defaultASICCount       = 120  // ASICs per hashboard
	defaultASICHashrate    = 0.29 // TH/s per ASIC (~35 TH/s / 120 ASICs)
	defaultASICTemperature = 72.0 // Celsius

	// PSU defaults
	defaultPSUCount         = 2
	defaultPSUInputVoltage  = 240.0  // Volts
	defaultPSUOutputVoltage = 14.5   // Volts
	defaultPSUInputCurrent  = 7.5    // Amps
	defaultPSUOutputCurrent = 120.0  // Amps
	defaultPSUInputPower    = 1800.0 // Watts
	defaultPSUOutputPower   = 1700.0 // Watts
	defaultPSUHotspotTemp   = 45.0   // Celsius
	defaultPSUAmbientTemp   = 30.0   // Celsius

	// Pool defaults
	defaultPoolURL            = "stratum+tcp://btc.example.com:3333"
	defaultPoolWorker         = "worker1"
	defaultPoolAcceptedShares = 12345
	defaultPoolRejectedShares = 10
	defaultPoolDifficulty     = 1048576.0

	// Software info
	defaultFirmwareVersion     = "1.8.0"
	defaultNextFirmwareVersion = "1.8.1" // staged on firmware upload, promoted to current after install+reboot
	defaultSoftwareName        = "Proto Mining Firmware"

	// Random variation percentage
	telemetryVariation = 0.05 // 5% random variation
)

// MiningState represents the mining state as a string.
type MiningState string

const (
	MiningStateMining        MiningState = "Mining"
	MiningStateStopped       MiningState = "Stopped"
	MiningStateNoPools       MiningState = "NoPools"
	MiningStatePoweringOn    MiningState = "PoweringOn"
	MiningStateDegraded      MiningState = "DegradedMining"
	MiningStatePoweringOff   MiningState = "PoweringOff"
	MiningStateError         MiningState = "Error"
	MiningStateUninitialized MiningState = "Uninitialized"
	MiningStateUnknown       MiningState = "Unknown"
)

// CoolingMode represents the cooling mode as a string.
type CoolingMode string

const (
	CoolingModeAuto    CoolingMode = "auto"
	CoolingModeManual  CoolingMode = "manual"
	CoolingModeOff     CoolingMode = "off"
	CoolingModeUnknown CoolingMode = "unknown"
)

// PerformanceMode represents the performance mode as a string.
type PerformanceMode string

const (
	PerformanceModeMaxHashrate PerformanceMode = "maximum_hashrate"
	PerformanceModeEfficiency  PerformanceMode = "efficiency"
)

// TuningAlgorithm represents the tuning algorithm as a string.
type TuningAlgorithm string

const (
	TuningAlgorithmNone                         TuningAlgorithm = "None"
	TuningAlgorithmVoltageImbalanceCompensation TuningAlgorithm = "VoltageImbalanceCompensation"
	TuningAlgorithmFuzzing                      TuningAlgorithm = "Fuzzing"
)

// PoolStatistics holds pool performance statistics.
type PoolStatistics struct {
	AcceptedShares    uint64  `json:"accepted_shares"`
	RejectedShares    uint64  `json:"rejected_shares"`
	CurrentDifficulty float64 `json:"current_difficulty"`
}

// Pool represents a mining pool configuration.
type Pool struct {
	Idx        uint32          `json:"idx"`
	Priority   int             `json:"priority"`
	Url        string          `json:"url"`
	Username   string          `json:"username"`
	Password   string          `json:"password"`
	Statistics *PoolStatistics `json:"statistics,omitempty"`
}

// MinerState holds the complete state of the simulated miner.
type MinerState struct {
	mu sync.RWMutex

	// Device identification
	SerialNumber string
	MacAddress   string
	Model        string
	Hostname     string

	// Authentication
	AuthPublicKey   string
	Password        string
	DefaultPassword string // When non-empty and Password == DefaultPassword, default_password_active is true
	AccessToken     string
	RefreshToken    string

	// Onboarding status - set to true when pools are configured
	Onboarded bool

	// Mining state
	MiningStateVal     MiningState
	CoolingModeVal     CoolingMode
	FanSpeedPct        uint32
	TargetTempC        float64
	PowerTargetW       uint32
	PerformanceModeVal PerformanceMode
	HashOnDisconnect   bool
	TuningAlgorithmVal TuningAlgorithm

	// Configured pools
	Pools     []*Pool
	PoolNames map[uint32]string

	// Network configuration
	IPAddress string
	NetMask   string
	Gateway   string
	DHCP      bool

	// Telemetry baseline values (can be modified by error injection)
	BaseHashrateTHS   float64
	BaseTemperatureC  float64
	BasePowerW        float64
	BaseEfficiencyJTH float64

	// Error injection configuration
	ErrorConfig ErrorConfig

	// Timing
	StartTime time.Time

	// Locate sequence active
	LocateActive bool

	// Firmware update simulation
	FWUpdateStatus    string // "current", "downloaded", "installing", "installed"
	FWCurrentVersion  string // running firmware version; initialized to defaultFirmwareVersion
	FWNewVersion      string // staged version after a successful upload; promoted to current on reboot
	FWPreviousVersion string // set after reboot following a firmware update

	// Reboot simulation
	Rebooting bool
}

// ErrorConfig holds configuration for simulating various error conditions.
type ErrorConfig struct {
	// Mining state override
	ForceMiningState *MiningState

	// Temperature errors
	OverrideTemperature float64 // Override average temperature (0 = use default)

	// Hashboard errors
	HashboardMissing    []int // Indices of "missing" hashboards
	HashboardErrorState []int // Indices of hashboards in error state

	// PSU errors
	PSUMissing    []int // Indices of "missing" PSUs
	PSUErrorState []int // Indices of PSUs in error state

	// Pool errors
	PoolsOffline bool // Simulate all pools being dead
}

// NewMinerState creates a new MinerState with default values.
func NewMinerState(serialNumber, macAddress string) *MinerState {
	return &MinerState{
		SerialNumber:       serialNumber,
		MacAddress:         macAddress,
		Model:              "Rig",
		Hostname:           "proto-miner-" + serialNumber[len(serialNumber)-4:],
		MiningStateVal:     MiningStateMining,
		CoolingModeVal:     CoolingModeAuto,
		FanSpeedPct:        defaultFanSpeedPct,
		TargetTempC:        defaultTargetTempC,
		PowerTargetW:       defaultPowerTargetW,
		PerformanceModeVal: PerformanceModeMaxHashrate,
		DHCP:               true,
		NetMask:            "255.255.255.0",
		Gateway:            "192.168.2.1",
		BaseHashrateTHS:    defaultHashrateTHS,
		BaseTemperatureC:   defaultTemperatureC,
		BasePowerW:         defaultPowerW,
		BaseEfficiencyJTH:  defaultEfficiencyJTH,
		Pools:              make([]*Pool, 0),
		PoolNames:          make(map[uint32]string),
		FWCurrentVersion:   defaultFirmwareVersion,
		StartTime:          time.Now(),
	}
}

// GetMiningState returns the current mining state.
func (s *MinerState) GetMiningState() MiningState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.miningState()
}

// miningState returns the effective mining state. Must be called with s.mu held.
func (s *MinerState) miningState() MiningState {
	// Check for forced state override
	if s.ErrorConfig.ForceMiningState != nil {
		return *s.ErrorConfig.ForceMiningState
	}

	// If no pools configured, report NO_POOLS state
	if len(s.Pools) == 0 {
		return MiningStateNoPools
	}

	return s.MiningStateVal
}

// GetMinerTelemetry returns current miner-level telemetry values with random variation.
func (s *MinerState) GetMinerTelemetry() (hashrate, temperature, power, efficiency float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Apply random variation to base values
	hashrate = applyVariation(s.BaseHashrateTHS, telemetryVariation)
	temperature = applyVariation(s.BaseTemperatureC, telemetryVariation)
	power = applyVariation(s.BasePowerW, telemetryVariation)
	efficiency = applyVariation(s.BaseEfficiencyJTH, telemetryVariation)

	// Override temperature if configured
	if s.ErrorConfig.OverrideTemperature > 0 {
		temperature = s.ErrorConfig.OverrideTemperature
	}

	// If not actively mining, reduce hashrate to 0
	effectiveState := s.miningState()
	if effectiveState != MiningStateMining &&
		effectiveState != MiningStateDegraded {
		hashrate = 0
		power = applyVariation(200.0, telemetryVariation) // Idle power
	}

	return
}

// GetHashboardCount returns the number of active hashboards.
func (s *MinerState) GetHashboardCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := defaultHashboardCount
	for _, idx := range s.ErrorConfig.HashboardMissing {
		if idx < count {
			count--
		}
	}
	return count
}

// IsHashboardMissing checks if a hashboard is marked as missing.
func (s *MinerState) IsHashboardMissing(index int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, idx := range s.ErrorConfig.HashboardMissing {
		if idx == index {
			return true
		}
	}
	return false
}

// IsHashboardInError checks if a hashboard is in error state.
func (s *MinerState) IsHashboardInError(index int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, idx := range s.ErrorConfig.HashboardErrorState {
		if idx == index {
			return true
		}
	}
	return false
}

// IsPSUMissing checks if a PSU is marked as missing.
func (s *MinerState) IsPSUMissing(index int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, idx := range s.ErrorConfig.PSUMissing {
		if idx == index {
			return true
		}
	}
	return false
}

// IsPSUInError checks if a PSU is in error state.
func (s *MinerState) IsPSUInError(index int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, idx := range s.ErrorConfig.PSUErrorState {
		if idx == index {
			return true
		}
	}
	return false
}

// SetMiningState safely updates the mining state.
func (s *MinerState) SetMiningState(state MiningState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MiningStateVal = state
}

// SetAuthKey safely sets the authentication public key.
func (s *MinerState) SetAuthKey(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AuthPublicKey = key
}

// GetAuthKey returns the current authentication public key.
func (s *MinerState) GetAuthKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AuthPublicKey
}

// ClearAuthKey clears the authentication public key and password to keep auth state consistent.
func (s *MinerState) ClearAuthKey() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AuthPublicKey = ""
	s.Password = ""
	s.AccessToken = ""
	s.RefreshToken = ""
}

func (s *MinerState) SeedDefaultPassword(password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DefaultPassword = password
	s.Password = password
}

// SetPassword safely sets the password.
func (s *MinerState) SetPassword(password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Password = password
}

// GetPassword returns the current password.
func (s *MinerState) GetPassword() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Password
}

func (s *MinerState) IsDefaultPasswordActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.DefaultPassword != "" && s.Password == s.DefaultPassword
}

// SetAccessToken safely stores the current bearer token issued by the simulator.
func (s *MinerState) SetAccessToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AccessToken = token
}

// GetAccessToken returns the current bearer token issued by the simulator.
func (s *MinerState) GetAccessToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AccessToken
}

// SetRefreshToken safely stores the current refresh token issued by the simulator.
func (s *MinerState) SetRefreshToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RefreshToken = token
}

// GetRefreshToken returns the current refresh token issued by the simulator.
func (s *MinerState) GetRefreshToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.RefreshToken
}

// SetOnboarded sets the onboarding status.
func (s *MinerState) SetOnboarded(onboarded bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Onboarded = onboarded
}

// IsOnboarded returns the current onboarding status.
func (s *MinerState) IsOnboarded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Onboarded
}

// AddPool adds a pool to the configuration.
func (s *MinerState) AddPool(pool *Pool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Pools = append(s.Pools, pool)
}

// SetPoolName stores the display name for a pool index used by the REST API.
func (s *MinerState) SetPoolName(idx uint32, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.PoolNames == nil {
		s.PoolNames = make(map[uint32]string)
	}

	if name == "" {
		delete(s.PoolNames, idx)
		return
	}

	s.PoolNames[idx] = name
}

// GetPoolName returns the configured display name for a pool index.
func (s *MinerState) GetPoolName(idx uint32) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PoolNames[idx]
}

// ClearPools removes all configured pools and their REST display names.
func (s *MinerState) ClearPools() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Pools = make([]*Pool, 0)
	s.PoolNames = make(map[uint32]string)
}

// RemovePools removes pools by index.
func (s *MinerState) RemovePools(indices []uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create a set of indices to remove
	toRemove := make(map[uint32]bool)
	for _, idx := range indices {
		toRemove[idx] = true
	}

	// Filter out removed pools
	newPools := make([]*Pool, 0, len(s.Pools))
	for _, pool := range s.Pools {
		if !toRemove[pool.Idx] {
			newPools = append(newPools, pool)
			continue
		}

		delete(s.PoolNames, pool.Idx)
	}
	s.Pools = newPools
}

// GetPools returns a copy of the current pool configuration.
func (s *MinerState) GetPools() []*Pool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pools := make([]*Pool, len(s.Pools))
	copy(pools, s.Pools)
	return pools
}

// SetCoolingMode updates the cooling mode, fan speed, and target temperature.
func (s *MinerState) SetCoolingMode(mode CoolingMode, speedPct *uint32, targetTempC *float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.CoolingModeVal = mode
	if speedPct != nil {
		s.FanSpeedPct = *speedPct
	}
	if targetTempC != nil {
		s.TargetTempC = *targetTempC
	}
}

// SetPowerTarget updates the power target, performance mode, and optionally hash-on-disconnect.
func (s *MinerState) SetPowerTarget(powerW uint32, mode PerformanceMode, hashOnDisconnect *bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.PowerTargetW = powerW
	s.PerformanceModeVal = mode
	if hashOnDisconnect != nil {
		s.HashOnDisconnect = *hashOnDisconnect
	}
}

// SetTuningAlgorithm updates the performance tuning algorithm.
func (s *MinerState) SetTuningAlgorithm(algo TuningAlgorithm) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TuningAlgorithmVal = algo
}

// SetLocateActive sets the locate sequence active state.
func (s *MinerState) SetLocateActive(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LocateActive = active
}

// applyVariation adds random variation to a base value.
func applyVariation(base, variationPct float64) float64 {
	variation := base * variationPct
	return base + (rand.Float64()*2-1)*variation
}
