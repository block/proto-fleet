package antminer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestClientPoolCredentialsReachActualHTTPStoredConfiguration(t *testing.T) {
	var mu sync.Mutex
	stored := web.MinerConfig{BitmainFanPWM: "73", Pools: []web.Pool{{URL: "stratum+tcp://old.example:3333", Username: "previous.wallet", Password: "old-secret"}}}
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="antminer", nonce="0123456789abcdef", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/cgi-bin/get_miner_conf.cgi":
			if err := json.NewEncoder(w).Encode(stored); err != nil {
				t.Errorf("encode configuration: %v", err)
			}
		case "/cgi-bin/set_miner_conf.cgi":
			if err := json.NewDecoder(r.Body).Decode(&stored); err != nil {
				t.Errorf("decode configuration: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writes++
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	address, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(address.Port(), 10, 32)
	require.NoError(t, err)
	// #nosec G115 -- ParseInt with bitSize 32 above bounds the web port.
	client, err := NewClient(address.Hostname(), 4028, int32(port), "http")
	require.NoError(t, err)
	require.NoError(t, client.SetCredentials(sdk.UsernamePassword{Username: "admin", Password: "login-password"}))
	submitted := []Pool{
		{Priority: 0, URL: "stratum+tcp://primary.example:3333", WorkerName: "primary.wallet", Password: "customer-primary-secret"},
		{Priority: 1, URL: "stratum+ssl://backup.example:443", WorkerName: "backup.wallet", Password: "customer-backup-secret"},
		{Priority: 2, URL: "stratum+tcp://last.example:3333", WorkerName: "last.wallet", Password: ""},
	}
	require.NoError(t, client.UpdatePools(t.Context(), submitted))
	actual, err := client.GetMinerConfig(t.Context())
	require.NoError(t, err)
	require.Len(t, actual.Pools, 3)
	for i, pool := range submitted {
		require.Equal(t, pool.URL, actual.Pools[i].URL)
		require.Equal(t, pool.WorkerName, actual.Pools[i].Username)
		require.Equal(t, pool.Password, actual.Pools[i].Password)
	}
	require.Equal(t, "73", actual.BitmainFanPWM, "unrelated device configuration is preserved")
	mu.Lock()
	actualWrites := writes
	mu.Unlock()
	require.Equal(t, 1, actualWrites)
	require.NoError(t, client.UpdatePools(t.Context(), submitted[:1]))
	actual, err = client.GetMinerConfig(t.Context())
	require.NoError(t, err)
	require.Len(t, actual.Pools, 1, "complete replacement removes previous backups")
	require.Equal(t, submitted[0].Password, actual.Pools[0].Password)
}

func TestPoolCredentialsStayPrivateWhenFormatted(t *testing.T) {
	secret := "customer-secret-never-log"
	pool := Pool{URL: "stratum+tcp://pool.example:3333", WorkerName: "wallet", Password: secret}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		require.NotContains(t, fmt.Sprintf(format, pool), secret)
	}
	encoded, err := json.Marshal(pool)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), secret)
}
