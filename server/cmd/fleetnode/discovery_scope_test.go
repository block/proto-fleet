package main

import (
	"context"
	"iter"
	"net"
	"net/netip"
	"sync"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	"github.com/stretchr/testify/require"
)

// scopeRecordingDiscoverer records the actual plugin boundary, including probes
// that return no report; checking results alone would miss out-of-scope work.
type scopeRecordingDiscoverer struct {
	*stubDiscoverer
	mu     sync.Mutex
	probed []string
}

func (d *scopeRecordingDiscoverer) Probe(ctx context.Context, ip, port string) (*gatewaypb.DiscoveredDeviceReport, error) {
	d.mu.Lock()
	d.probed = append(d.probed, ip+"|"+port)
	d.mu.Unlock()
	return d.stubDiscoverer.Probe(ctx, ip, port)
}

func TestDiscoverForCommandEnforcesLocalRangesBeforeScanningAndProbing(t *testing.T) {
	requests := []struct {
		name    string
		request *pairingpb.DiscoverRequest
		network bool
	}{
		{"IP list", discoverIPList([]string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}, []string{"4028"}), false},
		{"IP range", &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpRange{IpRange: &pairingpb.IPRangeModeRequest{StartIp: "10.0.0.1", EndIp: "10.0.0.4", Ports: []string{"4028"}}}}, false},
		{"network scan", discoverNetworkScan("10.0.0.1-4", []string{"4028"}), true},
		{"hostname list", discoverIPList([]string{"one.lan", "two.lan", "three.lan", "four.lan"}, []string{"4028"}), false},
	}
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			for _, scope := range []struct {
				name       string
				ranges     []string
				configured bool
			}{
				{"narrow", []string{"10.0.0.2-10.0.0.3"}, true},
				{"empty", []string{}, false},
				{"missing", nil, false},
			} {
				t.Run(scope.name, func(t *testing.T) {
					discoverer := &scopeRecordingDiscoverer{stubDiscoverer: &stubDiscoverer{probes: map[string]*gatewaypb.DiscoveredDeviceReport{
						"10.0.0.1|4028": {DeviceIdentifier: "one", DriverName: "virtual", UrlScheme: "http"},
						"10.0.0.2|4028": {DeviceIdentifier: "two", DriverName: "virtual", UrlScheme: "http"},
						"10.0.0.3|4028": {DeviceIdentifier: "three", DriverName: "virtual", UrlScheme: "http"},
						"10.0.0.4|4028": {DeviceIdentifier: "four", DriverName: "virtual", UrlScheme: "http"},
					}}}
					var scanned []string
					scannerCalls := 0
					runner := &RunCmd{
						DiscoveryRanges: scope.ranges,
						discoverer:      discoverer,
						resolver: stubResolver{
							"one.lan":   {{IP: net.ParseIP("10.0.0.1")}},
							"two.lan":   {{IP: net.ParseIP("10.0.0.2")}},
							"three.lan": {{IP: net.ParseIP("10.0.0.3")}},
							"four.lan":  {{IP: net.ParseIP("10.0.0.4")}},
						},
						scanner: scanFunc(func(ctx context.Context, targets iter.Seq[netip.Addr], ports []uint16, emit func(netscan.HostResult) error) error {
							scannerCalls++
							for target := range targets {
								scanned = append(scanned, target.String())
								if err := emit(netscan.HostResult{Addr: target, OpenPorts: ports}); err != nil {
									return err
								}
							}
							return ctx.Err()
						}),
					}
					reports, truncated, err := runner.discoverForCommand(t.Context(), request.request, discardLogger(t))
					require.False(t, truncated)
					if !scope.configured {
						require.ErrorContains(t, err, "configure discovery-ranges")
						require.Empty(t, reports)
						require.Empty(t, discoverer.probed, "an unconfigured node must never invoke a plugin probe")
						require.Zero(t, scannerCalls, "an unconfigured node must never invoke the TCP scanner")
						return
					}
					require.NoError(t, err)
					require.ElementsMatch(t, []string{"10.0.0.2|4028", "10.0.0.3|4028"}, discoverer.probed, "outside targets must not reach plugin probing, even if the plugin identifies them")
					if request.network {
						require.Equal(t, 1, scannerCalls)
						require.ElementsMatch(t, []string{"10.0.0.2", "10.0.0.3"}, scanned, "outside targets must not reach TCP scanning")
					} else {
						require.Zero(t, scannerCalls, "direct discovery retains its no-TCP-scan behavior")
					}
					require.Len(t, reports, 2)
				})
			}
		})
	}
}
