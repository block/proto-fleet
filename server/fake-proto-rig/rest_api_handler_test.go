package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
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
	state.SetAuthKey("old-auth-key")
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

	if state.GetAuthKey() != "" {
		t.Fatalf("expected auth key to be revoked after password change, got %q", state.GetAuthKey())
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

func TestPoolsAllowedWhenDefaultPasswordActive(t *testing.T) {
	// Firmware blocks only PUT /system/unlock while the default password is
	// active, so Fleet can provision pool settings before the operator changes
	// the factory password.
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
				t.Fatalf("expected pools to be allowed while default password is active, got 403; body=%s", rr.Body.String())
			}
		})
	}
}

func TestAuthenticatedRequestsAllowedWhenDefaultPasswordActive(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"defaultPass123"}`))
	loginRR := httptest.NewRecorder()
	mux.ServeHTTP(loginRR, loginReq)

	var tokens AuthTokens
	if err := json.Unmarshal(loginRR.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("failed to unmarshal auth tokens: %v; body=%s", err, loginRR.Body.String())
	}

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "mining status", method: http.MethodGet, path: "/api/v1/mining", wantStatus: http.StatusOK},
		{name: "cooling status", method: http.MethodGet, path: "/api/v1/cooling", wantStatus: http.StatusOK},
		{name: "network update", method: http.MethodPut, path: "/api/v1/network", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
			mux.ServeHTTP(rr, req)

			if rr.Code == http.StatusForbidden {
				t.Fatalf("expected %s %s to be allowed while default password is active, got 403; body=%s", tt.method, tt.path, rr.Body.String())
			}
			if rr.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d; body=%s", tt.wantStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestSystemUnlockBlockedWhenDefaultPasswordActive(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"defaultPass123"}`))
	loginRR := httptest.NewRecorder()
	mux.ServeHTTP(loginRR, loginReq)

	var tokens AuthTokens
	if err := json.Unmarshal(loginRR.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("failed to unmarshal auth tokens: %v; body=%s", err, loginRR.Body.String())
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system/unlock", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusForbidden, rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "DEFAULT_PASSWORD_ACTIVE") {
		t.Fatalf("expected DEFAULT_PASSWORD_ACTIVE response, got body=%s", rr.Body.String())
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

func TestChangePasswordRevokesPairedJWT(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
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

	changeRR := httptest.NewRecorder()
	changeReq := httptest.NewRequest(http.MethodPut, "/api/v1/auth/change-password",
		strings.NewReader(`{"current_password":"defaultPass123","new_password":"newPassword123"}`))
	changeReq.Header.Set("Authorization", "Bearer "+jwtToken)
	mux.ServeHTTP(changeRR, changeReq)

	if changeRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, changeRR.Code, changeRR.Body.String())
	}
	if state.GetAuthKey() != "" {
		t.Fatalf("expected auth key to be revoked after password change, got %q", state.GetAuthKey())
	}

	retryRR := httptest.NewRecorder()
	retryReq := httptest.NewRequest(http.MethodPost, "/api/v1/mining/start", nil)
	retryReq.Header.Set("Authorization", "Bearer "+jwtToken)
	mux.ServeHTTP(retryRR, retryReq)

	if retryRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, retryRR.Code, retryRR.Body.String())
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

func TestHandleSystemStatus_OmitsDefaultPasswordActive(t *testing.T) {
	// MDK-API 1.8.2 removed default_password_active from /api/v1/system/status.
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	h.handleSystemStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if _, ok := raw["default_password_active"]; ok {
		t.Fatal("expected default_password_active to be absent from system status")
	}
}

func TestHandleSecureStatus_GetReturnsState(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/secure", nil)
	h.handleSecureStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	var resp SecureResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Secure {
		t.Fatal("expected simulated device to report secure=false")
	}
	if resp.State.Sshd == "" || resp.State.NatsService == "" ||
		resp.State.Secureboot == "" || resp.State.CertificateValidity == "" {
		t.Fatalf("expected all secure state fields to be populated, got %+v", resp.State)
	}
}

func TestHandleSecureStatus_PutSetsOverride(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system/secure",
		strings.NewReader(`{"secure_override":true}`))
	h.handleSecureStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if !state.GetSecureOverride() {
		t.Fatal("expected secure override marker to be set")
	}

	// Missing secure_override is rejected with 422 like the firmware.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/system/secure", strings.NewReader(`{}`))
	h.handleSecureStatus(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	}
}

func TestHandleSecureStatus_PutRequiresAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system/secure",
		strings.NewReader(`{"secure_override":true}`))
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}

	// GET stays public per the firmware PUBLIC_ROUTES list.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/system/secure", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestHandleCurtailmentConfig_RoundTrip(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/curtailment/config", nil)
	h.handleCurtailmentConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	var defaults CurtailmentConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &defaults); err != nil {
		t.Fatalf("failed to unmarshal default config: %v", err)
	}
	if defaults.Enabled {
		t.Fatal("expected curtailment to default to disabled")
	}
	if defaults.FailPolicy != "closed" || defaults.RestorePolicy != "respect_manual_stop" {
		t.Fatalf("unexpected default policies: %+v", defaults)
	}

	newConfig := `{
		"enabled": true,
		"fail_policy": "open",
		"restore_policy": "respect_manual_stop",
		"nats_url": "nats://localhost:4222",
		"mcdd_grpc_addr": "127.0.0.1:2122",
		"status_publish_interval": "15s",
		"providers": [{
			"name": "maestro",
			"type": "maestro_mqtt",
			"enabled": true,
			"brokers": ["10.155.0.3", "10.155.0.4"],
			"port": 1883,
			"username": "maestro",
			"password": "mqtt-password",
			"topic": "maestro/target",
			"qos": 1,
			"stale_after": "4m",
			"reconnect_backoff": "5s"
		}]
	}`
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/curtailment/config", strings.NewReader(newConfig))
	h.handleCurtailmentConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	stored := state.GetCurtailmentConfig()
	if !stored.Enabled || stored.FailPolicy != "open" {
		t.Fatalf("expected stored config to reflect the PUT, got %+v", stored)
	}
	if len(stored.Providers) != 1 || len(stored.Providers[0].Brokers) != 2 {
		t.Fatalf("expected one provider with two brokers, got %+v", stored.Providers)
	}
}

func TestHandleCurtailmentConfig_PutValidation(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "bad fail_policy",
			body: `{"fail_policy":"sometimes","restore_policy":"respect_manual_stop","nats_url":"nats://localhost:4222","mcdd_grpc_addr":"127.0.0.1:2122","status_publish_interval":"15s","providers":[]}`,
		},
		{
			name: "bad nats_url",
			body: `{"fail_policy":"closed","restore_policy":"respect_manual_stop","nats_url":"nats://remote:4222","mcdd_grpc_addr":"127.0.0.1:2122","status_publish_interval":"15s","providers":[]}`,
		},
		{
			name: "interval above TTL",
			body: `{"fail_policy":"closed","restore_policy":"respect_manual_stop","nats_url":"nats://localhost:4222","mcdd_grpc_addr":"127.0.0.1:2122","status_publish_interval":"90s","providers":[]}`,
		},
		{
			name: "enabled provider without brokers",
			body: `{"fail_policy":"closed","restore_policy":"respect_manual_stop","nats_url":"nats://localhost:4222","mcdd_grpc_addr":"127.0.0.1:2122","status_publish_interval":"15s","providers":[{"name":"maestro","type":"maestro_mqtt","enabled":true,"brokers":[],"port":1883,"topic":"maestro/target","qos":1,"stale_after":"4m","reconnect_backoff":"5s"}]}`,
		},
		{
			name: "broker is a URL",
			body: `{"fail_policy":"closed","restore_policy":"respect_manual_stop","nats_url":"nats://localhost:4222","mcdd_grpc_addr":"127.0.0.1:2122","status_publish_interval":"15s","providers":[{"name":"maestro","type":"maestro_mqtt","enabled":true,"brokers":["mqtt://10.0.0.1"],"port":1883,"topic":"maestro/target","qos":1,"stale_after":"4m","reconnect_backoff":"5s"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/v1/curtailment/config", strings.NewReader(tt.body))
			h.handleCurtailmentConfig(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestHandleCurtailmentStatus_DefaultUnknown(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/curtailment/status", nil)
	h.handleCurtailmentStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	var status CurtailmentStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}
	if status.Active || status.Known {
		t.Fatalf("expected inactive unknown status, got %+v", status)
	}
	if status.Reason == nil || *status.Reason != "no_status_received" {
		t.Fatalf("expected reason 'no_status_received', got %v", status.Reason)
	}
}

func TestCurtailmentEndpoints_RequireAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	paths := []string{"/api/v1/curtailment/config", "/api/v1/curtailment/status"}
	for _, path := range paths {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d for unauthenticated %s, got %d; body=%s",
				http.StatusUnauthorized, path, rr.Code, rr.Body.String())
		}
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

func TestHandleChangePassword_NewPasswordMatchesDefault_Returns400(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
	h := NewRESTApiHandler(state)
	const expectedMessage = "New password cannot be the same as the default password"

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/change-password",
		strings.NewReader(`{"current_password":"defaultPass123","new_password":"defaultPass123"}`))
	h.handleChangePassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Message != expectedMessage {
		t.Fatalf("expected error message %q, got %q", expectedMessage, resp.Error.Message)
	}

	if state.GetPassword() != "defaultPass123" {
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

func TestHardwareDiscoveryRoutes_DoNotRequireBearerAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	for _, path := range []string{
		"/api/v1/hardware",
		"/api/v1/hardware/psus",
		"/api/v1/hashboards",
		"/api/v1/power-supplies",
	} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestHardwareDiscoveryRoutes_DuringReboot_Returns503(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.Rebooting = true
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	for _, path := range []string{
		"/api/v1/hardware",
		"/api/v1/hardware/psus",
		"/api/v1/hashboards",
		"/api/v1/power-supplies",
	} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected %d, got %d; body=%s", http.StatusServiceUnavailable, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestHashboardDetailRoute_RequiresBearerAuth(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/hashboards/HB-SN12345678-0", nil)
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

func TestNetworkRoute_GET_UsesSpecFieldNames(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.IPAddress = "192.168.2.50"
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Assert raw wire keys against the MDK spec (NetworkInfo_networkinfo): mac/ip,
	// not mac_address/ip_address. Decoding into the typed struct would hide a
	// json-tag regression, so inspect the raw object keys.
	var envelope struct {
		NetworkInfo map[string]json.RawMessage `json:"network-info"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body=%s", err, rr.Body.String())
	}

	for _, legacy := range []string{"mac_address", "ip_address"} {
		if _, ok := envelope.NetworkInfo[legacy]; ok {
			t.Fatalf("network-info still emits legacy key %q; body=%s", legacy, rr.Body.String())
		}
	}

	if got, ok := envelope.NetworkInfo["mac"]; !ok {
		t.Fatalf("network-info missing %q key; body=%s", "mac", rr.Body.String())
	} else if string(got) != `"00:11:22:33:44:55"` {
		t.Fatalf("expected mac %q, got %s", "00:11:22:33:44:55", got)
	}

	if got, ok := envelope.NetworkInfo["ip"]; !ok {
		t.Fatalf("network-info missing %q key; body=%s", "ip", rr.Body.String())
	} else if string(got) != `"192.168.2.50"` {
		t.Fatalf("expected ip %q, got %s", "192.168.2.50", got)
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

func TestGetPools_UsesSpecShareFieldNames(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.AddPool(&Pool{
		Idx:      0,
		Url:      "stratum+tcp://mine.ocean.xyz:3334",
		Username: "worker",
		Statistics: &PoolStatistics{
			AcceptedShares: 100,
			RejectedShares: 20,
		},
	})
	state.SetAccessToken("test-token")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Assert raw wire keys against the MDK spec (Pool): accepted/rejected, not
	// accepted_shares/rejected_shares. Decoding into PoolData would hide a
	// json-tag regression, so inspect the raw object keys.
	var envelope struct {
		Pools []map[string]json.RawMessage `json:"pools"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body=%s", err, rr.Body.String())
	}
	if len(envelope.Pools) != 1 {
		t.Fatalf("expected 1 pool, got %d; body=%s", len(envelope.Pools), rr.Body.String())
	}

	pool := envelope.Pools[0]
	for _, legacy := range []string{"accepted_shares", "rejected_shares"} {
		if _, ok := pool[legacy]; ok {
			t.Fatalf("pool still emits legacy key %q; body=%s", legacy, rr.Body.String())
		}
	}

	if got, ok := pool["accepted"]; !ok {
		t.Fatalf("pool missing %q key; body=%s", "accepted", rr.Body.String())
	} else if string(got) != "100" {
		t.Fatalf("expected accepted 100, got %s", got)
	}

	if got, ok := pool["rejected"]; !ok {
		t.Fatalf("pool missing %q key; body=%s", "rejected", rr.Body.String())
	} else if string(got) != "20" {
		t.Fatalf("expected rejected 20, got %s", got)
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

func TestHandlePairingAuthKey_POST_RotationAllowedWhenDefaultPasswordActive(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SeedDefaultPassword("defaultPass123")
	state.SetAuthKey("existing-key")
	state.SetAccessToken("issued-bearer")
	h := NewRESTApiHandler(state)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing/auth-key",
		strings.NewReader(`{"public_key":"new-key"}`))
	req.Header.Set("Authorization", "Bearer issued-bearer")
	mux.ServeHTTP(rr, req)

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

func TestHandlePairingAuthKey_DELETE_AllowedWhenDefaultPasswordActive(t *testing.T) {
	// Firmware no longer blocks pairing auth-key deletion while the default
	// password is active; DELETE remains authenticated.
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

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if state.GetAuthKey() != "" {
		t.Fatalf("expected auth key to be cleared, got %q", state.GetAuthKey())
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
	fakeTimer := installFakeLocateTimer(h)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time=30", nil)
	h.handleLocate(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
	if !state.IsLocateActive() {
		t.Fatal("expected locate mode to remain active")
	}
	fakeTimer.requireScheduled(t, 0, 30*time.Second)
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
	if state.IsLocateActive() {
		t.Fatal("expected locate mode to remain inactive on invalid input")
	}
}

func TestHandleLocate_EnableFalseClearsLocateMode(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.SetLocateActive(true)
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?enable=false&led_on_time=abc", nil)
	h.handleLocate(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
	if state.IsLocateActive() {
		t.Fatal("expected locate mode to be inactive")
	}
}

func TestHandleLocate_InvalidEnable_Returns400(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?enable=eventually", nil)
	h.handleLocate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
	if state.IsLocateActive() {
		t.Fatal("expected locate mode to remain inactive on invalid input")
	}
}

func TestHandleLocate_TimedLedOnTimeClearsLocateMode(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)
	fakeTimer := installFakeLocateTimer(h)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time=1", nil)
	h.handleLocate(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
	if !state.IsLocateActive() {
		t.Fatal("expected locate mode to become active")
	}
	fakeTimer.requireScheduled(t, 0, time.Second)
	fakeTimer.fire(t, 0)
	if state.IsLocateActive() {
		t.Fatal("expected locate mode to clear after timer fires")
	}
}

func TestHandleLocate_CapsLargeLedOnTime(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)
	fakeTimer := installFakeLocateTimer(h)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time=9223372036854775807", nil)
	h.handleLocate(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
	fakeTimer.requireScheduled(t, 0, maxLocateLEDOnTimeSecs*time.Second)
}

func TestHandleLocate_ZeroOrNegativeLedOnTimePersists(t *testing.T) {
	for _, ledOnTime := range []string{"0", "-5"} {
		t.Run("led_on_time="+ledOnTime, func(t *testing.T) {
			state := NewMinerState("SN12345678", "00:11:22:33:44:55")
			h := NewRESTApiHandler(state)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time="+ledOnTime, nil)
			h.handleLocate(rr, req)

			if rr.Code != http.StatusAccepted {
				t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
			}
			if !state.IsLocateActive() {
				t.Fatal("expected locate mode to persist")
			}
		})
	}
}

func TestHandleLocate_TimedRequestDoesNotClearLaterPersistentMode(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)
	fakeTimer := installFakeLocateTimer(h)

	timedRR := httptest.NewRecorder()
	timedReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time=1", nil)
	h.handleLocate(timedRR, timedReq)
	if timedRR.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, timedRR.Code, timedRR.Body.String())
	}

	persistentRR := httptest.NewRecorder()
	persistentReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time=0", nil)
	h.handleLocate(persistentRR, persistentReq)
	if persistentRR.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, persistentRR.Code, persistentRR.Body.String())
	}

	fakeTimer.requireScheduled(t, 0, time.Second)
	fakeTimer.requireCanceled(t, 0)
	fakeTimer.fire(t, 0)
	if !state.IsLocateActive() {
		t.Fatal("expected later persistent locate mode to remain active")
	}
}

func TestHandleLocate_TimedRequestCancelsEarlierTimedRequest(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)
	fakeTimer := installFakeLocateTimer(h)

	firstRR := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time=30", nil)
	h.handleLocate(firstRR, firstReq)
	if firstRR.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, firstRR.Code, firstRR.Body.String())
	}

	secondRR := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/locate?led_on_time=1", nil)
	h.handleLocate(secondRR, secondReq)
	if secondRR.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, secondRR.Code, secondRR.Body.String())
	}

	fakeTimer.requireScheduled(t, 0, 30*time.Second)
	fakeTimer.requireScheduled(t, 1, time.Second)
	fakeTimer.requireCanceled(t, 0)
	fakeTimer.requireActive(t, 1)

	fakeTimer.fire(t, 0)
	if !state.IsLocateActive() {
		t.Fatal("expected canceled earlier timer not to clear locate mode")
	}
	fakeTimer.fire(t, 1)
	if state.IsLocateActive() {
		t.Fatal("expected active later timer to clear locate mode")
	}
}

type scheduledLocateClear struct {
	duration time.Duration
	callback func()
	canceled bool
}

type fakeLocateTimer struct {
	scheduled []scheduledLocateClear
}

func installFakeLocateTimer(h *RESTApiHandler) *fakeLocateTimer {
	timer := &fakeLocateTimer{}
	h.scheduleLocateClear = func(duration time.Duration, callback func()) func() {
		index := len(timer.scheduled)
		timer.scheduled = append(timer.scheduled, scheduledLocateClear{
			duration: duration,
			callback: callback,
		})
		return func() {
			timer.scheduled[index].canceled = true
		}
	}
	return timer
}

func (f *fakeLocateTimer) requireScheduled(t *testing.T, index int, want time.Duration) {
	t.Helper()
	if len(f.scheduled) <= index {
		t.Fatalf("expected timer %d to be scheduled, got %d timers", index, len(f.scheduled))
	}
	if got := f.scheduled[index].duration; got != want {
		t.Fatalf("expected timer %d duration %s, got %s", index, want, got)
	}
}

func (f *fakeLocateTimer) fire(t *testing.T, index int) {
	t.Helper()
	if len(f.scheduled) <= index {
		t.Fatalf("expected timer %d to be scheduled, got %d timers", index, len(f.scheduled))
	}
	if f.scheduled[index].canceled {
		return
	}
	f.scheduled[index].callback()
}

func (f *fakeLocateTimer) requireCanceled(t *testing.T, index int) {
	t.Helper()
	if len(f.scheduled) <= index {
		t.Fatalf("expected timer %d to be scheduled, got %d timers", index, len(f.scheduled))
	}
	if !f.scheduled[index].canceled {
		t.Fatalf("expected timer %d to be canceled", index)
	}
}

func (f *fakeLocateTimer) requireActive(t *testing.T, index int) {
	t.Helper()
	if len(f.scheduled) <= index {
		t.Fatalf("expected timer %d to be scheduled, got %d timers", index, len(f.scheduled))
	}
	if f.scheduled[index].canceled {
		t.Fatalf("expected timer %d to remain active", index)
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

func TestStagedFirmwareVersion_UsesFilenameVersionWhenPresent(t *testing.T) {
	tests := []struct {
		name           string
		filename       string
		currentVersion string
		want           string
	}{
		{
			name:           "plain semantic version",
			filename:       "protoos-1.9.0.swu",
			currentVersion: "1.8.0",
			want:           "1.9.0",
		},
		{
			name:           "v-prefixed semantic version",
			filename:       "firmware-update-v2.0.2.swu",
			currentVersion: "1.8.0",
			want:           "2.0.2",
		},
		{
			name:           "fallback when filename has no version",
			filename:       "protoos-update.swu",
			currentVersion: "1.8.0",
			want:           defaultNextFirmwareVersion,
		},
		{
			name:           "fallback increments non-default current version",
			filename:       "protoos-update.swu",
			currentVersion: "2.3.4",
			want:           "2.3.5",
		},
		{
			name:           "does not extract partial four-part version",
			filename:       "protoos-1.2.3.4.swu",
			currentVersion: "1.8.0",
			want:           defaultNextFirmwareVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stagedFirmwareVersion(tt.filename, tt.currentVersion); got != tt.want {
				t.Fatalf("expected staged version %q, got %q", tt.want, got)
			}
		})
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

func TestHandleUpdate_PutProgressesToInstalled(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)
	const uploadedVersion = "2.4.6"

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "protoos-"+uploadedVersion+".swu")
	if err != nil {
		t.Fatalf("failed to create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("fake firmware bundle")); err != nil {
		t.Fatalf("failed to write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system/update", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	h.handleUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if req.MultipartForm != nil && len(req.MultipartForm.File) != 0 {
		t.Fatal("expected firmware upload to be streamed without buffering multipart file headers")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info := getSystemInfo(t, h)
		swUpdate, ok := info["sw_update_status"].(map[string]any)
		if !ok {
			t.Fatalf("expected sw_update_status object, got: %v", info["sw_update_status"])
		}

		if got := swUpdate["new_version"]; got != uploadedVersion {
			t.Fatalf("expected new_version %q after upload, got %v", uploadedVersion, got)
		}

		if swUpdate["status"] == "installed" {
			rebootRR := httptest.NewRecorder()
			rebootReq := httptest.NewRequest(http.MethodPost, "/api/v1/system/reboot", nil)
			h.handleReboot(rebootRR, rebootReq)

			if rebootRR.Code != http.StatusAccepted {
				t.Fatalf("expected %d from reboot, got %d; body=%s", http.StatusAccepted, rebootRR.Code, rebootRR.Body.String())
			}

			state.mu.Lock()
			state.Rebooting = false
			state.mu.Unlock()

			info = getSystemInfo(t, h)
			swUpdate, ok = info["sw_update_status"].(map[string]any)
			if !ok {
				t.Fatalf("expected sw_update_status object after reboot, got: %v", info["sw_update_status"])
			}
			if got := swUpdate["current_version"]; got != uploadedVersion {
				t.Fatalf("expected current_version %q after reboot, got %v", uploadedVersion, got)
			}
			if got := swUpdate["previous_version"]; got != defaultFirmwareVersion {
				t.Fatalf("expected previous_version %q after reboot, got %v", defaultFirmwareVersion, got)
			}
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	info := getSystemInfo(t, h)
	swUpdate, ok := info["sw_update_status"].(map[string]any)
	if !ok {
		t.Fatalf("expected sw_update_status object, got: %v", info["sw_update_status"])
	}
	t.Fatalf("expected uploaded firmware to reach %q, got %v", "installed", swUpdate["status"])
}

func TestHandleUpdate_PostFromDownloadedInstallsUpdate(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.mu.Lock()
	state.FWUpdateStatus = "downloaded"
	state.FWNewVersion = defaultNextFirmwareVersion
	state.mu.Unlock()

	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/update", nil)
	h.handleUpdate(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.mu.RLock()
		status := state.FWUpdateStatus
		state.mu.RUnlock()
		if status == "installed" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	state.mu.RLock()
	gotStatus := state.FWUpdateStatus
	state.mu.RUnlock()
	t.Fatalf("expected firmware status to reach %q, got %q", "installed", gotStatus)
}

func TestHandleUpdate_PutWhileDownloading_Rejects(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	state.mu.Lock()
	state.FWUpdateStatus = "downloading"
	state.FWNewVersion = defaultNextFirmwareVersion
	state.mu.Unlock()

	h := NewRESTApiHandler(state)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "protoos-update.swu")
	if err != nil {
		t.Fatalf("failed to create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("fake firmware bundle")); err != nil {
		t.Fatalf("failed to write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system/update", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	h.handleUpdate(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusConflict, rr.Code, rr.Body.String())
	}
}

func TestTelemetryService_GET_ReturnsRunningStatus(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/telemetry", nil)
	h.handleTelemetryConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	var status TelemetryServiceStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to unmarshal status: %v; body=%s", err, rr.Body.String())
	}
	if !status.Enabled {
		t.Errorf("expected telemetry enabled by default, got %v", status.Enabled)
	}
	if status.Message != "Telemetry is enabled" {
		t.Errorf("expected message %q, got %q", "Telemetry is enabled", status.Message)
	}
}

func TestTelemetryService_PUT_StopsServiceAndPersists(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system/telemetry", strings.NewReader(`{"enabled":false}`))
	h.handleTelemetryConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
	var status TelemetryServiceStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to unmarshal status: %v; body=%s", err, rr.Body.String())
	}
	if status.Enabled || status.Message != "Telemetry is disabled" {
		t.Errorf("expected disabled status, got %+v", status)
	}
	if state.IsTelemetryEnabled() {
		t.Error("expected telemetry state to be disabled after PUT")
	}

	// A subsequent GET reflects the persisted state.
	getRR := httptest.NewRecorder()
	h.handleTelemetryConfig(getRR, httptest.NewRequest(http.MethodGet, "/api/v1/system/telemetry", nil))
	var getStatus TelemetryServiceStatus
	if err := json.Unmarshal(getRR.Body.Bytes(), &getStatus); err != nil {
		t.Fatalf("failed to unmarshal status: %v; body=%s", err, getRR.Body.String())
	}
	if getStatus.Enabled {
		t.Error("expected GET after PUT to report telemetry disabled")
	}
}

func TestTelemetryService_PUT_InvalidBody_Returns400(t *testing.T) {
	// enabled is required by the schema, so a malformed body, an empty object,
	// or an explicit null must be rejected without mutating telemetry state.
	for _, tc := range []struct {
		name string
		body string
	}{
		{"malformed JSON", `{not json`},
		{"missing enabled", `{}`},
		{"null enabled", `{"enabled": null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewMinerState("SN12345678", "00:11:22:33:44:55")
			h := NewRESTApiHandler(state)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/v1/system/telemetry", strings.NewReader(tc.body))
			h.handleTelemetryConfig(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
			}
			if !state.IsTelemetryEnabled() {
				t.Error("telemetry must stay enabled (default) when the request is rejected")
			}
		})
	}
}

func TestPowerSuppliesUpdate_EmptyBody_Returns202(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/power-supplies/update", nil)
	h.handlePowerSuppliesUpdate(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
}

func TestPowerSuppliesUpdate_ValidPSUTypes_Returns202(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	body := `{"psu_types":{"1":"boco_bs502a17","2":"boco_bs402a17"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/power-supplies/update", strings.NewReader(body))
	h.handlePowerSuppliesUpdate(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusAccepted, rr.Code, rr.Body.String())
	}
}

func TestPowerSuppliesUpdate_UnknownPSUType_Returns422(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	body := `{"psu_types":{"1":"acme_9000"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/power-supplies/update", strings.NewReader(body))
	h.handlePowerSuppliesUpdate(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	}
}

func TestPowerSuppliesUpdate_InvalidSlot_Returns422(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	body := `{"psu_types":{"9":"chicony_s24"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/power-supplies/update", strings.NewReader(body))
	h.handlePowerSuppliesUpdate(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	}
}

func TestPowerSuppliesUpdate_MalformedJSON_Returns400(t *testing.T) {
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/power-supplies/update", strings.NewReader(`{"psu_types":`))
	h.handlePowerSuppliesUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d; body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
}

func TestHashboardEndpoints_ReportValidBoardEnum(t *testing.T) {
	// MDK-API 1.8.1 split the "B4" board enum into "B4_128"/"B4_192"; both the
	// /hardware and /hashboards handlers must report a value still in the enum.
	state := NewMinerState("SN12345678", "00:11:22:33:44:55")
	h := NewRESTApiHandler(state)

	hwRR := httptest.NewRecorder()
	h.handleHardware(hwRR, httptest.NewRequest(http.MethodGet, "/api/v1/hardware", nil))
	var hw HardwareInfo
	if err := json.Unmarshal(hwRR.Body.Bytes(), &hw); err != nil {
		t.Fatalf("failed to unmarshal hardware info: %v; body=%s", err, hwRR.Body.String())
	}
	if len(hw.HardwareInfo.Hashboards) == 0 {
		t.Fatal("expected at least one hashboard from /hardware")
	}
	for _, hb := range hw.HardwareInfo.Hashboards {
		if hb.Board != "B4_128" {
			t.Errorf("/hardware: expected board %q, got %q", "B4_128", hb.Board)
		}
	}

	hbRR := httptest.NewRecorder()
	h.handleHashboards(hbRR, httptest.NewRequest(http.MethodGet, "/api/v1/hashboards", nil))
	var hbs HashboardsResponse
	if err := json.Unmarshal(hbRR.Body.Bytes(), &hbs); err != nil {
		t.Fatalf("failed to unmarshal hashboards: %v; body=%s", err, hbRR.Body.String())
	}
	if len(hbs.Hashboards) == 0 {
		t.Fatal("expected at least one hashboard from /hashboards")
	}
	for _, hb := range hbs.Hashboards {
		if hb.Board != "B4_128" {
			t.Errorf("/hashboards: expected board %q, got %q", "B4_128", hb.Board)
		}
	}
}
