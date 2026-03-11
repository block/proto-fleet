package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleChangePassword_WrongCurrentPassword_Returns401(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetPassword("correctPassword")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/change-password",
		strings.NewReader(`{"current_password":"wrongPassword","new_password":"newPassword123"}`))
	h.handleChangePassword(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}

	if state.GetPassword() != "correctPassword" {
		t.Fatal("password should not have changed")
	}
}

func TestHandleChangePassword_CorrectCurrentPassword_Returns200(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetPassword("correctPassword")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/change-password",
		strings.NewReader(`{"current_password":"correctPassword","new_password":"newPassword123"}`))
	h.handleChangePassword(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if state.GetPassword() != "newPassword123" {
		t.Fatalf("expected password to be updated to %q, got %q", "newPassword123", state.GetPassword())
	}
}

func TestHandleLogin_WrongPassword_Returns401(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetPassword("correctPassword")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"password":"wrongPassword"}`))
	h.handleLogin(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}
}

func TestHandleLogin_CorrectPassword_Returns200(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetPassword("correctPassword")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"password":"correctPassword"}`))
	h.handleLogin(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestHandleLogin_NoPasswordSet_AcceptsAny(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"password":"anything"}`))
	h.handleLogin(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestHandleSetPassword_ValidPassword_StoresPassword(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		strings.NewReader(`{"password":"validPass123"}`))
	h.handleSetPassword(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if state.GetPassword() != "validPass123" {
		t.Fatalf("expected password %q, got %q", "validPass123", state.GetPassword())
	}

	if state.GetAuthKey() == "" {
		t.Fatal("expected auth key to be set")
	}
}

func TestHandleSetPassword_TooShort_Returns400(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		strings.NewReader(`{"password":"short"}`))
	h.handleSetPassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	if state.GetPassword() != "" {
		t.Fatal("password should not have been set")
	}
}

func TestHandleChangePassword_NewPasswordTooShort_Returns400(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetPassword("correctPassword")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/change-password",
		strings.NewReader(`{"current_password":"correctPassword","new_password":"short"}`))
	h.handleChangePassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	if state.GetPassword() != "correctPassword" {
		t.Fatal("password should not have changed")
	}
}

func TestClearAuthKey_AlsoClearsPassword(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAuthKey("some-key")
	state.SetPassword("somePassword")

	state.ClearAuthKey()

	if state.GetAuthKey() != "" {
		t.Fatal("expected auth key to be cleared")
	}
	if state.GetPassword() != "" {
		t.Fatal("expected password to be cleared")
	}
}

func TestHandleTestPoolConnection_InvalidURL_Returns400(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools/test-connection", strings.NewReader(`{"url":"aaa"}`))
	h.handleTestPoolConnection(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	var resp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body=%s", err, rr.Body.String())
	}
	if resp.Error.Message != "Invalid pool URL" {
		t.Fatalf("expected error message %q, got %q", "Invalid pool URL", resp.Error.Message)
	}
}

func TestHandleTestPoolConnection_ValidURL_Returns200(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools/test-connection", strings.NewReader(`{"url":"stratum+tcp://mine.ocean.xyz:3334"}`))
	h.handleTestPoolConnection(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestCreatePools_InvalidURL_DoesNotClearExistingPools(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55") // seed with an existing pool
	state.AddPool(&Pool{Idx: 0, Url: "stratum+tcp://mine.ocean.xyz:3334", Username: "u"})

	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", strings.NewReader(`[{"url":"aaa","username":"u"}]`))
	h.createPools(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	pools := state.GetPools()
	if len(pools) != 1 {
		t.Fatalf("expected existing pools to remain, got %d", len(pools))
	}
	if pools[0].Url != "stratum+tcp://mine.ocean.xyz:3334" {
		t.Fatalf("expected original pool url to remain, got %q", pools[0].Url)
	}
}

func TestHandleMiningTarget_HashOnDisconnectOnly(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	// Only send hash_on_disconnect, no power target or performance mode
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/mining/target",
		strings.NewReader(`{"hash_on_disconnect":true}`))
	h.handleMiningTarget(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp MiningTargetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if !resp.HashOnDisconnect {
		t.Fatal("expected hash_on_disconnect to be true")
	}
	if resp.PowerTargetWatts != defaultPowerTargetW {
		t.Fatalf("expected power target to remain %d, got %d", defaultPowerTargetW, resp.PowerTargetWatts)
	}
	if resp.PerformanceMode != "MaximumHashrate" {
		t.Fatalf("expected performance mode to remain MaximumHashrate, got %s", resp.PerformanceMode)
	}
}

func TestHandleMiningTarget_PerformanceModeEfficiency(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/mining/target",
		strings.NewReader(`{"performance_mode":"Efficiency"}`))
	h.handleMiningTarget(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp MiningTargetResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.PerformanceMode != "Efficiency" {
		t.Fatalf("expected Efficiency, got %s", resp.PerformanceMode)
	}
}

func TestHandleMiningTuning_ValidAlgorithm_PersistsToState(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/mining/tuning",
		strings.NewReader(`{"algorithm":"VoltageImbalanceCompensation"}`))
	h.handleMiningTuning(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp MiningTuningConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Algorithm != "VoltageImbalanceCompensation" {
		t.Fatalf("expected VoltageImbalanceCompensation, got %s", resp.Algorithm)
	}

	state.mu.RLock()
	algo := state.TuningAlgorithmVal
	state.mu.RUnlock()
	if algo != TuningAlgorithmVoltageImbalanceCompensation {
		t.Fatalf("expected state to have VoltageImbalanceCompensation, got %v", algo)
	}
}

func TestHandleMiningTuning_InvalidAlgorithm_Returns422(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/mining/tuning",
		strings.NewReader(`{"algorithm":"InvalidAlgo"}`))
	h.handleMiningTuning(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	}
}

func TestHandleMiningTuning_WrongMethod_Returns405(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mining/tuning", nil)
	h.handleMiningTuning(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusMethodNotAllowed, rr.Code, rr.Body.String())
	}
}

func TestHandleMiningTarget_PowerTargetOutOfRange_Returns422(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/mining/target",
		strings.NewReader(`{"power_target_watts":9999}`))
	h.handleMiningTarget(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	}

	if state.PowerTargetW != defaultPowerTargetW {
		t.Fatalf("expected power target to remain %d, got %d", defaultPowerTargetW, state.PowerTargetW)
	}
}

func TestHandleMiningTarget_NegativePowerTarget_Returns422(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/mining/target",
		strings.NewReader(`{"power_target_watts":-1}`))
	h.handleMiningTarget(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	}

	var resp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp.Error.Message != "power_target_watts must be positive" {
		t.Fatalf("expected positive error message, got %q", resp.Error.Message)
	}

	if state.PowerTargetW != defaultPowerTargetW {
		t.Fatalf("expected power target to remain %d, got %d", defaultPowerTargetW, state.PowerTargetW)
	}
}

func TestHandleMiningTarget_InvalidPerformanceMode_Returns422(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/mining/target",
		strings.NewReader(`{"performance_mode":"Turbo"}`))
	h.handleMiningTarget(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	}

	if state.PerformanceModeVal != PerformanceModeMaxHashrate {
		t.Fatal("expected performance mode to remain MaximumHashrate")
	}
}
