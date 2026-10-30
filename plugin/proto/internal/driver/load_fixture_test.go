package driver

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

// Exercise the owning fixture through the current plugin rather than duplicating
// its REST expectations. Both destinations reach one wildcard TLS listener.
func TestLoadFixtureProtoRESTContract(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the owning fixture")
	}
	addresses, err := net.InterfaceAddrs()
	require.NoError(t, err)
	secondIP := ""
	for _, address := range addresses {
		ip, ok := address.(*net.IPNet)
		if ok && ip.IP.To4() != nil && !ip.IP.IsLoopback() && !ip.IP.IsLinkLocalUnicast() {
			secondIP = ip.IP.String()
			break
		}
	}
	if secondIP == "" {
		t.Skip("two-destination test needs another local IPv4 interface")
	}
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
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
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
	require.NotEqual(t, infos[0].SerialNumber, infos[1].SerialNumber)
	require.NotEqual(t, infos[0].MacAddress, infos[1].MacAddress)
	wrong := sdk.SecretBundle{Version: "v1", Kind: sdk.UsernamePassword{Username: "admin", Password: "wrong-password"}}
	_, err = d.PairDevice(t.Context(), infos[0], wrong)
	require.Error(t, err, "fixture must reject incorrect credentials")
	require.NoError(t, devices[0].UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{{URL: "stratum+tcp://pool.example:3333", WorkerName: "worker-a", Password: "pool-password"}}))
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
}
