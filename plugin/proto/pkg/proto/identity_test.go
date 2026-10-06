package proto

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestBoundClientDoesNotFollowRedirects(t *testing.T) {
	for _, path := range []string{pairingInfoPath, "/api/v1/auth/login", "/api/v1/system/reboot", "/api/v1/auth/change-password", "/api/v1/system/update"} {
		t.Run(path, func(t *testing.T) {
			var redirectedRequests atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				redirectedRequests.Add(1)
				_, _ = w.Write([]byte(`{"cb_sn":"serial","mac":"AA:BB:CC:DD:EE:01"}`))
			}))
			t.Cleanup(target.Close)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == path {
					http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
					return
				}
				switch r.URL.Path {
				case pairingInfoPath:
					_, _ = w.Write([]byte(`{"cb_sn":"serial","mac":"AA:BB:CC:DD:EE:01"}`))
				case "/api/v1/auth/login":
					_, _ = w.Write([]byte(`{"access_token":"token"}`))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			parsed, err := url.Parse(server.URL)
			require.NoError(t, err)
			host, rawPort, err := net.SplitHostPort(parsed.Host)
			require.NoError(t, err)
			port, err := strconv.ParseInt(rawPort, 10, 32)
			require.NoError(t, err)
			client, err := NewClient(host, int32(port), "http")
			require.NoError(t, err)
			client.BindDeviceIdentity("serial", "AA:BB:CC:DD:EE:01")
			require.NoError(t, client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "password"}))
			switch path {
			case "/api/v1/auth/change-password":
				err = client.ChangePassword(context.Background(), "password", "new-password")
			case "/api/v1/system/update":
				err = client.UploadFirmware(context.Background(), sdk.FirmwareFile{Filename: "firmware.bin", Size: 1, Reader: strings.NewReader("x")})
			default:
				err = client.Reboot(context.Background())
			}
			require.Error(t, err, fmt.Sprintf("redirect on %s must fail", path))
			require.Zero(t, redirectedRequests.Load(), "bound identity, credentials, and commands must stay at the original endpoint")
		})
	}
}
