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

func TestConfigureStartupAuthState_SeedsDefaultPasswordBaseline(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	configureStartupAuthState(state)

	if got := state.GetPassword(); got != "" {
		t.Fatalf("expected startup password to remain unset when env is absent, got %q", got)
	}
	if state.IsDefaultPasswordActive() {
		t.Fatal("expected default password to remain inactive when env is absent")
	}

	t.Setenv("FAKE_RIG_DEFAULT_PASSWORD", "root19")
	configureStartupAuthState(state)

	if got := state.GetPassword(); got != "root19" {
		t.Fatalf("expected startup password %q, got %q", "root19", got)
	}
	if !state.IsDefaultPasswordActive() {
		t.Fatal("expected startup state to report default password active")
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
	state.SetAccessToken("old-access-token")
	state.SetRefreshToken("old-refresh-token")
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

	if state.GetAccessToken() != "" {
		t.Fatalf("expected access token to be revoked after password change, got %q", state.GetAccessToken())
	}
	if state.GetRefreshToken() != "" {
		t.Fatalf("expected refresh token to be revoked after password change, got %q", state.GetRefreshToken())
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

	var refreshed RefreshResponse
	if err := json.Unmarshal(refreshRR.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("failed to unmarshal refresh response: %v; body=%s", err, refreshRR.Body.String())
	}
	if refreshed.AccessToken == "" {
		t.Fatalf("expected non-empty access token, got %+v", refreshed)
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

func TestPoolsExemptFromDefaultPasswordGate(t *testing.T) {
	// Firmware's DEFAULT_PASSWORD_EXEMPT_PREFIXES includes /api/v1/pools so
	// Fleet can provision pool settings before the operator changes the
	// factory password. Authenticated pool requests must succeed even while
	// default_password_active is true.
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
	state.AddPool(&Pool{Idx: 0, Url: "stratum+tcp://pool.example.com:3333", Username: "worker"})
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"defaultPass123"}`))
	loginRR := httptest.NewRecorder()
	mux.ServeHTTP(loginRR, loginReq)

	if loginRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, loginRR.Code, loginRR.Body.String())
	}

	var tokens AuthTokens
	if err := json.Unmarshal(loginRR.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("failed to unmarshal auth tokens: %v; body=%s", err, loginRR.Body.String())
	}

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list pools", method: http.MethodGet, path: "/api/v1/pools"},
		{name: "read pool by id", method: http.MethodGet, path: "/api/v1/pools/0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
			mux.ServeHTTP(rr, req)

			if rr.Code == http.StatusForbidden {
				t.Fatalf("expected pools to be exempt from default-password gate, got 403; body=%s", rr.Body.String())
			}
		})
	}
}

func TestNonExemptRequestsBlockedWhenDefaultPasswordActive(t *testing.T) {
	// Sanity check: routes outside DEFAULT_PASSWORD_EXEMPT_PREFIXES still
	// return 403 while default_password_active is true.
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"defaultPass123"}`))
	loginRR := httptest.NewRecorder()
	mux.ServeHTTP(loginRR, loginReq)

	var tokens AuthTokens
	_ = json.Unmarshal(loginRR.Body.Bytes(), &tokens)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mining", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on non-exempt route, got %d; body=%s", rr.Code, rr.Body.String())
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

func TestHandleSetPassword_PasswordAlreadySet_Returns403(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		strings.NewReader(`{"password":"validPass123"}`))
	h.handleSetPassword(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusForbidden, rr.Code, rr.Body.String())
	}

	if state.GetPassword() != "defaultPass123" {
		t.Fatalf("expected password to remain %q, got %q", "defaultPass123", state.GetPassword())
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

func TestSystemRoute_DoesNotRequireBearerAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestHardwareRoute_RequiresBearerAuth(t *testing.T) {
	// /api/v1/hardware is NOT in firmware PUBLIC_ROUTES — it requires auth.
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/hardware", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}
}

func TestNetworkRoute_GET_DoesNotRequireBearerAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestNetworkRoute_GET_DuringReboot_Returns503(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.Rebooting = true
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusServiceUnavailable, rr.Code, rr.Body.String())
	}
}

func TestTestPoolConnectionRoute_RequiresBearerAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetAccessToken("test-token")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Without auth: should get 401
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools/test-connection",
		strings.NewReader(`{"url":"stratum+tcp://mine.ocean.xyz:3334","username":"worker"}`))
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d without auth, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}

	// With auth: should succeed
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/pools/test-connection",
		strings.NewReader(`{"url":"stratum+tcp://mine.ocean.xyz:3334","username":"worker"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d with auth, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
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

func TestCreatePools_PreservesPoolNamesInStateAndListResponse(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/pools",
		strings.NewReader(`[{"name":"Primary Pool","url":"stratum+tcp://mine.ocean.xyz:3334","username":"worker"}]`),
	)
	h.createPools(createRecorder, createRequest)

	if createRecorder.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, createRecorder.Code, createRecorder.Body.String())
	}

	pools := state.GetPools()
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}
	if got := state.GetPoolName(0); got != "Primary Pool" {
		t.Fatalf("expected pool name %q, got %q", "Primary Pool", got)
	}

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	h.handlePools(listRecorder, listRequest)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, listRecorder.Code, listRecorder.Body.String())
	}

	var response PoolsList
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body=%s", err, listRecorder.Body.String())
	}

	if len(response.Pools) != 1 {
		t.Fatalf("expected 1 pool in response, got %d", len(response.Pools))
	}
	if response.Pools[0].Name != "Primary Pool" {
		t.Fatalf("expected response pool name %q, got %q", "Primary Pool", response.Pools[0].Name)
	}
}

func TestUpdatePool_PreservesUpdatedPoolNameInGetResponse(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.AddPool(&Pool{
		Idx:      0,
		Url:      "stratum+tcp://mine.ocean.xyz:3334",
		Username: "worker",
	})
	state.SetPoolName(0, "Old Pool")
	h := NewRESTApiHandler(state)

	updateRecorder := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/pools/0",
		strings.NewReader(`{"name":"Renamed Pool","url":"stratum+tcp://mine.ocean.xyz:3334","username":"worker"}`),
	)
	h.handlePoolByID(updateRecorder, updateRequest)

	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, updateRecorder.Code, updateRecorder.Body.String())
	}

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/pools/0", nil)
	h.handlePoolByID(getRecorder, getRequest)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, getRecorder.Code, getRecorder.Body.String())
	}

	var response PoolResponse
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body=%s", err, getRecorder.Body.String())
	}

	if response.Pool.Name != "Renamed Pool" {
		t.Fatalf("expected response pool name %q, got %q", "Renamed Pool", response.Pool.Name)
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

func TestPairingAuthKeyRoute_POST_AllowsInitialPairWithoutAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/auth-key",
		strings.NewReader(`{"public_key":"test-key-123"}`))
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if state.GetAuthKey() != "test-key-123" {
		t.Fatalf("expected auth key %q, got %q", "test-key-123", state.GetAuthKey())
	}
}

func TestPairingAuthKeyRoute_POST_DuringReboot_Returns503(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.Rebooting = true
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/auth-key",
		strings.NewReader(`{"public_key":"test-key-123"}`))
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusServiceUnavailable, rr.Code, rr.Body.String())
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

func TestHandlePairingAuthKey_DELETE_BlockedWhenDefaultPasswordActive(t *testing.T) {
	// Firmware does NOT exempt DELETE /pairing/auth-key from the default-password
	// gate — it's not in DEFAULT_PASSWORD_EXEMPT_PREFIXES. So Fleet unpair against
	// a never-rotated device returns 403. Callers must tolerate this (and
	// ideally surface the remediation state) rather than assume revocation
	// always succeeds.
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
	state.SetAuthKey("existing-key")
	state.SetAccessToken("issued-bearer")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pairing/auth-key", nil)
	req.Header.Set("Authorization", "Bearer issued-bearer")
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusForbidden, rr.Code, rr.Body.String())
	}
	if state.GetAuthKey() != "existing-key" {
		t.Fatalf("expected auth key to remain, got %q", state.GetAuthKey())
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

// --- /api/v1/system tests ---

// Unmarshals the /api/v1/system response into a generic map so we assert against
// the wire JSON field names (e.g. "new_version") rather than Go struct names.
func getSystemInfo(t *testing.T, h *RESTApiHandler) map[string]any {
	t.Helper()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	h.handleSystem(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var envelope map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	info, ok := envelope["system-info"].(map[string]any)
	if !ok {
		t.Fatalf("expected system-info object in response, got: %s", rr.Body.String())
	}
	return info
}

func TestHandleSystem_IncludesManufacturerAndModel(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	info := getSystemInfo(t, h)

	// ProtoOS identifies a Proto Rig by product_name === "Proto Rig"
	// (see client/src/protoOS/store/hooks/useSystemInfo.ts). The simulator
	// must match that exact string or the UI treats it as an unknown device.
	if got := info["product_name"]; got != "Proto Rig" {
		t.Fatalf("expected product_name %q, got %v", "Proto Rig", got)
	}
	if got := info["manufacturer"]; got != "Proto" {
		t.Fatalf("expected manufacturer %q, got %v", "Proto", got)
	}
	if got := info["model"]; got != "Rig" {
		t.Fatalf("expected model %q, got %v", "Rig", got)
	}
}

func TestHandleSystem_OsVariantIsReleasePerSpec(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	info := getSystemInfo(t, h)

	osInfo, ok := info["os"].(map[string]any)
	if !ok {
		t.Fatalf("expected os object, got: %v", info["os"])
	}

	// The OpenAPI OsInfo.variant enum is {"release","mfg","dev","unknown"}.
	// Keep the simulator on a spec-valid value so contract tests pass.
	switch osInfo["variant"] {
	case "release", "mfg", "dev", "unknown":
	default:
		t.Fatalf("expected os.variant to be one of release/mfg/dev/unknown, got %v", osInfo["variant"])
	}
}

func TestHandleSystem_SwUpdateStatusUsesSpecFieldNames(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	info := getSystemInfo(t, h)

	swUpdate, ok := info["sw_update_status"].(map[string]any)
	if !ok {
		t.Fatalf("expected sw_update_status object, got: %v", info["sw_update_status"])
	}

	if got := swUpdate["status"]; got != "current" {
		t.Fatalf("expected status %q on fresh start, got %v", "current", got)
	}
	if got := swUpdate["current_version"]; got != defaultFirmwareVersion {
		t.Fatalf("expected current_version %q, got %v", defaultFirmwareVersion, got)
	}

	// Previous version is only populated after an install+reboot cycle; verify it's
	// absent on fresh start so clients don't mistake "" for a real prior version.
	if _, present := swUpdate["previous_version"]; present {
		t.Fatalf("expected previous_version to be omitted on fresh start, got %v", swUpdate["previous_version"])
	}

	// Ensure the old (pre-spec) field name isn't serialized anymore.
	if _, present := swUpdate["available_version"]; present {
		t.Fatal("response must not include legacy available_version field; OpenAPI spec uses new_version")
	}
}

func TestHandleSystem_PreviousVersionSetAfterInstallReboot(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	// Simulate the post-install, pre-reboot state: a firmware bundle with a new
	// version has been uploaded and installed; the reboot should promote it to
	// current and move the old current into previous.
	state.mu.Lock()
	state.FWUpdateStatus = "installed"
	state.FWNewVersion = defaultNextFirmwareVersion
	state.mu.Unlock()

	rebootRR := httptest.NewRecorder()
	rebootReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/reboot", nil)
	h.handleReboot(rebootRR, rebootReq)

	if rebootRR.Code != http.StatusAccepted {
		t.Fatalf("expected %d from reboot, got %d; body=%s", http.StatusAccepted, rebootRR.Code, rebootRR.Body.String())
	}

	// handleReboot starts a background goroutine that clears Rebooting after 10s; for
	// the test we just clear the flag directly so the system endpoint responds.
	state.mu.Lock()
	state.Rebooting = false
	state.mu.Unlock()

	info := getSystemInfo(t, h)
	swUpdate, ok := info["sw_update_status"].(map[string]any)
	if !ok {
		t.Fatalf("expected sw_update_status object, got: %v", info["sw_update_status"])
	}

	if got := swUpdate["current_version"]; got != defaultNextFirmwareVersion {
		t.Fatalf("expected current_version %q after install+reboot, got %v", defaultNextFirmwareVersion, got)
	}
	if got := swUpdate["previous_version"]; got != defaultFirmwareVersion {
		t.Fatalf("expected previous_version %q after install+reboot, got %v", defaultFirmwareVersion, got)
	}
	if swUpdate["current_version"] == swUpdate["previous_version"] {
		t.Fatalf("current_version and previous_version must differ after an upgrade, both are %v", swUpdate["current_version"])
	}
	if got := swUpdate["status"]; got != "current" {
		t.Fatalf("expected status to reset to %q after reboot, got %v", "current", got)
	}

	// OS.version should also reflect the promoted firmware version -- real
	// firmware bundles upgrade OS + services together.
	osInfo, ok := info["os"].(map[string]any)
	if !ok {
		t.Fatalf("expected os object, got: %v", info["os"])
	}
	if got := osInfo["version"]; got != defaultNextFirmwareVersion {
		t.Fatalf("expected os.version to be promoted to %q, got %v", defaultNextFirmwareVersion, got)
	}
}

func TestHandleSystem_RebootWithoutInstall_DoesNotSetPreviousVersion(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rebootRR := httptest.NewRecorder()
	rebootReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/reboot", nil)
	h.handleReboot(rebootRR, rebootReq)

	state.mu.Lock()
	state.Rebooting = false
	state.mu.Unlock()

	info := getSystemInfo(t, h)
	swUpdate, ok := info["sw_update_status"].(map[string]any)
	if !ok {
		t.Fatalf("expected sw_update_status object, got: %v", info["sw_update_status"])
	}

	if _, present := swUpdate["previous_version"]; present {
		t.Fatalf("expected previous_version to be absent after reboot without prior install, got %v", swUpdate["previous_version"])
	}
}

// Regression: a PUT /api/v1/system/update that arrives while an install is
// already "installed" and awaiting reboot must not clobber FWUpdateStatus /
// FWNewVersion. Before the fix, any PUT (malformed or not) in this state
// silently reset both fields and caused handleReboot to skip version promotion.
func TestHandleUpdate_PutWhileInstalled_RejectsAndPreservesStagedVersion(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	// Simulate a completed install awaiting reboot. Driving the full 60s async
	// lifecycle through handleUpdate would be too slow for a unit test, so we
	// seed the state directly; the paths under test (handleUpdate's guard and
	// handleReboot's promotion) are unaffected by how we got here.
	state.mu.Lock()
	state.FWUpdateStatus = "installed"
	state.FWNewVersion = defaultNextFirmwareVersion
	state.mu.Unlock()

	// A second PUT with malformed multipart body must return 409 BEFORE any
	// state mutation, not fall through and wipe the staged version.
	malformedReq := httptest.NewRequest(http.MethodPut, "/api/v1/system/update", strings.NewReader(""))
	malformedReq.Header.Set("Content-Type", "multipart/form-data; boundary=notused")
	malformedRR := httptest.NewRecorder()
	h.handleUpdate(malformedRR, malformedReq)

	if malformedRR.Code != http.StatusConflict {
		t.Fatalf("expected %d for PUT while installed, got %d; body=%s",
			http.StatusConflict, malformedRR.Code, malformedRR.Body.String())
	}

	state.mu.RLock()
	gotStatus := state.FWUpdateStatus
	gotNewVersion := state.FWNewVersion
	state.mu.RUnlock()

	if gotStatus != "installed" {
		t.Fatalf("expected FWUpdateStatus to remain %q after rejected re-upload, got %q", "installed", gotStatus)
	}
	if gotNewVersion != defaultNextFirmwareVersion {
		t.Fatalf("expected FWNewVersion to remain %q after rejected re-upload, got %q", defaultNextFirmwareVersion, gotNewVersion)
	}

	// Confirm the subsequent reboot still promotes the staged version.
	rebootRR := httptest.NewRecorder()
	rebootReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/reboot", nil)
	h.handleReboot(rebootRR, rebootReq)

	if rebootRR.Code != http.StatusAccepted {
		t.Fatalf("expected %d from reboot, got %d", http.StatusAccepted, rebootRR.Code)
	}

	state.mu.Lock()
	state.Rebooting = false
	state.mu.Unlock()

	info := getSystemInfo(t, h)
	swUpdate, ok := info["sw_update_status"].(map[string]any)
	if !ok {
		t.Fatalf("expected sw_update_status object, got: %v", info["sw_update_status"])
	}
	if got := swUpdate["current_version"]; got != defaultNextFirmwareVersion {
		t.Fatalf("expected current_version %q after promotion, got %v", defaultNextFirmwareVersion, got)
	}
	if got := swUpdate["previous_version"]; got != defaultFirmwareVersion {
		t.Fatalf("expected previous_version %q after promotion, got %v", defaultFirmwareVersion, got)
	}
}
