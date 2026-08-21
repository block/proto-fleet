package driver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPairDevice_AuthenticationFailureReturnsSDKAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pairing/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cb_sn":"proto-serial-1","mac":"00:11:22:33:44:55"}`))
		case "/api/v1/system":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"system-info":{"model":"Proto Rig","manufacturer":"Proto"}}`))
		case "/api/v1/auth/login":
			w.WriteHeader(http.StatusUnauthorized)
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

	_, err = driver.PairDevice(context.Background(), sdk.DeviceInfo{
		Host:            host,
		Port:            int32(port),
		URLScheme:       serverURL.Scheme,
		SerialNumber:    "proto-serial-1",
		Model:           "Proto Rig",
		Manufacturer:    "Proto",
		MacAddress:      "00:11:22:33:44:55",
		FirmwareVersion: "1.0.0",
	}, sdk.SecretBundle{
		Kind: sdk.UsernamePassword{Username: "admin", Password: "wrong"},
	})

	require.Error(t, err)
	var sdkErr sdk.SDKError
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, sdk.ErrCodeAuthenticationFailed, sdkErr.Code)
	require.Error(t, sdkErr.Err)
	assert.Contains(t, sdkErr.Err.Error(), "invalid credentials")
}
