package minerproxy

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
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
		"auth/password?x",         // decoded %3F: "?" would split into a query upstream, evading the block
		"auth/change-password#x",  // decoded %23: "#" would split into a fragment upstream
		"pools?x",                 // query delimiter smuggled into a write path
		"system%2Freboot",         // double-encoded slash (%252F -> %2F) the miner may decode to "/"
		"system%2ereboot",         // residual encoded dot
		"system%252Freboot",       // triple-encoded slash
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
