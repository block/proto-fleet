package driver

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

// secondLocalIPv4 returns a second destination for the fixture's wildcard
// listener. Linux routes all of 127.0.0.0/8 to loopback listeners; elsewhere a
// LAN address is needed.
func secondLocalIPv4(t *testing.T) string {
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

// Exercise the owning fixture through the current plugin rather than duplicating
// its REST expectations. Both destinations reach one wildcard TLS listener.
func TestLoadFixtureProtoRESTContract(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the owning fixture")
	}

	// Arrange
	secondIP := secondLocalIPv4(t)
	directory := t.TempDir()
	binary := filepath.Join(directory, "fake-proto-rig")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	build.Dir = "../../../../server/fake-proto-rig"
	build.Env = append(os.Environ(), "GOWORK=off")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "fixture build: %s", output)
	addressPath := filepath.Join(directory, "addresses.txt")
	require.NoError(t, os.WriteFile(addressPath, []byte("127.0.0.1\n"+secondIP+"\n"), 0600))
	reference := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := reference.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	require.NoError(t, err)
	reference.Close()
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600))
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600))
	listener, err := net.Listen("tcp4", "0.0.0.0:0") // #nosec G102 -- Reserve one port for both local test destinations.
	require.NoError(t, err)
	tcpAddress, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := tcpAddress.Port
	require.NoError(t, listener.Close())
	command := exec.Command(binary)
	command.Env = append(os.Environ(), "HTTP_PORT="+strconv.Itoa(port), "GRPC_PORT=", "FAKE_MINER_ADDRESSES_FILE="+addressPath, "FAKE_RIG_PASSWORD=load-password", "FAKE_RIG_DEFAULT_PASSWORD=", "FAKE_MINER_RESPONSE_DELAY=1ms", "FAKE_MINER_METRICS_FILE=", "TLS_CERT_FILE="+certPath, "TLS_KEY_FILE="+keyPath)
	var logs bytes.Buffer
	command.Stdout, command.Stderr = &logs, &logs
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case err := <-done:
			require.NoError(t, err, "fixture logs: %s", logs.String())
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("fixture shutdown timed out")
		}
	})
	require.Eventually(t, func() bool {
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 50*time.Millisecond)
		if err != nil {
			return false
		}
		connection.Close()
		return true
	}, 3*time.Second, 10*time.Millisecond)
	d, err := New(port)
	require.NoError(t, err)
	credentials := sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "admin", Password: "load-password"}}

	// Act
	var infos []sdk.DeviceInfo
	var devices []sdk.Device
	for index, ip := range []string{"127.0.0.1", secondIP} {
		info, err := d.DiscoverDevice(t.Context(), ip, strconv.Itoa(port))
		require.NoError(t, err)
		require.Equal(t, "https", info.URLScheme)
		require.Equal(t, sdk.DeviceClassRig, info.DeviceClass)
		paired, err := d.PairDevice(t.Context(), info, credentials)
		require.NoError(t, err)
		result, err := d.NewDevice(t.Context(), "load-rig-"+strconv.Itoa(index), paired, credentials)
		require.NoError(t, err)
		device := result.Device
		t.Cleanup(func() { require.NoError(t, device.Close(t.Context())) })
		infos = append(infos, info)
		devices = append(devices, device)
	}

	// Assert
	require.NotEqual(t, infos[0].SerialNumber, infos[1].SerialNumber)
	require.NotEqual(t, infos[0].MacAddress, infos[1].MacAddress)

	// Act
	wrong := sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "admin", Password: "wrong-password"}}
	_, err = d.PairDevice(t.Context(), infos[0], wrong)

	// Assert
	require.Error(t, err, "fixture must reject incorrect credentials")

	// Act
	require.NoError(t, devices[0].UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{{URL: "stratum+tcp://pool.example:3333", WorkerName: "worker-a", Password: "pool-password"}}))

	// Arrange
	// The fixture-only audit reads the effect of the production LED write.
	// Before the existing per-device checks blink both rigs, prove isolation.
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})))
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}}
	t.Cleanup(transport.CloseIdleConnections)
	auditClient := &http.Client{Transport: transport, Timeout: 3 * time.Second}

	// Act
	require.NoError(t, devices[0].BlinkLED(t.Context()))

	// Assert
	for index, ip := range []string{"127.0.0.1", secondIP} {
		active, status := readProtoLoadLEDAudit(t, auditClient, ip, port, "load-password")
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, index == 0, active, "LED writes must not leak between socket destinations")
	}
	for index, device := range devices {
		pools, err := device.GetMiningPools(t.Context())
		require.NoError(t, err)
		if index == 0 {
			require.Len(t, pools, 1)
			require.Equal(t, "worker-a", pools[0].Username)
		} else {
			require.Empty(t, pools, "pool writes must not leak between socket destinations")
		}
		metrics, err := device.Status(t.Context())
		require.NoError(t, err)
		require.Len(t, metrics.HashBoards, 4)
		require.Len(t, metrics.PSUMetrics, 2)
		require.NoError(t, device.BlinkLED(t.Context()))
	}

	t.Run("applied write with lost acknowledgement is not replayed", func(t *testing.T) {
		// Arrange
		var attemptedWrites, appliedWrites atomic.Int32
		proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			forwarded := request.Clone(request.Context())
			forwarded.URL.Scheme = "https"
			forwarded.URL.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			forwarded.RequestURI = ""
			write := request.Method == http.MethodPost && request.URL.Path == "/api/v1/pools"
			if write {
				attemptedWrites.Add(1)
				if request.Header.Get("Authorization") == "" {
					t.Error("pool write must use the production bearer authentication")
				}
			}
			response, err := transport.RoundTrip(forwarded)
			if err != nil {
				t.Errorf("forward to actual TLS fixture: %v", err)
				http.Error(w, "fixture unavailable", http.StatusBadGateway)
				return
			}
			defer response.Body.Close()
			if write && response.StatusCode == http.StatusOK {
				// Drain the real handler's success before losing its acknowledgement.
				if _, err := io.Copy(io.Discard, response.Body); err != nil {
					t.Errorf("read backend acknowledgement: %v", err)
					return
				}
				appliedWrites.Add(1)
				panic(http.ErrAbortHandler)
			}
			for key, values := range response.Header {
				w.Header()[key] = values
			}
			w.WriteHeader(response.StatusCode)
			_, _ = io.Copy(w, response.Body)
		}))
		defer proxy.Close()
		endpoint, err := url.Parse(proxy.URL)
		require.NoError(t, err)
		proxyPort, err := strconv.ParseInt(endpoint.Port(), 10, 32)
		require.NoError(t, err)
		proxyInfo := infos[0]
		proxyInfo.Host = endpoint.Hostname()
		proxyInfo.Port = int32(proxyPort)
		paired, err := d.PairDevice(t.Context(), proxyInfo, credentials)
		require.NoError(t, err)
		result, err := d.NewDevice(t.Context(), "lost-ack-rig", paired, credentials)
		require.NoError(t, err)
		defer func() { require.NoError(t, result.Device.Close(t.Context())) }()

		// Act
		err = result.Device.UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{{URL: "stratum+tcp://lost-ack.example:3333", WorkerName: "applied-without-ack", Password: "pool-secret"}})

		// Assert
		require.Error(t, err, "lost acknowledgement must not report success")
		// Independent native readback bypasses the proxy that dropped the ACK.
		pools, err := devices[0].GetMiningPools(t.Context())
		require.NoError(t, err)
		require.Len(t, pools, 1)
		require.Equal(t, "applied-without-ack", pools[0].Username)
		require.Equal(t, "stratum+tcp://lost-ack.example:3333", pools[0].URL)
		require.EqualValues(t, 1, attemptedWrites.Load(), "uncertain writes must not be replayed")
		require.EqualValues(t, 1, appliedWrites.Load())
	})

	// Act
	require.NoError(t, devices[0].UpdateMinerPassword(t.Context(), "load-password", "changed-password"))

	// Assert
	changed := sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "admin", Password: "changed-password"}}
	_, err = d.PairDevice(t.Context(), infos[0], credentials)
	require.Error(t, err, "changed destination must reject its old password")
	_, err = d.PairDevice(t.Context(), infos[0], changed)
	require.NoError(t, err)
	_, err = d.PairDevice(t.Context(), infos[1], changed)
	require.Error(t, err, "changed credentials must not leak to the second destination")
	_, err = d.PairDevice(t.Context(), infos[1], credentials)
	require.NoError(t, err)
	active, status := readProtoLoadLEDAudit(t, auditClient, "127.0.0.1", port, "changed-password")
	require.Equal(t, http.StatusOK, status)
	require.True(t, active)
	_, status = readProtoLoadLEDAudit(t, auditClient, secondIP, port, "changed-password")
	require.Equal(t, http.StatusUnauthorized, status)
}

// Only the load-only audit uses a direct HTTP request. LED writes and password
// rotation above always go through the production SDK driver.
func readProtoLoadLEDAudit(t *testing.T, client *http.Client, address string, port int, password string) (bool, int) {
	t.Helper()
	base := "https://" + net.JoinHostPort(address, strconv.Itoa(port))
	body, err := json.Marshal(map[string]string{"password": password})
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/api/v1/auth/login", bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, response.StatusCode
	}
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&tokens))
	require.NotEmpty(t, tokens.AccessToken)
	request, err = http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/fake-api/v1/load/led", nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	audit, err := client.Do(request)
	require.NoError(t, err)
	defer audit.Body.Close()
	if audit.StatusCode != http.StatusOK {
		return false, audit.StatusCode
	}
	var state struct {
		Active bool `json:"active"`
	}
	require.NoError(t, json.NewDecoder(audit.Body).Decode(&state))
	return state.Active, audit.StatusCode
}
