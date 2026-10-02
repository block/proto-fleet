package ha

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"time"

	"github.com/block/proto-fleet/server/internal/transportguard"
)

// EndpointHeartbeatFile is refreshed only after keepalived's local proxy check succeeds.
const EndpointHeartbeatFile = "/run/proto-fleet-ha/endpoint-heartbeat"

func configuredEndpoint(config Config, serviceTLS *tls.Config) (RuntimeConfig, func()) {
	if config.EndpointMode != endpointModeExternal {
		ip := netip.MustParseAddr(config.EndpointIP)
		return RuntimeConfig{
			EndpointHealthy: newEndpointHealth(ip, config.EndpointInterface, EndpointHeartbeatFile, endpointHeartbeatTimeout),
			EndpointOwned:   newEndpointOwned(ip, config.EndpointInterface),
		}, func() {}
	}
	return externalEndpoint(config, serviceTLS, "127.0.0.1:443")
}

func externalEndpoint(config Config, serviceTLS *tls.Config, localAddress string) (RuntimeConfig, func()) {
	// Probe only this host's nginx with its verified node identity. The load
	// balancer is not an ownership authority and must never feed election.
	transport := &http.Transport{
		TLSClientConfig: serviceTLS.Clone(), Proxy: nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, localAddress)
		},
	}
	client := &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: transportguard.RejectRedirect}
	return RuntimeConfig{EndpointHealthy: localProxyHealth(client, "https://"+config.EndpointNodeIP+"/api-proxy/health/active")}, transport.CloseIdleConnections
}

func localProxyHealth(client *http.Client, endpoint string) func() bool {
	return func() bool {
		response, err := client.Get(endpoint)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return response.StatusCode == http.StatusOK
	}
}

func newEndpointOwned(endpointIP netip.Addr, endpointInterface string) func() bool {
	return func() bool { return localAddressAssigned(endpointIP, endpointInterface) }
}

// Endpoint health requires both local VIP ownership and proof that keepalived
// is still successfully probing the active client path.
func newEndpointHealth(endpointIP netip.Addr, endpointInterface, heartbeatFile string, timeout time.Duration) func() bool {
	return func() bool {
		if !localAddressAssigned(endpointIP, endpointInterface) {
			return false
		}
		info, err := os.Stat(heartbeatFile)
		if err != nil {
			return false
		}
		age := time.Since(info.ModTime())
		return age >= 0 && age <= timeout
	}
}

func localAddressAssigned(want netip.Addr, interfaceName string) bool {
	// Re-read every sample because keepalived can move the VIP asynchronously.
	networkInterface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return false
	}
	addresses, err := networkInterface.Addrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr() == want {
			return true
		}
	}
	return false
}
