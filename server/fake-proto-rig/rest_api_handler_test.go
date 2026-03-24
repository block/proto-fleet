package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewMinerState_DefaultModelIsRig(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")

	if state.Model != "Rig" {
		t.Fatalf("expected default model %q, got %q", "Rig", state.Model)
	}
}

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

func TestHandleRefresh_ValidRefreshToken_Returns200(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	loginRR := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"password":"anything"}`))
	h.handleLogin(loginRR, loginReq)

	if loginRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, loginRR.Code, loginRR.Body.String())
	}

	var initialTokens AuthTokens
	if err := json.Unmarshal(loginRR.Body.Bytes(), &initialTokens); err != nil {
		t.Fatalf("failed to unmarshal auth tokens: %v; body=%s", err, loginRR.Body.String())
	}

	refreshRR := httptest.NewRecorder()
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
		strings.NewReader(fmt.Sprintf(`{"refresh_token":%q}`, initialTokens.RefreshToken)))
	h.handleRefresh(refreshRR, refreshReq)

	if refreshRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, refreshRR.Code, refreshRR.Body.String())
	}

	var refreshedTokens AuthTokens
	if err := json.Unmarshal(refreshRR.Body.Bytes(), &refreshedTokens); err != nil {
		t.Fatalf("failed to unmarshal auth tokens: %v; body=%s", err, refreshRR.Body.String())
	}
	if refreshedTokens.AccessToken == "" || refreshedTokens.RefreshToken == "" {
		t.Fatalf("expected rotated tokens, got %+v", refreshedTokens)
	}
	if refreshedTokens.RefreshToken == initialTokens.RefreshToken {
		t.Fatal("expected refresh token to rotate")
	}
	if state.GetAccessToken() != refreshedTokens.AccessToken {
		t.Fatal("expected access token state to match refreshed token")
	}
	if state.GetRefreshToken() != refreshedTokens.RefreshToken {
		t.Fatal("expected refresh token state to match refreshed token")
	}
}

func TestHandleRefresh_InvalidRefreshToken_Returns401(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetRefreshToken("valid-refresh-token")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
		strings.NewReader(`{"refresh_token":"bogus-refresh-token"}`))
	h.handleRefresh(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}
}

func TestProtectedRouteRequiresBearerToken(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mining/start", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}
}

func TestProtectedRouteAcceptsIssuedBearerToken(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetMiningState(MiningStateStopped)
	state.AddPool(&Pool{Idx: 0, Url: "stratum+tcp://pool.example.com:3333", Username: "worker"})
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"anything"}`))
	loginRR := httptest.NewRecorder()
	mux.ServeHTTP(loginRR, loginReq)

	if loginRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, loginRR.Code, loginRR.Body.String())
	}

	var tokens AuthTokens
	if err := json.Unmarshal(loginRR.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("failed to unmarshal auth tokens: %v; body=%s", err, loginRR.Body.String())
	}
	if tokens.AccessToken == "" {
		t.Fatal("expected access token to be set")
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mining/start", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
	if state.GetMiningState() != MiningStateMining {
		t.Fatalf("expected mining state %q, got %q", MiningStateMining, state.GetMiningState())
	}
}

func TestLogoutInvalidatesIssuedBearerToken(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetMiningState(MiningStateStopped)
	state.AddPool(&Pool{Idx: 0, Url: "stratum+tcp://pool.example.com:3333", Username: "worker"})
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"anything"}`))
	loginRR := httptest.NewRecorder()
	mux.ServeHTTP(loginRR, loginReq)

	if loginRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, loginRR.Code, loginRR.Body.String())
	}

	var tokens AuthTokens
	if err := json.Unmarshal(loginRR.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("failed to unmarshal auth tokens: %v; body=%s", err, loginRR.Body.String())
	}
	if tokens.AccessToken == "" {
		t.Fatal("expected access token to be set")
	}

	protectedReq := httptest.NewRequest(http.MethodPost, "/api/v1/mining/start", nil)
	protectedReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	protectedRR := httptest.NewRecorder()
	mux.ServeHTTP(protectedRR, protectedReq)

	if protectedRR.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, protectedRR.Code, protectedRR.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader("{}"))
	logoutReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	logoutRR := httptest.NewRecorder()
	mux.ServeHTTP(logoutRR, logoutReq)

	if logoutRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, logoutRR.Code, logoutRR.Body.String())
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/api/v1/mining/stop", nil)
	retryReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	retryRR := httptest.NewRecorder()
	mux.ServeHTTP(retryRR, retryReq)

	if retryRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, retryRR.Code, retryRR.Body.String())
	}
}

func TestProtectedRouteAcceptsPairedJWT(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetMiningState(MiningStateStopped)

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("failed to marshal public key: %v", err)
	}
	state.SetAuthKey(base64.StdEncoding.EncodeToString(publicKeyDER))

	jwtToken, err := signTestJWT(privateKey, state.SerialNumber, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("failed to sign jwt: %v", err)
	}

	h := NewRESTApiHandler(state)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mining/start", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
}

func signTestJWT(privateKey ed25519.PrivateKey, serialNumber string, exp time.Time) (string, error) {
	headerJSON := []byte(`{"alg":"EdDSA","typ":"JWT"}`)
	payloadJSON := []byte(fmt.Sprintf(`{"miner_sn":%q,"iat":%d,"exp":%d}`, serialNumber, time.Now().Unix(), exp.Unix()))

	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := header + "." + payload
	signature := ed25519.Sign(privateKey, []byte(signingInput))

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func TestHandleSetPassword_ValidPassword_StoresPassword(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAuthKey("existing-auth-key")
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

	if state.GetAuthKey() != "existing-auth-key" {
		t.Fatalf("expected auth key to remain unchanged, got %q", state.GetAuthKey())
	}
}

func TestHandleSystemStatus_PasswordSetUsesPasswordState(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAuthKey("existing-auth-key")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	h.handleSystemStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp SystemStatuses
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.PasswordSet {
		t.Fatal("expected password_set to be false when only auth key is configured")
	}

	state.SetPassword("validPass123")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	h.handleSystemStatus(rr, req)

	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !resp.PasswordSet {
		t.Fatal("expected password_set to be true when password is configured")
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
	state.SetAccessToken("access-token")
	state.SetRefreshToken("refresh-token")

	state.ClearAuthKey()

	if state.GetAuthKey() != "" {
		t.Fatal("expected auth key to be cleared")
	}
	if state.GetPassword() != "" {
		t.Fatal("expected password to be cleared")
	}
	if state.GetAccessToken() != "" {
		t.Fatal("expected access token to be cleared")
	}
	if state.GetRefreshToken() != "" {
		t.Fatal("expected refresh token to be cleared")
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

func TestTestPoolConnectionRoute_DoesNotRequireBearerAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools/test-connection",
		strings.NewReader(`{"url":"stratum+tcp://mine.ocean.xyz:3334","username":"worker"}`))
	mux.ServeHTTP(rr, req)

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

func TestCreatePools_PersistsConfiguredPriorities(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", strings.NewReader(`[
		{"url":"stratum+tcp://pool-a.example.com:3333","username":"worker-a","priority":2},
		{"url":"stratum+tcp://pool-b.example.com:3333","username":"worker-b","priority":0},
		{"url":"stratum+tcp://pool-c.example.com:3333","username":"worker-c","priority":1}
	]`))
	h.createPools(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	pools := state.GetPools()
	if len(pools) != 3 {
		t.Fatalf("expected 3 pools, got %d", len(pools))
	}
	if pools[0].Priority != 2 || pools[1].Priority != 0 || pools[2].Priority != 1 {
		t.Fatalf("expected priorities [2 0 1], got [%d %d %d]", pools[0].Priority, pools[1].Priority, pools[2].Priority)
	}

	getRR := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	h.getPools(getRR, getReq)

	if getRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, getRR.Code, getRR.Body.String())
	}

	var resp PoolsList
	if err := json.Unmarshal(getRR.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body=%s", err, getRR.Body.String())
	}

	if len(resp.Pools) != 3 {
		t.Fatalf("expected 3 pools in response, got %d", len(resp.Pools))
	}
	if resp.Pools[0].Priority != 2 || resp.Pools[1].Priority != 0 || resp.Pools[2].Priority != 1 {
		t.Fatalf("expected response priorities [2 0 1], got [%d %d %d]", resp.Pools[0].Priority, resp.Pools[1].Priority, resp.Pools[2].Priority)
	}
}

func TestUpdatePool_PersistsPriorityAndSerializesIt(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.AddPool(&Pool{Idx: 0, Priority: 0, Url: "stratum+tcp://pool.example.com:3333", Username: "worker"})
	h := NewRESTApiHandler(state)

	updateRR := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/pools/0", strings.NewReader(`{"priority":2}`))
	h.updatePool(updateRR, updateReq, 0)

	if updateRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, updateRR.Code, updateRR.Body.String())
	}

	pools := state.GetPools()
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}
	if pools[0].Priority != 2 {
		t.Fatalf("expected pool priority to be updated to 2, got %d", pools[0].Priority)
	}

	getRR := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/pools/0", nil)
	h.getPool(getRR, getReq, 0)

	if getRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, getRR.Code, getRR.Body.String())
	}

	var resp PoolResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body=%s", err, getRR.Body.String())
	}
	if resp.Pool.Priority != 2 {
		t.Fatalf("expected serialized pool priority 2, got %d", resp.Pool.Priority)
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

// --- Pairing endpoint tests ---

func TestHandlePairingInfo_GET_ReturnsMACAndCBSN(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pairing/info", nil)
	h.handlePairingInfo(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp PairingInfoResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp.MAC != "00:11:22:33:44:55" {
		t.Fatalf("expected MAC %q, got %q", "00:11:22:33:44:55", resp.MAC)
	}
	if resp.CBSN != "SN12345678" {
		t.Fatalf("expected CBSN %q, got %q", "SN12345678", resp.CBSN)
	}
}

func TestHandlePairingInfo_WrongMethod_Returns405(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/info", nil)
	h.handlePairingInfo(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusMethodNotAllowed, rr.Code, rr.Body.String())
	}
}

func TestHandlePairingAuthKey_POST_SetsKey(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/auth-key",
		strings.NewReader(`{"public_key":"test-key-123"}`))
	h.handlePairingAuthKey(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "test-key-123" {
		t.Fatalf("expected auth key %q, got %q", "test-key-123", state.GetAuthKey())
	}
}

func TestHandlePairingAuthKey_POST_MissingPublicKey_Returns400(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/auth-key",
		strings.NewReader(`{"public_key":""}`))
	h.handlePairingAuthKey(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "" {
		t.Fatal("auth key should not have been set")
	}
}

func TestHandlePairingAuthKey_DELETE_ClearsKey(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAuthKey("existing-key")
	state.SetPassword("somePassword")
	state.SetAccessToken("mock-token")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pairing/auth-key", nil)
	req.Header.Set("Authorization", "Bearer mock-token")
	h.handlePairingAuthKey(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "" {
		t.Fatal("expected auth key to be cleared")
	}
	if state.GetPassword() != "" {
		t.Fatal("expected password to be cleared")
	}
}

func TestHandlePairingAuthKey_POST_RotationRequiresAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAuthKey("existing-key")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/auth-key",
		strings.NewReader(`{"public_key":"new-key"}`))
	h.handlePairingAuthKey(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "existing-key" {
		t.Fatal("auth key should not have changed without auth")
	}
}

func TestHandlePairingAuthKey_POST_RotationRejectsInvalidBearer(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAuthKey("existing-key")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/auth-key",
		strings.NewReader(`{"public_key":"new-key"}`))
	req.Header.Set("Authorization", "Bearer bogus-token")
	h.handlePairingAuthKey(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "existing-key" {
		t.Fatal("auth key should not have changed with invalid auth")
	}
}

func TestHandlePairingAuthKey_POST_RotationAcceptsIssuedBearerToken(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAuthKey("existing-key")
	state.SetAccessToken("valid-token")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/auth-key",
		strings.NewReader(`{"public_key":"new-key"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	h.handlePairingAuthKey(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "new-key" {
		t.Fatalf("expected auth key %q, got %q", "new-key", state.GetAuthKey())
	}
}

func TestHandlePairingAuthKey_POST_RotationAcceptsPairedJWT(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("failed to marshal public key: %v", err)
	}
	state.SetAuthKey(base64.StdEncoding.EncodeToString(publicKeyDER))

	h := NewRESTApiHandler(state)
	jwtToken, err := signTestJWT(privateKey, state.SerialNumber, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("failed to sign jwt: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/auth-key",
		strings.NewReader(`{"public_key":"new-key"}`))
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	h.handlePairingAuthKey(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "new-key" {
		t.Fatalf("expected auth key %q, got %q", "new-key", state.GetAuthKey())
	}
}

func TestHandlePairingAuthKey_DELETE_RequiresAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAuthKey("existing-key")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pairing/auth-key", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "existing-key" {
		t.Fatal("auth key should not have been cleared without auth")
	}
}

func TestHandlePairingAuthKey_DELETE_RejectsInvalidBearer(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAuthKey("existing-key")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pairing/auth-key", nil)
	req.Header.Set("Authorization", "Bearer bogus-token")
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "existing-key" {
		t.Fatal("auth key should not have been cleared with invalid auth")
	}
}

func TestHandleLocate_EmptyBodyIsIdempotent(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetLocateActive(true)
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time=30", nil)
	h.handleLocate(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
	if !state.LocateActive {
		t.Fatal("expected locate mode to remain active")
	}
}

func TestHandleLocate_InvalidLedOnTime_Returns400(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time=abc", nil)
	h.handleLocate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
	if state.LocateActive {
		t.Fatal("expected locate mode to remain inactive on invalid input")
	}
}

func TestHandleMining_UsesCanonicalStateStrings(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetMiningState(MiningStateUnknown)
	state.AddPool(&Pool{Idx: 0, Url: "stratum+tcp://pool.example.com:3333", Username: "worker"})
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mining", nil)
	h.handleMining(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp MiningStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.MiningStatus.Status != string(MiningStateUnknown) {
		t.Fatalf("expected status %q, got %q", MiningStateUnknown, resp.MiningStatus.Status)
	}
}

func TestHandleErrors_ReturnsSpecShape(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/errors", nil)
	h.handleErrors(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "[]\n" {
		t.Fatalf("expected spec-shaped empty errors response, got %q", got)
	}
}

// --- Cooling endpoint tests ---

func TestHandleCooling_GET_AutoMode_IncludesTargetTemp(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	targetTemp := 55.0
	state.SetCoolingMode(CoolingModeAuto, nil, &targetTemp)
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cooling", nil)
	h.handleCooling(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp CoolingStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.CoolingStatus.FanMode != "Auto" {
		t.Fatalf("expected fan_mode %q, got %q", "Auto", resp.CoolingStatus.FanMode)
	}
	if resp.CoolingStatus.TargetTempC == nil {
		t.Fatal("expected target_temperature_c to be present in Auto mode")
	}
	if *resp.CoolingStatus.TargetTempC != 55.0 {
		t.Fatalf("expected target_temperature_c 55.0, got %f", *resp.CoolingStatus.TargetTempC)
	}
	if resp.CoolingStatus.SpeedPercentage != int(defaultFanSpeedPct) {
		t.Fatalf("expected speed_percentage %d, got %d", defaultFanSpeedPct, resp.CoolingStatus.SpeedPercentage)
	}
}

func TestHandleCooling_GET_ManualMode_OmitsTargetTemp(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	speed := uint32(80)
	state.SetCoolingMode(CoolingModeManual, &speed, nil)
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cooling", nil)
	h.handleCooling(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp CoolingStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.CoolingStatus.FanMode != "Manual" {
		t.Fatalf("expected fan_mode %q, got %q", "Manual", resp.CoolingStatus.FanMode)
	}
	if resp.CoolingStatus.SpeedPercentage != int(speed) {
		t.Fatalf("expected speed_percentage %d, got %d", speed, resp.CoolingStatus.SpeedPercentage)
	}
	if resp.CoolingStatus.TargetTempC != nil {
		t.Fatalf("expected target_temperature_c to be omitted in Manual mode, got %v", *resp.CoolingStatus.TargetTempC)
	}
}

func TestHandleCooling_PUT_AutoMode_SetsTargetTemp(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cooling",
		strings.NewReader(`{"mode":"Auto","target_temperature_c":60.5}`))
	h.handleCooling(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	state.mu.RLock()
	targetTemp := state.TargetTempC
	speed := state.FanSpeedPct
	mode := state.CoolingModeVal
	state.mu.RUnlock()

	if mode != CoolingModeAuto {
		t.Fatalf("expected Auto mode, got %v", mode)
	}
	if targetTemp != 60.5 {
		t.Fatalf("expected target temp 60.5, got %f", targetTemp)
	}
	if speed != defaultFanSpeedPct {
		t.Fatalf("expected speed to remain %d, got %d", defaultFanSpeedPct, speed)
	}
}

func TestHandleCooling_PUT_ManualMode_IgnoresTargetTemp(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	state.mu.RLock()
	originalTemp := state.TargetTempC
	state.mu.RUnlock()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cooling",
		strings.NewReader(`{"mode":"Manual","speed_percentage":75,"target_temperature_c":99.9}`))
	h.handleCooling(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	state.mu.RLock()
	targetTemp := state.TargetTempC
	speed := state.FanSpeedPct
	mode := state.CoolingModeVal
	state.mu.RUnlock()

	if mode != CoolingModeManual {
		t.Fatalf("expected Manual mode, got %v", mode)
	}
	if targetTemp != originalTemp {
		t.Fatalf("expected target temp to remain %f in Manual mode, got %f", originalTemp, targetTemp)
	}
	if speed != 75 {
		t.Fatalf("expected speed to be updated to 75 in Manual mode, got %d", speed)
	}
}

// --- ASIC id field tests ---

func TestHandleHashboardASIC_ID_Format(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	tests := []struct {
		asicID     int
		expectedID string
	}{
		{0, "A0"},
		{1, "A1"},
		{9, "A9"},
		{10, "B0"},
		{13, "B3"},
		{20, "C0"},
		{35, "D5"},
	}

	for _, tc := range tests {
		rr := httptest.NewRecorder()
		path := fmt.Sprintf("/api/v1/hashboards/HB-SN12345678-0/%d", tc.asicID)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.handleHashboardByID(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("ASIC %d: expected %d, got %d; body=%s", tc.asicID, http.StatusOK, rr.Code, rr.Body.String())
		}

		var resp map[string]ASICStats
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("ASIC %d: failed to unmarshal: %v", tc.asicID, err)
		}

		asic, ok := resp["asic-stats"]
		if !ok {
			t.Fatalf("ASIC %d: missing asic-stats key in response", tc.asicID)
		}
		if asic.ID != tc.expectedID {
			t.Fatalf("ASIC %d: expected id %q, got %q", tc.asicID, tc.expectedID, asic.ID)
		}
		if asic.Row != tc.asicID/10 {
			t.Fatalf("ASIC %d: expected row %d, got %d", tc.asicID, tc.asicID/10, asic.Row)
		}
		if asic.Column != tc.asicID%10 {
			t.Fatalf("ASIC %d: expected column %d, got %d", tc.asicID, tc.asicID%10, asic.Column)
		}
	}
}
