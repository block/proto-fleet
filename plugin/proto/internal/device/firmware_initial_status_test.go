package device

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestNewDeviceVerifiesFirmwareOnEveryFreshHandle(t *testing.T) {
	firmwareVersion := "2.0.0"
	firmwareReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/pairing/info" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cb_sn":"proto-serial","mac":"aa:bb:cc:dd:ee:ff"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/login":
			_, _ = w.Write([]byte(`{"access_token":"test-token","refresh_token":"r"}`))
		case "/api/v1/mining":
			_, _ = w.Write([]byte(`{"mining-status":{"status":"Mining"}}`))
		case "/api/v1/pools":
			_, _ = w.Write([]byte(`{"pools":[{"id":0,"url":"stratum+tcp://pool.example:3333","user":"worker"}]}`))
		case "/api/v1/telemetry":
			_, _ = w.Write([]byte(`{}`))
		case "/api/v1/system/status":
			_, _ = w.Write([]byte(`{"default_password_active":false}`))
		case "/api/v1/system":
			firmwareReads++
			_, _ = fmt.Fprintf(w, `{"system-info":{"os":{"name":"ProtoOS","version":%q}}}`, firmwareVersion)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	host, portString, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	port, err := strconv.ParseInt(portString, 10, 32)
	require.NoError(t, err)

	// Fleet Node repeatedly seeds fresh handles with the last stored version.
	// Each handle must read the current firmware rather than trust that hint.
	for i, version := range []string{"2.0.0", "3.0.0", "3.0.0"} {
		firmwareVersion = version
		dev, err := New(t.Context(), "node-miner", sdk.DeviceInfo{
			Host: host, Port: int32(port), URLScheme: "http", FirmwareVersion: "1.0.0",
		}, sdk.UsernamePassword{Username: "admin", Password: "proto"}, SetStatusTTL(0))
		require.NoError(t, err)
		for range 2 {
			metrics, err := dev.Status(t.Context())
			require.NoError(t, err)
			require.Equal(t, version, metrics.FirmwareVersion)
		}
		require.Equal(t, i+1, firmwareReads, "later polls on the same handle must remain throttled")
		require.NoError(t, dev.Close(context.Background()))
	}
}
