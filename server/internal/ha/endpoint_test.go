package ha

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExternalEndpointProvesLocalTLSProxyHealth(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api-proxy/health/active", r.URL.Path)
		if status == http.StatusFound {
			w.Header().Set("Location", "/api-proxy/health/active")
		}
		w.WriteHeader(status)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	serviceTLS := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}
	config := Config{EndpointMode: endpointModeExternal, EndpointNodeIP: "127.0.0.1"}
	endpoint, cleanup := externalEndpoint(config, serviceTLS, server.Listener.Addr().String())
	defer cleanup()
	require.Nil(t, endpoint.EndpointOwned, "external routing must not make a passive node claim a VIP")
	probe := endpoint.EndpointHealthy
	require.True(t, probe())
	status = http.StatusServiceUnavailable
	require.False(t, probe())
	status = http.StatusFound
	require.False(t, probe(), "redirects must not prove another proxy healthy")
	status = http.StatusOK
	untrusted, closeUntrusted := externalEndpoint(config, &tls.Config{MinVersion: tls.VersionTLS13}, server.Listener.Addr().String())
	defer closeUntrusted()
	require.False(t, untrusted.EndpointHealthy(), "unknown TLS issuer must fail closed")
	config.EndpointNodeIP = "10.0.1.10"
	wrongIdentity, closeWrongIdentity := externalEndpoint(config, serviceTLS, server.Listener.Addr().String())
	defer closeWrongIdentity()
	require.False(t, wrongIdentity.EndpointHealthy(), "dialing loopback must still verify the configured node identity")
}

func TestEndpointHealthRequiresLocalAddressAndFreshHeartbeat(t *testing.T) {
	// Arrange
	heartbeat := filepath.Join(t.TempDir(), "endpoint-heartbeat")
	require.NoError(t, os.WriteFile(heartbeat, nil, 0o600))
	loopback := interfaceForAddress(t, netip.MustParseAddr("127.0.0.1"))
	healthy := newEndpointHealth(netip.MustParseAddr("127.0.0.1"), loopback, heartbeat, time.Second)

	// Act and assert
	require.True(t, healthy())
	stale := time.Now().Add(-2 * time.Second)
	require.NoError(t, os.Chtimes(heartbeat, stale, stale))
	require.False(t, healthy())

	future := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(heartbeat, future, future))
	require.False(t, healthy())
	fresh := time.Now()
	require.NoError(t, os.Chtimes(heartbeat, fresh, fresh))

	unassigned := newEndpointHealth(netip.MustParseAddr("192.0.2.1"), loopback, heartbeat, time.Second)
	require.False(t, unassigned())

	wrongInterface := newEndpointHealth(netip.MustParseAddr("127.0.0.1"), "missing-interface", heartbeat, time.Second)
	require.False(t, wrongInterface())
}

func interfaceForAddress(t *testing.T, want netip.Addr) string {
	t.Helper()

	interfaces, err := net.Interfaces()
	require.NoError(t, err)
	for _, networkInterface := range interfaces {
		addresses, err := networkInterface.Addrs()
		require.NoError(t, err)
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err == nil && prefix.Addr() == want {
				return networkInterface.Name
			}
		}
	}
	t.Fatalf("no interface owns %s", want)
	return ""
}
