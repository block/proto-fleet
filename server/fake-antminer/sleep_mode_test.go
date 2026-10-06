package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetConfigSleepModePersistsAndZeroesHashrate(t *testing.T) {
	state := &MinerState{
		HashRate:        110,
		Temperature:     45,
		BitmainWorkMode: WorkModeNormal,
		Pools: []Pool{
			{URL: "stratum+tcp://pool.example.com:3333", User: "worker", Pass: "x"},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/cgi-bin/set_miner_conf.cgi", strings.NewReader(`{
		"bitmain-work-mode":"1",
		"pools":[{"url":"stratum+tcp://pool.example.com:4444","user":"worker2","pass":"y"}]
	}`))
	rec := httptest.NewRecorder()

	createSetConfigHandler(state).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	configResp := httptest.NewRecorder()
	createMinerConfigHandler(state).ServeHTTP(configResp, httptest.NewRequest(http.MethodGet, "/cgi-bin/get_miner_conf.cgi", nil))
	if configResp.Code != http.StatusOK {
		t.Fatalf("expected config status %d, got %d", http.StatusOK, configResp.Code)
	}

	var config map[string]any
	if err := json.Unmarshal(configResp.Body.Bytes(), &config); err != nil {
		t.Fatalf("unmarshal config response: %v", err)
	}
	if got := config["bitmain-work-mode"]; got != WorkModeSleep {
		t.Fatalf("expected bitmain-work-mode %q, got %#v", WorkModeSleep, got)
	}

	summaryResp := httptest.NewRecorder()
	createMinerSummaryHandler(state).ServeHTTP(summaryResp, httptest.NewRequest(http.MethodGet, "/cgi-bin/summary.cgi", nil))
	if summaryResp.Code != http.StatusOK {
		t.Fatalf("expected summary status %d, got %d", http.StatusOK, summaryResp.Code)
	}

	var summary struct {
		Summary []struct {
			Rate5s  float64 `json:"rate_5s"`
			Rate30m float64 `json:"rate_30m"`
			RateAvg float64 `json:"rate_avg"`
			Status  []struct {
				Status string `json:"status"`
			} `json:"status"`
		} `json:"SUMMARY"`
	}
	if err := json.Unmarshal(summaryResp.Body.Bytes(), &summary); err != nil {
		t.Fatalf("unmarshal summary response: %v", err)
	}
	if len(summary.Summary) != 1 {
		t.Fatalf("expected 1 summary entry, got %d", len(summary.Summary))
	}
	if got := summary.Summary[0].Rate5s; got != 0 {
		t.Fatalf("expected summary rate_5s 0, got %v", got)
	}
	if got := summary.Summary[0].Rate30m; got != 0 {
		t.Fatalf("expected summary rate_30m 0, got %v", got)
	}
	if got := summary.Summary[0].RateAvg; got != 0 {
		t.Fatalf("expected summary rate_avg 0, got %v", got)
	}
	if len(summary.Summary[0].Status) != 1 {
		t.Fatalf("expected 1 summary status entry, got %d", len(summary.Summary[0].Status))
	}
	if got := summary.Summary[0].Status[0].Status; got != "sleeping" {
		t.Fatalf("expected summary status %q, got %q", "sleeping", got)
	}

	statsResp := httptest.NewRecorder()
	createStatsHandler(state).ServeHTTP(statsResp, httptest.NewRequest(http.MethodGet, "/cgi-bin/stats.cgi", nil))
	if statsResp.Code != http.StatusOK {
		t.Fatalf("expected stats status %d, got %d", http.StatusOK, statsResp.Code)
	}

	var stats struct {
		Stats []struct {
			Rate5s     float64 `json:"rate_5s"`
			ChainPower string  `json:"chain_power"`
			Chain      []struct {
				RateReal float64 `json:"rate_real"`
			} `json:"chain"`
		} `json:"STATS"`
	}
	if err := json.Unmarshal(statsResp.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal stats response: %v", err)
	}
	if len(stats.Stats) != 1 {
		t.Fatalf("expected 1 stats entry, got %d", len(stats.Stats))
	}
	if got := stats.Stats[0].Rate5s; got != 0 {
		t.Fatalf("expected stats rate_5s 0, got %v", got)
	}
	if got := stats.Stats[0].ChainPower; got != "30 W" {
		t.Fatalf("expected stats chain_power %q, got %q", "30 W", got)
	}
	for _, chain := range stats.Stats[0].Chain {
		if chain.RateReal != 0 {
			t.Fatalf("expected chain rate_real 0, got %v", chain.RateReal)
		}
	}

	rpcSummary := generateSummaryResponse(state)
	if len(rpcSummary.Summary) != 1 {
		t.Fatalf("expected 1 RPC summary entry, got %d", len(rpcSummary.Summary))
	}
	if got := rpcSummary.Summary[0].GHS5s; got != 0 {
		t.Fatalf("expected RPC GHS 5s 0, got %v", got)
	}
	if got := rpcSummary.Summary[0].GHSav; got != 0 {
		t.Fatalf("expected RPC GHS av 0, got %v", got)
	}
	if got := rpcSummary.Summary[0].GHS30m; got != 0 {
		t.Fatalf("expected RPC GHS 30m 0, got %v", got)
	}

	rpcDevs := generateDevsResponse(state)
	if len(rpcDevs.Devices) != 1 {
		t.Fatalf("expected 1 RPC dev entry, got %d", len(rpcDevs.Devices))
	}
	if got := rpcDevs.Devices[0].MHS5s; got != 0 {
		t.Fatalf("expected RPC MHS 5s 0, got %v", got)
	}
	if got := rpcDevs.Devices[0].MHSav; got != 0 {
		t.Fatalf("expected RPC MHS av 0, got %v", got)
	}

	rpcStats := generateStatsResponse(state)
	if len(rpcStats.Stats) != 2 {
		t.Fatalf("expected 2 RPC stats entries, got %d", len(rpcStats.Stats))
	}
	if got := rpcStats.Stats[1]["chain_power"]; got != "30 W" {
		t.Fatalf("expected RPC chain_power %q, got %#v", "30 W", got)
	}
}

func TestRPCStatsReportsMiningPower(t *testing.T) {
	state := &MinerState{
		MinerType:       "Antminer S19j Pro",
		HashRate:        110,
		BitmainWorkMode: WorkModeNormal,
	}

	response := requestRPCStats(t, state)
	if len(response.Stats) != 2 {
		t.Fatalf("expected firmware and mining stats entries, got %d", len(response.Stats))
	}
	if got := response.Stats[0]["Type"]; got != state.MinerType {
		t.Fatalf("expected firmware type %q, got %#v", state.MinerType, got)
	}
	if got := response.Stats[1]["chain_power"]; got != "3250 W" {
		t.Fatalf("expected RPC chain_power %q, got %#v", "3250 W", got)
	}
}

func requestRPCStats(t *testing.T, state *MinerState) StatsResponse {
	t.Helper()

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	go handleRPCConnection(serverConn, state)

	if err := json.NewEncoder(clientConn).Encode(RPCRequest{Command: "stats"}); err != nil {
		t.Fatalf("encode RPC stats request: %v", err)
	}

	var response StatsResponse
	if err := json.NewDecoder(clientConn).Decode(&response); err != nil {
		t.Fatalf("decode RPC stats response: %v", err)
	}
	return response
}

func TestSetConfigLegacySleepModePersists(t *testing.T) {
	state := &MinerState{
		HashRate:    110,
		Temperature: 45,
		MinerMode:   WorkModeNormal,
	}

	req := httptest.NewRequest(http.MethodPost, "/cgi-bin/set_miner_conf.cgi", strings.NewReader(`{
		"miner-mode":"1"
	}`))
	rec := httptest.NewRecorder()

	createSetConfigHandler(state).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	configResp := httptest.NewRecorder()
	createMinerConfigHandler(state).ServeHTTP(configResp, httptest.NewRequest(http.MethodGet, "/cgi-bin/get_miner_conf.cgi", nil))
	if configResp.Code != http.StatusOK {
		t.Fatalf("expected config status %d, got %d", http.StatusOK, configResp.Code)
	}

	var config map[string]any
	if err := json.Unmarshal(configResp.Body.Bytes(), &config); err != nil {
		t.Fatalf("unmarshal config response: %v", err)
	}
	if got := config["miner-mode"]; got != WorkModeSleep {
		t.Fatalf("expected miner-mode %q, got %#v", WorkModeSleep, got)
	}

	rpcSummary := generateSummaryResponse(state)
	if len(rpcSummary.Summary) != 1 {
		t.Fatalf("expected 1 RPC summary entry, got %d", len(rpcSummary.Summary))
	}
	if got := rpcSummary.Summary[0].GHS5s; got != 0 {
		t.Fatalf("expected RPC GHS 5s 0, got %v", got)
	}
}
