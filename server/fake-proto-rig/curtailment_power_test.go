package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMinerTelemetryPowerStates(t *testing.T) {
	tests := []struct {
		name       string
		state      MiningState
		withPool   bool
		wantState  MiningState
		wantPowerW float64
	}{
		{
			name:       "mining",
			state:      MiningStateMining,
			withPool:   true,
			wantState:  MiningStateMining,
			wantPowerW: defaultPowerW,
		},
		{
			name:       "stopped idle",
			state:      MiningStateStopped,
			withPool:   true,
			wantState:  MiningStateStopped,
			wantPowerW: defaultIdlePowerW,
		},
		{
			name:       "poolless idle",
			state:      MiningStateMining,
			wantState:  MiningStateNoPools,
			wantPowerW: defaultIdlePowerW,
		},
		{
			name:       "poolless curtailed",
			state:      MiningStateCurtailed,
			wantState:  MiningStateCurtailed,
			wantPowerW: defaultCurtailPowerW,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newPowerTestMinerState(tt.state, tt.withPool)
			assertPowerState(t, state, tt.wantState, tt.wantPowerW)
		})
	}
}

func TestMiningCommandsCurtailAndRestorePowerState(t *testing.T) {
	tests := []struct {
		name         string
		initialState MiningState
		withPool     bool
		wantRestored MiningState
		wantPowerW   float64
	}{
		{
			name:         "mining device",
			initialState: MiningStateMining,
			withPool:     true,
			wantRestored: MiningStateMining,
			wantPowerW:   defaultPowerW,
		},
		{
			name:         "intentionally stopped device",
			initialState: MiningStateStopped,
			withPool:     true,
			wantRestored: MiningStateStopped,
			wantPowerW:   defaultIdlePowerW,
		},
		{
			name:         "poolless device",
			initialState: MiningStateMining,
			wantRestored: MiningStateNoPools,
			wantPowerW:   defaultIdlePowerW,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newPowerTestMinerState(tt.initialState, tt.withPool)
			handler := NewRESTApiHandler(state)

			curtailReq := httptest.NewRequest(http.MethodPost, "/api/v1/mining/stop", nil)
			curtailReq.Header.Set("X-Proto-Fleet-Curtailment", "full")
			curtailResp := httptest.NewRecorder()
			handler.handleMiningStop(curtailResp, curtailReq)

			if curtailResp.Code != http.StatusAccepted {
				t.Fatalf("expected curtail status %d, got %d", http.StatusAccepted, curtailResp.Code)
			}
			assertPowerState(t, state, MiningStateCurtailed, defaultCurtailPowerW)

			restorePath := "/api/v1/mining/stop"
			restoreHandler := handler.handleMiningStop
			if tt.initialState == MiningStateMining && tt.withPool {
				restorePath = "/api/v1/mining/start"
				restoreHandler = handler.handleMiningStart
			}
			restoreResp := httptest.NewRecorder()
			restoreHandler(restoreResp, httptest.NewRequest(http.MethodPost, restorePath, nil))

			if restoreResp.Code != http.StatusAccepted {
				t.Fatalf("expected restore status %d, got %d", http.StatusAccepted, restoreResp.Code)
			}
			assertPowerState(t, state, tt.wantRestored, tt.wantPowerW)
		})
	}
}

func newPowerTestMinerState(miningState MiningState, withPool bool) *MinerState {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetMiningState(miningState)
	if withPool {
		state.AddPool(&Pool{Idx: 0, Url: "stratum+tcp://pool.example:3333", Username: "worker"})
	}
	return state
}

func assertPowerState(t *testing.T, state *MinerState, wantState MiningState, wantPower float64) {
	t.Helper()

	if got := state.GetMiningState(); got != wantState {
		t.Fatalf("expected mining state %q, got %q", wantState, got)
	}
	hashrate, _, power, _ := state.GetMinerTelemetry()
	minPower := wantPower * (1 - telemetryVariation)
	maxPower := wantPower * (1 + telemetryVariation)
	if power < minPower || power > maxPower {
		t.Fatalf("expected power in [%v, %v], got %v", minPower, maxPower, power)
	}
	if wantState == MiningStateMining && hashrate <= 0 {
		t.Fatalf("expected positive hashrate, got %v", hashrate)
	}
	if wantState != MiningStateMining && hashrate != 0 {
		t.Fatalf("expected zero hashrate, got %v", hashrate)
	}
}
