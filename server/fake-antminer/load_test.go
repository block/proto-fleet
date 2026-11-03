package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDigestRejectsWrongPassword(t *testing.T) {
	// Arrange
	// The hash is computed for a different miner's password. Checking only the
	// username would hide credential leakage between destination addresses.
	auth := `Digest username="root", realm="Antminer", nonce="test", uri="/cgi-bin/stats.cgi", qop=auth, nc=00000001, cnonce="test", response="` + md5Hash(md5Hash("root:Antminer:other-miner")+":test:00000001:test:auth:"+md5Hash("GET:/cgi-bin/stats.cgi")) + `"`

	// Act
	accepted := isValidLoadDigestAuth(auth, "root", "root", "GET", "/cgi-bin/stats.cgi")

	// Assert
	if accepted {
		t.Fatal("accepted another miner's password")
	}
}

func loadTestFleet(t *testing.T, addresses string) *minerFleet {
	t.Helper()
	path := filepath.Join(t.TempDir(), "addresses")
	if err := os.WriteFile(path, []byte(addresses), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_MINER_ADDRESSES_FILE", path)
	fleet, err := loadMinerFleet()
	if err != nil {
		t.Fatal(err)
	}
	return fleet
}

func localRequest(method, path, destination, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Host = "127.0.0.1" // Host must never select the state.
	if destination != "" {
		request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP(destination), Port: 80}))
	}
	return request
}

func TestLoadDestinationAllowlistAndIdentity(t *testing.T) {
	// Arrange
	fleet := loadTestFleet(t, "127.0.0.1\n127.0.0.2\n")
	for _, destination := range []string{"127.0.0.3", ""} {
		// Act
		response := httptest.NewRecorder()
		fleet.ServeHTTP(response, localRequest("GET", "/health", destination, ""))

		// Assert
		if response.Code != 404 {
			t.Fatalf("destination %q: status %d", destination, response.Code)
		}
	}
	// Act
	first, _ := fleet.endpoint(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 4028})
	second, _ := fleet.endpoint(&net.TCPAddr{IP: net.ParseIP("127.0.0.2"), Port: 80})

	// Assert
	if first.state.SerialNumber == second.state.SerialNumber || first.state.MacAddress == second.state.MacAddress || first.state.boardSerial(0) == second.state.boardSerial(0) {
		t.Fatal("duplicate identities")
	}

	// Act
	response := httptest.NewRecorder()
	fleet.ServeHTTP(response, localRequest("GET", "/cgi-bin/get_network_info.cgi", "127.0.0.2", ""))

	// Assert
	if !strings.Contains(response.Body.String(), "127.0.0.2") {
		t.Fatalf("Host selected wrong state: %s", response.Body)
	}
	if len(fleet.miners) != 2 {
		t.Fatal("request changed inventory")
	}
}

func TestLoadAddressValidation(t *testing.T) {
	for _, input := range []string{"", "::1", "0.0.0.0", "224.0.0.1", "127.0.0.1\n127.0.0.1", "not-an-ip"} {
		t.Run(input, func(t *testing.T) {
			// Arrange
			path := filepath.Join(t.TempDir(), "addresses")
			if err := os.WriteFile(path, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_MINER_ADDRESSES_FILE", path)

			// Act
			_, err := loadMinerFleet()

			// Assert
			if err == nil {
				t.Fatal("accepted invalid inventory")
			}
		})
	}
}

func TestDelayCancellationBeforeStateLock(t *testing.T) {
	// Arrange
	fleet := loadTestFleet(t, "127.0.0.1")
	fleet.delay = time.Second
	endpoint, _ := fleet.endpoint(&net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	endpoint.state.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r := localRequest("POST", "/cgi-bin/set_miner_conf.cgi", "127.0.0.1", `{"bitmain-work-mode":"1"}`)
		ctx = context.WithValue(ctx, http.LocalAddrContextKey, r.Context().Value(http.LocalAddrContextKey))
		fleet.ServeHTTP(httptest.NewRecorder(), r.WithContext(ctx))
		close(done)
	}()

	// Act
	cancel()

	// Assert
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		endpoint.state.mu.Unlock()
		t.Fatal("cancelled request waited for state lock")
	}
	endpoint.state.mu.Unlock()
	if endpoint.state.BitmainWorkMode != WorkModeNormal {
		t.Fatal("cancelled write changed mode")
	}

	// Act
	start := time.Now()
	if err := waitResponse(context.Background(), 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Assert
	if time.Since(start) < 5*time.Millisecond {
		t.Fatal("delay was skipped")
	}
}

func TestLoadMetricsBoundedAndFlush(t *testing.T) {
	// Arrange
	fleet := loadTestFleet(t, "127.0.0.1")

	// Act
	for i := 0; i < 100; i++ {
		fleet.ServeHTTP(httptest.NewRecorder(), localRequest("GET", fmt.Sprintf("/unknown/%d", i), "127.0.0.1", ""))
	}

	// Assert
	if len(fleet.metrics.operations) != 1 {
		t.Fatal("unbounded operation keys")
	}

	// Act
	path := filepath.Join(t.TempDir(), "metrics.jsonl")
	t.Setenv("FAKE_MINER_METRICS_FILE", path)
	if err := fleet.startMetrics(context.Background()); err != nil {
		t.Fatal(err)
	}
	fleet.stopMetrics()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	var metrics struct {
		Requests uint64 `json:"requests_total"`
		Errors   uint64 `json:"errors_total"`
	}
	if err := json.Unmarshal(data, &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.Requests != 100 || metrics.Errors != 100 {
		t.Fatalf("unexpected metrics: %s", data)
	}
}

func TestSingleMinerDefaults(t *testing.T) {
	// Arrange
	t.Setenv("FAKE_MINER_ADDRESSES_FILE", "")
	t.Setenv("IP_ADDRESS", "127.0.0.1")

	// Act
	fleet, err := loadMinerFleet()
	if err != nil {
		t.Fatal(err)
	}
	state := fleet.single.state

	// Assert
	if state.MinerType != "Antminer S19j Pro" || state.SerialNumber != "fake-antminer-1" || state.MacAddress != "00:11:22:33:44:55" || state.Username != "root" || state.Password != "root" || state.LoadMode {
		t.Fatalf("changed defaults: %+v", state)
	}
}

// A subprocess helper gives the real plugin ephemeral TCP/HTTP ports without
// adding production listener configuration or importing plugin dependencies.
func TestServePluginFixture(t *testing.T) {
	path := os.Getenv("FAKE_ANTMINER_TEST_PORTS_FILE")
	if path == "" {
		t.Skip("subprocess helper")
	}
	fleet, err := loadMinerFleet()
	if err != nil {
		t.Fatal(err)
	}
	rpc, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	web, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer web.Close()
	go func() { _ = http.Serve(web, fleet) }()
	ports := fmt.Sprintf("%d %d", rpc.Addr().(*net.TCPAddr).Port, web.Addr().(*net.TCPAddr).Port)
	if err := os.WriteFile(path, []byte(ports), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		conn, err := rpc.Accept()
		if err != nil {
			return
		}
		go fleet.handleRPC(context.Background(), conn)
	}
}

func TestRPCRejectsUnknownDestination(t *testing.T) {
	// Arrange
	fleet := loadTestFleet(t, "127.0.0.2")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			fleet.handleRPC(context.Background(), conn)
		}
	}()
	conn, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var response [1]byte

	// Act
	n, err := conn.Read(response[:])

	// Assert
	if err == nil || n != 0 {
		t.Fatal("unknown destination returned miner response")
	}
	<-done
	if len(fleet.miners) != 1 {
		t.Fatal("unknown TCP destination created miner")
	}
}

func TestLoadDelayValidation(t *testing.T) {
	for _, duration := range []string{"-1ms", "invalid"} {
		// Arrange
		t.Setenv("FAKE_MINER_RESPONSE_DELAY", duration)

		// Act
		_, err := loadMinerFleet()

		// Assert
		if err == nil {
			t.Fatalf("accepted delay %q", duration)
		}
	}
}

func TestLoadShardInventoryAndTelemetry(t *testing.T) {
	// Arrange
	var addresses strings.Builder
	for i := 1; i <= 4000; i++ {
		fmt.Fprintf(&addresses, "10.44.%d.%d\n", i/256, i%256)
	}

	// Act
	fleet := loadTestFleet(t, addresses.String())

	// Assert
	if len(fleet.miners) != 4000 {
		t.Fatalf("expected 4000 miners, got %d", len(fleet.miners))
	}
	macs, serials := map[string]bool{}, map[string]bool{}
	for _, endpoint := range fleet.miners {
		state := endpoint.state
		if macs[state.MacAddress] || serials[state.SerialNumber] {
			t.Fatal("shard identity collision")
		}
		macs[state.MacAddress], serials[state.SerialNumber] = true, true
		state.advanceTelemetry()
		first := state.HashRate
		state.advanceTelemetry()
		if state.HashRate == first || state.HashRate < 110 || state.HashRate > 111 {
			t.Fatal("telemetry must change within healthy bounds")
		}
	}
}
