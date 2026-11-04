package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLoadDigestChallengeIsPerMiner(t *testing.T) {
	// Arrange
	fleet := loadTestFleet(t, "127.0.0.1\n127.0.0.2\n")
	const path = "/cgi-bin/stats.cgi"
	challenge := func(destination string) string {
		response := httptest.NewRecorder()
		fleet.ServeHTTP(response, localRequest("GET", path, destination, ""))
		match := regexp.MustCompile(`nonce="([^"]+)"`).FindStringSubmatch(response.Header().Get("WWW-Authenticate"))
		if response.Code != http.StatusUnauthorized || len(match) != 2 {
			t.Fatalf("challenge: %d %v", response.Code, response.Header())
		}
		return match[1]
	}
	authorization := func(nonce string) string {
		hash := md5Hash(md5Hash("root:Antminer:root") + ":" + nonce + ":00000001:test:auth:" + md5Hash("GET:"+path))
		return fmt.Sprintf(`Digest username="root", realm="Antminer", nonce="%s", uri="%s", qop=auth, nc=00000001, cnonce="test", response="%s"`, nonce, path, hash)
	}
	first := authorization(challenge("127.0.0.1"))
	for _, trial := range []struct {
		destination, auth string
		status            int
	}{
		{"127.0.0.1", first, http.StatusOK},
		{"127.0.0.2", first, http.StatusUnauthorized},
		{"127.0.0.2", authorization(challenge("127.0.0.2")), http.StatusOK},
	} {
		request := localRequest("GET", path, trial.destination, "")
		request.Header.Set("Authorization", trial.auth)
		response := httptest.NewRecorder()

		// Act
		fleet.ServeHTTP(response, request)

		// Assert
		if response.Code != trial.status {
			t.Errorf("destination %s: got %d want %d", trial.destination, response.Code, trial.status)
		}
	}
}

func TestRunFixtureMain(t *testing.T) {
	if os.Getenv("FAKE_ANTMINER_TEST_MAIN") != "1" {
		t.Skip("subprocess helper")
	}
	fleet, err := loadMinerFleet()
	if err != nil {
		t.Fatal(err)
	}
	// Test-only gates keep both handlers in flight until the parent observes
	// shutdown. The RPC marker requires the accepted handler to read bytes.
	for _, endpoint := range fleet.miners {
		handler := endpoint.http.protected
		endpoint.http.protected = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := os.WriteFile(os.Getenv("FAKE_ANTMINER_TEST_ENTERED"), []byte("entered"), 0600); err != nil {
				panic(err)
			}
			if err := waitForFixtureFile(os.Getenv("FAKE_ANTMINER_TEST_RELEASE")); err != nil {
				panic(err)
			}
			handler.ServeHTTP(w, r)
		})
	}
	rpc, err := net.Listen("tcp4", os.Getenv("FAKE_ANTMINER_TEST_RPC"))
	if err != nil {
		t.Fatal(err)
	}
	runMinerFleet(fleet, &fixtureRPCListener{rpc}, os.Getenv("FAKE_ANTMINER_TEST_HTTP"))
}

type fixtureRPCListener struct{ net.Listener }

func (l *fixtureRPCListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &fixtureRPCConn{conn}, nil
}

type fixtureRPCConn struct{ net.Conn }

func (c *fixtureRPCConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		if err := os.WriteFile(os.Getenv("FAKE_ANTMINER_TEST_RPC_ENTERED"), []byte("entered"), 0600); err != nil {
			return 0, err
		}
		if err := waitForFixtureFile(os.Getenv("FAKE_ANTMINER_TEST_RELEASE")); err != nil {
			return 0, err
		}
	}
	return n, err
}

func waitForFixtureFile(path string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestShutdownMetricsIncludeInFlightHTTPAndRPC(t *testing.T) {
	if testing.Short() {
		t.Skip("fixture subprocess")
	}

	// Arrange
	addressesForServer := make([]string, 2)
	for i := range addressesForServer {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addressesForServer[i] = listener.Addr().String()
		listener.Close()
	}
	dir := t.TempDir()
	addresses := filepath.Join(dir, "addresses")
	if err := os.WriteFile(addresses, []byte("127.0.0.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	metrics := filepath.Join(dir, "metrics.jsonl")
	entered := filepath.Join(dir, "entered")
	rpcEntered := filepath.Join(dir, "rpc-entered")
	release := filepath.Join(dir, "release")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestRunFixtureMain$")
	command.Env = append(os.Environ(), "FAKE_ANTMINER_TEST_MAIN=1", "FAKE_ANTMINER_TEST_ENTERED="+entered, "FAKE_ANTMINER_TEST_RPC_ENTERED="+rpcEntered, "FAKE_ANTMINER_TEST_RELEASE="+release, "FAKE_ANTMINER_TEST_RPC="+addressesForServer[0], "FAKE_ANTMINER_TEST_HTTP="+addressesForServer[1], "FAKE_MINER_ADDRESSES_FILE="+addresses, "FAKE_MINER_RESPONSE_DELAY=0", "FAKE_MINER_METRICS_FILE="+metrics)
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	ready := make(chan struct{})
	shuttingDown := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stderr)
		seen := 0
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "Shutting down fake Antminer server") {
				close(shuttingDown)
			}
			if strings.Contains(scanner.Text(), "server listening") {
				seen++
				if seen == 2 {
					close(ready)
				}
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture did not listen")
	}

	// Act
	web, err := net.DialTimeout("tcp4", addressesForServer[1], time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer web.Close()
	if _, err := io.WriteString(web, "GET /health HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := waitForFixtureFile(entered); err != nil {
		t.Fatal(err)
	}
	rpc, err := net.DialTimeout("tcp4", addressesForServer[0], time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	if _, err := io.WriteString(rpc, `{"command":"summary"}`); err != nil {
		t.Fatal(err)
	}
	if err := waitForFixtureFile(rpcEntered); err != nil {
		t.Fatal(err)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-shuttingDown:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture did not begin shutdown")
	}
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := web.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(web), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if body, err := io.ReadAll(response.Body); err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("HTTP request did not drain successfully: status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	if err := rpc.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var summary SummaryResponse
	if err := json.NewDecoder(rpc).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Summary) != 1 {
		t.Fatalf("RPC request did not drain successfully: %+v", summary)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("shutdown did not finish")
	}

	// Assert
	data, err := os.ReadFile(metrics)
	if err != nil {
		t.Fatal(err)
	}
	var final struct {
		Operations map[string]operationStat `json:"operations"`
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if err := json.Unmarshal([]byte(line), &final); err != nil {
			t.Fatal(err)
		}
	}
	if final.Operations["http:/health"].Requests != 1 || final.Operations["rpc:summary"].Requests != 1 {
		t.Fatalf("final metrics lost accepted requests: %s", data)
	}
	if final.Operations["http:/health"].Errors != 0 || final.Operations["rpc:summary"].Errors != 0 {
		t.Fatalf("in-flight requests were aborted at shutdown: %s", data)
	}
}
