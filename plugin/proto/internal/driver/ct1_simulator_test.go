package driver

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

// Exercise the real simulator process, native transport, normalization, and REST coexistence.
func TestCT1Simulator(t *testing.T) {
	simulator, err := filepath.Abs("../../../../server/fake-ct1")
	require.NoError(t, err)
	binary := filepath.Join(t.TempDir(), "fake-ct1")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	build.Dir = simulator
	build.Env = append(os.Environ(), "GOWORK=off")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(), "HTTP_PORT=0", "GRPC_PORT=0", "SERVER_ID=ct1-integration", "CT1_DEMO_MODULE_STATES=false")
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	ports := make(chan [2]string, 2)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			for _, kind := range []string{"HTTP", "gRPC"} {
				_, address, ok := strings.Cut(line, kind+" server listening on ")
				if ok {
					_, port, err := net.SplitHostPort(address)
					if err == nil {
						ports <- [2]string{kind, port}
					}
				}
			}
		}
	}()
	addresses := map[string]string{}
	for len(addresses) < 2 {
		select {
		case value := <-ports:
			addresses[value[0]] = value[1]
		case <-time.After(15 * time.Second):
			t.Fatal("simulator did not start both listeners")
		}
	}
	t.Setenv("CT1_WEB_PORT", addresses["HTTP"])
	driver, err := New(0)
	require.NoError(t, err)
	info, err := driver.DiscoverDevice(t.Context(), "127.0.0.1", addresses["gRPC"])
	require.NoError(t, err)
	require.Equal(t, "ct1-integration", info.SerialNumber)
	info, err = driver.PairDevice(t.Context(), info, sdk.SecretBundle{})
	require.NoError(t, err)
	handle, err := driver.NewDevice(t.Context(), "ct1-integration", info, sdk.SecretBundle{})
	require.NoError(t, err)
	defer handle.Device.Close(t.Context())
	metrics, err := handle.Device.Status(t.Context())
	require.NoError(t, err)
	boards := 0
	for _, tank := range metrics.Container.Tanks {
		for _, module := range tank.Modules {
			for _, board := range module.Hashboards {
				require.Equal(t, "occupied", board.Occupancy)
				require.NotNil(t, board.HashrateHS)
				boards++
			}
		}
	}
	require.Equal(t, 288, boards)
	require.NotNil(t, metrics.PowerW)
	require.NotNil(t, metrics.EfficiencyJH)
	// Exercise the same plugin handle used by Fleet controls against native RPC.
	tankMiner, ok := handle.Device.(sdk.DeviceTankMiner)
	require.True(t, ok)
	for _, mining := range []bool{false, true} {
		require.NoError(t, tankMiner.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: &mining}))
		observed, err := handle.Device.Status(t.Context())
		require.NoError(t, err)
		for _, tank := range observed.Container.Tanks {
			for _, module := range tank.Modules {
				expected := sdk.ModuleHashing
				if tank.Position == 1 && !mining {
					expected = sdk.ModuleSleeping
				}
				require.Equal(t, expected, module.State, "tank %d", tank.Position)
			}
		}
	}
	web, ok, err := handle.Device.TryGetWebViewURL(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "http://127.0.0.1:"+addresses["HTTP"], web)
	response, err := http.Get(web + "/api/v2/system/info")
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
}
