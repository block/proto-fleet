package device

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoredPoolReadbackObservesFirmwareAndAllSlots(t *testing.T) {
	var mu sync.Mutex
	slots := []map[string]any{
		{"url": "stratum+tcp://pool.example:3333", "user": "customer.worker", "pass": "stored-private"},
		{"url": "", "user": "", "pass": ""},
		{"url": "stratum+ssl://backup.example:443", "user": "backup.worker"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/cgi-bin/get_miner_conf.cgi", r.URL.Path)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="0123456789abcdef", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if err := json.NewEncoder(w).Encode(map[string]any{"pools": slots}); err != nil {
			t.Errorf("encode fixture: %v", err)
		}
	}))
	defer server.Close()
	address, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(address.Port(), 10, 32)
	require.NoError(t, err)
	client, err := antminer.NewClient(address.Hostname(), 4028, int32(port), "http") // #nosec G115 -- parsed into 32 bits above.
	require.NoError(t, err)
	require.NoError(t, client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "login-password"}))
	device := &Device{client: client}
	observed, err := device.GetStoredMiningPools(t.Context())
	require.NoError(t, err)
	require.Len(t, observed, 3)
	require.Equal(t, int32(1), observed[1].Priority)
	require.Empty(t, observed[1].URL, "disabled slots must remain visible")
	require.NotNil(t, observed[1].Password)
	require.Empty(t, *observed[1].Password)
	require.Nil(t, observed[2].Password, "omitted firmware password cannot prove empty")
	require.NotNil(t, observed[0].Password)
	require.Equal(t, "stored-private", *observed[0].Password)
	mu.Lock()
	slots[0]["pass"] = "firmware-changed-private"
	slots[2]["pass"] = "********"
	mu.Unlock()
	observed, err = device.GetStoredMiningPools(t.Context())
	require.NoError(t, err)
	require.Equal(t, "firmware-changed-private", *observed[0].Password, "readback is fresh actual storage")
	require.Nil(t, observed[2].Password, "masked firmware credentials remain unproven")
	mu.Lock()
	delete(slots[1], "url")
	mu.Unlock()
	observed, err = device.GetStoredMiningPools(t.Context())
	require.Error(t, err)
	require.Nil(t, observed, "omitted destination cannot prove disabled slot")
	mu.Lock()
	slots[1]["url"] = ""
	mu.Unlock()
	mu.Lock()
	slots = append(slots, map[string]any{"url": "stratum+tcp://extra.example:3333", "user": "old.worker", "pass": "extra-private"})
	mu.Unlock()
	observed, err = device.GetStoredMiningPools(t.Context())
	require.Error(t, err)
	require.Nil(t, observed, "extra slots must not be silently truncated")
	require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), "private")
}

func TestStoredPoolReadbackRejectsMissingPoolsAndPreservesExplicitEmpty(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		errorCode sdk.ErrorCode
		status    int
		count     int
	}{
		{name: "omitted pools", body: `{}`, errorCode: sdk.ErrCodeInvalidConfig},
		{name: "null pools", body: `{"pools":null}`, errorCode: sdk.ErrCodeInvalidConfig},
		{name: "temporary read failure", body: `private error must not escape`, status: http.StatusServiceUnavailable, errorCode: sdk.ErrCodeDeviceUnavailable},
		{name: "cached credentials rejected", body: `private error must not escape`, status: http.StatusUnauthorized, errorCode: sdk.ErrCodeAuthenticationFailed},
		{name: "explicit empty pools", body: `{"pools":[]}`},
		{name: "missing credential", body: `{"pools":[{"url":"stratum+tcp://pool.example:3333","user":"worker"}]}`, count: 1},
		{name: "null credential", body: `{"pools":[{"url":"stratum+tcp://pool.example:3333","user":"worker","pass":null}]}`, count: 1},
		{name: "masked credential", body: `{"pools":[{"url":"stratum+tcp://pool.example:3333","user":"worker","pass":"********"}]}`, count: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/cgi-bin/get_miner_conf.cgi", r.URL.Path)
				if r.Header.Get("Authorization") == "" {
					w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="0123456789abcdef", algorithm=MD5, qop="auth"`)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, err := w.Write([]byte(tc.body))
				assert.NoError(t, err)
			}))
			defer server.Close()
			address, err := url.Parse(server.URL)
			require.NoError(t, err)
			port, err := strconv.ParseInt(address.Port(), 10, 32)
			require.NoError(t, err)
			client, err := antminer.NewClient(address.Hostname(), 4028, int32(port), "http") // #nosec G115 -- parsed into 32 bits above.
			require.NoError(t, err)
			require.NoError(t, client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "login-password"}))
			device := &Device{client: client}
			pools, err := device.GetStoredMiningPools(t.Context())
			if tc.errorCode != "" {
				require.Error(t, err)
				require.Nil(t, pools, "incomplete response must not look like verified empty storage")
				var sdkErr sdk.SDKError
				require.ErrorAs(t, err, &sdkErr)
				require.Equal(t, tc.errorCode, sdkErr.Code)
				require.NotContains(t, fmt.Sprintf("%v %+v %#v", err, err, err), "private error")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, pools, "explicit empty list proves empty storage")
			require.Len(t, pools, tc.count)
			if tc.count != 0 {
				require.Nil(t, pools[0].Password, "missing or masked credentials must remain unproven")
			}
		})
	}
}
