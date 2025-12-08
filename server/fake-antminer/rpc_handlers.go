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
				Type:        "Antminer " + state.MinerType,
			},
		},
	}
}

func generateSummaryResponse(state *MinerState) SummaryResponse {
	state.mu.RLock()
	defer state.mu.RUnlock()

	now := time.Now().Unix()

	hashRateGHS := state.HashRate * thsToGhsConversionFactor

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
				GHS5s:              hashRateGHS - hashrate5sVariation,
				GHSav:              hashRateGHS,
				GHS30m:             hashRateGHS + hashrate30mVariation,
				FoundBlocks:        mockDiff1Work,
				Getwork:            mockGetworkCount,
				Accepted:           mockAcceptedCount,
				Rejected:           mockRejectedCount,
				HardwareErrors:     mockHardwareErrorsCount,
				Utility:            mockUtilityValue,
				Discarded:          mockDiscardedCount,
				Stale:              mockStaleCount,
				GetFailures:        mockGetFailuresCount,
				LocalWork:          mockLocalWorkCount,
				RemoteFailures:     mockRemoteFailuresCount,
				NetworkBlocks:      mockNetworkBlocksCount,
				TotalMH:            mockTotalMH,
				WorkUtility:        mockWorkUtility,
				DifficultyAccepted: mockDifficultyAccepted,
				DifficultyRejected: mockDifficultyRejected,
				DifficultyStale:    mockDifficultyStale,
				BestShare:          mockBestShare,
				DeviceHardwarePerc: mockDevicePercentage,
				DeviceRejectedPerc: mockDevicePercentage,
				PoolRejectedPerc:   mockDevicePercentage,
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

	pools := make([]PoolStatus, len(state.Pools))
	for i, pool := range state.Pools {
		pools[i] = PoolStatus{
			URL:        pool.URL,
			User:       pool.User,
			Status:     "Alive",
			Priority:   i,
			GetWorks:   DefaultGetWorks,
			Accepted:   DefaultAccepted,
			Rejected:   DefaultRejected,
			Discarded:  DefaultDiscarded,
			LastShare:  int(now) - DefaultLastShareDelay,
			Difficulty: DefaultDifficulty,
			Diff1Share: DefaultAccepted,
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

	// Create one device according to the example format
	devices := []DeviceInfo{
		{
			ASC:                 mockASCIndex,
			Name:                "BTM_SOC",
			ID:                  mockDeviceID,
			Enabled:             "Y",
			Status:              "Alive",
			Tenperature:         DefaultTemperature,
			MHSav:               mockDevicePercentage,
			MHS5s:               mockDevicePercentage,
			Accepted:            mockAcceptedCount,
			Rejected:            mockRejectedCount,
			HardwareErrors:      mockDiff1Work,
			Utility:             mockDevicePercentage,
			LastSharePool:       mockLastSharePool,
			LastShareTime:       now - deviceTimeOffset,
			TotalMH:             mockDevicePercentage,
			Diff1Work:           mockDiff1Work,
			DifficultyAccepted:  mockDifficultyValue,
			DifficultyRejected:  int(mockDifficultyRejected),
			LastShareDifficulty: now - deviceTimeOffset,
			LastValidWork:       now - deviceTimeOffset,
			DeviceHardwarePerc:  mockDevicePercentage,
			DeviceRejectedPerc:  mockDevicePercentage,
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
