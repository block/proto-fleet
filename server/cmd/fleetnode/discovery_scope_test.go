package main

import (
	"context"
	"fmt"
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
				want       []string
			}{
				{"narrow", []string{"10.0.0.2-10.0.0.3"}, true, []string{"10.0.0.2", "10.0.0.3"}},
				{"outside only", []string{"10.0.1.1"}, true, nil},
				{"empty", []string{}, false, nil},
				{"missing", nil, false, nil},
			} {
				t.Run(scope.name, func(t *testing.T) {
					// Arrange
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

					// Act
					reports, truncated, err := runner.discoverForCommand(t.Context(), request.request, discardLogger(t))

					// Assert
					require.False(t, truncated)
					if !scope.configured {
						require.ErrorContains(t, err, "configure discovery-ranges")
						require.Empty(t, reports)
						require.Empty(t, discoverer.probed, "an unconfigured node must never invoke a plugin probe")
						require.Zero(t, scannerCalls, "an unconfigured node must never invoke the TCP scanner")
						return
					}
					require.NoError(t, err)
					var wantProbes []string
					for _, addr := range scope.want {
						wantProbes = append(wantProbes, addr+"|4028")
					}
					require.ElementsMatch(t, wantProbes, discoverer.probed, "outside targets must not reach plugin probing, even if the plugin identifies them")
					if request.network {
						require.Equal(t, 1, scannerCalls)
						require.ElementsMatch(t, scope.want, scanned, "outside targets must not reach TCP scanning")
					} else {
						require.Zero(t, scannerCalls, "direct discovery retains its no-TCP-scan behavior")
					}
					require.Len(t, reports, len(scope.want))
				})
			}
		})
	}
}

func TestDiscoverForCommandAutomaticScanUsesEffectiveRanges(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured []string
		legacy     string
		want       []string
	}{
		{"multiple normalized ranges override legacy subnet", []string{"10.0.1.2-10.0.1.3", "10.0.0.1/30", "10.0.1.2", "10.0.0.2"}, "192.168.1.0/24", []string{"10.0.0.0", "10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.1.2", "10.0.1.3"}},
		{"legacy fallback", nil, "10.0.0.1/30", []string{"10.0.0.0", "10.0.0.1", "10.0.0.2", "10.0.0.3"}},
		{"explicit empty disables", []string{}, "10.0.0.0/30", nil},
		{"missing disables", nil, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			discoverer := &scopeRecordingDiscoverer{stubDiscoverer: &stubDiscoverer{}}
			var scanned []string
			r := &RunCmd{
				DiscoveryRanges:      tc.configured,
				LocalDiscoverySubnet: tc.legacy,
				discoverer:           discoverer,
				scanner: scanFunc(func(_ context.Context, targets iter.Seq[netip.Addr], ports []uint16, emit func(netscan.HostResult) error) error {
					for addr := range targets {
						scanned = append(scanned, addr.String())
						if err := emit(netscan.HostResult{Addr: addr, OpenPorts: ports}); err != nil {
							return err
						}
					}
					return nil
				}),
			}
			reports, truncated, err := r.discoverForCommand(t.Context(), discoverNetworkScan(netscan.LocalSubnetTarget, []string{"4028"}), discardLogger(t))
			if len(tc.want) == 0 {
				require.ErrorContains(t, err, "configure discovery-ranges")
			} else {
				require.NoError(t, err)
			}
			require.False(t, truncated)
			require.Empty(t, reports)
			require.ElementsMatch(t, tc.want, scanned)
			var wantProbes []string
			for _, addr := range tc.want {
				wantProbes = append(wantProbes, addr+"|4028")
			}
			require.ElementsMatch(t, wantProbes, discoverer.probed)
		})
	}
}

func TestDiscoverForCommandAutomaticScanTargetLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ranges []string
		want   int
	}{
		{"4096 unique addresses", []string{"10.0.0.0/20", "10.0.0.1"}, 4096},
		{"4097 unique addresses", []string{"10.0.0.0/20", "10.0.16.0"}, 0},
		{"broad configured range", []string{"10.0.0.0/8"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scanned, scannerCalls := 0, 0
			r := &RunCmd{DiscoveryRanges: tc.ranges, discoverer: &stubDiscoverer{}, scanner: scanFunc(func(_ context.Context, targets iter.Seq[netip.Addr], _ []uint16, _ func(netscan.HostResult) error) error {
				scannerCalls++
				for range targets {
					scanned++
				}
				return nil
			})}
			reports, truncated, err := r.discoverForCommand(t.Context(), discoverNetworkScan(netscan.LocalSubnetTarget, []string{"4028"}), discardLogger(t))
			if tc.want == 0 {
				var ce *commandError
				require.ErrorAs(t, err, &ce)
				require.Equal(t, gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE, ce.code)
				require.ErrorContains(t, err, fmt.Sprintf("exceeds the limit of %d", maxIPsPerCommand))
				require.Zero(t, scannerCalls)
			} else {
				require.NoError(t, err)
			}
			require.False(t, truncated)
			require.Empty(t, reports)
			require.Equal(t, tc.want, scanned, "over-cap automatic discovery must reject before scanning any address")
		})
	}
}
