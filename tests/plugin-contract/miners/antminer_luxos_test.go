package miners_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/tests/plugin-contract/harness"
	"github.com/block/proto-fleet/tests/plugin-contract/miners"
	"github.com/block/proto-fleet/tests/plugin-contract/mockapi"
)

// This suite exercises the real ASIC-rs LuxOS backend against a synthetic RPC
// endpoint. It verifies addpool serialization, not acceptance by live firmware.
func TestAntminerLuxOS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping contract test in short mode")
	}
	mock := startLuxPoolRPC(t)
	driver := harness.StartAsicrsWithConfig(t, `plugin:
  log_level: debug
  discovery_timeout_seconds: 10
  telemetry_cache_ttl_seconds: 0

miners:
  antminer:
    stock:
      enabled: false
    luxos:
      enabled: true
`)
	ctx := miners.TestCtx(t)
	_, err := driver.Handshake(ctx)
	require.NoError(t, err)
	info, err := driver.DiscoverDevice(ctx, "127.0.0.1", "80")
	require.NoError(t, err)
	require.Equal(t, "LuxOS", info.Manufacturer)
	result, err := driver.NewDevice(ctx, "test-antminer-luxos-pools", info, miners.TestSecret())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, result.Device.Close(ctx))
	})

	t.Run("addpool_preserves_passwords", func(t *testing.T) {
		submitted := []sdk.MiningPoolConfig{
			{Priority: 2, URL: "stratum+tcp://last.example:3333", WorkerName: "last.wallet", Password: ""},
			{Priority: 0, URL: "stratum+tcp://primary.example:3333", WorkerName: "primary.wallet", Password: "primary-customer-secret"},
			{Priority: 1, URL: "stratum+tcp://backup.example:3333", WorkerName: "backup.wallet", Password: "backup-customer-secret"},
		}
		require.NoError(t, result.Device.UpdateMiningPools(miners.TestCtx(t), submitted))
		require.Equal(t, []string{
			"stratum+tcp://primary.example:3333,primary.wallet,primary-customer-secret,0",
			"stratum+tcp://backup.example:3333,backup.wallet,backup-customer-secret,0",
			"stratum+tcp://last.example:3333,last.wallet,,0",
		}, mock.addPoolParameters(), "the real cgminer-style request must include an explicit empty password without substituting x")
	})
}

type luxPoolRPC struct {
	mu         sync.Mutex
	addPools   []string
	unexpected []string
}

func (s *luxPoolRPC) addPoolParameters() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.addPools...)
}

func startLuxPoolRPC(t *testing.T) *luxPoolRPC {
	t.Helper()
	listener := mockapi.ListenWithRetry(t, "127.0.0.1:4028")
	s := &luxPoolRPC{}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				var req struct {
					Command   string `json:"command"`
					Parameter string `json:"parameter"`
				}
				if err := json.NewDecoder(conn).Decode(&req); err != nil {
					return
				}
				response := map[string]any{"STATUS": []any{map[string]string{"STATUS": "S", "Msg": "ok"}}}
				switch req.Command {
				case "version":
					response["VERSION"] = []any{map[string]string{"LUXMINER": "2025.4.8", "Miner": "2025.4.8", "API": "3.7", "Type": "Antminer S19k Pro"}}
				case "groups":
					response["GROUPS"] = []any{}
				case "pools":
					response["POOLS"] = []any{}
				case "addpool":
					s.mu.Lock()
					s.addPools = append(s.addPools, req.Parameter)
					s.mu.Unlock()
				case "devdetails":
					response["DEVDETAILS"] = []any{}
				case "addgroup", "config", "stats", "summary", "events", "fans", "power", "profiles", "temps", "devs", "voltageget", "healthchipget":
					// Empty telemetry responses suffice for the synthetic device's
					// capability probe; only the pool-write transport is qualified.
				default:
					response["STATUS"] = []any{map[string]string{"STATUS": "E", "Msg": "unsupported synthetic command"}}
					s.mu.Lock()
					s.unexpected = append(s.unexpected, req.Command)
					s.mu.Unlock()
				}
				_ = json.NewEncoder(conn).Encode(response)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		workers.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		require.Empty(t, s.unexpected, "mock received unexpected RPC commands")
	})
	return s
}
