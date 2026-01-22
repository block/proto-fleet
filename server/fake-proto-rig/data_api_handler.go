package main

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api/miner_data_apiconnect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_fan_api"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_psu_api"
)

var _ miner_data_apiconnect.MinerDataApiHandler = (*DataApiHandler)(nil)

// DataApiHandler implements MinerDataApi for the fake miner.
type DataApiHandler struct {
	state *MinerState
}

// NewDataApiHandler creates a new DataApiHandler.
func NewDataApiHandler(state *MinerState) *DataApiHandler {
	return &DataApiHandler{state: state}
}

// GetSoftwareInfo returns software/firmware version information.
func (h *DataApiHandler) GetSoftwareInfo(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.SoftwareInfoResponse], error) {
	return connect.NewResponse(&miner_data_api.SoftwareInfoResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
		SwInfo: &miner_data_api.SoftwareInfo{
			Name:    defaultSoftwareName,
			Version: defaultFirmwareVersion,
		},
	}), nil
}

// GetCoolingMode returns the current cooling configuration.
func (h *DataApiHandler) GetCoolingMode(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.CoolingModeResponse], error) {
	h.state.mu.RLock()
	mode := h.state.CoolingMode
	speedPct := h.state.FanSpeedPct
	h.state.mu.RUnlock()

	// Generate fan status for 4 fans
	fanCount := 4
	fanStatus := make([]*miner_fan_api.FanStatus, fanCount)
	for i := range fanCount {
		rpm := uint32(applyVariation(float64(defaultFanSpeedRPM), telemetryVariation))
		pct := speedPct
		fanStatus[i] = &miner_fan_api.FanStatus{
			Index:           uint32(i),
			TachometerValue: rpm,
			PercentageValue: &pct,
		}
	}

	return connect.NewResponse(&miner_data_api.CoolingModeResponse{
		Result:          miner_common_api.ApiResult_RESULT_SUCCESS,
		Mode:            mode,
		SpeedPercentage: speedPct,
		FanStatus:       fanStatus,
	}), nil
}

// GetMiningStatus returns the current mining status and statistics.
func (h *DataApiHandler) GetMiningStatus(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.MiningStatusResponse], error) {
	miningState := h.state.GetMiningState()
	hashrate, temperature, power, efficiency := h.state.GetMinerTelemetry()

	// Generate hashboard status
	hashboards := make([]*miner_data_api.HashboardStatusResponse, 0, defaultHashboardCount)
	for i := range defaultHashboardCount {
		if h.state.IsHashboardMissing(i) {
			continue
		}

		hbState := miner_data_api.HashboardState_HASHBOARD_STATE_MINING
		if h.state.IsHashboardInError(i) {
			hbState = miner_data_api.HashboardState_HASHBOARD_STATE_ERROR
		} else if miningState != miner_data_api.MiningState_MINING_STATE_MINING {
			hbState = miner_data_api.HashboardState_HASHBOARD_STATE_OFF
		}

		hbHashrate := applyVariation(defaultHashboardHashrate, telemetryVariation)
		if miningState != miner_data_api.MiningState_MINING_STATE_MINING || hbState == miner_data_api.HashboardState_HASHBOARD_STATE_ERROR {
			hbHashrate = 0
		}

		hashboards = append(hashboards, &miner_data_api.HashboardStatusResponse{
			Result:       miner_common_api.ApiResult_RESULT_SUCCESS,
			HashboardId:  uint32(i),
			SerialNumber: fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, i),
			State:        hbState,
			UptimeS:      uint32(h.state.StartTime.Unix()),
			PowerOnCount: 1,
			MiningStatistics: &miner_data_api.MiningStatistics{
				HashrateMhS:      hbHashrate * 1e6, // Convert TH/s to MH/s
				IdealHashrateMhS: defaultHashboardHashrate * 1e6,
				VoltageMv:        applyVariation(defaultHashboardVoltage*1000, telemetryVariation),
				CurrentMa:        applyVariation(defaultHashboardCurrent*1000, telemetryVariation),
				PowerUsageW:      applyVariation(defaultHashboardPower, telemetryVariation),
				EfficiencyJth:    applyVariation(defaultEfficiencyJTH, telemetryVariation),
				HbAvgTempC:       applyVariation(defaultHashboardAvgTemp, telemetryVariation),
				HbInletTempC:     applyVariation(defaultHashboardInletTemp, telemetryVariation),
				HbOutletTempC:    applyVariation(defaultHashboardOutletTemp, telemetryVariation),
				AsicAvgTempC:     applyVariation(defaultASICTemperature, telemetryVariation),
				AsicMinTempC:     applyVariation(defaultASICTemperature-5, telemetryVariation),
				AsicMaxTempC:     applyVariation(defaultASICTemperature+5, telemetryVariation),
			},
		})
	}

	h.state.mu.RLock()
	powerTargetW := h.state.PowerTargetW
	h.state.mu.RUnlock()

	return connect.NewResponse(&miner_data_api.MiningStatusResponse{
		Result:     miner_common_api.ApiResult_RESULT_SUCCESS,
		State:      miningState,
		Hashboards: hashboards,
		MiningStatistics: &miner_data_api.MiningStatistics{
			HashrateMhS:      hashrate * 1e6, // Convert TH/s to MH/s
			IdealHashrateMhS: defaultIdealHashrate * 1e6,
			PowerUsageW:      power,
			PowerTargetW:     powerTargetW,
			EfficiencyJth:    efficiency,
			HbAvgTempC:       temperature,
			AsicAvgTempC:     applyVariation(defaultASICTemperature, telemetryVariation),
		},
	}), nil
}

// GetPowerTarget returns the current power target configuration.
func (h *DataApiHandler) GetPowerTarget(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.PowerTargetResponse], error) {
	h.state.mu.RLock()
	powerTarget := h.state.PowerTargetW
	perfMode := h.state.PerformanceMode
	h.state.mu.RUnlock()

	return connect.NewResponse(&miner_data_api.PowerTargetResponse{
		Result:                miner_common_api.ApiResult_RESULT_SUCCESS,
		PowerTargetW:          powerTarget,
		PerformanceMode:       perfMode,
		PowerTargetMinW:       defaultPowerTargetMin,
		PowerTargetMaxW:       defaultPowerTargetMax,
		DefaultPowerTargetW:   defaultPowerTargetW,
		PhaseBalancingEnabled: false,
	}), nil
}

// GetHardwareInfo returns hardware information about the miner.
func (h *DataApiHandler) GetHardwareInfo(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.HardwareInfoResponse], error) {
	// Generate hashboard info
	hashboards := make([]*miner_data_api.HashboardInfo, 0, defaultHashboardCount)
	for i := range defaultHashboardCount {
		if h.state.IsHashboardMissing(i) {
			continue
		}
		hashboards = append(hashboards, &miner_data_api.HashboardInfo{
			Id:              uint32(i),
			SerialNumber:    fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, i),
			ApiVersion:      "1.0",
			Board:           "B4-HB",
			ChipId:          "BM1370",
			MiningAsic:      "BM1370",
			MiningAsicCount: defaultASICCount,
			TempSensorCount: 4,
			UsbPort:         uint32(i),
			Slot:            uint32(i),
			Firmware: &miner_data_api.HashboardInfo_HashboardFirmwareAsset{
				Version: defaultFirmwareVersion,
				GitHash: "abc123",
				Build:   miner_common_api.Build_BUILD_RELEASE,
			},
		})
	}

	// Generate PSU info
	psus := make([]*miner_psu_api.PsuInfo, 0, defaultPSUCount)
	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			continue
		}
		psus = append(psus, &miner_psu_api.PsuInfo{
			Id:           uint32(i),
			SerialNumber: fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			Vendor:       "Proto",
			Model:        "PSU-3600W",
			HwRevision:   "1.0",
			Slot:         uint32(i),
			Firmware: &miner_psu_api.PsuFirmwareAsset{
				AppVersion: "1.2.0",
				BlVersion:  "1.0.0",
			},
		})
	}

	// Generate fan info
	fans := make([]*miner_data_api.FanInfo, 4)
	for i := range 4 {
		minRPM := uint32(1000)
		maxRPM := uint32(6000)
		fans[i] = &miner_data_api.FanInfo{
			Id:     uint32(i),
			Name:   fmt.Sprintf("Fan %d", i+1),
			MinRpm: &minRPM,
			MaxRpm: &maxRPM,
		}
	}

	return connect.NewResponse(&miner_data_api.HardwareInfoResponse{
		Result:     miner_common_api.ApiResult_RESULT_SUCCESS,
		Hashboards: hashboards,
		Psus:       psus,
		Fans:       fans,
		CbInfo: &miner_data_api.ControlBoardInfo{
			MachineName:  h.state.Model,
			BoardId:      "CB-001",
			SerialNumber: h.state.SerialNumber,
			Mpu: &miner_data_api.ControlBoardInfo_MPUInfo{
				ModelName: "ARM Cortex-A53",
				Hardware:  "BCM2835",
				Serial:    h.state.SerialNumber,
			},
			Firmware: &miner_data_api.ControlBoardInfo_ControlBoardLinuxAsset{
				Name:    "ProtoOS",
				Version: defaultFirmwareVersion,
				GitHash: "abc123",
				Variant: "production",
			},
		},
	}), nil
}

// GetHashboardStatus returns status for a specific hashboard.
func (h *DataApiHandler) GetHashboardStatus(ctx context.Context, req *connect.Request[miner_data_api.HashboardStatusRequest]) (*connect.Response[miner_data_api.HashboardStatusResponse], error) {
	// Find the hashboard by serial number
	sn := req.Msg.HashboardSn

	// Parse the index from serial number format "HB-<miner_sn>-<index>"
	var idx int
	_, err := fmt.Sscanf(sn, "HB-"+h.state.SerialNumber+"-%d", &idx)
	if err != nil || idx < 0 || idx >= defaultHashboardCount {
		return connect.NewResponse(&miner_data_api.HashboardStatusResponse{
			Result: miner_common_api.ApiResult_RESULT_ERR_HB_NOT_FOUND,
		}), nil
	}

	if h.state.IsHashboardMissing(idx) {
		return connect.NewResponse(&miner_data_api.HashboardStatusResponse{
			Result: miner_common_api.ApiResult_RESULT_ERR_HB_NOT_FOUND,
		}), nil
	}

	miningState := h.state.GetMiningState()
	hbState := miner_data_api.HashboardState_HASHBOARD_STATE_MINING
	if h.state.IsHashboardInError(idx) {
		hbState = miner_data_api.HashboardState_HASHBOARD_STATE_ERROR
	} else if miningState != miner_data_api.MiningState_MINING_STATE_MINING {
		hbState = miner_data_api.HashboardState_HASHBOARD_STATE_OFF
	}

	hbHashrate := applyVariation(defaultHashboardHashrate, telemetryVariation)
	if miningState != miner_data_api.MiningState_MINING_STATE_MINING || hbState == miner_data_api.HashboardState_HASHBOARD_STATE_ERROR {
		hbHashrate = 0
	}

	return connect.NewResponse(&miner_data_api.HashboardStatusResponse{
		Result:       miner_common_api.ApiResult_RESULT_SUCCESS,
		HashboardId:  uint32(idx),
		SerialNumber: sn,
		State:        hbState,
		UptimeS:      uint32(h.state.StartTime.Unix()),
		PowerOnCount: 1,
		MiningStatistics: &miner_data_api.MiningStatistics{
			HashrateMhS:      hbHashrate * 1e6,
			IdealHashrateMhS: defaultHashboardHashrate * 1e6,
			VoltageMv:        applyVariation(defaultHashboardVoltage*1000, telemetryVariation),
			CurrentMa:        applyVariation(defaultHashboardCurrent*1000, telemetryVariation),
			PowerUsageW:      applyVariation(defaultHashboardPower, telemetryVariation),
			EfficiencyJth:    applyVariation(defaultEfficiencyJTH, telemetryVariation),
			HbAvgTempC:       applyVariation(defaultHashboardAvgTemp, telemetryVariation),
			HbInletTempC:     applyVariation(defaultHashboardInletTemp, telemetryVariation),
			HbOutletTempC:    applyVariation(defaultHashboardOutletTemp, telemetryVariation),
		},
	}), nil
}

// GetAsicStatus returns status for a specific ASIC.
func (h *DataApiHandler) GetAsicStatus(ctx context.Context, req *connect.Request[miner_data_api.AsicStatusRequest]) (*connect.Response[miner_data_api.AsicStatusResponse], error) {
	asicID := req.Msg.HashboardAsicId
	if asicID == nil {
		return connect.NewResponse(&miner_data_api.AsicStatusResponse{
			Result: miner_common_api.ApiResult_RESULT_ERR_ASIC_NOT_FOUND,
		}), nil
	}

	asicIndex := asicID.AsicIndex
	if asicIndex >= defaultASICCount {
		return connect.NewResponse(&miner_data_api.AsicStatusResponse{
			Result: miner_common_api.ApiResult_RESULT_ERR_ASIC_NOT_FOUND,
		}), nil
	}

	miningState := h.state.GetMiningState()
	asicHashrate := applyVariation(defaultASICHashrate, telemetryVariation)
	if miningState != miner_data_api.MiningState_MINING_STATE_MINING {
		asicHashrate = 0
	}

	// Calculate row/column based on ASIC index (assuming 10 columns)
	row := asicIndex / 10
	column := asicIndex % 10

	return connect.NewResponse(&miner_data_api.AsicStatusResponse{
		Result:          miner_common_api.ApiResult_RESULT_SUCCESS,
		HashboardAsicId: asicID,
		Row:             row,
		Column:          column,
		MiningStatistics: &miner_data_api.AsicMiningStatistics{
			HashrateMhS:      asicHashrate * 1e6,
			IdealHashrateMhS: defaultASICHashrate * 1e6,
			ErrorRatePercent: applyVariation(0.01, 1.0), // ~1% error rate
			TemperatureC:     applyVariation(defaultASICTemperature, telemetryVariation),
			VoltageMv:        applyVariation(1200, telemetryVariation), // ~1.2V
			FrequencyMhz:     applyVariation(600, telemetryVariation),  // ~600MHz
		},
	}), nil
}

// GetTimeSeriesData returns historical time series data.
func (h *DataApiHandler) GetTimeSeriesData(ctx context.Context, req *connect.Request[miner_data_api.TimeSeriesDataRequest]) (*connect.Response[miner_data_api.TimeSeriesDataResponse], error) {
	// For simulation, return empty time series (not implemented in detail)
	return connect.NewResponse(&miner_data_api.TimeSeriesDataResponse{
		Result:     miner_common_api.ApiResult_RESULT_SUCCESS,
		DataPoints: []*miner_data_api.TimeSeriesDataResponse_TimeSeriesDataPoint{},
	}), nil
}

// GetUnifiedTimeSeriesData returns comprehensive time series data.
func (h *DataApiHandler) GetUnifiedTimeSeriesData(ctx context.Context, req *connect.Request[miner_data_api.UnifiedTimeSeriesDataRequest]) (*connect.Response[miner_data_api.UnifiedTimeSeriesDataResponse], error) {
	// For simulation, return empty unified time series (not implemented in detail)
	return connect.NewResponse(&miner_data_api.UnifiedTimeSeriesDataResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
	}), nil
}

// GetPools returns the currently configured mining pools.
func (h *DataApiHandler) GetPools(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.PoolsResponse], error) {
	pools := h.state.GetPools()

	// Apply connection status based on error config
	h.state.mu.RLock()
	poolsOffline := h.state.ErrorConfig.PoolsOffline
	h.state.mu.RUnlock()

	for _, pool := range pools {
		if poolsOffline {
			pool.ConnectionStatus = miner_data_api.PoolConnectionStatus_POOL_CONNECTION_STATUS_DEAD
		} else if pool.Url != "" {
			pool.ConnectionStatus = miner_data_api.PoolConnectionStatus_POOL_CONNECTION_STATUS_ACTIVE
		} else {
			pool.ConnectionStatus = miner_data_api.PoolConnectionStatus_POOL_CONNECTION_STATUS_IDLE
		}
	}

	return connect.NewResponse(&miner_data_api.PoolsResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
		Pools:  pools,
	}), nil
}

// GetErrors returns error history.
func (h *DataApiHandler) GetErrors(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.ErrorsResponse], error) {
	// For simulation, return empty error list
	return connect.NewResponse(&miner_data_api.ErrorsResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
		Errors: []*miner_data_api.ErrorFromDb{},
	}), nil
}

// GetPsuStatusList returns status for all PSUs.
func (h *DataApiHandler) GetPsuStatusList(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.PsuStatusListResponse], error) {
	statusList := make([]*miner_psu_api.PsuStatus, 0, defaultPSUCount)
	failedList := make([]uint32, 0)

	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			failedList = append(failedList, uint32(i))
			continue
		}

		psuState := miner_psu_api.PsuState_PSU_STATE_OUTPUT_VOLTAGE_READY
		if h.state.IsPSUInError(i) {
			psuState = miner_psu_api.PsuState_PSU_STATE_ERROR
		}

		statusList = append(statusList, &miner_psu_api.PsuStatus{
			Id:    uint32(i),
			State: psuState,
			Slot:  uint32(i),
			Measurements: []*miner_psu_api.PsuMeasurement{
				{MeasurementType: miner_psu_api.PsuMeasurementType_PSU_MEASUREMENT_TYPE_INPUT_VOLTAGE, Value: int32(applyVariation(defaultPSUInputVoltage*1000, telemetryVariation)), Unit: miner_common_api.MetricUnit_METRIC_UNIT_MILLI},
				{MeasurementType: miner_psu_api.PsuMeasurementType_PSU_MEASUREMENT_TYPE_OUTPUT_VOLTAGE, Value: int32(applyVariation(defaultPSUOutputVoltage*1000, telemetryVariation)), Unit: miner_common_api.MetricUnit_METRIC_UNIT_MILLI},
				{MeasurementType: miner_psu_api.PsuMeasurementType_PSU_MEASUREMENT_TYPE_INPUT_CURRENT, Value: int32(applyVariation(defaultPSUInputCurrent*1000, telemetryVariation)), Unit: miner_common_api.MetricUnit_METRIC_UNIT_MILLI},
				{MeasurementType: miner_psu_api.PsuMeasurementType_PSU_MEASUREMENT_TYPE_OUTPUT_CURRENT, Value: int32(applyVariation(defaultPSUOutputCurrent*1000, telemetryVariation)), Unit: miner_common_api.MetricUnit_METRIC_UNIT_MILLI},
				{MeasurementType: miner_psu_api.PsuMeasurementType_PSU_MEASUREMENT_TYPE_INPUT_POWER, Value: int32(applyVariation(defaultPSUInputPower, telemetryVariation)), Unit: miner_common_api.MetricUnit_METRIC_UNIT_BASE},
				{MeasurementType: miner_psu_api.PsuMeasurementType_PSU_MEASUREMENT_TYPE_OUTPUT_POWER, Value: int32(applyVariation(defaultPSUOutputPower, telemetryVariation)), Unit: miner_common_api.MetricUnit_METRIC_UNIT_BASE},
				{MeasurementType: miner_psu_api.PsuMeasurementType_PSU_MEASUREMENT_TYPE_HOTSPOT_TEMPERATURE, Value: int32(applyVariation(defaultPSUHotspotTemp, telemetryVariation)), Unit: miner_common_api.MetricUnit_METRIC_UNIT_BASE},
				{MeasurementType: miner_psu_api.PsuMeasurementType_PSU_MEASUREMENT_TYPE_AMBIENT_TEMPERATURE, Value: int32(applyVariation(defaultPSUAmbientTemp, telemetryVariation)), Unit: miner_common_api.MetricUnit_METRIC_UNIT_BASE},
			},
		})
	}

	return connect.NewResponse(&miner_data_api.PsuStatusListResponse{
		Result:          miner_common_api.ApiResult_RESULT_SUCCESS,
		StatusList:      statusList,
		FailedPsuIdList: failedList,
	}), nil
}

// GetPsuInfoList returns hardware info for all PSUs.
func (h *DataApiHandler) GetPsuInfoList(ctx context.Context, req *connect.Request[miner_common_api.EmptyRequest]) (*connect.Response[miner_data_api.PsuInfoListResponse], error) {
	infoList := make([]*miner_psu_api.PsuInfo, 0, defaultPSUCount)
	failedList := make([]uint32, 0)

	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			failedList = append(failedList, uint32(i))
			continue
		}

		infoList = append(infoList, &miner_psu_api.PsuInfo{
			Id:           uint32(i),
			SerialNumber: fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			Vendor:       "Proto",
			Model:        "PSU-3600W",
			HwRevision:   "1.0",
			Slot:         uint32(i),
			Firmware: &miner_psu_api.PsuFirmwareAsset{
				AppVersion: "1.2.0",
				BlVersion:  "1.0.0",
			},
		})
	}

	return connect.NewResponse(&miner_data_api.PsuInfoListResponse{
		Result:          miner_common_api.ApiResult_RESULT_SUCCESS,
		InfoList:        infoList,
		FailedPsuIdList: failedList,
	}), nil
}

// GetTelemetryValues returns comprehensive telemetry data.
func (h *DataApiHandler) GetTelemetryValues(ctx context.Context, req *connect.Request[miner_data_api.GetTelemetryValuesRequest]) (*connect.Response[miner_data_api.GetTelemetryValuesResponse], error) {
	hashrate, temperature, power, efficiency := h.state.GetMinerTelemetry()
	miningState := h.state.GetMiningState()

	response := &miner_data_api.GetTelemetryValuesResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
	}

	// Check requested levels (default to miner if empty)
	levels := req.Msg.Levels
	if len(levels) == 0 {
		levels = []miner_data_api.TelemetryLevel{miner_data_api.TelemetryLevel_TELEMETRY_LEVEL_MINER}
	}

	includeASICs := false
	for _, level := range levels {
		switch level {
		case miner_data_api.TelemetryLevel_TELEMETRY_LEVEL_MINER:
			response.Miner = &miner_data_api.MinerTelemetryValues{
				HashrateThS:   hashrate,
				TemperatureC:  temperature,
				PowerW:        power,
				EfficiencyJTh: efficiency,
			}

		case miner_data_api.TelemetryLevel_TELEMETRY_LEVEL_HASHBOARD:
			response.Hashboards = h.generateHashboardTelemetry(miningState, false)

		case miner_data_api.TelemetryLevel_TELEMETRY_LEVEL_PSU:
			response.Psus = h.generatePSUTelemetry()

		case miner_data_api.TelemetryLevel_TELEMETRY_LEVEL_ASIC:
			includeASICs = true
		}
	}

	// If ASIC level requested, regenerate hashboards with ASIC data
	if includeASICs && response.Hashboards == nil {
		response.Hashboards = h.generateHashboardTelemetry(miningState, true)
	} else if includeASICs && response.Hashboards != nil {
		response.Hashboards = h.generateHashboardTelemetry(miningState, true)
	}

	return connect.NewResponse(response), nil
}

// generateHashboardTelemetry creates hashboard telemetry data.
func (h *DataApiHandler) generateHashboardTelemetry(miningState miner_data_api.MiningState, includeASICs bool) []*miner_data_api.HashboardTelemetryValues {
	hashboards := make([]*miner_data_api.HashboardTelemetryValues, 0, defaultHashboardCount)

	for i := range defaultHashboardCount {
		if h.state.IsHashboardMissing(i) {
			continue
		}

		hbHashrate := applyVariation(defaultHashboardHashrate, telemetryVariation)
		if miningState != miner_data_api.MiningState_MINING_STATE_MINING || h.state.IsHashboardInError(i) {
			hbHashrate = 0
		}

		voltage := applyVariation(defaultHashboardVoltage, telemetryVariation)
		current := applyVariation(defaultHashboardCurrent, telemetryVariation)

		hbTelemetry := &miner_data_api.HashboardTelemetryValues{
			Index:               uint32(i),
			SerialNumber:        fmt.Sprintf("HB-%s-%d", h.state.SerialNumber, i),
			HashrateThS:         hbHashrate,
			InletTemperatureC:   applyVariation(defaultHashboardInletTemp, telemetryVariation),
			OutletTemperatureC:  applyVariation(defaultHashboardOutletTemp, telemetryVariation),
			AverageTemperatureC: applyVariation(defaultHashboardAvgTemp, telemetryVariation),
			PowerW:              applyVariation(defaultHashboardPower, telemetryVariation),
			EfficiencyJTh:       applyVariation(defaultEfficiencyJTH, telemetryVariation),
			VoltageV:            &voltage,
			CurrentA:            &current,
		}

		if includeASICs {
			hbTelemetry.Asics = h.generateASICTelemetry(miningState, i)
		}

		hashboards = append(hashboards, hbTelemetry)
	}

	return hashboards
}

// generateASICTelemetry creates ASIC telemetry data for a hashboard.
func (h *DataApiHandler) generateASICTelemetry(miningState miner_data_api.MiningState, hashboardIndex int) *miner_data_api.AsicTelemetryValues {
	hashrateValues := make([]float64, defaultASICCount)
	tempValues := make([]float64, defaultASICCount)

	for i := range defaultASICCount {
		asicHashrate := applyVariation(defaultASICHashrate, telemetryVariation)
		if miningState != miner_data_api.MiningState_MINING_STATE_MINING || h.state.IsHashboardInError(hashboardIndex) {
			asicHashrate = 0
		}
		hashrateValues[i] = asicHashrate
		tempValues[i] = applyVariation(defaultASICTemperature, telemetryVariation)
	}

	return &miner_data_api.AsicTelemetryValues{
		HashrateThS:  hashrateValues,
		TemperatureC: tempValues,
	}
}

// generatePSUTelemetry creates PSU telemetry data.
func (h *DataApiHandler) generatePSUTelemetry() []*miner_data_api.PsuTelemetryValues {
	psus := make([]*miner_data_api.PsuTelemetryValues, 0, defaultPSUCount)

	for i := range defaultPSUCount {
		if h.state.IsPSUMissing(i) {
			continue
		}

		psus = append(psus, &miner_data_api.PsuTelemetryValues{
			Index:               uint32(i),
			SerialNumber:        fmt.Sprintf("PSU-%s-%d", h.state.SerialNumber, i),
			InputVoltageV:       applyVariation(defaultPSUInputVoltage, telemetryVariation),
			OutputVoltageV:      applyVariation(defaultPSUOutputVoltage, telemetryVariation),
			InputCurrentA:       applyVariation(defaultPSUInputCurrent, telemetryVariation),
			OutputCurrentA:      applyVariation(defaultPSUOutputCurrent, telemetryVariation),
			InputPowerW:         applyVariation(defaultPSUInputPower, telemetryVariation),
			OutputPowerW:        applyVariation(defaultPSUOutputPower, telemetryVariation),
			HotspotTemperatureC: applyVariation(defaultPSUHotspotTemp, telemetryVariation),
			AmbientTemperatureC: applyVariation(defaultPSUAmbientTemp, telemetryVariation),
			AverageTemperatureC: applyVariation((defaultPSUHotspotTemp+defaultPSUAmbientTemp)/2, telemetryVariation),
		})
	}

	return psus
}
