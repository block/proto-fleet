package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	telemetrypb "github.com/block/proto-fleet/server/generated/grpc/telemetry/v1"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/plugins/mappers"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Cover the actual simulator -> plugin RPC -> mapper -> node encoder -> ACK
// contract, so growth in firmware fixtures or plugin output cannot go unnoticed.
func TestCT1SimulatorTelemetryUpload(t *testing.T) {
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	build := func(source, target string) {
		cmd := exec.CommandContext(t.Context(), "go", "build", "-o", target, ".")
		cmd.Dir = filepath.Join(root, source)
		workspace := filepath.Join(root, "go.work")
		if source == "server/fake-ct1" {
			workspace = "off" // The simulator is a separate module.
		}
		cmd.Env = append(os.Environ(), "GOWORK="+workspace)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	simulator := filepath.Join(t.TempDir(), "fake-ct1")
	pluginDir := t.TempDir()
	build("server/fake-ct1", simulator)
	build("plugin/proto", filepath.Join(pluginDir, "proto-plugin"))
	for _, scenario := range []string{"healthy", "mixed", "all-board-faults"} {
		t.Run(scenario, func(t *testing.T) {
			grpcPort := startTelemetryCT1Simulator(t, simulator, scenario)
			manager := plugins.NewManager(&plugins.Config{
				Enabled: true, PluginsDir: pluginDir, MaxStartupTimeSeconds: 10,
				ShutdownGracePeriodSeconds: 5, LogLevel: "error",
			})
			t.Cleanup(func() { require.NoError(t, manager.Shutdown(context.Background())) })
			require.NoError(t, manager.LoadPlugins(t.Context()))
			plugin, ok := manager.GetPluginByDriverName("proto")
			require.True(t, ok)
			info, err := plugin.Driver.DiscoverDevice(t.Context(), "127.0.0.1", grpcPort)
			require.NoError(t, err)
			handle, err := plugin.Driver.NewDevice(t.Context(), "ct1-upload", info, sdk.SecretBundle{})
			require.NoError(t, err)
			defer handle.Device.Close(context.Background())
			observed, err := handle.Device.Status(t.Context())
			require.NoError(t, err)
			metrics := mappers.SDKDeviceMetricsToV2(observed)
			original, err := json.Marshal(metrics)
			require.NoError(t, err)
			result, err := telemetryResultFromV2("ct1-upload", metrics, deviceStatusFromSDKHealth(observed.Health))
			require.NoError(t, err)
			require.NoError(t, protovalidate.Validate(result))
			require.LessOrEqual(t, proto.Size(result), maxAckPayloadBytes)
			t.Logf("real CT1 JSON: before=%d uploaded=%d", len(original), len(result.DeviceMetricsJson))
			if scenario != "all-board-faults" {
				// A hard regression check on real output, without transport
				// compaction masking growth in the ordinary simulator scenarios.
				require.LessOrEqual(t, len(original), maxTelemetryDeviceMetricsJSONBytes)
				require.Equal(t, original, result.DeviceMetricsJson)
			} else {
				require.Greater(t, len(original), maxTelemetryDeviceMetricsJSONBytes)
			}
			unchanged, err := json.Marshal(metrics)
			require.NoError(t, err)
			require.Equal(t, original, unchanged, "upload compaction must not mutate the cached observation")

			gateway := &controlFakeGateway{}
			gateway.queue(telemetryCmd(t, validTelemetryRequest()))
			runControlLoopOnce(t, &RunCmd{telemetry: &stubTelemetryFetcher{result: result}}, gateway)
			acks := gateway.acksCopy()
			require.Len(t, acks, 1)
			require.Equal(t, pb.AckCode_ACK_CODE_OK, acks[0].GetCode())
			require.NoError(t, protovalidate.Validate(acks[0]))
			decodedResult := &telemetrypb.FleetNodeTelemetryResult{}
			require.NoError(t, proto.Unmarshal(acks[0].GetPayload(), decodedResult))
			require.NoError(t, protovalidate.Validate(decodedResult))
			var decoded modelsV2.DeviceMetrics
			require.NoError(t, json.Unmarshal(decodedResult.DeviceMetricsJson, &decoded))
			require.Equal(t, metrics.Health, decoded.Health)
			require.Equal(t, metrics.HashrateHS, decoded.HashrateHS)
			require.Equal(t, metrics.PowerW, decoded.PowerW)
			require.Len(t, decoded.Container.Tanks, 6)
			truncated := false
			for ti, tank := range decoded.Container.Tanks {
				require.Len(t, tank.Modules, 16)
				for mi, module := range tank.Modules {
					expected := metrics.Container.Tanks[ti].Modules[mi]
					require.Equal(t, expected.State, module.State)
					require.Equal(t, expected.Metrics, module.Metrics)
					require.Len(t, module.Hashboards, 3)
					if scenario == "all-board-faults" {
						require.Equal(t, sdk.MinerStatusError, module.State)
						if expected.Issues != nil && len(expected.Issues.Items) > 0 {
							require.NotEmpty(t, module.Issues.Items)
							require.Less(t, len(module.Issues.Items), len(expected.Issues.Items))
							require.False(t, module.Issues.Complete)
							require.Empty(t, module.Issues.SourceTimes)
							truncated = true
						}
					}
					for bi, board := range module.Hashboards {
						want := expected.Hashboards[bi]
						want.HealthJSON, want.MetadataJSON = board.HealthJSON, board.MetadataJSON
						require.Equal(t, want, board, "all typed board evidence survives compaction")
					}
				}
			}
			if scenario == "all-board-faults" {
				require.True(t, truncated, "exercise normalized-issue truncation as well as raw diagnostics")
			}
		})
	}
}

func startTelemetryCT1Simulator(t *testing.T, binary, scenario string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(), "HTTP_PORT=0", "GRPC_PORT=0", "SERVER_ID=ct1-upload",
		"CT1_TANKS=6", "CT1_MODULES_PER_TANK=16", "CT1_SLOTS_PER_MODULE=3", "CT1_UNLOCATED_BOARDS=0",
		"CT1_DEMO_MODULE_STATES="+strconv.FormatBool(scenario == "mixed"),
		"CT1_DEMO_ALL_BOARD_FAULTS="+strconv.FormatBool(scenario == "all-board-faults"))
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	ports := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if _, address, ok := strings.Cut(scanner.Text(), "gRPC server listening on "); ok {
				_, port, err := net.SplitHostPort(address)
				if err == nil {
					ports <- port
				}
			}
		}
	}()
	select {
	case port := <-ports:
		return port
	case <-time.After(15 * time.Second):
		t.Fatal("CT1 simulator did not start")
		return ""
	}
}
