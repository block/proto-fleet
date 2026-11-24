package device

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/block/proto-fleet/plugin/proto/pkg/proto"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateMiningPoolsDeliversSDKPasswords(t *testing.T) {
	var received []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/pools", r.URL.Path)
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	device := newPoolTestDevice(t, server)
	pools := []sdk.MiningPoolConfig{
		{Priority: 0, URL: "stratum+tcp://primary.example:3333", WorkerName: "account.primary", Password: "primary-password"},
		{Priority: 1, URL: "stratum+tcp://backup.example:3333", WorkerName: "account.backup", Password: "backup-password"},
		{Priority: 2, URL: "stratum+tcp://last.example:3333", WorkerName: "account.last", Password: ""},
	}
	require.NoError(t, device.UpdateMiningPools(t.Context(), pools))
	require.Len(t, received, len(pools))
	for i, pool := range pools {
		raw, present := received[i]["password"]
		require.True(t, present, "empty password must still be delivered")
		var password string
		require.NoError(t, json.Unmarshal(raw, &password))
		assert.Equal(t, pool.Password, password)
	}
}

func newPoolTestDevice(t *testing.T, server *httptest.Server) *Device {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(parsed.Port(), 10, 32)
	require.NoError(t, err)
	client, err := proto.NewClient(parsed.Hostname(), int32(port), "http")
	require.NoError(t, err)
	return &Device{id: "pool-password-test", client: client}
}

func TestUpdateMiningPoolsRedactsFirmwarePasswordEcho(t *testing.T) {
	const secret = "pool-private-password"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"rejected password=` + secret + `"}}`))
	}))
	defer server.Close()
	device := newPoolTestDevice(t, server)
	err := device.UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{{URL: "stratum+tcp://pool.example:3333", WorkerName: "worker", Password: secret}})
	require.Error(t, err)
	assert.EqualError(t, err, "failed to update mining pools: forbidden: access denied")
	assert.NotContains(t, err.Error(), secret)
}
