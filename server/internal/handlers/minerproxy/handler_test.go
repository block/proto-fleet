package minerproxy

import (
	"net/http"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/authz"
)

func TestPermissionFor(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		proxyPath string
		want      string
	}{
		{
			name:      "read endpoints require miner read",
			method:    http.MethodGet,
			proxyPath: "/api/v1/network",
			want:      authz.PermMinerRead,
		},
		{
			name:      "log downloads require log download permission for get",
			method:    http.MethodGet,
			proxyPath: "/api/v1/system/logs",
			want:      authz.PermMinerDownloadLogs,
		},
		{
			name:      "log downloads require log download permission for head",
			method:    http.MethodHead,
			proxyPath: "/api/v1/system/logs",
			want:      authz.PermMinerDownloadLogs,
		},
		{
			name:      "timeseries post is a read-style query",
			method:    http.MethodPost,
			proxyPath: "/api/v1/timeseries",
			want:      authz.PermMinerRead,
		},
		{
			name:      "pools writes require pool update",
			method:    http.MethodPut,
			proxyPath: "/api/v1/pools/1",
			want:      authz.PermMinerUpdatePools,
		},
		{
			name:      "power target writes require power target permission",
			method:    http.MethodPut,
			proxyPath: "/api/v1/mining/target",
			want:      authz.PermMinerSetPowerTarget,
		},
		{
			name:      "firmware writes require firmware update",
			method:    http.MethodPost,
			proxyPath: "/api/v1/system/update",
			want:      authz.PermMinerFirmwareUpdate,
		},
		{
			name:      "psu firmware writes require firmware update",
			method:    http.MethodPost,
			proxyPath: "/api/v1/power-supplies/update",
			want:      authz.PermMinerFirmwareUpdate,
		},
		{
			name:      "tag writes require rename",
			method:    http.MethodPut,
			proxyPath: "/api/v1/system/tag",
			want:      authz.PermMinerRename,
		},
		{
			name:      "security settings writes require password update",
			method:    http.MethodPut,
			proxyPath: "/api/v1/system/ssh",
			want:      authz.PermMinerUpdatePassword,
		},
		{
			name:      "unknown mutating endpoints do not fall through to read",
			method:    http.MethodPost,
			proxyPath: "/api/v1/new-setting",
			want:      authz.PermMinerUpdatePassword,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := permissionFor(tt.method, tt.proxyPath); got != tt.want {
				t.Fatalf("permissionFor(%q, %q) = %q, want %q", tt.method, tt.proxyPath, got, tt.want)
			}
		})
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
