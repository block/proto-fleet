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
		fleet.ServeHTTP(response, request)
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
	// A test-only handler gate marks entry before delaying completion. The
	// parent can signal only after the actual HTTP request has been accepted.
	for ip, endpoint := range fleet.miners {
		handler := endpoint.http
		endpoint.http = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := os.WriteFile(os.Getenv("FAKE_ANTMINER_TEST_ENTERED"), []byte("entered"), 0600); err != nil {
				panic(err)
			}
			time.Sleep(time.Second)
			handler.ServeHTTP(w, r)
		})
		fleet.miners[ip] = endpoint
	}
	runMinerFleet(fleet, os.Getenv("FAKE_ANTMINER_TEST_RPC"), os.Getenv("FAKE_ANTMINER_TEST_HTTP"))
}

func TestShutdownMetricsIncludeInFlightHTTPAndRPC(t *testing.T) {
	if testing.Short() {
		t.Skip("fixture subprocess")
	}
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
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestRunFixtureMain$")
	command.Env = append(os.Environ(), "FAKE_ANTMINER_TEST_MAIN=1", "FAKE_ANTMINER_TEST_ENTERED="+entered, "FAKE_ANTMINER_TEST_RPC="+addressesForServer[0], "FAKE_ANTMINER_TEST_HTTP="+addressesForServer[1], "FAKE_MINER_ADDRESSES_FILE="+addresses, "FAKE_MINER_RESPONSE_DELAY=1s", "FAKE_MINER_METRICS_FILE="+metrics)
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stderr)
		seen := 0
		for scanner.Scan() {
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
	web, err := net.DialTimeout("tcp4", addressesForServer[1], time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer web.Close()
	if _, err := io.WriteString(web, "GET /health HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"); err != nil {
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
	// Wait for the test-only marker inside the actual HTTP handler. RPC is
	// either delaying or complete; either outcome must appear in final totals.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(entered); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("HTTP handler did not enter")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
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
}
