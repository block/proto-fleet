package minerproxy

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/authz"
)

func TestPermissionsFor(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		proxyPath string
		want      []string
	}{
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
			name:      "log download is read-only",
			method:    http.MethodGet,
			proxyPath: "/api/v1/system/logs",
			want:      []string{authz.PermMinerDownloadLogs},
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
			got, ok := proxyPathFor(rest)
			if !ok {
				t.Fatalf("proxyPathFor(%q) ok=false, want true", rest)
			}
			if want := "/api/v1/" + rest; got != want {
				t.Fatalf("proxyPathFor(%q) = %q, want %q", rest, got, want)
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
	}
	for _, rest := range nonCanonical {
		t.Run("rejects "+rest, func(t *testing.T) {
			if _, ok := proxyPathFor(rest); ok {
				t.Fatalf("proxyPathFor(%q) ok=true, want false (non-canonical path)", rest)
			}
		})
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
	base := proxyTarget{deviceIdentifier: "device-1", baseURL: "http://10.0.0.42:50051", passwordEnc: creds}
	moved := proxyTarget{deviceIdentifier: "device-1", baseURL: "http://10.0.0.99:50051", passwordEnc: creds}

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
			target := proxyTarget{passwordEnc: tt.passwordEnc}
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
}

func TestCopyResponseHeadersDropsSetCookie(t *testing.T) {
	src := http.Header{}
	src.Add("Content-Type", "application/json")
	src.Add("Set-Cookie", "miner_session=unsafe; Path=/")

	dst := http.Header{}
	copyResponseHeaders(dst, src)

	if got := dst.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := dst.Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("Set-Cookie values = %v, want none", got)
	}
}
