package antminer_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer"
	"github.com/block/proto-fleet/server/sdk/v1"
)

// Exercise the owning fixture and production client together over real sockets;
// canned API responses would not detect state or credential leaks in dispatch.
func TestLoadFixtureContract(t *testing.T) {
	if testing.Short() {
		t.Skip("fixture subprocess contract")
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "")
	}
	addresses := []string{"127.0.0.1"}
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range interfaces {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() {
			addresses = append(addresses, ip.String())
			break
		}
	}
	if len(addresses) != 2 {
		t.Skip("requires two locally routable IPv4 addresses")
	}
	fixtureDir, err := filepath.Abs("../../../../server/fake-antminer")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "fixture.test")
	build := exec.Command("go", "test", "-c", "-o", binary)
	build.Dir = fixtureDir
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	addressFile := filepath.Join(dir, "addresses")
	if err := os.WriteFile(addressFile, []byte(addresses[0]+"\n"+addresses[1]+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	portsFile := filepath.Join(dir, "ports")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestServePluginFixture$")
	command.Env = append(os.Environ(), "FAKE_MINER_ADDRESSES_FILE="+addressFile, "FAKE_MINER_RESPONSE_DELAY=5ms", "FAKE_ANTMINER_TEST_PORTS_FILE="+portsFile)
	var logs bytes.Buffer
	command.Stdout, command.Stderr = &logs, &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait()
		if t.Failed() {
			t.Log(logs.String())
		}
	})
	var rpcPort, webPort int32
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(portsFile)
		if err == nil {
			if _, err := fmt.Sscanf(string(data), "%d %d", &rpcPort, &webPort); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	clients := make([]*antminer.Client, 2)
	identities := make([]*antminer.DeviceInfo, 2)
	for i, address := range addresses {
		client, err := antminer.NewClient(address, rpcPort, webPort, "http")
		if err != nil {
			t.Fatal(err)
		}
		clients[i] = client
		t.Cleanup(client.Close)
		info, err := client.GetDeviceInfo(t.Context()) // unauthenticated RPC discovery
		if err != nil || info.Model != "Antminer S19j Pro" {
			t.Fatalf("discovery: %+v %v", info, err)
		}
		if err := client.Pair(t.Context(), sdk.UsernamePassword{Username: "root", Password: "root"}); err != nil {
			t.Fatal(err)
		}
		identities[i], err = client.GetDeviceInfo(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	}
	if identities[0].SerialNumber == identities[1].SerialNumber || identities[0].MacAddress == identities[1].MacAddress {
		t.Fatal("destination identities collided")
	}
	telemetry, err := clients[0].GetTelemetry(t.Context())
	if err != nil || telemetry.HashrateHS == nil || *telemetry.HashrateHS <= 0 || len(telemetry.HashBoards) != 3 {
		t.Fatalf("telemetry: %+v %v", telemetry, err)
	}
	if err := clients[0].StopMining(t.Context()); err != nil {
		t.Fatal(err)
	}
	first, err := clients[0].GetSummary(t.Context())
	if err != nil || first.Summary[0].GHS5s != 0 {
		t.Fatalf("HTTP sleep not reflected by TCP: %+v %v", first, err)
	}
	second, err := clients[1].GetSummary(t.Context())
	if err != nil || second.Summary[0].GHS5s <= 0 {
		t.Fatalf("sleep leaked: %+v %v", second, err)
	}
	if err := clients[0].UpdatePools(t.Context(), []antminer.Pool{{URL: "stratum+tcp://changed:3333", WorkerName: "changed", Password: "secret"}}); err != nil {
		t.Fatal(err)
	}
	pools, err := clients[0].GetPools(t.Context())
	if err != nil || pools.Pools[0].User != "changed" {
		t.Fatalf("HTTP pool write not reflected by TCP: %+v %v", pools, err)
	}
	otherPools, err := clients[1].GetMinerConfig(t.Context())
	if err != nil || otherPools.Pools[0].Username == "changed" || otherPools.Pools[0].Password == "secret" {
		t.Fatalf("pool credentials leaked: %+v %v", otherPools, err)
	}

	t.Run("applied write with lost acknowledgement is not replayed", func(t *testing.T) {
		var authenticatedWrites, appliedWrites atomic.Int32
		transport := &http.Transport{}
		defer transport.CloseIdleConnections()
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			forwarded := request.Clone(request.Context())
			forwarded.URL.Scheme = "http"
			forwarded.URL.Host = net.JoinHostPort(addresses[0], fmt.Sprint(webPort))
			forwarded.RequestURI = ""
			write := request.Method == http.MethodPost && request.URL.Path == "/cgi-bin/set_miner_conf.cgi" && request.Header.Get("Authorization") != ""
			if write {
				authenticatedWrites.Add(1)
			}
			response, err := transport.RoundTrip(forwarded)
			if err != nil {
				t.Errorf("forward to actual fixture: %v", err)
				http.Error(w, err.Error(), 502)
				return
			}
			defer response.Body.Close()
			if write && response.StatusCode == http.StatusOK {
				// The real handler has applied the mutation before returning success.
				if _, err := io.Copy(io.Discard, response.Body); err != nil {
					t.Errorf("read backend acknowledgement: %v", err)
				}
				appliedWrites.Add(1)
				panic(http.ErrAbortHandler) // Lose the acknowledgement after application.
			}
			for key, values := range response.Header {
				w.Header()[key] = values
			}
			w.WriteHeader(response.StatusCode)
			_, _ = io.Copy(w, response.Body)
		}))
		defer proxy.Close()
		endpoint, err := url.Parse(proxy.URL)
		if err != nil {
			t.Fatal(err)
		}
		var port int32
		if _, err := fmt.Sscan(endpoint.Port(), &port); err != nil {
			t.Fatal(err)
		}
		client, err := antminer.NewClient(addresses[0], rpcPort, port, "http")
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err := client.Pair(t.Context(), sdk.UsernamePassword{Username: "root", Password: "root"}); err != nil {
			t.Fatal(err)
		}
		err = client.UpdatePools(t.Context(), []antminer.Pool{{URL: "stratum+tcp://lost-ack:3333", WorkerName: "applied-without-ack", Password: "secret"}})
		if err == nil {
			t.Fatal("lost acknowledgement reported success")
		}
		config, err := clients[0].GetMinerConfig(t.Context())
		if err != nil || len(config.Pools) != 1 || config.Pools[0].Username != "applied-without-ack" {
			t.Fatalf("lost acknowledgement write was not applied: %+v %v", config, err)
		}
		if got, applied := authenticatedWrites.Load(), appliedWrites.Load(); got != 1 || applied != 1 {
			t.Fatalf("uncertain write replayed: authenticated=%d applied=%d", got, applied)
		}
	})
	if err := clients[0].BlinkLED(t.Context(), time.Minute); err != nil {
		t.Fatal(err)
	}
	for i, address := range addresses {
		active, status := readLoadLEDAudit(t, address, webPort, "root")
		if status != http.StatusOK || active != (i == 0) {
			t.Fatalf("destination %s LED audit: active=%v status=%d", address, active, status)
		}
	}
	if err := clients[0].ChangePassword(t.Context(), "root", "changed-password"); err != nil {
		t.Fatal(err)
	}
	if _, status := readLoadLEDAudit(t, addresses[1], webPort, "changed-password"); status != http.StatusUnauthorized {
		t.Fatalf("other destination's changed password accepted by LED audit: %d", status)
	}
	if err := clients[1].SetCredentials(sdk.UsernamePassword{Username: "root", Password: "changed-password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := clients[1].GetDeviceInfo(t.Context()); err == nil {
		t.Fatal("another IP accepted changed password")
	}
	if err := clients[1].SetCredentials(sdk.UsernamePassword{Username: "root", Password: "root"}); err != nil {
		t.Fatal(err)
	}
	if _, err := clients[1].GetDeviceInfo(t.Context()); err != nil {
		t.Fatalf("other miner default credential changed: %v", err)
	}
	if err := clients[0].SetCredentials(sdk.UsernamePassword{Username: "root", Password: "root"}); err != nil {
		t.Fatal(err)
	}
	if _, err := clients[0].GetDeviceInfo(t.Context()); err == nil {
		t.Fatal("changed miner accepted old password")
	}
	deadlineCtx, stop := context.WithTimeout(t.Context(), time.Millisecond)
	defer stop()
	if _, err := clients[1].GetDeviceInfo(deadlineCtx); err == nil {
		t.Fatal("deadline did not expire against delayed fixture")
	}
}

// The audit is fixture-only, so authenticate its GET directly while BlinkLED
// above still uses the actual production client and write path.
func readLoadLEDAudit(t *testing.T, address string, port int32, password string) (bool, int) {
	t.Helper()
	endpoint := "http://" + net.JoinHostPort(address, fmt.Sprint(port)) + "/fake-api/v1/load/led"
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	challenge := response.Header.Get("WWW-Authenticate")
	response.Body.Close()
	match := regexp.MustCompile(`nonce="([^"]+)"`).FindStringSubmatch(challenge)
	if response.StatusCode != http.StatusUnauthorized || len(match) != 2 {
		t.Fatalf("audit challenge: %d %q", response.StatusCode, challenge)
	}
	hash := func(value string) string { sum := md5.Sum([]byte(value)); return hex.EncodeToString(sum[:]) }
	const path = "/fake-api/v1/load/led"
	digest := hash(hash("root:Antminer:"+password) + ":" + match[1] + ":00000001:test:auth:" + hash("GET:"+path))
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", fmt.Sprintf(`Digest username="root", realm="Antminer", nonce="%s", uri="%s", qop=auth, nc=00000001, cnonce="test", response="%s"`, match[1], path, digest))
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, response.StatusCode
	}
	var result struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result.Active, response.StatusCode
}
