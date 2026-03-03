package main

import (
	"math"
	"math/rand/v2"
	"time"
)

// Metric generation constants.
const (
	// Hash rate: ~190 TH/s expressed in H/s
	baseHashRateTHs        = 190.0
	thsToHs                = 1e12
	baseHashRateHs         = baseHashRateTHs * thsToHs
	hashRateDeviceJitter   = 0.05 // ±5% per-device base variation
	hashRatePointJitter    = 0.02 // ±2% per-point noise
	hashRateDipProbability = 0.005
	hashRateDipMinFactor   = 0.50
	hashRateDipMaxFactor   = 0.80
	// Outlier injection for chart-scaling validation.
	// Tune these values to make spikes more/less visible in local testing.
	outlierPointProbability = 0.003
	outlierMultiplier       = 5.0

	// Temperature: sinusoidal 24h cycle
	baseTempC         = 55.0
	tempAmplitudeC    = 8.0
	tempNoiseC        = 2.0
	tempPeakHourUTC   = 15.0 // peak temperature at 15:00 UTC
	hoursPerDay       = 24.0
	maxDevicePhaseRad = math.Pi / 4 // max per-device phase offset

	// Ambient/inlet/outlet temperature offsets
	baseAmbientTempC   = 25.0
	ambientAmplitudeC  = 3.0
	inletOffsetMinC    = 2.0
	inletOffsetRangeC  = 3.0
	outletOffsetMinC   = 5.0
	outletOffsetRangeC = 5.0

	// Power
	basePowerW  = 3200.0
	powerNoiseW = 50.0

	// Fan RPM
	baseFanRPM            = 4000.0
	fanTempCoefficientRPM = 30.0 // RPM increase per °C above baseTempC
	fanNoiseRPM           = 200.0

	// Voltage / Current
	baseVoltageV  = 12.5
	voltageNoiseV = 0.2

	// Chip specs (typical ASIC miner values)
	chipFrequencyBaseMHz  = 550.0
	chipFrequencyNoiseMHz = 20.0

	// Health probability thresholds (cumulative)
	healthActiveProb   = 0.95
	healthInactiveProb = 0.98  // 0.95 + 0.03
	healthWarningProb  = 0.995 // 0.98 + 0.015

	// Health status values matching the database schema
	healthActive   = "health_healthy_active"
	healthInactive = "health_healthy_inactive"
	healthWarning  = "health_warning"
	healthCritical = "health_critical"

	// Metric kind value
	metricKindGauge = "metric_kind_gauge"
)

// Typical ASIC chip counts per device model.
var chipCounts = []int{114, 126, 138}

type deviceMetric struct {
	Time             time.Time
	DeviceIdentifier string
	HashRateHs       float64
	HashRateHsKind   string
	TempC            float64
	TempCKind        string
	FanRPM           float64
	FanRPMKind       string
	PowerW           float64
	PowerWKind       string
	EfficiencyJH     float64
	EfficiencyJHKind string
	VoltageV         float64
	VoltageVKind     string
	CurrentA         float64
	CurrentAKind     string
	InletTempC       float64
	OutletTempC      float64
	AmbientTempC     float64
	ChipCount        int
	ChipCountKind    string
	ChipFrequencyMHz float64
	Health           string
}

// deviceProfile holds per-device random offsets for consistent variation.
type deviceProfile struct {
	identifier   string
	hashRateBase float64
	phaseOffset  float64
	chipCount    int
}

func newDeviceProfile(identifier string, index int) deviceProfile {
	hashJitter := 1.0 + (rand.Float64()*2-1)*hashRateDeviceJitter
	phase := rand.Float64() * maxDevicePhaseRad

	return deviceProfile{
		identifier:   identifier,
		hashRateBase: baseHashRateHs * hashJitter,
		phaseOffset:  phase,
		chipCount:    chipCounts[index%len(chipCounts)],
	}
}

func generateMetrics(profiles []deviceProfile, start, end time.Time, interval time.Duration, outliers bool) []deviceMetric {
	totalPoints := int(end.Sub(start) / interval)
	metrics := make([]deviceMetric, 0, totalPoints*len(profiles))

	for _, profile := range profiles {
		for t := start; t.Before(end); t = t.Add(interval) {
			metrics = append(metrics, generatePoint(profile, t, outliers))
		}
	}

	return metrics
}

func generatePoint(p deviceProfile, t time.Time, outliers bool) deviceMetric {
	hourOfDay := float64(t.Hour()) + float64(t.Minute())/60.0

	// Sinusoidal temperature: peaks at tempPeakHourUTC
	tempPhase := 2 * math.Pi * (hourOfDay - tempPeakHourUTC) / hoursPerDay
	chipTemp := baseTempC + tempAmplitudeC*math.Sin(tempPhase+p.phaseOffset) +
		(rand.Float64()*2-1)*tempNoiseC

	ambientTemp := baseAmbientTempC + ambientAmplitudeC*math.Sin(tempPhase+p.phaseOffset)
	inletTemp := ambientTemp + inletOffsetMinC + rand.Float64()*inletOffsetRangeC
	outletTemp := chipTemp - outletOffsetMinC - rand.Float64()*outletOffsetRangeC

	// Hash rate with occasional dips
	hashRate := p.hashRateBase * (1.0 + (rand.Float64()*2-1)*hashRatePointJitter)
	isDip := rand.Float64() < hashRateDipProbability
	if isDip {
		dipFactor := hashRateDipMinFactor + rand.Float64()*(hashRateDipMaxFactor-hashRateDipMinFactor)
		hashRate = p.hashRateBase * dipFactor
	}
	if outliers && rand.Float64() < outlierPointProbability {
		hashRate *= outlierMultiplier
	}

	// Power scales proportionally with hash rate
	powerRatio := hashRate / p.hashRateBase
	powerW := basePowerW*powerRatio + (rand.Float64()*2-1)*powerNoiseW

	// Efficiency: J/TH = power_w / (hash_rate_hs / 1e12)
	hashRateTH := hashRate / thsToHs
	var efficiencyJH float64
	if hashRateTH > 0 {
		efficiencyJH = powerW / hashRateTH
	}

	// Fan RPM scales with temperature
	tempDelta := chipTemp - baseTempC
	fanRPM := baseFanRPM + tempDelta*fanTempCoefficientRPM + (rand.Float64()*2-1)*fanNoiseRPM
	if fanRPM < 0 {
		fanRPM = 0
	}

	// Voltage and current
	voltageV := baseVoltageV + (rand.Float64()*2-1)*voltageNoiseV
	var currentA float64
	if voltageV > 0 {
		currentA = powerW / voltageV
	}

	chipFreq := chipFrequencyBaseMHz + (rand.Float64()*2-1)*chipFrequencyNoiseMHz

	health := randomHealth(isDip)

	return deviceMetric{
		Time:             t,
		DeviceIdentifier: p.identifier,
		HashRateHs:       hashRate,
		HashRateHsKind:   metricKindGauge,
		TempC:            chipTemp,
		TempCKind:        metricKindGauge,
		FanRPM:           fanRPM,
		FanRPMKind:       metricKindGauge,
		PowerW:           powerW,
		PowerWKind:       metricKindGauge,
		EfficiencyJH:     efficiencyJH,
		EfficiencyJHKind: metricKindGauge,
		VoltageV:         voltageV,
		VoltageVKind:     metricKindGauge,
		CurrentA:         currentA,
		CurrentAKind:     metricKindGauge,
		InletTempC:       inletTemp,
		OutletTempC:      outletTemp,
		AmbientTempC:     ambientTemp,
		ChipCount:        p.chipCount,
		ChipCountKind:    metricKindGauge,
		ChipFrequencyMHz: chipFreq,
		Health:           health,
	}
}

func randomHealth(isDip bool) string {
	if isDip {
		if rand.Float64() < 0.5 {
			return healthWarning
		}
		return healthCritical
	}

	r := rand.Float64()
	switch {
	case r < healthActiveProb:
		return healthActive
	case r < healthInactiveProb:
		return healthInactive
	case r < healthWarningProb:
		return healthWarning
	default:
		return healthCritical
	}
}
