package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

func handleRPCConnection(conn net.Conn, state *MinerState) {
	defer conn.Close()

	// Set a read deadline to prevent hanging connections
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		log.Printf("Failed to set read deadline: %v", err)
		return
	}

	// Read request
	var request RPCRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&request); err != nil {
		log.Printf("Failed to decode request: %v", err)
		return
	}

	log.Printf("Received RPC command: %s", request.Command)

	// Process command and create response
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

	// Send response
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
					Code:   11,
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

	// Calculate some values based on state
	hashRateGHS := state.HashRate * 1000 // Convert TH/s to GH/s

	return SummaryResponse{
		RPCResponse: RPCResponse{
			Status: []StatusInfo{
				{
					Status:      "S",
					When:        now,
					Code:        11,
					Msg:         "Summary",
					Description: "cgminer 1.0.0",
				},
			},
		},
		Summary: []SummaryInfo{
			{
				Elapsed:            DefaultElapsedTime,
				GHS5s:              hashRateGHS - 2000, // Slight variation for 5s
				GHSav:              hashRateGHS,
				GHS30m:             hashRateGHS + 1000, // Slight variation for 30m
				FoundBlocks:        0,
				Getwork:            34537,
				Accepted:           73280,
				Rejected:           75,
				HardwareErrors:     63,
				Utility:            12.79,
				Discarded:          32426300,
				Stale:              44,
				GetFailures:        3,
				LocalWork:          32459750,
				RemoteFailures:     0,
				NetworkBlocks:      571,
				TotalMH:            8.18439346825e13,
				WorkUtility:        3320110.48,
				DifficultyAccepted: 18995326976.0,
				DifficultyRejected: 19333120.0,
				DifficultyStale:    0.0,
				BestShare:          13416269691,
				DeviceHardwarePerc: 0.0,
				DeviceRejectedPerc: 0.0,
				PoolRejectedPerc:   0.0,
				PoolStalePerc:      0.0,
				LastGetwork:        now,
			},
		},
		ID: 1,
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
					Code:   7,
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
			ASC:                 0,
			Name:                "BTM_SOC",
			ID:                  0,
			Enabled:             "Y",
			Status:              "Alive",
			Tenperature:         0.0,
			MHSav:               0.0,
			MHS5s:               0.0,
			Accepted:            73280,
			Rejected:            75,
			HardwareErrors:      0,
			Utility:             0.0,
			LastSharePool:       0,
			LastShareTime:       now - 6,
			TotalMH:             0.0,
			Diff1Work:           0,
			DifficultyAccepted:  18995326976,
			DifficultyRejected:  19333120,
			LastShareDifficulty: now - 6,
			LastValidWork:       now - 6,
			DeviceHardwarePerc:  0.0,
			DeviceRejectedPerc:  0.0,
			DeviceElapsed:       DefaultElapsedTime,
		},
	}

	return DevsResponse{
		RPCResponse: RPCResponse{
			Status: []StatusInfo{
				{
					Status:      "S",
					When:        now,
					Code:        9,
					Msg:         "1 ASC(s)",
					Description: "cgminer 1.0.0",
				},
			},
		},
		Devices: devices,
		ID:      1,
	}
}
