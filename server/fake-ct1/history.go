package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const historyRetention = 7 * 24 * time.Hour

type recordedBoard struct {
	Hashrate, Power, Temperature float64
	Mining                       bool
}
type recordedSample struct {
	At     time.Time
	Boards []recordedBoard
	Plant  containerTelemetryResponse
}
type simulationHistory struct {
	Baseline *ContainerState
	Samples  []recordedSample
}

func (s *ContainerState) recordSample() recordedSample {
	sample := recordedSample{At: s.sampleTime, Plant: s.containerTelemetry(), Boards: make([]recordedBoard, 0, s.BoardCount())}
	for _, b := range s.eachBoard() {
		v := s.boardSample(b)
		sample.Boards = append(sample.Boards, recordedBoard{v.Hashrate, v.Power, v.BoardTemp, b.isMining()})
	}
	return sample
}

// The pre-start backfill is the immutable initial scenario. After startup we
// retain actual minute samples and control transitions, including while no one
// is polling. Advancing happens before a mutation, so it cannot rewrite history.
func (s *ContainerState) initHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.history != nil {
		return
	}
	baseline := newContainerState(s.Config, s.StartTime)
	s.history = &simulationHistory{Baseline: baseline, Samples: []recordedSample{s.recordSample()}}
}
func (s *ContainerState) recordUntil(now time.Time) {
	if s.history == nil {
		return
	}
	samples := s.history.Samples
	last := samples[len(samples)-1].At
	for at := maxTime(last.Truncate(time.Minute).Add(time.Minute), now.Add(-historyRetention).Truncate(time.Minute)); !at.After(now); at = at.Add(time.Minute) {
		s.sampleTime = at
		s.history.Samples = append(s.history.Samples, s.recordSample())
	}
	cutoff := now.Add(-historyRetention)
	samples = s.history.Samples
	index := sort.Search(len(samples), func(i int) bool { return !samples[i].At.Before(cutoff) })
	if index > 1 {
		s.history.Samples = append([]recordedSample(nil), samples[index-1:]...)
	}
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (s *ContainerState) sampleAt(at time.Time) (recordedSample, bool) {
	if at.After(s.sampleTime) || at.Before(s.sampleTime.Add(-historyRetention)) {
		return recordedSample{}, false
	}
	if at.Before(s.StartTime) {
		baseline := s.history.Baseline
		baseline.sampleTime = at
		return baseline.recordSample(), true
	}
	samples := s.history.Samples
	i := sort.Search(len(samples), func(i int) bool { return samples[i].At.After(at) })
	if i == 0 {
		return recordedSample{}, false
	}
	return samples[i-1], true
}

func (v recordedSample) metric(metric, identifier string, boardIndex int) (float64, bool) {
	p := v.Plant.Plant
	switch metric {
	case timeSeriesMetricHashrate, timeSeriesMetricPower, timeSeriesMetricTemperature:
		sum, count := 0.0, 0
		for i, b := range v.Boards {
			if boardIndex >= 0 && boardIndex != i {
				continue
			}
			if !b.Mining && metric != timeSeriesMetricTemperature {
				continue
			}
			count++
			switch metric {
			case timeSeriesMetricHashrate:
				sum += b.Hashrate
			case timeSeriesMetricPower:
				sum += b.Power
			case timeSeriesMetricTemperature:
				sum += b.Temperature
			}
		}
		if metric == timeSeriesMetricTemperature {
			if count == 0 {
				return 0, false
			}
			sum /= float64(count)
		}
		return sum, true
	case "TIME_SERIES_METRIC_PLANT_POWER":
		return p.TotalPowerW, true
	case "TIME_SERIES_METRIC_PLANT_FACILITY_POWER":
		return p.FacilityPowerW, true
	case "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_POWER":
		return p.HeatRejectionPowerW, true
	case "TIME_SERIES_METRIC_PLANT_INSIDE_AMBIENT_TEMPERATURE":
		return p.InsideAmbientTemperatureC, true
	case "TIME_SERIES_METRIC_PLANT_OUTSIDE_AMBIENT_TEMPERATURE", "TIME_SERIES_METRIC_PLANT_AMBIENT_TEMPERATURE":
		return p.OutsideAmbientTemperatureC, true
	case "TIME_SERIES_METRIC_PLANT_INLET_TEMPERATURE":
		return p.InletTemperatureC, true
	case "TIME_SERIES_METRIC_PLANT_OUTLET_TEMPERATURE":
		return p.OutletTemperatureC, true
	case "TIME_SERIES_METRIC_PLANT_OUTLET_A_TEMPERATURE":
		return p.OutletATemperatureC, true
	case "TIME_SERIES_METRIC_PLANT_OUTLET_B_TEMPERATURE":
		return p.OutletBTemperatureC, true
	case "TIME_SERIES_METRIC_PLANT_FLOW_RATE":
		return p.FlowRateLpm, true
	case "TIME_SERIES_METRIC_PLANT_PUE":
		return p.PUE, p.TotalPowerW > 0
	case "TIME_SERIES_METRIC_PLANT_POWER_FACTOR":
		return p.PowerFactor, true
	case "TIME_SERIES_METRIC_PLANT_TANK_A_LIQUID_LEVEL":
		return p.TankALiquidLevelMm, true
	case "TIME_SERIES_METRIC_PLANT_TANK_B_LIQUID_LEVEL":
		return p.TankBLiquidLevelMm, true
	}
	for _, fan := range v.Plant.CoolingFans {
		if fan.ComponentID == identifier && strings.HasPrefix(metric, "TIME_SERIES_METRIC_PLANT_FAN_") {
			if strings.HasSuffix(metric, "FAN_SPEED") {
				return fan.SpeedPct, true
			}
			return fan.PowerW, true
		}
	}
	for _, pdu := range v.Plant.PowerDistributions {
		if pdu.ComponentID == identifier && strings.HasPrefix(metric, "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_") {
			if strings.HasSuffix(metric, "ACTIVE_FEEDS") {
				return float64(pdu.ActiveFeedCount), true
			}
			return pdu.ActivePowerW, true
		}
	}
	for _, feed := range v.Plant.PowerFeeds {
		if feed.PowerFeedID == identifier {
			switch metric {
			case "TIME_SERIES_METRIC_POWER_FEED_POWER":
				return feed.PowerW, true
			case "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE":
				return feed.VoltageV, true
			}
		}
	}
	for _, phase := range v.Plant.Phases {
		if phase.Phase == identifier {
			switch metric {
			case "TIME_SERIES_METRIC_PLANT_PHASE_POWER":
				return phase.PowerW, true
			case "TIME_SERIES_METRIC_PLANT_PHASE_CURRENT":
				return phase.CurrentA, true
			case "TIME_SERIES_METRIC_PLANT_PHASE_VOLTAGE":
				return phase.VoltageV, true
			}
		}
	}
	return 0, false
}

func (s *ContainerState) historyMetric(metric, identifier string) (unit string, boardIndex int, err error) {
	boardIndex = -1
	switch metric {
	case timeSeriesMetricHashrate:
		unit = "TH/s"
	case timeSeriesMetricPower:
		unit = "W"
	case "TIME_SERIES_METRIC_POWER_FEED_POWER":
		return "W", -1, nil
	case "TIME_SERIES_METRIC_POWER_FEED_VOLTAGE":
		return "V", -1, nil
	case timeSeriesMetricTemperature:
		unit = "C"
	default:
		if !strings.HasPrefix(metric, "TIME_SERIES_METRIC_PLANT_") {
			return "", -1, fmt.Errorf("unsupported metric")
		}
		switch strings.TrimPrefix(metric, "TIME_SERIES_METRIC_PLANT_") {
		case "POWER", "FACILITY_POWER", "HEAT_REJECTION_POWER", "DISTRIBUTION_POWER", "FAN_POWER", "PHASE_POWER":
			unit = "W"
		case "INSIDE_AMBIENT_TEMPERATURE", "OUTSIDE_AMBIENT_TEMPERATURE", "AMBIENT_TEMPERATURE", "INLET_TEMPERATURE", "OUTLET_TEMPERATURE", "OUTLET_A_TEMPERATURE", "OUTLET_B_TEMPERATURE":
			unit = "C"
		case "FLOW_RATE":
			unit = "L/min"
		case "PUE", "POWER_FACTOR", "DISTRIBUTION_ACTIVE_FEEDS":
			unit = ""
		case "TANK_A_LIQUID_LEVEL", "TANK_B_LIQUID_LEVEL":
			unit = "mm"
		case "FAN_SPEED":
			unit = "%"
		case "PHASE_CURRENT":
			unit = "A"
		case "PHASE_VOLTAGE":
			unit = "V"
		default:
			return "", -1, fmt.Errorf("unsupported metric")
		}
		return unit, -1, nil
	}
	if identifier != "" {
		b := s.findBoard(identifier)
		if b == nil {
			return "", -1, fmt.Errorf("unknown hashboard")
		}
		for i, board := range s.eachBoard() {
			if board == b {
				boardIndex = i
				break
			}
		}
	}
	return unit, boardIndex, nil
}

func (s *ContainerState) recordTransition() {
	sample := s.recordSample()
	samples := s.history.Samples
	if samples[len(samples)-1].At.Equal(sample.At) {
		samples[len(samples)-1] = sample
		return
	}
	s.history.Samples = append(samples, sample)
}
