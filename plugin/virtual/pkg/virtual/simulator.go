// Package virtual provides telemetry simulation for virtual miners.
package virtual

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/block/proto-fleet/plugin/virtual/internal/config"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	teraHashToHash = 1e12
	percentFactor  = 100.0

	// Power and temperature variance constants
	idlePowerRatio        = 0.1  // Idle power is 10% of mining power
	powerVarianceRatio    = 0.05 // 5% power variance during mining
	boardTempVarianceC    = 2.0  // Per-board temperature variance
	asicTempVarianceC     = 1.0  // Per-ASIC temperature variance
	psuVoltageVarianceV   = 0.2  // PSU output voltage variance
	inputVoltageVarianceV = 5.0  // Input voltage variance
	nominalPSUVoltage     = 12.0
	nominalInputVoltage   = 220.0
)

// Simulator generates realistic telemetry for a virtual miner.
type Simulator struct {
	config *config.VirtualMinerConfig
	rng    *rand.Rand
	mu     sync.Mutex // Protects rng which is not thread-safe
}

// NewSimulator creates a new telemetry simulator for the given miner config.
func NewSimulator(cfg *config.VirtualMinerConfig) *Simulator {
	return &Simulator{
		config: cfg,
		rng:    rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0)),
	}
}

// GenerateMetrics creates a DeviceMetrics snapshot for the virtual miner.
// This method is safe for concurrent use.
func (s *Simulator) GenerateMetrics(deviceID string, isMining bool) sdk.DeviceMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	health := sdk.HealthHealthyActive
	if !isMining {
		health = sdk.HealthHealthyInactive
	}

	// Check for error injection
	if s.config.Behavior.ErrorInjection.Enabled {
		if s.rng.Float64() < s.config.Behavior.ErrorInjection.OfflineProbability {
			health = sdk.HealthUnknown
			return sdk.DeviceMetrics{
				DeviceID:  deviceID,
				Timestamp: now,
				Health:    health,
			}
		}
	}

	hashrate := s.calculateHashrateLocked(isMining)
	temp := s.calculateTemperatureLocked()
	power := s.calculatePowerLocked(isMining)
	efficiency := s.calculateEfficiency(hashrate, power)

	return sdk.DeviceMetrics{
		DeviceID:         deviceID,
		Timestamp:        now,
		Health:           health,
		HashrateHS:       toMetricValue(hashrate * teraHashToHash),
		TempC:            toMetricValue(temp),
		PowerW:           toMetricValue(power),
		EfficiencyJH:     toMetricValue(efficiency),
		HashBoards:       s.generateHashboardsLocked(isMining),
		FanMetrics:       s.generateFanMetricsLocked(),
		PSUMetrics:       s.generatePSUMetricsLocked(isMining, power),
		StratumV2Support: stratumV2SupportFor(s.config),
	}
}

// stratumV2SupportFor maps the static config toggle onto the SDK enum.
// Virtual miners always have a deterministic answer — unlike real plugins
// where firmware probing can fail — so we never report Unknown here.
func stratumV2SupportFor(cfg *config.VirtualMinerConfig) sdk.StratumV2SupportStatus {
	if cfg == nil {
		return sdk.StratumV2SupportUnsupported
	}
	if cfg.StratumV2Supported {
		return sdk.StratumV2SupportSupported
	}
	return sdk.StratumV2SupportUnsupported
}

// calculateHashrateLocked requires mu to be held.
func (s *Simulator) calculateHashrateLocked(isMining bool) float64 {
	if !isMining {
		return 0
	}

	baseline := s.config.BaselineHashrateTHS
	variancePercent := s.config.Behavior.HashrateVariancePercent
	variance := baseline * (variancePercent / percentFactor)
	return baseline + s.randomVarianceLocked(variance)
}

// calculateTemperatureLocked requires mu to be held.
func (s *Simulator) calculateTemperatureLocked() float64 {
	baseline := s.config.BaselineTempC
	variance := s.config.Behavior.TempVarianceC
	return baseline + s.randomVarianceLocked(variance)
}

// calculatePowerLocked requires mu to be held.
func (s *Simulator) calculatePowerLocked(isMining bool) float64 {
	if !isMining {
		return s.config.BaselinePowerW * idlePowerRatio
	}

	baseline := s.config.BaselinePowerW
	variance := baseline * powerVarianceRatio
	return baseline + s.randomVarianceLocked(variance)
}

func (s *Simulator) calculateEfficiency(hashrateTHS, powerW float64) float64 {
	if hashrateTHS <= 0 || powerW <= 0 {
		return 0
	}
	// EfficiencyJH expects J/H (Joules per Hash), not J/TH
	// J/H = Watts / (TH/s * 1e12) = Watts / (H/s)
	hashrateHS := hashrateTHS * teraHashToHash
	return powerW / hashrateHS
}

// generateHashboardsLocked requires mu to be held.
func (s *Simulator) generateHashboardsLocked(isMining bool) []sdk.HashBoardMetrics {
	boards := make([]sdk.HashBoardMetrics, s.config.Hashboards)

	for i := range boards {
		status := sdk.ComponentStatusHealthy

		// Check for degraded board error injection
		if s.config.Behavior.ErrorInjection.Enabled {
			if s.rng.Float64() < s.config.Behavior.ErrorInjection.DegradedBoardProbability {
				status = sdk.ComponentStatusWarning
			}
		}

		boardHashrate := s.calculateHashrateLocked(isMining) / float64(s.config.Hashboards)
		boardTemp := s.calculateTemperatureLocked() + s.randomVarianceLocked(boardTempVarianceC)
		chipCount := int32(s.config.ASICsPerBoard)

		boards[i] = sdk.HashBoardMetrics{
			ComponentInfo: sdk.ComponentInfo{
				Index:  int32(i),
				Name:   "",
				Status: status,
			},
			HashRateHS: toMetricValue(boardHashrate * teraHashToHash),
			TempC:      toMetricValue(boardTemp),
			ChipCount:  &chipCount,
			ASICs:      s.generateASICsLocked(i, isMining, boardTemp),
		}
	}

	return boards
}

// generateASICsLocked requires mu to be held.
func (s *Simulator) generateASICsLocked(boardIndex int, isMining bool, boardTemp float64) []sdk.ASICMetrics {
	asics := make([]sdk.ASICMetrics, s.config.ASICsPerBoard)
	asicHashrate := s.calculateHashrateLocked(isMining) / float64(s.config.Hashboards) / float64(s.config.ASICsPerBoard)

	for i := range asics {
		asicTemp := boardTemp + s.randomVarianceLocked(asicTempVarianceC)
		asics[i] = sdk.ASICMetrics{
			ComponentInfo: sdk.ComponentInfo{
				Index:  int32(i),
				Status: sdk.ComponentStatusHealthy,
			},
			TempC:      toMetricValue(asicTemp),
			HashrateHS: toMetricValue(asicHashrate * teraHashToHash),
		}
	}

	return asics
}

// generateFanMetricsLocked requires mu to be held.
func (s *Simulator) generateFanMetricsLocked() []sdk.FanMetrics {
	fans := make([]sdk.FanMetrics, s.config.FanCount)

	for i := range fans {
		rpm := float64(s.config.FanRPMMin) + s.rng.Float64()*float64(s.config.FanRPMMax-s.config.FanRPMMin)
		fans[i] = sdk.FanMetrics{
			ComponentInfo: sdk.ComponentInfo{
				Index:  int32(i),
				Status: sdk.ComponentStatusHealthy,
			},
			RPM: toMetricValue(rpm),
		}
	}

	return fans
}

// generatePSUMetricsLocked requires mu to be held.
func (s *Simulator) generatePSUMetricsLocked(isMining bool, totalPower float64) []sdk.PSUMetrics {
	return []sdk.PSUMetrics{
		{
			ComponentInfo: sdk.ComponentInfo{
				Index:  0,
				Status: sdk.ComponentStatusHealthy,
			},
			OutputPowerW:   toMetricValue(totalPower),
			OutputVoltageV: toMetricValue(nominalPSUVoltage + s.randomVarianceLocked(psuVoltageVarianceV)),
			InputVoltageV:  toMetricValue(nominalInputVoltage + s.randomVarianceLocked(inputVoltageVarianceV)),
		},
	}
}

// randomVarianceLocked requires mu to be held.
func (s *Simulator) randomVarianceLocked(maxVariance float64) float64 {
	return (s.rng.Float64()*2 - 1) * maxVariance
}

func toMetricValue(value float64) *sdk.MetricValue {
	return &sdk.MetricValue{
		Value: value,
		Kind:  sdk.MetricKindGauge,
	}
}
