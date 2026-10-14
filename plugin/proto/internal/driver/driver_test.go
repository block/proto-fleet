package driver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPairDevice_MissingCredentialsOrDefiniteRejectionReturnsSDKAuthError(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        int
		body          string
		disconnect    bool
		cancel        bool
		wantAuth      bool
		emptyPassword bool
	}{
		{name: "empty password", emptyPassword: true, wantAuth: true},
		{name: "firmware rejects credentials", status: http.StatusUnauthorized, wantAuth: true},
		{name: "firmware rejects request", status: http.StatusBadRequest},
		{name: "firmware forbids login", status: http.StatusForbidden},
		{name: "firmware unavailable", status: http.StatusServiceUnavailable},
		{name: "malformed login response", status: http.StatusOK, body: "{"},
		{name: "connection ends without login result", disconnect: true},
		{name: "login canceled", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			loginComplete := make(chan struct{})
			var loginCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/pairing/info":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"cb_sn":"proto-serial-1","mac":"00:11:22:33:44:55"}`))
				case "/api/v1/system":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"system-info":{"model":"Proto Rig","manufacturer":"Proto"}}`))
				case "/api/v1/auth/login":
					loginCalls.Add(1)
					if tc.cancel {
						cancel()
						// Wait for PairDevice to observe cancellation before returning an empty HTTP 200.
						<-loginComplete
						return
					}
					if tc.disconnect {
						hijacker, ok := w.(http.Hijacker)
						if !ok {
							t.Error("test server cannot hijack login connection")
							return
						}
						conn, _, err := hijacker.Hijack()
						if err != nil {
							t.Errorf("hijack login: %v", err)
							return
						}
						if err := conn.Close(); err != nil {
							t.Errorf("close login: %v", err)
						}
						return
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			serverURL, err := url.Parse(server.URL)
			require.NoError(t, err)
			host, portText, err := net.SplitHostPort(serverURL.Host)
			require.NoError(t, err)
			port, err := strconv.ParseInt(portText, 10, 32)
			require.NoError(t, err)

			driver, err := New(0)
			require.NoError(t, err)

			password := "wrong"
			if tc.emptyPassword {
				password = ""
			}
			_, err = driver.PairDevice(ctx, sdk.DeviceInfo{
				Host:            host,
				Port:            int32(port),
				URLScheme:       serverURL.Scheme,
				SerialNumber:    "proto-serial-1",
				Model:           "Proto Rig",
				Manufacturer:    "Proto",
				MacAddress:      "00:11:22:33:44:55",
				FirmwareVersion: "1.0.0",
			}, sdk.SecretBundle{
				Kind: sdk.UsernamePassword{Username: "admin", Password: password},
			})
			close(loginComplete)

			require.Error(t, err)
			var sdkErr sdk.SDKError
			gotAuth := errors.As(err, &sdkErr) && sdkErr.Code == sdk.ErrCodeAuthenticationFailed
			require.Equal(t, tc.wantAuth, gotAuth, "only missing credentials or definite firmware rejection permit another password trial: %v", err)
			require.True(t, errors.As(err, &sdkErr))
			if !tc.wantAuth {
				require.Equal(t, sdk.ErrCodeDeviceUnavailable, sdkErr.Code)
			}
			if tc.wantAuth {
				require.Error(t, sdkErr.Err)
				if tc.emptyPassword {
					assert.Contains(t, sdkErr.Err.Error(), "password is required")
					require.Zero(t, loginCalls.Load(), "empty passwords must not reach firmware login")
				} else {
					assert.Contains(t, sdkErr.Err.Error(), "invalid credentials")
				}
			}
			if tc.cancel {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}
