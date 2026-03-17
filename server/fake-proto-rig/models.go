package main

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_command_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_data_api"
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
	defaultFirmwareVersion = "1.7.6"
	defaultSoftwareName    = "Proto Mining Firmware"

	// Random variation percentage
	telemetryVariation = 0.05 // 5% random variation
)

// MinerState holds the complete state of the simulated miner.
type MinerState struct {
	mu sync.RWMutex

	// Device identification
	SerialNumber string
	MacAddress   string
	Model        string
	Hostname     string

	// Authentication
	AuthPublicKey string
	Password      string

	// Onboarding status - set to true when pools are configured
	Onboarded bool

	// Mining state
	MiningState      miner_data_api.MiningState
	CoolingMode      miner_data_api.CoolingMode
	FanSpeedPct      uint32
	PowerTargetW     uint32
	PerformanceMode  miner_data_api.PerformanceMode
	HashOnDisconnect bool
	TuningAlgorithm  miner_command_api.TuningAlgorithm

	// Configured pools
	Pools []*miner_data_api.Pool

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
}

// ErrorConfig holds configuration for simulating various error conditions.
type ErrorConfig struct {
	// Mining state override
	ForceMiningState *miner_data_api.MiningState

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
		SerialNumber:      serialNumber,
		MacAddress:        macAddress,
		Model:             "Proto B4",
		Hostname:          "proto-miner-" + serialNumber[len(serialNumber)-4:],
		MiningState:       miner_data_api.MiningState_MINING_STATE_MINING,
		CoolingMode:       miner_data_api.CoolingMode_COOLING_MODE_AUTO,
		FanSpeedPct:       defaultFanSpeedPct,
		PowerTargetW:      defaultPowerTargetW,
		PerformanceMode:   miner_data_api.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE,
		DHCP:              true,
		NetMask:           "255.255.255.0",
		Gateway:           "192.168.2.1",
		BaseHashrateTHS:   defaultHashrateTHS,
		BaseTemperatureC:  defaultTemperatureC,
		BasePowerW:        defaultPowerW,
		BaseEfficiencyJTH: defaultEfficiencyJTH,
		Pools:             make([]*miner_data_api.Pool, 0),
		StartTime:         time.Now(),
	}
}

// GetMiningState returns the current mining state.
func (s *MinerState) GetMiningState() miner_data_api.MiningState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check for forced state override
	if s.ErrorConfig.ForceMiningState != nil {
		return *s.ErrorConfig.ForceMiningState
	}

	// If no pools configured, report NO_POOLS state
	if len(s.Pools) == 0 {
		return miner_data_api.MiningState_MINING_STATE_NO_POOLS
	}

	return s.MiningState
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

	// If not mining, reduce hashrate to 0
	if s.MiningState != miner_data_api.MiningState_MINING_STATE_MINING &&
		s.MiningState != miner_data_api.MiningState_MINING_STATE_DEGRADED_MINING {
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
func (s *MinerState) SetMiningState(state miner_data_api.MiningState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MiningState = state
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
func (s *MinerState) AddPool(pool *miner_data_api.Pool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Pools = append(s.Pools, pool)
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
	newPools := make([]*miner_data_api.Pool, 0, len(s.Pools))
	for _, pool := range s.Pools {
		if !toRemove[pool.Idx] {
			newPools = append(newPools, pool)
		}
	}
	s.Pools = newPools
}

// GetPools returns a copy of the current pool configuration.
func (s *MinerState) GetPools() []*miner_data_api.Pool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pools := make([]*miner_data_api.Pool, len(s.Pools))
	copy(pools, s.Pools)
	return pools
}

// SetCoolingMode updates the cooling mode and fan speed.
func (s *MinerState) SetCoolingMode(mode miner_data_api.CoolingMode, speedPct *uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.CoolingMode = mode
	if speedPct != nil {
		s.FanSpeedPct = *speedPct
	}
}

// SetPowerTarget updates the power target, performance mode, and optionally hash-on-disconnect.
func (s *MinerState) SetPowerTarget(powerW uint32, mode miner_data_api.PerformanceMode, hashOnDisconnect *bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.PowerTargetW = powerW
	s.PerformanceMode = mode
	if hashOnDisconnect != nil {
		s.HashOnDisconnect = *hashOnDisconnect
	}
}

// SetTuningAlgorithm updates the performance tuning algorithm.
func (s *MinerState) SetTuningAlgorithm(algo miner_command_api.TuningAlgorithm) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TuningAlgorithm = algo
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
