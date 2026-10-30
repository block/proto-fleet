package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func fullLoadConfig() ContainerConfig {
	return ContainerConfig{Tanks: 6, ModulesPerTank: 16, SlotsPerModule: 3, AsicsPerBoard: 312, Seed: 1}
}

func TestLoadInventoryAndControllerIdentities(t *testing.T) {
	macs, serials, modules, psus := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	total := 0
	for controller := range 18 {
		cfg := fullLoadConfig()
		cfg.InstanceID = fmt.Sprintf("controller-%02d", controller)
		cfg.IdentityNamespace = cfg.InstanceID
		if controller == 17 {
			cfg.OccupiedBoards = 104
		}
		state := NewContainerState(cfg)
		repeated := NewContainerState(cfg)
		for i, m := range state.Modules {
			if modules[m.ModuleID] || m.ModuleID != repeated.Modules[i].ModuleID {
				t.Fatalf("unstable or duplicate module %s", m.ModuleID)
			}
			modules[m.ModuleID] = true
			psu := ""
			for _, slot := range m.Slots {
				if slot.Board == nil {
					continue
				}
				b := slot.Board
				if macs[b.MAC] || serials[b.Serial] || repeated.findBoard(b.MAC) == nil {
					t.Fatalf("duplicate or unstable board %s/%s", b.MAC, b.Serial)
				}
				macs[b.MAC], serials[b.Serial] = true, true
				identity, _, _ := state.modulePsu(b)
				if psu == "" {
					psu = identity.Serial
					if psus[psu] {
						t.Fatalf("duplicate PSU %s", psu)
					}
					psus[psu] = true
				} else if identity.Serial != psu {
					t.Fatal("boards in one module report different PSUs")
				}
			}
		}
		total += state.BoardCount()
		if controller != 17 {
			continue
		}
		layout := state.containerMapDTO()
		occupied, empty := 0, 0
		for _, tank := range layout.ContainerMap.Tanks {
			for _, module := range tank.Modules {
				if len(module.Slots) != 3 {
					t.Fatal("changed normal slot capacity")
				}
				for position, slot := range module.Slots {
					if slot.ConfiguredLocation == nil || slot.ConfiguredLocation.HashboardPosition != int64str(position+1) {
						t.Fatal("missing empty-slot placement")
					}
					if slot.Occupied {
						occupied++
						if slot.Hashboard == nil || slot.Hashboard.MiningState != miningStateMining {
							t.Fatal("occupied board is not healthy")
						}
					} else {
						empty++
						if slot.Hashboard != nil {
							t.Fatal("empty slot has a board")
						}
					}
				}
			}
		}
		if occupied != 104 || empty != 184 {
			t.Fatalf("occupied=%d empty=%d", occupied, empty)
		}
		if got := state.miningStatusDTO(); got.TotalBoards != 104 || got.ActiveBoards != 104 {
			t.Fatal(got)
		}
		api := &grpcAPI{state: state}
		changed, err := api.SetPowerTargetScoped(t.Context(), &mdk.SetPowerTargetScopedRequest{Watts: 2700})
		if err != nil || changed.GetBoardsUpdated() != 104 {
			t.Fatalf("scoped write: %v %v", changed, err)
		}
		for _, b := range state.eachBoard() {
			if b.targetWatts() != 2700 {
				t.Fatal("power target readback mismatch")
			}
		}
		// The last occupied module has two boards; its empty third slot stays empty.
		last := state.Modules[34]
		if last.Slots[0].Board == nil || last.Slots[1].Board == nil || last.Slots[2].Board != nil {
			t.Fatal("partial module is not first-N occupied")
		}
	}
	if total != 5000 || len(macs) != total || len(serials) != total {
		t.Fatalf("wrong load inventory: %d", total)
	}
	// Existing single-controller module API IDs remain available without a namespace.
	if got := NewContainerState(fullLoadConfig()).Modules[0].ModuleID; got != "T1-M01" {
		t.Fatalf("default module ID changed: %s", got)
	}
}

func TestLoadConfigurationValidation(t *testing.T) {
	for _, value := range []string{"0", "-1", "289", "104.0", "garbage"} {
		if _, err := parseOccupiedBoards(value, 288); err == nil {
			t.Fatalf("accepted occupied count %q", value)
		}
	}
	for _, value := range []string{"", "1", "104", "288"} {
		if _, err := parseOccupiedBoards(value, 288); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"-1ms", "oops"} {
		if _, err := parseResponseDelay(value); err == nil {
			t.Fatalf("accepted delay %q", value)
		}
	}
	for _, value := range []string{"", "0", "5ms"} {
		if _, err := parseResponseDelay(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"localhost", "127.0.0.1:9095", "0.0.0.0", "::", "10.0.0.999"} {
		if _, err := parseListenAddress(value); err == nil {
			t.Fatalf("accepted address %q", value)
		}
	}
	for _, value := range []string{"", "127.0.0.1", "::1"} {
		if _, err := parseListenAddress(value); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDelayedWritesExpireBeforeMutation(t *testing.T) {
	cfg := testConfig()
	cfg.ResponseDelay = 150 * time.Millisecond
	state := NewContainerState(cfg)
	handler, closeHandler, err := newHTTPHandler(state, "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeHandler()
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(state)
	defer server.Stop()
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	for _, transport := range []string{"grpc", "connect", "rest"} {
		t.Run(transport, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			switch transport {
			case "grpc":
				_, err := mdk.NewMiningServiceClient(connection).StopMining(ctx, &mdk.StopMiningRequest{})
				if status.Code(err) != codes.DeadlineExceeded {
					t.Fatalf("wanted deadline: %v", err)
				}
			case "connect":
				_, err := mdkv2connect.NewMiningServiceClient(http.DefaultClient, httpServer.URL).StopMining(ctx, connect.NewRequest(&mdk.StopMiningRequest{}))
				if connect.CodeOf(err) != connect.CodeDeadlineExceeded {
					t.Fatalf("wanted deadline: %v", err)
				}
			case "rest":
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpServer.URL+"/api/v2/mining/stop", nil)
				if err != nil {
					t.Fatal(err)
				}
				response, err := http.DefaultClient.Do(req)
				if response != nil {
					response.Body.Close()
				}
				if err == nil || ctx.Err() == nil {
					t.Fatalf("wanted deadline: %v", err)
				}
			}
		})
	}
	time.Sleep(160 * time.Millisecond)
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, b := range state.eachBoard() {
		if !b.isMining() {
			t.Fatal("expired write mutated board state")
		}
	}
}

func TestResponseDelayPrecedesLocks(t *testing.T) {
	state := NewContainerState(testConfig())
	state.Config.ResponseDelay = time.Hour
	state.requests.Lock()
	state.mu.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- state.serve(ctx, false, func() { t.Error("canceled request dispatched") }) }()
	select {
	case err := <-done:
		if err != context.DeadlineExceeded {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Error("response delay acquired a simulation lock")
	}
	state.mu.Unlock()
	state.requests.Unlock()
}

func TestAggregateMetrics(t *testing.T) {
	state := NewContainerState(testConfig())
	path := filepath.Join(t.TempDir(), "metrics.jsonl")
	stop, err := state.startMetrics(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewRESTApiHandler(state)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	for i := range 50 {
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v2/hashboards/missing-%d/asics", i), nil)
		mux.ServeHTTP(httptest.NewRecorder(), request)
	}
	stop()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Timestamp  time.Time                `json:"timestamp"`
		Requests   uint64                   `json:"requests_total"`
		Errors     uint64                   `json:"errors_total"`
		Operations map[string]operationStat `json:"operations"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &got); err != nil {
		t.Fatal(err)
	}
	if got.Timestamp.IsZero() || got.Requests != 50 || got.Errors != 50 || len(got.Operations) != 1 {
		t.Fatalf("unbounded or incorrect metrics: %+v", got)
	}
}

// Run startup in a child so invalid environment configuration exercises main's
// fail-fast path and listener tests use the same binding code as the binary.
func TestCT1Process(t *testing.T) {
	if os.Getenv("CT1_TEST_PROCESS") == "1" {
		main()
	}
}

func TestProcessListenerBindingAndStartupValidation(t *testing.T) {
	for _, occupied := range []string{"104", "0", "289", "nonsense"} {
		t.Run(occupied, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCT1Process$")
			command.Env = append(os.Environ(), "CT1_TEST_PROCESS=1", "LISTEN_ADDRESS=127.0.0.1", "GRPC_PORT=0", "HTTP_PORT=0", "CT1_OCCUPIED_BOARDS="+occupied, "CT1_TANKS=6", "CT1_MODULES_PER_TANK=16", "CT1_SLOTS_PER_MODULE=3")
			if occupied != "104" {
				output, err := command.CombinedOutput()
				if err == nil || !strings.Contains(string(output), "invalid CT1_OCCUPIED_BOARDS") || strings.Contains(string(output), "server listening") {
					t.Fatalf("did not reject invalid count before listen: %v %s", err, output)
				}
				return
			}
			stderr, err := command.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cancel(); _ = command.Wait() }()
			scanner := bufio.NewScanner(stderr)
			listeners := 0
			for scanner.Scan() {
				line := scanner.Text()
				if !strings.Contains(line, "server listening on ") {
					continue
				}
				_, address, _ := strings.Cut(line, "server listening on ")
				host, _, err := net.SplitHostPort(address)
				if err != nil || host != "127.0.0.1" {
					t.Fatalf("listener ignored bind address: %s", line)
				}
				listeners++
				if listeners == 2 {
					return
				}
			}
			t.Fatalf("started %d listeners: %v", listeners, scanner.Err())
		})
	}
}
