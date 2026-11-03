package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

type drainProcess struct {
	ctx     context.Context
	port    int
	marker  string
	metrics string
	logs    *os.File
	done    chan error
	command *exec.Cmd
}

func startDrainProcess(t *testing.T) *drainProcess {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	directory := t.TempDir()
	process := &drainProcess{
		port:    port,
		marker:  filepath.Join(directory, "accepted"),
		metrics: filepath.Join(directory, "metrics.jsonl"),
		done:    make(chan error, 1),
	}
	process.logs, err = os.Create(filepath.Join(directory, "process.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.logs.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	process.ctx = ctx
	process.command = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCT1DrainProcess$")
	process.command.Env = append(os.Environ(), "CT1_DRAIN_CHILD=1", "CT1_DRAIN_RPC_PORT="+strconv.Itoa(port), "CT1_DRAIN_MARKER="+process.marker, "FAKE_MINER_METRICS_FILE="+process.metrics)
	process.command.Stdout, process.command.Stderr = process.logs, process.logs
	if err := process.command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.command.Process.Kill() })
	go func() { process.done <- process.command.Wait() }()
	return process
}

var httpListeningLine = regexp.MustCompile(`HTTP server listening on (\S+)`)

func (p *drainProcess) waitForHTTPAddress(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(p.logs.Name())
		if match := httpListeningLine.FindSubmatch(data); match != nil {
			return string(match[1])
		}
		if time.Now().After(deadline) {
			t.Fatalf("process did not start: %s", data)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (p *drainProcess) terminate(t *testing.T) map[string]operationStat {
	t.Helper()
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-p.done:
		if err != nil {
			data, _ := os.ReadFile(p.logs.Name())
			t.Fatalf("process: %v %s", err, data)
		}
	case <-p.ctx.Done():
		t.Fatal("shutdown did not finish")
	}
	file, err := os.Open(p.metrics)
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
	return last.Operations
}

func TestSIGTERMDrainsNativeRPCBeforeFinalMetrics(t *testing.T) {
	// Arrange
	process := startDrainProcess(t)
	process.waitForHTTPAddress(t)
	connection, err := grpc.NewClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(process.port)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	callDone := make(chan error, 1)
	go func() {
		_, err := mdk.NewSystemServiceClient(connection).GetSystemInfo(process.ctx, &mdk.GetSystemInfoRequest{})
		callDone <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(process.marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native RPC was not accepted")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Act
	operations := process.terminate(t)

	// Assert
	key := "/mdk.v2.SystemService/GetSystemInfo"
	if value := operations[key]; value.Requests != 1 || value.Errors != 0 {
		t.Fatal(fmt.Sprintf("accepted native RPC missing from final metrics: %+v", operations))
	}
	if err := <-callDone; err != nil {
		t.Fatalf("accepted RPC did not drain: %v", err)
	}
}

func TestSIGTERMDrainsRESTAndConnectBeforeFinalMetrics(t *testing.T) {
	// Arrange
	process := startDrainProcess(t)
	base := "http://" + process.waitForHTTPAddress(t)
	type result struct {
		status int
		err    error
	}
	send := func(request *http.Request) chan result {
		out := make(chan result, 1)
		go func() {
			response, err := http.DefaultClient.Do(request.WithContext(process.ctx))
			if err != nil {
				out <- result{err: err}
				return
			}
			defer response.Body.Close()
			_, _ = io.Copy(io.Discard, response.Body)
			out <- result{status: response.StatusCode}
		}()
		return out
	}
	rest, err := http.NewRequest(http.MethodGet, base+"/api/v2/system/info", nil)
	if err != nil {
		t.Fatal(err)
	}
	connect, err := http.NewRequest(http.MethodPost, base+"/mdk.v2.SystemService/GetSystemInfo", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	connect.Header.Set("Content-Type", "application/json")
	restDone, connectDone := send(rest), send(connect)
	// Acceptance is not observable from outside the process; the child delays
	// each response by a second, so the requests are still in flight afterwards.
	time.Sleep(300 * time.Millisecond)

	// Act
	operations := process.terminate(t)

	// Assert
	for name, done := range map[string]chan result{"REST": restDone, "Connect": connectDone} {
		if got := <-done; got.err != nil || got.status != http.StatusOK {
			t.Errorf("%s request did not drain: status=%d err=%v", name, got.status, got.err)
		}
	}
	for _, key := range []string{"GET /api/v2/system/info", "/mdk.v2.SystemService/GetSystemInfo"} {
		if value := operations[key]; value.Requests != 1 || value.Errors != 0 {
			t.Errorf("%s missing from final metrics: %+v", key, operations)
		}
	}
}
