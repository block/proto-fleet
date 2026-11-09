package main

import (
	"bufio"
	"bytes"
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
	"syscall"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

type acceptedRPCMarker struct {
	path          string
	gateDirectory string
	state         *ContainerState
}

func (m acceptedRPCMarker) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}
func (m acceptedRPCMarker) HandleRPC(_ context.Context, event stats.RPCStats) {
	if _, ok := event.(*stats.InHeader); ok {
		_ = os.WriteFile(m.path, []byte("accepted"), 0600)
		if m.gateDirectory != "" {
			waitForDrainFile(filepath.Join(m.gateDirectory, "release-rpc"))
		}
	}
	if _, ok := event.(*stats.End); ok && m.gateDirectory != "" {
		// Hold the final snapshot after the RPC's metric has been recorded.
		// The test releases it only after signaling during the flush phase.
		m.state.metrics.mu.Lock()
		_ = os.WriteFile(filepath.Join(m.gateDirectory, "metrics-locked"), nil, 0600)
		go func() {
			waitForDrainFile(filepath.Join(m.gateDirectory, "release-metrics"))
			m.state.metrics.mu.Unlock()
		}()
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
	gateDirectory := os.Getenv("CT1_DRAIN_GATE_DIRECTORY")
	if gateDirectory != "" {
		cfg.ResponseDelay = 0
	}
	state := NewContainerState(cfg)
	marker := acceptedRPCMarker{path: os.Getenv("CT1_DRAIN_MARKER"), gateDirectory: gateDirectory, state: state}
	if err := runContainer(state, "127.0.0.1", rpcPort, 0, "", grpc.StatsHandler(marker)); err != nil {
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

func startDrainProcess(t *testing.T, extraEnv ...string) *drainProcess {
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
	process.command.Env = append(process.command.Env, extraEnv...)
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
	p.signal(t)
	return p.wait(t)
}

func (p *drainProcess) signal(t *testing.T) {
	t.Helper()
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
}

func (p *drainProcess) wait(t *testing.T) map[string]operationStat {
	t.Helper()
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

// A 100 Continue response proves that the server has entered the handler and
// is reading its body. Withholding that body keeps the HTTP drain in flight
// without depending on a scheduler delay or adding production test hooks.
type pendingDrainRequest struct {
	connection net.Conn
	reader     *bufio.Reader
	body       string
}

func beginDrainRequest(t *testing.T, process *drainProcess, address, method, path, body string) *pendingDrainRequest {
	t.Helper()
	connection, err := (&net.Dialer{}).DialContext(process.ctx, "tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	deadline, _ := process.ctx.Deadline()
	_ = connection.SetDeadline(deadline)
	_, err = fmt.Fprintf(connection, "%s %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nExpect: 100-continue\r\n\r\n", method, path, address, len(body))
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusContinue {
		t.Fatalf("request was not accepted: %d", response.StatusCode)
	}
	return &pendingDrainRequest{connection, reader, body}
}

func (r *pendingDrainRequest) finish(t *testing.T) {
	t.Helper()
	if _, err := io.WriteString(r.connection, r.body); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(r.reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("accepted HTTP request did not drain: %d", response.StatusCode)
	}
}

func TestSIGTERMDrainsRESTAndConnectBeforeFinalMetrics(t *testing.T) {
	process := startDrainProcess(t)
	address := process.waitForHTTPAddress(t)
	rest := beginDrainRequest(t, process, address, http.MethodPut, "/api/v2/mining/target-power", `{"watts":2700}`)
	connect := beginDrainRequest(t, process, address, http.MethodPost, "/mdk.v2.SystemService/GetSystemInfo", "{}")

	process.signal(t)
	rest.finish(t)
	connect.finish(t)
	operations := process.wait(t)

	for _, key := range []string{"PUT /api/v2/mining/target-power", "/mdk.v2.SystemService/GetSystemInfo"} {
		if value := operations[key]; value.Requests != 1 || value.Errors != 0 {
			t.Errorf("%s missing from final metrics: %+v", key, operations)
		}
	}
}

func waitForDrainFile(path string) {
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForDrainCondition(t *testing.T, process *drainProcess, description string, ready func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(process.ctx, 2*time.Second)
	defer cancel()
	for !ready() {
		select {
		case err := <-process.done:
			logs, _ := os.ReadFile(process.logs.Name())
			t.Fatalf("process exited while waiting for %s: %v\n%s", description, err, logs)
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", description)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestSIGTERMStopsBothTransportsAndProtectsFinalFlush(t *testing.T) {
	directory := t.TempDir()
	process := startDrainProcess(t, "CT1_DRAIN_GATE_DIRECTORY="+directory)
	address := process.waitForHTTPAddress(t)
	rest := beginDrainRequest(t, process, address, http.MethodPut, "/api/v2/mining/target-power", `{"watts":2700}`)
	rpcAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(process.port))
	connection, err := grpc.NewClient(rpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	callDone := make(chan error, 1)
	client := mdk.NewSystemServiceClient(connection)
	go func() {
		_, err := client.GetSystemInfo(process.ctx, &mdk.GetSystemInfoRequest{})
		callDone <- err
	}()
	waitForDrainCondition(t, process, "accepted native RPC", func() bool {
		_, err := os.Stat(process.marker)
		return err == nil
	})

	process.signal(t)
	for name, address := range map[string]string{"HTTP": address, "gRPC": rpcAddress} {
		waitForDrainCondition(t, process, name+" listener shutdown during HTTP drain", func() bool {
			connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				connection.Close()
			}
			return err != nil
		})
	}
	// GracefulStop closes listeners before sending GOAWAY on existing
	// transports. Wait until the client observes that drain; a closed listener
	// alone does not prevent another RPC from being accepted on this connection.
	waitForDrainCondition(t, process, "gRPC client observing shutdown", func() bool {
		return connection.GetState() != connectivity.Ready
	})
	// The existing connection must reject a late RPC, even while its accepted
	// predecessor and the HTTP body are both still held by the test.
	ctx, cancel := context.WithTimeout(process.ctx, time.Second)
	defer cancel()
	_, err = client.GetSystemInfo(ctx, &mdk.GetSystemInfoRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("late RPC was not rejected during HTTP drain: %v", err)
	}
	process.signal(t)
	rest.finish(t)
	// HTTP has finished; gRPC is still draining and must remain protected.
	process.signal(t)
	if err := os.WriteFile(filepath.Join(directory, "release-rpc"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-callDone; err != nil {
		t.Fatalf("accepted RPC did not drain: %v", err)
	}
	waitForDrainCondition(t, process, "final metrics flush", func() bool {
		_, locked := os.Stat(filepath.Join(directory, "metrics-locked"))
		logs, _ := os.ReadFile(process.logs.Name())
		return locked == nil && bytes.Contains(logs, []byte("Flushing final fake CT1 metrics"))
	})
	process.signal(t)
	if err := os.WriteFile(filepath.Join(directory, "release-metrics"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	operations := process.wait(t)
	for _, key := range []string{"PUT /api/v2/mining/target-power", "/mdk.v2.SystemService/GetSystemInfo"} {
		if value := operations[key]; value.Requests != 1 || value.Errors != 0 {
			t.Errorf("%s final metric = %+v, want one successful accepted request", key, value)
		}
	}
}

func TestStartupHTTPFailureDrainsNativeServerAndFlushesMetrics(t *testing.T) {
	for _, test := range []struct {
		name         string
		occupiedPort bool
	}{
		{name: "invalid web root"},
		{name: "occupied HTTP port", occupiedPort: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			rpcPort := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			httpPort, webRoot := 0, filepath.Join(t.TempDir(), "missing-web-root")
			if test.occupiedPort {
				occupied, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer occupied.Close()
				httpPort, webRoot = occupied.Addr().(*net.TCPAddr).Port, ""
			}
			metrics := filepath.Join(t.TempDir(), "metrics.jsonl")
			t.Setenv("FAKE_MINER_METRICS_FILE", metrics)
			if err := runContainer(NewContainerState(testConfig()), "127.0.0.1", rpcPort, httpPort, webRoot); err == nil {
				t.Fatal("startup succeeded despite invalid HTTP setup")
			}
			listener, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(rpcPort)))
			if err != nil {
				t.Fatalf("startup failure left gRPC listener open: %v", err)
			}
			listener.Close()
			data, err := os.ReadFile(metrics)
			if err != nil || !json.Valid(bytes.TrimSpace(data)) {
				t.Fatalf("startup failure did not flush final metrics: %s, %v", data, err)
			}
		})
	}
}
