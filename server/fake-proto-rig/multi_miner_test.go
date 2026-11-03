package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func addressFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "addresses.txt")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func startRigServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- startHTTPServer(ctx, NewMinerState("SINGLE", "02:00:00:00:00:00"), port) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("server did not shut down")
		}
	})
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if os.Getenv("TLS_CERT_FILE") != "" {
		return "https://" + addr
	}
	return "http://" + addr
}

func rigRequest(t *testing.T, client *http.Client, method, url, token string, body io.Reader) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately disagree with the socket destination: Host cannot select a miner.
	req.Host = "203.0.113.99"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}

func TestServerUsesAddressAllowlist(t *testing.T) {
	// Arrange
	t.Setenv("FAKE_MINER_ADDRESSES_FILE", addressFile(t, "127.0.0.1\n"))
	base := startRigServer(t)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()

	// Act
	status, body := rigRequest(t, client, "GET", base+"/api/v1/pairing/info", "", nil)

	// Assert
	if status != http.StatusOK {
		t.Fatalf("pairing status %d: %s", status, body)
	}
	var pairing PairingInfoResponse
	if err := json.Unmarshal(body, &pairing); err != nil {
		t.Fatal(err)
	}
	if pairing.CBSN != "PROTO-SIM-7F000001" || pairing.MAC != "02:00:7F:00:00:01" {
		t.Fatalf("identity must come from destination 127.0.0.1: %+v", pairing)
	}
}

// otherLocalIPv4 returns a second destination for the fixture's wildcard
// listener. Linux routes all of 127.0.0.0/8 to loopback listeners; elsewhere a
// LAN address is needed.
func otherLocalIPv4(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "linux" {
		return "127.0.0.2"
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		network, ok := address.(*net.IPNet)
		if ok && network.IP.To4() != nil && !network.IP.IsLoopback() && !network.IP.IsLinkLocalUnicast() {
			return network.IP.String()
		}
	}
	if os.Getenv("CI") != "" {
		t.Fatal("two-destination test needs a second local IPv4 address")
	}
	t.Skip("two-destination test needs a second local IPv4 address")
	return ""
}

func rigLogin(t *testing.T, client *http.Client, base, password string) string {
	t.Helper()
	status, body := rigRequest(t, client, "POST", base+"/api/v1/auth/login", "", strings.NewReader(`{"password":"`+password+`"}`))
	if status != http.StatusOK {
		t.Fatalf("login status %d: %s", status, body)
	}
	var tokens AuthTokens
	if err := json.Unmarshal(body, &tokens); err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken == "" {
		t.Fatal("login returned no token")
	}
	return tokens.AccessToken
}

func TestMultiMinerSocketStateAndCredentialsAreIsolated(t *testing.T) {
	// Arrange
	secondIP := otherLocalIPv4(t)
	t.Setenv("FAKE_MINER_ADDRESSES_FILE", addressFile(t, "127.0.0.1\n"+secondIP+"\n"))
	t.Setenv("FAKE_RIG_PASSWORD", "initial-password")
	first := startRigServer(t)
	second := strings.Replace(first, "127.0.0.1", secondIP, 1)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()

	// Act
	var identities []PairingInfoResponse
	for _, base := range []string{first, second} {
		status, body := rigRequest(t, client, "GET", base+"/api/v1/pairing/info", "", nil)
		if status != http.StatusOK {
			t.Fatalf("pairing status %d: %s", status, body)
		}
		var identity PairingInfoResponse
		if err := json.Unmarshal(body, &identity); err != nil {
			t.Fatal(err)
		}
		identities = append(identities, identity)
	}

	// Assert
	if identities[0].CBSN == identities[1].CBSN || identities[0].MAC == identities[1].MAC {
		t.Fatalf("duplicate identities: %+v", identities)
	}

	// Act
	tokenA, tokenB := rigLogin(t, client, first, "initial-password"), rigLogin(t, client, second, "initial-password")

	// Assert
	if status, body := rigRequest(t, client, "GET", second+"/api/v1/pools", tokenA, nil); status != http.StatusUnauthorized {
		t.Fatalf("cross-miner token accepted: %d %s", status, body)
	}

	// Act
	if status, body := rigRequest(t, client, "POST", first+"/api/v1/pools", tokenA, strings.NewReader(`[{"url":"stratum+tcp://pool.example:3333","username":"worker-a","password":"secret-a"}]`)); status != http.StatusOK {
		t.Fatalf("pool write: %d %s", status, body)
	}

	// Assert
	for index, base := range []string{first, second} {
		token := []string{tokenA, tokenB}[index]
		status, body := rigRequest(t, client, "GET", base+"/api/v1/pools", token, nil)
		var pools PoolsList
		if status != http.StatusOK {
			t.Fatalf("pool read: %d %s", status, body)
		}
		if err := json.Unmarshal(body, &pools); err != nil {
			t.Fatal(err)
		}
		if index == 0 && (len(pools.Pools) != 1 || pools.Pools[0].User != "worker-a") {
			t.Fatalf("write did not persist: %s", body)
		}
		if index == 1 && len(pools.Pools) != 0 {
			t.Fatalf("pool state leaked: %s", body)
		}
	}

	// Act
	if status, body := rigRequest(t, client, "PUT", first+"/api/v1/auth/change-password", tokenA, strings.NewReader(`{"current_password":"initial-password","new_password":"changed-password"}`)); status != http.StatusOK {
		t.Fatalf("password write: %d %s", status, body)
	}

	// Assert
	if status, body := rigRequest(t, client, "POST", first+"/api/v1/auth/login", "", strings.NewReader(`{"password":"initial-password"}`)); status != http.StatusUnauthorized {
		t.Fatalf("old password accepted: %d %s", status, body)
	}
	rigLogin(t, client, first, "changed-password")
	rigLogin(t, client, second, "initial-password")
}

func TestAddressAllowlistRejectsOtherSocketDestinations(t *testing.T) {
	// Arrange
	secondIP := otherLocalIPv4(t)
	t.Setenv("FAKE_MINER_ADDRESSES_FILE", addressFile(t, "127.0.0.1\n"))
	base := strings.Replace(startRigServer(t), "127.0.0.1", secondIP, 1)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, path := range []string{"/api/v1/pairing/info", "/health", "/api/v1/auth/login"} {
		// Act
		status, body := rigRequest(t, client, "GET", base+path, "", nil)

		// Assert
		if status != http.StatusNotFound {
			t.Fatalf("unlisted socket destination returned %d: %s", status, body)
		}
	}
}

func TestMultiMinerIdentityIsStableAndAddressFileIsExact(t *testing.T) {
	// Arrange
	path := addressFile(t, "127.0.0.1\n127.0.0.1\n192.0.2.3\n")

	// Act
	first, err := newMultiMinerHandler(path)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(first.miners) != 2 {
		t.Fatalf("address entries: %d", len(first.miners))
	}

	// Act
	serial, mac := minerIdentityForIP(net.ParseIP("192.0.2.3"))

	// Assert
	if serial != "PROTO-SIM-C0000203" || mac != "02:00:C0:00:02:03" {
		t.Fatalf("unstable identity %q %q", serial, mac)
	}
	for _, contents := range []string{"", "10.0.0.0/24\n", "::1\n", "::ffff:192.0.2.1\n", "192.0.2.1\nnot-an-ip\n"} {
		// Act
		_, err := newMultiMinerHandler(addressFile(t, contents))

		// Assert
		if err == nil {
			t.Fatalf("accepted invalid address file %q", contents)
		}
	}
}

func TestSingleMinerServerDefaultsArePreserved(t *testing.T) {
	// Arrange
	t.Setenv("FAKE_MINER_ADDRESSES_FILE", "")
	base := startRigServer(t)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()

	// Act
	status, body := rigRequest(t, client, "GET", base+"/api/v1/pairing/info", "", nil)

	// Assert
	var identity PairingInfoResponse
	if err := json.Unmarshal(body, &identity); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || identity.CBSN != "SINGLE" {
		t.Fatalf("single miner default changed: %d %s", status, body)
	}
}
