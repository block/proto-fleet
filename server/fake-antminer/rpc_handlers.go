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
	case "stats":
		response = generateStatsResponse(state)
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

func generateStatsResponse(state *MinerState) StatsResponse {
	state.mu.RLock()
	defer state.mu.RUnlock()

	now := time.Now().Unix()
	return StatsResponse{
		RPCResponse: RPCResponse{
			Status: []StatusInfo{
				{
					Status: "S",
					When:   now,
					Code:   70,
					Msg:    "CGMiner stats",
				},
			},
		},
		Stats: []struct {
			BMMiner     string  `json:"BMMiner"`
			Miner       string  `json:"Miner"`
			CompileTime string  `json:"CompileTime"`
			Type        string  `json:"Type"`
			Stats       float64 `json:"stats"`
			ID          string  `json:"ID"`
			Elapsed     int     `json:"Elapsed"`
			Calls       int     `json:"Calls"`
			Wait        float64 `json:"Wait"`
			Max         float64 `json:"Max"`
			Min         float64 `json:"Min"`
		}{
			{
				BMMiner:     "2.0.0",
				Miner:       state.MinerType,
				CompileTime: "2023-05-01",
				Type:        "Antminer " + state.MinerType,
				Stats:       0,
				ID:          "BTM",
				Elapsed:     DefaultElapsedTime,
				Calls:       0,
				Wait:        0,
				Max:         0,
				Min:         999999,
			},
		},
	}
}

func generateSummaryResponse(state *MinerState) SummaryResponse {
	state.mu.RLock()
	defer state.mu.RUnlock()

	now := time.Now().Unix()
	return SummaryResponse{
		RPCResponse: RPCResponse{
			Status: []StatusInfo{
				{
					Status: "S",
					When:   now,
					Code:   11,
					Msg:    "Summary",
				},
			},
		},
		Summary: []struct {
			Elapsed   int     `json:"elapsed"`
			Rate5s    float64 `json:"rate_5s"`
			Rate30m   float64 `json:"rate_30m"`
			RateAvg   float64 `json:"rate_avg"`
			RateIdeal float64 `json:"rate_ideal"`
			RateUnit  string  `json:"rate_unit"`
			HwAll     int     `json:"hw_all"`
			BestShare int64   `json:"bestshare"`
		}{
			{
				Elapsed:   DefaultElapsedTime,
				Rate5s:    state.HashRate,
				Rate30m:   state.HashRate,
				RateAvg:   state.HashRate,
				RateIdeal: state.HashRate,
				RateUnit:  DefaultHashRateUnit,
				HwAll:     0,
				BestShare: DefaultBestShare,
			},
		},
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
	
	// Create 3 devices with slightly different temperatures and stats
	devices := []DeviceInfo{
		{
			ASC:      0,
			Name:     "ASC0",
			ID:       0,
			Enabled:  "Y",
			Status:   "Alive",
			Temp:     state.Temperature,
			MHS5s:    state.HashRate * 1000 * 1000 / 3,
			MHS30m:   state.HashRate * 1000 * 1000 / 3,
			MHSav:    state.HashRate * 1000 * 1000 / 3,
			Accepted: DefaultAccepted,
			Rejected: DefaultRejected,
		},
		{
			ASC:      1,
			Name:     "ASC1",
			ID:       1,
			Enabled:  "Y",
			Status:   "Alive",
			Temp:     state.Temperature + 1.5,
			MHS5s:    state.HashRate * 1000 * 1000 / 3,
			MHS30m:   state.HashRate * 1000 * 1000 / 3,
			MHSav:    state.HashRate * 1000 * 1000 / 3,
			Accepted: DefaultAccepted + 1,
			Rejected: DefaultRejected,
		},
		{
			ASC:      2,
			Name:     "ASC2",
			ID:       2,
			Enabled:  "Y",
			Status:   "Alive",
			Temp:     state.Temperature + 0.5,
			MHS5s:    state.HashRate * 1000 * 1000 / 3,
			MHS30m:   state.HashRate * 1000 * 1000 / 3,
			MHSav:    state.HashRate * 1000 * 1000 / 3,
			Accepted: DefaultAccepted + 2,
			Rejected: DefaultRejected,
		},
	}

	return DevsResponse{
		RPCResponse: RPCResponse{
			Status: []StatusInfo{
				{
					Status: "S",
					When:   now,
					Code:   9,
					Msg:    "3 ASC(s)",
				},
			},
		},
		Devices: devices,
	}
}
