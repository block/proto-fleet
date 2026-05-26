package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

const (
	// RPC connection timeout
	rpcConnectionTimeout = 30 * time.Second

	// RPC status codes
	statusCodeVersion = 11
	statusCodeSummary = 11
	statusCodePools   = 7
	statusCodeDevices = 9

	// Hashrate variation values (GH/s)
	hashrate5sVariation  = 2000
	hashrate30mVariation = 1000

	// Time offset for device responses (seconds)
	deviceTimeOffset = 6

	// Test data values for summary response
	mockGetworkCount        = 34537
	mockAcceptedCount       = 73280
	mockRejectedCount       = 75
	mockHardwareErrorsCount = 63
	mockUtilityValue        = 12.79
	mockDiscardedCount      = 32426300
	mockStaleCount          = 44
	mockGetFailuresCount    = 3
	mockLocalWorkCount      = 32459750
	mockRemoteFailuresCount = 0
	mockNetworkBlocksCount  = 571
	mockTotalMH             = 8.18439346825e13
	mockWorkUtility         = 3320110.48
	mockDifficultyAccepted  = 18995326976.0
	mockDifficultyRejected  = 19333120.0
	mockDifficultyStale     = 0.0
	mockBestShare           = 13416269691
	mockDevicePercentage    = 0.0
	mockASCIndex            = 0
	mockDeviceID            = 0
	mockDiff1Work           = 0
	mockLastSharePool       = 0
	mockDifficultyValue     = 18995326976
	mockMessageID           = 1

	// Conversion factor for TH/s to GH/s
	thsToGhsConversionFactor = 1000
)

func handleRPCConnection(conn net.Conn, state *MinerState) {
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(rpcConnectionTimeout)); err != nil {
		log.Printf("Failed to set read deadline: %v", err)
		return
	}

	var request RPCRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&request); err != nil {
		log.Printf("Failed to decode request: %v", err)
		return
	}

	log.Printf("Received RPC command: %s", request.Command)

	var response interface{}
	switch request.Command {
	case "version":
		response = generateVersionResponse(state)
	case "summary":
		response = generateSummaryResponse(state)
	case "pools":
		response = generatePoolsResponse(state)
	case "devs":
		response = generateDevsResponse(state)
	default:
		log.Printf("Unknown command: %s", request.Command)
		response = map[string]string{"error": "unknown command"}
	}

	if err := json.NewEncoder(conn).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
		return
	}
}

func generateVersionResponse(state *MinerState) VersionResponse {
	state.mu.RLock()
	defer state.mu.RUnlock()

	now := time.Now().Unix()
	return VersionResponse{
		RPCResponse: RPCResponse{
			Status: []StatusInfo{
				{
					Status: "S",
					When:   now,
					Code:   statusCodeVersion,
					Msg:    "BMMiner versions",
				},
			},
		},
		Version: []VersionInfo{
			{
				BMMiner:     "2.0.0",
				API:         "3.1",
				Miner:       state.MinerType,
				CompileTime: "2023-05-01",
				Type:        state.MinerType,
			},
		},
	}
}

func generateSummaryResponse(state *MinerState) SummaryResponse {
	state.mu.RLock()
	defer state.mu.RUnlock()

	now := time.Now().Unix()

	hashRateGHS := state.effectiveHashRateLocked() * thsToGhsConversionFactor

	// Apply error configuration
	hwErrors := mockHardwareErrorsCount
	hwPercent := mockDevicePercentage
	if state.ErrorConfig.HWErrorCount > 0 {
		hwErrors = state.ErrorConfig.HWErrorCount
	}
	if state.ErrorConfig.HWErrorPercent > 0 {
		hwPercent = state.ErrorConfig.HWErrorPercent
	}

	rejected := mockRejectedCount
	rejectedPercent := mockDevicePercentage
	if state.ErrorConfig.RejectedCount > 0 {
		rejected = state.ErrorConfig.RejectedCount
	}
	if state.ErrorConfig.RejectedPercent > 0 {
		rejectedPercent = state.ErrorConfig.RejectedPercent
	}

	stale := mockStaleCount
	if state.ErrorConfig.StaleCount > 0 {
		stale = state.ErrorConfig.StaleCount
	}

	ghs5s := hashRateGHS - hashrate5sVariation
	ghsav := hashRateGHS
	ghs30m := hashRateGHS + hashrate30mVariation
	totalMH := mockTotalMH
	utility := mockUtilityValue
	workUtility := mockWorkUtility
	if hashRateGHS == 0 {
		ghs5s = 0
		ghsav = 0
		ghs30m = 0
		totalMH = 0
		utility = 0
		workUtility = 0
	}

	return SummaryResponse{
		RPCResponse: RPCResponse{
			Status: []StatusInfo{
				{
					Status:      "S",
					When:        now,
					Code:        statusCodeSummary,
					Msg:         "Summary",
					Description: "cgminer 1.0.0",
				},
			},
		},
		Summary: []SummaryInfo{
			{
				Elapsed:            DefaultElapsedTime,
				GHS5s:              ghs5s,
				GHSav:              ghsav,
				GHS30m:             ghs30m,
				FoundBlocks:        mockDiff1Work,
				Getwork:            mockGetworkCount,
				Accepted:           mockAcceptedCount,
				Rejected:           rejected,
				HardwareErrors:     hwErrors,
				Utility:            utility,
				Discarded:          mockDiscardedCount,
				Stale:              stale,
				GetFailures:        mockGetFailuresCount,
				LocalWork:          mockLocalWorkCount,
				RemoteFailures:     mockRemoteFailuresCount,
				NetworkBlocks:      mockNetworkBlocksCount,
				TotalMH:            totalMH,
				WorkUtility:        workUtility,
				DifficultyAccepted: mockDifficultyAccepted,
				DifficultyRejected: mockDifficultyRejected,
				DifficultyStale:    mockDifficultyStale,
				BestShare:          mockBestShare,
				DeviceHardwarePerc: hwPercent,
				DeviceRejectedPerc: rejectedPercent,
				PoolRejectedPerc:   rejectedPercent,
				PoolStalePerc:      mockDevicePercentage,
				LastGetwork:        now,
			},
		},
		ID: mockMessageID,
	}
}

func generatePoolsResponse(state *MinerState) PoolsResponse {
	state.mu.RLock()
	defer state.mu.RUnlock()

	now := time.Now().Unix()

	// Apply error configuration
	poolStatus := "Alive"
	if state.ErrorConfig.PoolNotAlive {
		poolStatus = "Dead"
	}

	getFailures := int64(0)
	if state.ErrorConfig.PoolGetFailures > 0 {
		getFailures = int64(state.ErrorConfig.PoolGetFailures)
	}

	remoteFailures := int64(0)
	if state.ErrorConfig.PoolRemoteFailures > 0 {
		remoteFailures = int64(state.ErrorConfig.PoolRemoteFailures)
	}

	pools := make([]PoolStatus, len(state.Pools))
	for i, pool := range state.Pools {
		pools[i] = PoolStatus{
			URL:            pool.URL,
			User:           pool.User,
			Status:         poolStatus,
			Priority:       i,
			GetWorks:       DefaultGetWorks,
			Accepted:       DefaultAccepted,
			Rejected:       DefaultRejected,
			Discarded:      DefaultDiscarded,
			LastShare:      int(now) - DefaultLastShareDelay,
			Difficulty:     DefaultDifficulty,
			Diff1Share:     DefaultAccepted,
			GetFailures:    getFailures,
			RemoteFailures: remoteFailures,
		}
	}

	return PoolsResponse{
		RPCResponse: RPCResponse{
			Status: []StatusInfo{
				{
					Status: "S",
					When:   now,
					Code:   statusCodePools,
					Msg:    fmt.Sprintf("%d Pool(s)", len(state.Pools)),
				},
			},
		},
		Pools: pools,
	}
}

func generateDevsResponse(state *MinerState) DevsResponse {
	state.mu.RLock()
	defer state.mu.RUnlock()

	now := time.Now().Unix()

	// Apply error configuration
	boardTemp := DefaultTemperature
	if state.ErrorConfig.BoardTemperature > 0 {
		boardTemp = state.ErrorConfig.BoardTemperature
	}

	boardEnabled := "Y"
	if state.ErrorConfig.BoardDisabled {
		boardEnabled = "N"
	}

	boardStatus := "Alive"
	if state.ErrorConfig.BoardNotAlive {
		boardStatus = "Dead"
	}

	boardHashrate := state.effectiveHashRateLocked() * thsToGhsConversionFactor
	if state.ErrorConfig.BoardNotHashing {
		boardHashrate = 0
	}

	hwErrors := mockDiff1Work
	if state.ErrorConfig.HWErrorCount > 0 {
		hwErrors = state.ErrorConfig.HWErrorCount
	}

	hwPercent := mockDevicePercentage
	if state.ErrorConfig.HWErrorPercent > 0 {
		hwPercent = state.ErrorConfig.HWErrorPercent
	}

	rejected := mockRejectedCount
	rejectedPercent := mockDevicePercentage
	if state.ErrorConfig.RejectedCount > 0 {
		rejected = state.ErrorConfig.RejectedCount
	}
	if state.ErrorConfig.RejectedPercent > 0 {
		rejectedPercent = state.ErrorConfig.RejectedPercent
	}

	// Create one device according to the example format
	devices := []DeviceInfo{
		{
			ASC:                 mockASCIndex,
			Name:                "BTM_SOC",
			ID:                  mockDeviceID,
			Enabled:             boardEnabled,
			Status:              boardStatus,
			Tenperature:         boardTemp,
			MHSav:               boardHashrate,
			MHS5s:               boardHashrate,
			Accepted:            mockAcceptedCount,
			Rejected:            rejected,
			HardwareErrors:      hwErrors,
			Utility:             mockDevicePercentage,
			LastSharePool:       mockLastSharePool,
			LastShareTime:       now - deviceTimeOffset,
			TotalMH:             mockDevicePercentage,
			Diff1Work:           mockDiff1Work,
			DifficultyAccepted:  mockDifficultyValue,
			DifficultyRejected:  int(mockDifficultyRejected),
			LastShareDifficulty: now - deviceTimeOffset,
			LastValidWork:       now - deviceTimeOffset,
			DeviceHardwarePerc:  hwPercent,
			DeviceRejectedPerc:  rejectedPercent,
			DeviceElapsed:       DefaultElapsedTime,
		},
	}

	return DevsResponse{
		RPCResponse: RPCResponse{
			Status: []StatusInfo{
				{
					Status:      "S",
					When:        now,
					Code:        statusCodeDevices,
					Msg:         "1 ASC(s)",
					Description: "cgminer 1.0.0",
				},
			},
		},
		Devices: devices,
		ID:      mockMessageID,
	}
}
