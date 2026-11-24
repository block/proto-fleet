package minerproxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/authn"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

// testServiceMasterKey is a valid 32-byte (base64) AES key for encrypt.Service
// in tests; it matches the throwaway key used across the server test suite.
const testServiceMasterKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

// newForwardTestHandler builds a Handler with only the fields forward/tokenFor/
// login touch: real HTTP clients, a real encrypt.Service, and a token cache. It
// deliberately omits the auth/query stack, so tests drive forward() directly
// against an httptest.Server URL (forward has no SSRF guard — that lives in
// resolveTarget).
func newForwardTestHandler(t *testing.T) *Handler {
	t.Helper()
	enc, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: testServiceMasterKey})
	if err != nil {
		t.Fatalf("encrypt.NewService: %v", err)
	}
	return &Handler{
		encryptService: enc,
		httpClient:     newProxyHTTPClient(false),
		httpsClient:    newProxyHTTPClient(true),
		tokens:         make(map[string]cachedToken),
	}
}

func TestPermissionsFor(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		proxyPath string
		want      []string
	}{
		{name: "CT1 container start", method: http.MethodPost, proxyPath: "/api/v2/mining/start", want: []string{authz.PermMinerStartMining}},
		{name: "CT1 container stop", method: http.MethodPost, proxyPath: "/api/v2/mining/stop", want: []string{authz.PermMinerStopMining}},
		{name: "CT1 scoped start", method: http.MethodPost, proxyPath: "/api/v2/mining/scoped/start", want: []string{authz.PermMinerStartMining}},
		{name: "CT1 scoped stop", method: http.MethodPost, proxyPath: "/api/v2/mining/scoped/stop", want: []string{authz.PermMinerStopMining}},
		{name: "CT1 identify", method: http.MethodPost, proxyPath: "/api/v2/hashboards/identify-led", want: []string{authz.PermMinerBlinkLED}},
		{name: "CT1 isolation", method: http.MethodPost, proxyPath: "/api/v2/mining/isolation", want: []string{authz.PermMinerStartMining, authz.PermMinerStopMining}},
		{name: "CT1 power target", method: http.MethodPut, proxyPath: "/api/v2/mining/target-power", want: []string{authz.PermMinerSetPowerTarget}},
		{name: "CT1 module run-in", method: http.MethodPost, proxyPath: "/api/v2/modules/T1-M01/run-in", want: []string{authz.PermMinerStartMining, authz.PermMinerStopMining, authz.PermMinerSetPowerTarget}},
		{name: "CT1 coolant pump", method: http.MethodPost, proxyPath: "/api/v2/power/container/controls/coolant_pump", want: []string{authz.PermMinerSetCoolingMode}},
		{name: "CT1 dry cooler setpoint", method: http.MethodPost, proxyPath: "/api/v2/power/container/setpoints/dry_cooler_auto_temp_c", want: []string{authz.PermMinerSetCoolingMode}},
		{name: "CT1 tank lights", method: http.MethodPost, proxyPath: "/api/v2/power/container/controls/tank_led_light", want: []string{authz.PermMinerBlinkLED}},
		{name: "CT1 controls read", method: http.MethodGet, proxyPath: "/api/v2/power/container/controls", want: []string{authz.PermMinerRead}},
		{name: "CT1 pool history read", method: http.MethodGet, proxyPath: "/api/v2/telemetry/shares/history", want: []string{authz.PermMinerRead}},
		{
			name:      "read endpoints require miner read",
			method:    http.MethodGet,
			proxyPath: "/api/v1/network",
			want:      []string{authz.PermMinerRead},
		},
		{
			name:      "log downloads require log download permission for get",
			method:    http.MethodGet,
			proxyPath: "/api/v1/system/logs",
			want:      []string{authz.PermMinerDownloadLogs},
		},
		{
			name:      "log downloads require log download permission for head",
			method:    http.MethodHead,
			proxyPath: "/api/v1/system/logs",
			want:      []string{authz.PermMinerDownloadLogs},
		},
		{
			name:      "timeseries post is a read-style query",
			method:    http.MethodPost,
			proxyPath: "/api/v1/timeseries",
			want:      []string{authz.PermMinerRead},
		},
		{
			name:      "pools writes require pool update",
			method:    http.MethodPut,
			proxyPath: "/api/v1/pools/1",
			want:      []string{authz.PermMinerUpdatePools},
		},
		{
			name:      "power target writes require power target permission",
			method:    http.MethodPut,
			proxyPath: "/api/v1/mining/target",
			want:      []string{authz.PermMinerSetPowerTarget},
		},
		{
			name:      "firmware writes require firmware update and reboot",
			method:    http.MethodPost,
			proxyPath: "/api/v1/system/update",
			want:      []string{authz.PermMinerFirmwareUpdate, authz.PermMinerReboot},
		},
		{
			name:      "update check requires firmware update but not reboot",
			method:    http.MethodPost,
			proxyPath: "/api/v1/system/update/check",
			want:      []string{authz.PermMinerFirmwareUpdate},
		},
		{
			name:      "psu firmware writes require firmware update and reboot",
			method:    http.MethodPost,
			proxyPath: "/api/v1/power-supplies/update",
			want:      []string{authz.PermMinerFirmwareUpdate, authz.PermMinerReboot},
		},
		{
			name:      "tag writes require rename",
			method:    http.MethodPut,
			proxyPath: "/api/v1/system/tag",
			want:      []string{authz.PermMinerRename},
		},
		{
			name:      "security settings writes require password update",
			method:    http.MethodPut,
			proxyPath: "/api/v1/system/ssh",
			want:      []string{authz.PermMinerUpdatePassword},
		},
		{
			name:      "unknown mutating endpoints do not fall through to read",
			method:    http.MethodPost,
			proxyPath: "/api/v1/new-setting",
			want:      []string{authz.PermMinerUpdatePassword},
		},
		{
			name:      "a write under the logs prefix is not authorized as a mere log download",
			method:    http.MethodPost,
			proxyPath: "/api/v1/system/logs",
			want:      []string{authz.PermMinerUpdatePassword},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := permissionsFor(tt.method, tt.proxyPath); !slices.Equal(got, tt.want) {
				t.Fatalf("permissionsFor(%q, %q) = %v, want %v", tt.method, tt.proxyPath, got, tt.want)
			}
		})
	}
}

func TestProxyPathFor(t *testing.T) {
	canonical := []string{"system/tag", "pools/1", "system/logs", "mining/target", "system/update"}
	for _, rest := range canonical {
		t.Run("accepts "+rest, func(t *testing.T) {
			got, ok := proxyPathFor("v1", http.MethodGet, rest)
			if !ok {
				t.Fatalf("proxyPathFor(%q, %q, %q) ok=false, want true", "v1", http.MethodGet, rest)
			}
			if want := "/api/v1/" + rest; got != want {
				t.Fatalf("proxyPathFor(%q, %q, %q) = %q, want %q", "v1", http.MethodGet, rest, got, want)
			}
		})
	}

	t.Run("accepts the MDK v2 system info probe", func(t *testing.T) {
		got, ok := proxyPathFor("v2", http.MethodGet, "system/info")
		if !ok {
			t.Fatal("proxyPathFor(v2 system info) ok=false, want true")
		}
		if want := "/api/v2/system/info"; got != want {
			t.Fatalf("proxyPathFor(v2 system info) = %q, want %q", got, want)
		}
	})

	unsupported := []struct {
		name       string
		apiVersion string
		method     string
		rest       string
	}{
		{name: "unknown API version", apiVersion: "v3", method: http.MethodGet, rest: "system/info"},
		{name: "unregistered v2 endpoint", apiVersion: "v2", method: http.MethodGet, rest: "firmware"},
		{name: "v2 probe mutation", apiVersion: "v2", method: http.MethodPost, rest: "system/info"},
	}
	for _, tt := range unsupported {
		t.Run("rejects "+tt.name, func(t *testing.T) {
			if _, ok := proxyPathFor(tt.apiVersion, tt.method, tt.rest); ok {
				t.Fatalf("proxyPathFor(%q, %q, %q) ok=true, want false", tt.apiVersion, tt.method, tt.rest)
			}
		})
	}

	// Non-canonical inputs that path.Clean would rewrite — the dot-segment
	// smuggling, encoded-slash, double-slash, and trailing-slash bypasses.
	nonCanonical := []string{
		"system/logs/../../pools", // decoded ..%2f..%2f traversal
		"../../auth/password",     // escape the /api/v1 prefix
		"system//tag",             // empty segment
		"auth/password/",          // trailing slash evades exact-match blocklist
		"pools/../system/reboot",  // privilege downgrade attempt
		"auth/password?x",         // decoded %3F: "?" would split into a query upstream, evading the block
		"auth/change-password#x",  // decoded %23: "#" would split into a fragment upstream
		"pools?x",                 // query delimiter smuggled into a write path
		"system%2Freboot",         // double-encoded slash (%252F -> %2F) the miner may decode to "/"
		"system%2ereboot",         // residual encoded dot
		"system%252Freboot",       // triple-encoded slash
	}
	for _, rest := range nonCanonical {
		t.Run("rejects "+rest, func(t *testing.T) {
			if _, ok := proxyPathFor("v1", http.MethodGet, rest); ok {
				t.Fatalf("proxyPathFor(%q, %q, %q) ok=true, want false (non-canonical path)", "v1", http.MethodGet, rest)
			}
		})
	}
}

func TestRequiresMinerAuthentication(t *testing.T) {
	if requiresMinerAuthentication("/api/v2/system/info") {
		t.Fatal("MDK v2 system info probe must not invoke the MDK v1 login flow")
	}
	if !requiresMinerAuthentication("/api/v1/system") {
		t.Fatal("MDK v1 endpoints must retain miner authentication")
	}
}

// TestV2AllowlistDrivesProxyAndAuth pins both the allowlist decision
// (proxyPathFor) and the auth decision (requiresMinerAuthentication) to the
// single v2AllowlistedEndpoints table, so the two can't drift apart: every
// allowlisted entry must be accepted by proxyPathFor and its auth requirement
// must match requiresMinerAuthentication.
func TestV2AllowlistDrivesProxyAndAuth(t *testing.T) {
	for _, e := range v2AllowlistedEndpoints {
		t.Run(e.method+" "+e.rest, func(t *testing.T) {
			got, ok := proxyPathFor("v2", e.method, e.rest)
			if !ok {
				t.Fatalf("proxyPathFor(v2, %q, %q) ok=false, want true", e.method, e.rest)
			}
			if got != e.proxyPath() {
				t.Fatalf("proxyPathFor(v2, %q, %q) = %q, want %q", e.method, e.rest, got, e.proxyPath())
			}
			if requiresMinerAuthentication(got) != e.requiresAuth {
				t.Fatalf("requiresMinerAuthentication(%q) = %v, want %v",
					got, requiresMinerAuthentication(got), e.requiresAuth)
			}
		})
	}
}

// TestForwardV2ProbeSkipsMinerLoginOn401 asserts a v2 system/info probe acquires
// no miner token: it performs no login, forwards no Authorization header, caches
// no token, and (because the endpoint is unauthenticated) is never eligible for
// ServeHTTP's 401 -> clearToken -> forceLogin retry — a 401 passes straight
// through. The upstream is mocked to return 401 for the probe.
func TestForwardV2ProbeSkipsMinerLoginOn401(t *testing.T) {
	var loginHits int32
	var sawAuthHeader int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			atomic.AddInt32(&loginHits, 1)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(loginResponse{AccessToken: "tok"})
		case "/api/v2/system/info":
			if r.Header.Get("Authorization") != "" {
				atomic.AddInt32(&sawAuthHeader, 1)
			}
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	h := newForwardTestHandler(t)
	// Credentials ARE present, to prove the probe skips login by policy, not for
	// lack of a password.
	pwEnc, err := h.encryptService.Encrypt([]byte("miner-pw"))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	target := proxyTarget{dispatch: dispatchProxyOperation,
		deviceIdentifier: "dev-1",
		baseURL:          srv.URL,
		passwordEnc:      sql.NullString{String: pwEnc, Valid: true},
	}
	src := httptest.NewRequest(http.MethodGet, "/miners/dev-1/api/v2/system/info", nil)

	resp, err := h.forward(context.Background(), src, target, "/api/v2/system/info", nil, false)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 passed through untouched", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&loginHits); n != 0 {
		t.Fatalf("v2 probe attempted %d miner logins, want 0", n)
	}
	if n := atomic.LoadInt32(&sawAuthHeader); n != 0 {
		t.Fatal("v2 probe forwarded an Authorization header, want none")
	}
	if _, cached := h.lookupToken(target.cacheKey()); cached {
		t.Fatal("v2 probe cached a miner token, want none acquired")
	}
	// The auth gate that guards ServeHTTP's 401 retry must reject this endpoint.
	if requiresMinerAuthentication("/api/v2/system/info") {
		t.Fatal("v2 system/info must not require miner auth (so no 401 retry runs)")
	}
}

// TestForwardV1RunsForceLoginRetryOn401 exercises the building blocks of
// ServeHTTP's v1 401 retry against a mocked upstream that rejects the first
// (stale-token) request and accepts the forced-login retry: an initial forward
// logs in and is 401'd, then clearToken + forward(forceLogin=true) mints a fresh
// token and succeeds. Two logins must occur, and the retry must present the new
// token.
func TestForwardV1RunsForceLoginRetryOn401(t *testing.T) {
	var loginHits int32
	var systemHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			n := atomic.AddInt32(&loginHits, 1)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(loginResponse{AccessToken: fmt.Sprintf("tok-%d", n)})
		case "/api/v1/system":
			n := atomic.AddInt32(&systemHits, 1)
			if n == 1 {
				if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
					t.Errorf("first call Authorization = %q, want Bearer tok-1", got)
				}
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tok-2" {
				t.Errorf("retry Authorization = %q, want Bearer tok-2", got)
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	h := newForwardTestHandler(t)
	pwEnc, err := h.encryptService.Encrypt([]byte("miner-pw"))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	target := proxyTarget{dispatch: dispatchProxyOperation,
		deviceIdentifier: "dev-1",
		baseURL:          srv.URL,
		passwordEnc:      sql.NullString{String: pwEnc, Valid: true},
	}
	src := httptest.NewRequest(http.MethodGet, "/miners/dev-1/api/v1/system", nil)

	if !requiresMinerAuthentication("/api/v1/system") {
		t.Fatal("v1 endpoints must require miner auth")
	}

	// Initial forward: logs in (tok-1), miner returns 401.
	resp, err := h.forward(context.Background(), src, target, "/api/v1/system", nil, false)
	if err != nil {
		t.Fatalf("initial forward: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("initial forward status = %d, want 401", resp.StatusCode)
	}

	// Mirror ServeHTTP's retry: drop the cached token and force a fresh login.
	h.clearToken(target.cacheKey())
	resp, err = h.forward(context.Background(), src, target, "/api/v1/system", nil, true)
	if err != nil {
		t.Fatalf("retry forward: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retry forward status = %d, want 200", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&loginHits); n != 2 {
		t.Fatalf("miner login count = %d, want 2 (initial + forced retry)", n)
	}
}

func TestCopyRequestHeadersStripsAuthAndCookie(t *testing.T) {
	src := http.Header{}
	src.Add("Content-Type", "application/json")
	src.Add("Authorization", "Bearer caller-token")
	src.Add("Cookie", "fleet_session=secret")
	src.Add("Connection", "keep-alive")

	dst := http.Header{}
	copyRequestHeaders(dst, src)

	if got := dst.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	for _, h := range []string{"Authorization", "Cookie", "Connection"} {
		if v := dst.Values(h); len(v) != 0 {
			t.Fatalf("%s forwarded to miner = %v, want stripped", h, v)
		}
	}
}

func TestIsUnproxyableEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		proxyPath string
		want      bool
	}{
		{name: "v2 pool create blocked", method: http.MethodPost, proxyPath: "/api/v2/pools", want: true},
		{name: "v2 pool edit blocked", method: http.MethodPut, proxyPath: "/api/v2/pools/42", want: true},
		{name: "v2 pool delete blocked", method: http.MethodDelete, proxyPath: "/api/v2/pools/42", want: true},
		{name: "v2 pool read allowed", method: http.MethodGet, proxyPath: "/api/v2/pools", want: false},
		{name: "password set blocked", method: http.MethodPut, proxyPath: "/api/v1/auth/password", want: true},
		{name: "change password blocked", method: http.MethodPut, proxyPath: "/api/v1/auth/change-password", want: true},
		{name: "pool write blocked (put)", method: http.MethodPut, proxyPath: "/api/v1/pools/1", want: true},
		{name: "pool write blocked (post)", method: http.MethodPost, proxyPath: "/api/v1/pools", want: true},
		{name: "pool delete blocked", method: http.MethodDelete, proxyPath: "/api/v1/pools/1", want: true},
		{name: "pool read allowed", method: http.MethodGet, proxyPath: "/api/v1/pools", want: false},
		{name: "login allowed", method: http.MethodPost, proxyPath: "/api/v1/auth/login", want: false},
		{name: "refresh allowed", method: http.MethodPost, proxyPath: "/api/v1/auth/refresh", want: false},
		{name: "tag write allowed", method: http.MethodPut, proxyPath: "/api/v1/system/tag", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnproxyableEndpoint(tt.method, tt.proxyPath); got != tt.want {
				t.Fatalf("isUnproxyableEndpoint(%q, %q) = %v, want %v", tt.method, tt.proxyPath, got, tt.want)
			}
		})
	}
}

func TestCacheKeyVariesByEndpoint(t *testing.T) {
	creds := sql.NullString{String: "enc", Valid: true}
	base := proxyTarget{dispatch: dispatchProxyOperation, deviceIdentifier: "device-1", baseURL: "http://10.0.0.42:50051", passwordEnc: creds}
	moved := proxyTarget{dispatch: dispatchProxyOperation, deviceIdentifier: "device-1", baseURL: "http://10.0.0.99:50051", passwordEnc: creds}

	if base.cacheKey() == moved.cacheKey() {
		t.Fatal("cacheKey must differ when the resolved endpoint changes for the same device")
	}

	// A token cached for the original endpoint must not be reused after the
	// address moves, so credentials/tokens never cross endpoints.
	h := &Handler{tokens: make(map[string]cachedToken)}
	h.storeToken(base.cacheKey(), "token-for-original")
	if _, ok := h.lookupToken(moved.cacheKey()); ok {
		t.Fatal("token cached for the original endpoint leaked to the moved endpoint")
	}
}

func TestHasCredentials(t *testing.T) {
	tests := []struct {
		name        string
		passwordEnc sql.NullString
		want        bool
	}{
		{name: "valid non-empty password", passwordEnc: sql.NullString{String: "enc", Valid: true}, want: true},
		{name: "valid but empty password", passwordEnc: sql.NullString{String: "", Valid: true}, want: false},
		{name: "null password", passwordEnc: sql.NullString{Valid: false}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := proxyTarget{dispatch: dispatchProxyOperation, passwordEnc: tt.passwordEnc}
			if got := target.hasCredentials(); got != tt.want {
				t.Fatalf("hasCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLookupTokenEvictsExpired(t *testing.T) {
	h := &Handler{tokens: map[string]cachedToken{
		"device-1": {token: "stale", expiresAt: time.Now().Add(-time.Minute)},
	}}

	if token, ok := h.lookupToken("device-1"); ok || token != "" {
		t.Fatalf("lookupToken returned (%q, %v), want expired miss", token, ok)
	}
	if _, exists := h.tokens["device-1"]; exists {
		t.Fatal("expired entry should be deleted on lookup")
	}
}

func TestStoreTokenStaysBounded(t *testing.T) {
	h := &Handler{tokens: make(map[string]cachedToken)}

	for i := range maxCachedTokens + 100 {
		h.storeToken(string(rune(i)), "token")
	}

	if len(h.tokens) > maxCachedTokens {
		t.Fatalf("token cache size = %d, want <= %d", len(h.tokens), maxCachedTokens)
	}
}

func TestMinerHost(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		port    string
		want    string
		wantErr bool
	}{
		{name: "ipv4 default port", ip: "10.0.0.5", port: "", want: "10.0.0.5"},
		{name: "ipv4 zero port treated as default", ip: "10.0.0.5", port: "0", want: "10.0.0.5"},
		{name: "ipv4 explicit port", ip: "10.0.0.5", port: "8080", want: "10.0.0.5:8080"},
		{name: "ipv6 default port is bracketed", ip: "2001:db8::1", port: "", want: "[2001:db8::1]"},
		{name: "ipv6 explicit port is bracketed", ip: "2001:db8::1", port: "8080", want: "[2001:db8::1]:8080"},
		{name: "invalid port", ip: "10.0.0.5", port: "notaport", wantErr: true},
		{name: "out-of-range port", ip: "10.0.0.5", port: "70000", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := minerHost(netip.MustParseAddr(tt.ip), tt.port)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("minerHost(%q, %q) err = nil, want error", tt.ip, tt.port)
				}
				return
			}
			if err != nil {
				t.Fatalf("minerHost(%q, %q) err = %v", tt.ip, tt.port, err)
			}
			if got != tt.want {
				t.Fatalf("minerHost(%q, %q) = %q, want %q", tt.ip, tt.port, got, tt.want)
			}
			// The bracketed host must round-trip through url.URL without the
			// address being mis-parsed as host:port.
			u := url.URL{Scheme: "http", Host: got}
			if parsed, perr := url.Parse(u.String()); perr != nil || parsed.Hostname() == "" {
				t.Fatalf("url.Parse(%q) hostname=%q err=%v", u.String(), parsed.Hostname(), perr)
			}
		})
	}
}

func TestParseRoutableMinerAddr(t *testing.T) {
	allowed := []string{"10.0.0.42", "192.168.1.10", "172.16.5.5", "203.0.113.7", "fc00::1", "2001:db8::1"}
	for _, ip := range allowed {
		t.Run("allows "+ip, func(t *testing.T) {
			if _, err := parseRoutableMinerAddr(ip); err != nil {
				t.Fatalf("parseRoutableMinerAddr(%q) = %v, want nil", ip, err)
			}
		})
	}

	rejected := []string{
		"127.0.0.1",              // loopback
		"::1",                    // loopback v6
		"169.254.169.254",        // link-local / cloud metadata
		"fe80::1",                // link-local v6
		"224.0.0.1",              // multicast
		"0.0.0.0",                // unspecified
		"::ffff:127.0.0.1",       // IPv4-mapped loopback
		"::ffff:169.254.169.254", // IPv4-mapped metadata
		"not-an-ip",              // hostname / non-literal
		"miner.local",            // hostname (no DNS rebinding)
	}
	for _, ip := range rejected {
		t.Run("rejects "+ip, func(t *testing.T) {
			if _, err := parseRoutableMinerAddr(ip); err == nil {
				t.Fatalf("parseRoutableMinerAddr(%q) = nil error, want rejection", ip)
			}
		})
	}
}

func TestIsRenderingNavigation(t *testing.T) {
	tests := []struct {
		dest string
		want bool
	}{
		{dest: "document", want: true},
		{dest: "iframe", want: true},
		{dest: "frame", want: true},
		{dest: "embed", want: true},
		{dest: "object", want: true},
		{dest: "empty", want: false},
		{dest: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.dest, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/miners/m1/api/v1/system/info", nil)
			if tt.dest != "" {
				r.Header.Set("Sec-Fetch-Dest", tt.dest)
			}
			if got := isRenderingNavigation(r); got != tt.want {
				t.Fatalf("isRenderingNavigation(Sec-Fetch-Dest=%q) = %v, want %v", tt.dest, got, tt.want)
			}
		})
	}
}

func TestIsDisallowedCrossOriginWrite(t *testing.T) {
	req := func(method string, headers map[string]string) *http.Request {
		r := httptest.NewRequest(method, "https://fleet.example/miners/m1/api/v1/mining/stop", nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}

	tests := []struct {
		name    string
		request *http.Request
		want    bool
	}{
		{"same-origin write allowed", req(http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin"}), false},
		{"same-site write rejected", req(http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-site"}), true},
		{"cross-site write rejected", req(http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site"}), true},
		{"none write rejected", req(http.MethodPost, map[string]string{"Sec-Fetch-Site": "none"}), true},
		{"non-browser write allowed (no fetch metadata)", req(http.MethodPut, nil), false},
		{"matching Origin fallback allowed", req(http.MethodPost, map[string]string{"Origin": "https://fleet.example"}), false},
		{"mismatched Origin fallback rejected", req(http.MethodPost, map[string]string{"Origin": "https://evil.example"}), true},
		{"cross-site read is not a write", req(http.MethodGet, map[string]string{"Sec-Fetch-Site": "cross-site"}), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDisallowedCrossOriginWrite(tt.request); got != tt.want {
				t.Fatalf("isDisallowedCrossOriginWrite = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetResponseHardeningHeaders(t *testing.T) {
	h := http.Header{}
	// A hostile miner trying to relax the policy must not win.
	h.Set("Content-Security-Policy", "default-src *")
	h.Set("X-Content-Type-Options", "")

	setResponseHardeningHeaders(h)

	if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := h.Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
		t.Fatalf("Content-Security-Policy = %q, want sandbox policy", got)
	}
	if got := h.Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := h.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestCopyResponseHeadersUsesAllowlist(t *testing.T) {
	src := http.Header{}
	// Allowlisted content metadata should pass through.
	src.Add("Content-Type", "application/json")
	src.Add("Content-Disposition", "attachment; filename=logs.csv")
	// Origin-affecting / cache / unknown headers a miner must not set on the
	// Fleet origin must be dropped (default-deny). Caching headers are dropped
	// so authenticated per-user responses are never browser-cached.
	src.Add("Set-Cookie", "miner_session=unsafe; Path=/")
	src.Add("Clear-Site-Data", `"*"`)
	src.Add("Strict-Transport-Security", "max-age=63072000")
	src.Add("Content-Security-Policy", "default-src *")
	src.Add("X-Frame-Options", "ALLOWALL")
	src.Add("Cache-Control", "public, max-age=86400")
	src.Add("Expires", "Tue, 01 Jan 2030 00:00:00 GMT")
	src.Add("ETag", `"abc"`)
	src.Add("Last-Modified", "Tue, 01 Jan 2030 00:00:00 GMT")
	src.Add("X-Surprise-Header", "anything")

	dst := http.Header{}
	copyResponseHeaders(dst, src)

	for _, h := range []string{"Content-Type", "Content-Disposition"} {
		if dst.Get(h) == "" {
			t.Fatalf("allowlisted header %q was dropped", h)
		}
	}
	for _, h := range []string{
		"Set-Cookie", "Clear-Site-Data", "Strict-Transport-Security",
		"Content-Security-Policy", "X-Frame-Options", "X-Surprise-Header",
		"Cache-Control", "Expires", "ETag", "Last-Modified",
	} {
		if got := dst.Values(h); len(got) != 0 {
			t.Fatalf("non-allowlisted header %q passed through: %v", h, got)
		}
	}
}

func TestCT1SettingsPaths(t *testing.T) {
	for _, endpoint := range []struct{ method, rest string }{
		{http.MethodGet, "container/map"}, {http.MethodGet, "mining/status"},
		{http.MethodGet, "telemetry/container"}, {http.MethodGet, "pools/diagnostics"},
		{http.MethodGet, "telemetry/shares/history"},
		{http.MethodGet, "hashboards/02:00:00:00:01:01/asics"},
		{http.MethodPut, "mining/target-power"}, {http.MethodPost, "modules/T1-M01/run-in"},
		{http.MethodPost, "power/container/controls/coolant_pump"},
		{http.MethodPost, "power/container/setpoints/dry_cooler_auto_temp_c"},
	} {
		t.Run(endpoint.rest, func(t *testing.T) {
			upstream, ok := proxyPathFor("v2", endpoint.method, endpoint.rest)
			if !ok || requiresMinerAuthentication(upstream) {
				t.Fatalf("CT1 endpoint not proxyable without v1 login: %s", endpoint.rest)
			}
		})
	}
	for _, endpoint := range []struct{ method, rest string }{
		{http.MethodPut, "pools/1"}, {http.MethodPost, "pools"}, {http.MethodDelete, "pools/1"},
		{http.MethodPost, "telemetry/shares/history"}, {http.MethodPut, "telemetry/shares/history"},
		{http.MethodDelete, "telemetry/shares/history"}, {http.MethodGet, "telemetry/shares/history/extra"},
		{http.MethodPost, "system/reboot"}, {http.MethodPost, "power/container/main-circuit-breaker"},
		{http.MethodPost, "power/container/controls/unknown"},
		{http.MethodDelete, "modules/T1-M01/run-in"}, {http.MethodPost, "modules/T1-M01/extra/run-in"},
		{http.MethodPost, "modules/../run-in"}, {http.MethodPost, "modules//run-in"},
		{http.MethodPost, "modules/T1-M01%2fextra/run-in"}, {http.MethodPost, "modules/T1-M01?x/run-in"},
		{http.MethodGet, "hashboards/../asics"}, {http.MethodGet, "hashboards/one/two/asics"},
	} {
		t.Run(endpoint.method+endpoint.rest, func(t *testing.T) {
			if _, ok := proxyPathFor("v2", endpoint.method, endpoint.rest); ok {
				t.Fatalf("unexpectedly allowed %s %s", endpoint.method, endpoint.rest)
			}
		})
	}
}

func TestForwardCT1PoolHistory(t *testing.T) {
	const query = "poolId=2&grouping=SHARE_QUALITY_HISTORY_GROUPING_TOTAL&range.start=2026-09-16T00%3A00%3A00Z&range.end=2026-09-17T00%3A00%3A00Z&bucketSize=BUCKET_SIZE_1H"
	const body = `{"buckets":[{"time":"2026-09-16T00:00:00Z","accepted":"9","rejected":"1","sampleCount":"1"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/telemetry/shares/history" || r.URL.RawQuery != query {
			t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("caller credentials forwarded to CT1")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()

	h := newForwardTestHandler(t)
	target := proxyTarget{dispatch: dispatchProxyOperation, deviceIdentifier: "ct1", baseURL: upstream.URL}
	src := httptest.NewRequest(http.MethodGet, "/miners/ct1/api/v2/telemetry/shares/history?"+query, nil)
	src.Header.Set("Authorization", "Bearer fleet-token")
	src.Header.Set("Cookie", "fleet_session=private")
	path, ok := proxyPathFor("v2", src.Method, "telemetry/shares/history")
	if !ok {
		t.Fatal("pool history must be proxyable")
	}
	resp, err := h.forward(context.Background(), src, target, path, nil, requiresMinerAuthentication(path))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || string(got) != body {
		t.Fatalf("history response: status=%d body=%s error=%v", resp.StatusCode, got, err)
	}
}

func TestForwardCT1RunInWithoutV1Login(t *testing.T) {
	const body = `{"sampleInterval":"10s","tuningSweep":{"dwellDuration":"600s","powerTarget":{"startPowerTargetWatts":1200,"endPowerTargetWatts":1200,"stepPowerTargetWatts":1}}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/modules/T1-M01/run-in" || r.URL.Query().Get("taskId") != "0123456789abcdef0123456789abcdef" {
			t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("caller credentials forwarded to CT1")
		}
		got, err := io.ReadAll(r.Body)
		if err != nil || string(got) != body {
			t.Errorf("request body changed: %s, %v", got, err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()
	h := newForwardTestHandler(t)
	encrypted, err := h.encryptService.Encrypt([]byte("stored-v1-password"))
	if err != nil {
		t.Fatal(err)
	}
	target := proxyTarget{dispatch: dispatchProxyOperation, deviceIdentifier: "ct1", baseURL: upstream.URL, passwordEnc: sql.NullString{String: encrypted, Valid: true}}
	src := httptest.NewRequest(http.MethodPost, "/miners/ct1/api/v2/modules/T1-M01/run-in?taskId=0123456789abcdef0123456789abcdef", nil)
	src.Header.Set("Cookie", "fleet_session=private")
	resp, err := h.forward(context.Background(), src, target, "/api/v2/modules/T1-M01/run-in", []byte(body), false)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// Each mutation is explicitly allowlisted: related methods and child paths must
// not gain access, and CT1 commands must never invoke the MDK v1 login flow.
func TestCT1ModuleActions(t *testing.T) {
	const scope = `"scope":{"identifiers":["02:00:00:00:01:01","02:00:00:00:01:02"]}`
	const boardResults = `{"results":[{"macAddress":"02:00:00:00:01:01","applied":true},{"macAddress":"02:00:00:00:01:02","applied":false,"message":"disconnected"}]}`
	for _, endpoint := range []struct{ rest, body, response string }{
		{"mining/start", "{}", "{}"},
		{"mining/stop", "{}", "{}"},
		{"mining/scoped/start", "{" + scope + "}", boardResults},
		{"mining/scoped/stop", "{" + scope + "}", boardResults},
		{"hashboards/identify-led", "{" + scope + `,"mode":"HASHBOARD_IDENTIFY_LED_MODE_BLINK"}`, `{"boardsUpdated":2}`},
		{"mining/isolation", "{" + scope + `,"isolated":true}`, boardResults},
		{"mining/isolation", "{" + scope + `,"isolated":false}`, boardResults},
	} {
		rest, body, response := endpoint.rest, endpoint.body, endpoint.response
		t.Run(rest, func(t *testing.T) {
			proxyPath, ok := proxyPathFor("v2", http.MethodPost, rest)
			if !ok || proxyPath != "/api/v2/"+rest || requiresMinerAuthentication(proxyPath) {
				t.Fatalf("command must be explicitly proxyable without v1 login: %s", rest)
			}
			for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
				if _, ok := proxyPathFor("v2", method, rest); ok {
					t.Errorf("unexpectedly allowed %s %s", method, rest)
				}
			}
			if _, ok := proxyPathFor("v2", http.MethodPost, rest+"/extra"); ok {
				t.Error("unexpectedly allowed child endpoint")
			}
			var hits atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != proxyPath {
					t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("caller credentials forwarded to CT1")
				}
				got, err := io.ReadAll(r.Body)
				if err != nil || string(got) != body {
					t.Errorf("request body changed: %s, %v", got, err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, response)
			}))
			defer upstream.Close()
			h := newForwardTestHandler(t)
			encrypted, err := h.encryptService.Encrypt([]byte("stored-v1-password"))
			if err != nil {
				t.Fatal(err)
			}
			target := proxyTarget{dispatch: dispatchProxyOperation, deviceIdentifier: "ct1", baseURL: upstream.URL, passwordEnc: sql.NullString{String: encrypted, Valid: true}}
			src := httptest.NewRequest(http.MethodPost, "/miners/ct1"+proxyPath, nil)
			src.Header.Set("Authorization", "Bearer fleet-token")
			src.Header.Set("Cookie", "fleet_session=private")
			resp, err := h.forward(context.Background(), src, target, proxyPath, []byte(body), requiresMinerAuthentication(proxyPath))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			got, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != http.StatusOK || string(got) != response {
				t.Fatalf("command response changed: status=%d body=%s error=%v", resp.StatusCode, got, err)
			}
			if hits.Load() != 1 {
				t.Fatalf("expected one command and no login, got %d requests", hits.Load())
			}
		})
	}
}

func TestCT1ModuleActionPermissionGates(t *testing.T) {
	siteID, otherSiteID := int64(42), int64(43)
	for _, endpoint := range []struct {
		rest     string
		required []string
	}{
		{"mining/scoped/start", []string{authz.PermMinerStartMining}},
		{"mining/scoped/stop", []string{authz.PermMinerStopMining}},
		{"hashboards/identify-led", []string{authz.PermMinerBlinkLED}},
		{"mining/isolation", []string{authz.PermMinerStartMining, authz.PermMinerStopMining}},
	} {
		t.Run(endpoint.rest, func(t *testing.T) {
			check := func(permissions []string, grantedSite int64) bool {
				ctx := authn.SetInfo(context.Background(), &session.Info{UserID: 1, OrganizationID: 1})
				ctx = middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &grantedSite, Permissions: permissions}}))
				for _, permission := range permissionsFor(http.MethodPost, "/api/v2/"+endpoint.rest) {
					if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: &siteID}); err != nil {
						if httpStatusForError(err) != http.StatusForbidden {
							t.Fatalf("expected forbidden, got %v", err)
						}
						return false
					}
				}
				return true
			}
			if !check(endpoint.required, siteID) {
				t.Error("required permissions should authorize the command at their assigned site")
			}
			if check(endpoint.required, otherSiteID) {
				t.Error("permissions at a different site must not authorize the command")
			}
			if check([]string{authz.PermMinerRead, authz.PermMinerUpdatePassword}, siteID) {
				t.Error("unrelated miner permissions must not authorize the command")
			}
			for i, missing := range endpoint.required {
				remaining := append(slices.Clone(endpoint.required[:i]), endpoint.required[i+1:]...)
				if check(remaining, siteID) {
					t.Errorf("command allowed without %s", missing)
				}
			}
		})
	}
}

func TestAccountSelectionDeniesRawMinerProxy(t *testing.T) {
	h := newForwardTestHandler(t)
	h.sessionService = session.NewService(session.Config{CookieName: "fleet_session"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/miners/miner/api/v1/pools", nil)
	req.Header.Set("X-Fleet-Account-Id", "d9a46a3a-1f64-4bd5-94ea-c0d241be9df0")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("account-selected proxy status = %d, want 403", response.Code)
	}
}

// Forwarding unit fixtures omit the SQL target resolver; production targets
// acquire binding locks before either kind of transport call.
func dispatchProxyOperation(ctx context.Context, mutation bool, call func(context.Context) error) error {
	if mutation {
		return devicewrite.Dispatch(ctx, call)
	}
	return devicewrite.Observe(ctx, call)
}
