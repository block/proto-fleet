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
