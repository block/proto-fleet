package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"
)

type acceptedRPCMarker struct{ path string }

func (m acceptedRPCMarker) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}
func (m acceptedRPCMarker) HandleRPC(_ context.Context, event stats.RPCStats) {
	if _, ok := event.(*stats.InHeader); ok {
		_ = os.WriteFile(m.path, []byte("accepted"), 0600)
	}
}
func (m acceptedRPCMarker) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}
func (m acceptedRPCMarker) HandleConn(context.Context, stats.ConnStats) {}

func TestCT1DrainProcess(t *testing.T) {
	if os.Getenv("CT1_DRAIN_CHILD") != "1" {
		return
	}
	cfg := testConfig()
	cfg.ResponseDelay = time.Second
	rpcPort, _ := strconv.Atoi(os.Getenv("CT1_DRAIN_RPC_PORT"))
	if err := runContainer(NewContainerState(cfg), "127.0.0.1", rpcPort, 0, "", grpc.StatsHandler(acceptedRPCMarker{os.Getenv("CT1_DRAIN_MARKER")})); err != nil {
		t.Fatal(err)
	}
}

func TestSIGTERMDrainsNativeRPCBeforeFinalMetrics(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	directory := t.TempDir()
	marker := filepath.Join(directory, "accepted")
	metrics := filepath.Join(directory, "metrics.jsonl")
	logs, err := os.Create(filepath.Join(directory, "process.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCT1DrainProcess$")
	command.Env = append(os.Environ(), "CT1_DRAIN_CHILD=1", "CT1_DRAIN_RPC_PORT="+strconv.Itoa(port), "CT1_DRAIN_MARKER="+marker, "FAKE_MINER_METRICS_FILE="+metrics)
	command.Stdout, command.Stderr = logs, logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill() }()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(logs.Name())
		if strings.Contains(string(data), "HTTP server listening") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("process did not start: %s", data)
		}
		time.Sleep(5 * time.Millisecond)
	}
	connection, err := grpc.NewClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	callDone := make(chan error, 1)
	go func() {
		_, err := mdk.NewSystemServiceClient(connection).GetSystemInfo(ctx, &mdk.GetSystemInfoRequest{})
		callDone <- err
	}()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native RPC was not accepted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			data, _ := os.ReadFile(logs.Name())
			t.Fatalf("process: %v %s", err, data)
		}
	case <-ctx.Done():
		t.Fatal("shutdown did not finish")
	}
	file, err := os.Open(metrics)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var last struct {
		Operations map[string]operationStat `json:"operations"`
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := json.Unmarshal(scanner.Bytes(), &last); err != nil {
			t.Fatal(err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	key := "/mdk.v2.SystemService/GetSystemInfo"
	if value := last.Operations[key]; value.Requests != 1 || value.Errors != 0 {
		t.Fatal(fmt.Sprintf("accepted native RPC missing from final metrics: %+v", last.Operations))
	}
	if err := <-callDone; err != nil {
		t.Fatalf("accepted RPC did not drain: %v", err)
	}
}
