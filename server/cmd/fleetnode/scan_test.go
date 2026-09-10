package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
)

type scanFunc func(context.Context, iter.Seq[netip.Addr], []uint16, func(netscan.HostResult) error) error

func (f scanFunc) Scan(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, emit func(netscan.HostResult) error) error {
	return f(ctx, addrs, ports, emit)
}

type allOpenScanner struct{}

func (allOpenScanner) Scan(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, emit func(netscan.HostResult) error) error {
	for addr := range addrs {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if err := emit(netscan.HostResult{Addr: addr, OpenPorts: ports}); err != nil {
			return err
		}
	}
	return nil
}

type stubResolver map[string][]net.IPAddr

func (r stubResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	return r[host], nil
}

func TestExplicitDiscoveryIdentifiesDevicesWithoutTCPListeners(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  *pairingpb.DiscoverRequest
	}{
		{"IP list", discoverIPList([]string{"10.255.0.0", "10.255.0.1", "10.255.0.1"}, []string{"4028", "080", "80"})},
		{"IP range", &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpRange{IpRange: &pairingpb.IPRangeModeRequest{StartIp: "10.255.0.0", EndIp: "10.255.0.1", Ports: []string{"4028", "080", "80"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Like the virtual driver, this discoverer identifies configured
			// addresses in memory; no network listeners back these devices.
			r := &RunCmd{
				discoverer: &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
					"10.255.0.0|4028": {DeviceIdentifier: "virtual-one", DriverName: "virtual", UrlScheme: "http"},
					"10.255.0.1|80":   {DeviceIdentifier: "virtual-two", DriverName: "virtual", UrlScheme: "http"},
				}},
				scanner: scanFunc(func(context.Context, iter.Seq[netip.Addr], []uint16, func(netscan.HostResult) error) error {
					t.Error("explicit discovery must not require open TCP ports")
					return nil
				}),
			}
			reports, truncated, err := r.discoverForCommand(t.Context(), tc.req, discardLogger(t))
			require.NoError(t, err)
			assert.False(t, truncated)
			require.Len(t, reports, 2)
			identifiers := []string{reports[0].GetDeviceIdentifier(), reports[1].GetDeviceIdentifier()}
			assert.ElementsMatch(t, []string{"virtual-one", "virtual-two"}, identifiers)
		})
	}
}

func TestExplicitDiscoveryCancelsPluginProbes(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  *pairingpb.DiscoverRequest
	}{
		{"IP list", discoverIPList([]string{"10.255.0.1"}, []string{"4028"})},
		{"IP range", &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpRange{IpRange: &pairingpb.IPRangeModeRequest{StartIp: "10.255.0.1", EndIp: "10.255.0.1", Ports: []string{"4028"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			disc := newBlockingDiscoverer("10.255.0.1")
			r := &RunCmd{discoverer: disc}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, _, err := r.discoverForCommand(ctx, tc.req, discardLogger(t))
				done <- err
			}()
			select {
			case <-disc.started["10.255.0.1"]:
			case <-time.After(time.Second):
				t.Fatal("plugin identification did not start")
			}
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("plugin identification did not stop on cancellation")
			}
		})
	}
}

func discoverNetworkScan(target string, ports []string) *pairingpb.DiscoverRequest {
	return &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_NetworkScan{NetworkScan: &pairingpb.NetworkScanModeRequest{Target: target, Ports: ports}}}
}

func TestNetworkScanOnlyIdentifiesOpenPorts(t *testing.T) {
	var scanned []netip.Addr
	disc := &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
		"10.0.0.1|4028": {DeviceIdentifier: "open", DriverName: "antminer", UrlScheme: "http"},
		"10.0.0.1|80":   {DeviceIdentifier: "closed", DriverName: "antminer", UrlScheme: "http"},
	}}
	r := &RunCmd{discoverer: disc, scanner: scanFunc(func(_ context.Context, addrs iter.Seq[netip.Addr], ports []uint16, emit func(netscan.HostResult) error) error {
		scanned = slices.Collect(addrs)
		assert.Equal(t, []uint16{80, 4028}, ports)
		return emit(netscan.HostResult{Addr: netip.MustParseAddr("10.0.0.1"), OpenPorts: []uint16{4028}})
	})}
	reports, truncated, err := r.discoverForCommand(t.Context(), discoverNetworkScan("10.0.0.0/30", []string{"4028", "80"}), discardLogger(t))
	require.NoError(t, err)
	assert.False(t, truncated)
	require.Len(t, reports, 1)
	assert.Equal(t, "open", reports[0].DeviceIdentifier)
	assert.Equal(t, []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2")}, scanned)
}

func TestScanAndProbeWithRealTCPListener(t *testing.T) {
	open, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = open.Close() })
	openAddr, ok := open.Addr().(*net.TCPAddr)
	require.True(t, ok)
	openPort := openAddr.AddrPort().Port()
	r := &RunCmd{discoverer: &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
		"127.0.0.1|" + strconv.Itoa(int(openPort)): {DeviceIdentifier: "open", DriverName: "antminer", UrlScheme: "http"},
	}}}
	reports, truncated, err := r.scanAndProbe(t.Context(), slices.Values([]netip.Addr{netip.MustParseAddr("127.0.0.1")}), []uint16{openPort}, discardLogger(t))
	require.NoError(t, err)
	assert.False(t, truncated)
	require.Len(t, reports, 1)
	assert.Equal(t, "open", reports[0].DeviceIdentifier)
}

func TestDiscoveryIdentificationStartsBeforeScanCompletes(t *testing.T) {
	disc := newBlockingDiscoverer("10.0.0.1")
	identified := make(chan struct{})
	r := &RunCmd{discoverer: disc, scanner: scanFunc(func(ctx context.Context, _ iter.Seq[netip.Addr], _ []uint16, emit func(netscan.HostResult) error) error {
		if err := emit(netscan.HostResult{Addr: netip.MustParseAddr("10.0.0.1"), OpenPorts: []uint16{4028}}); err != nil {
			return err
		}
		select {
		case <-disc.started["10.0.0.1"]:
			close(identified)
			return errors.New("scan stopped after identification began")
		case <-ctx.Done():
			return ctx.Err()
		}
	})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := r.discoverForCommand(ctx, discoverNetworkScan("10.0.0.1", []string{"4028"}), discardLogger(t))
		done <- err
	}()
	select {
	case <-identified:
	case <-time.After(time.Second):
		t.Fatal("identification did not start while scanning")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorContains(t, err, "scan stopped")
	case <-time.After(time.Second):
		t.Fatal("scan did not drain on cancellation")
	}
}

func TestControlLoopRetainsReportsAfterScannerFailure(t *testing.T) {
	for _, reportFailure := range []bool{false, true} {
		t.Run(strconv.FormatBool(reportFailure), func(t *testing.T) {
			r := &RunCmd{
				discoverer: &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
					"10.0.0.1|4028": {DeviceIdentifier: "identified", DriverName: "antminer", UrlScheme: "http"},
				}},
				scanner: scanFunc(func(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, emit func(netscan.HostResult) error) error {
					if err := (allOpenScanner{}).Scan(ctx, addrs, ports, emit); err != nil {
						return err
					}
					return errors.New("local socket resource failure")
				}),
			}
			fake := &controlFakeGateway{}
			if reportFailure {
				fake.setBehavior(controlFakeBehavior{reportErr: errors.New("upload failed")})
			}
			fake.queue(discoverPayload(t, discoverNetworkScan("10.0.0.1", []string{"4028"})))
			runControlLoopOnce(t, r, fake)
			require.Len(t, fake.reportsCopy(), 1)
			require.Len(t, fake.reportsCopy()[0].Devices, 1)
			want := pb.AckCode_ACK_CODE_SCAN_FAILED
			if reportFailure {
				want = pb.AckCode_ACK_CODE_REPORT_FAILED
			}
			assert.Equal(t, want, fake.acksCopy()[0].Code)
		})
	}
}

func TestControlLoopRetainsReportsAfterScannerDeadline(t *testing.T) {
	r := &RunCmd{
		discoverer: &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
			"10.0.0.1|4028": {DeviceIdentifier: "identified", DriverName: "antminer", UrlScheme: "http"},
		}},
		scanner: scanFunc(func(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, emit func(netscan.HostResult) error) error {
			if err := (allOpenScanner{}).Scan(ctx, addrs, ports, emit); err != nil {
				return err
			}
			return context.DeadlineExceeded
		}),
	}
	fake := &controlFakeGateway{}
	fake.queue(discoverPayload(t, discoverNetworkScan("10.0.0.1", []string{"4028"})))
	runControlLoopOnce(t, r, fake)
	require.Len(t, fake.reportsCopy(), 1)
	require.Len(t, fake.reportsCopy()[0].Devices, 1)
	assert.Equal(t, pb.AckCode_ACK_CODE_PARTIAL, fake.acksCopy()[0].Code)
}

func TestControlLoopClosedPortsSendNoEmptyReports(t *testing.T) {
	r := &RunCmd{discoverer: &stubDiscoverer{}, scanner: scanFunc(func(context.Context, iter.Seq[netip.Addr], []uint16, func(netscan.HostResult) error) error {
		return nil
	})}
	fake := &controlFakeGateway{}
	fake.queue(discoverPayload(t, discoverNetworkScan("10.0.0.1", []string{"4028"})))
	runControlLoopOnce(t, r, fake)
	assert.Empty(t, fake.reportsCopy())
	assert.Equal(t, pb.AckCode_ACK_CODE_OK, fake.acksCopy()[0].Code)
}

func TestNetworkTargetsLocalSubnetPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		subnets []string
		want    int
	}{
		{"private subnet", []string{"10.0.0.0/30"}, 2},
		{"public subnet", []string{"8.8.8.0/24"}, 0},
		{"IPv6 subnet", []string{"fd00::/120"}, 0},
		{"broad subnet", []string{"10.0.0.0/16"}, 0},
		{"aggregate over cap", []string{fmt.Sprintf("10.0.0.0/%d", discoverylimits.MinIPv4PrefixBits), fmt.Sprintf("10.128.0.0/%d", discoverylimits.MinIPv4PrefixBits)}, 0},
		{"empty detection", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &RunCmd{localSubnets: func() ([]string, error) { return tc.subnets, nil }}
			addrs, err := r.networkScanTargets(t.Context(), &pairingpb.NetworkScanModeRequest{Target: netscan.LocalSubnetTarget})
			if tc.want == 0 {
				var ce *commandError
				require.ErrorAs(t, err, &ce)
				assert.Equal(t, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, ce.code)
				return
			}
			require.NoError(t, err)
			assert.Len(t, slices.Collect(addrs), tc.want)
		})
	}
}

func TestConfiguredSubnetAvoidsPlatformDetection(t *testing.T) {
	r := &RunCmd{LocalDiscoverySubnet: "10.90.0.0/30"}
	addrs, err := r.networkScanTargets(t.Context(), &pairingpb.NetworkScanModeRequest{Target: netscan.LocalSubnetTarget})
	require.NoError(t, err)
	assert.Len(t, slices.Collect(addrs), 2)
}

func TestDiscoveryCommandTargetBoundaries(t *testing.T) {
	addresses := make([]string, 4096)
	devices := make(map[string]*pb.DiscoveredDeviceReport, len(addresses))
	for i := range addresses {
		addresses[i] = fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		devices[addresses[i]+"|80"] = &pb.DiscoveredDeviceReport{DeviceIdentifier: addresses[i], DriverName: "virtual", UrlScheme: "http"}
	}
	for _, tc := range []struct {
		name string
		req  *pairingpb.DiscoverRequest
		want int
	}{
		{"list at 4096", discoverIPList(addresses, []string{"80"}), 4096},
		{"list over 4096", discoverIPList(append(slices.Clone(addresses), "10.0.16.0"), []string{"80"}), 0},
		{"range at 4096", &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpRange{IpRange: &pairingpb.IPRangeModeRequest{StartIp: "10.0.0.0", EndIp: "10.0.15.255", Ports: []string{"80"}}}}, 4096},
		{"range over 4096", &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpRange{IpRange: &pairingpb.IPRangeModeRequest{StartIp: "10.0.0.0", EndIp: "10.0.16.0", Ports: []string{"80"}}}}, 0},
		{"network at /20", &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_NetworkScan{NetworkScan: &pairingpb.NetworkScanModeRequest{Target: "10.0.0.0/20", Ports: []string{"80"}}}}, 4094},
		{"network over /20", &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_NetworkScan{NetworkScan: &pairingpb.NetworkScanModeRequest{Target: "10.0.0.0/19", Ports: []string{"80"}}}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &RunCmd{discoverer: &stubDiscoverer{probes: devices}}
			if tc.req.GetNetworkScan() != nil {
				r.scanner = allOpenScanner{}
			}
			reports, _, err := r.discoverForCommand(t.Context(), tc.req, discardLogger(t))
			if tc.want == 0 {
				var ce *commandError
				require.ErrorAs(t, err, &ce)
				assert.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, ce.code)
				assert.Empty(t, reports, "over-cap requests must fail before discovery")
				return
			}
			require.NoError(t, err)
			require.Len(t, reports, tc.want)
		})
	}
}

func TestIPListChoosesPrivateDNSBeforeIPv4Preference(t *testing.T) {
	r := &RunCmd{
		discoverer: &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
			"fd00::1|4028": {DeviceIdentifier: "identified", DriverName: "virtual", UrlScheme: "http"},
		}},
		resolver: stubResolver{"miner.lan": {{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("fd00::1")}}},
	}
	reports, _, err := r.discoverForCommand(t.Context(), discoverIPList([]string{"miner.lan"}, []string{"4028"}), discardLogger(t))
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Equal(t, "fd00::1", reports[0].GetIpAddress())
}

type reportBudgetClient struct {
	gatewayClient
	mu        sync.Mutex
	deadlines []time.Time
	counts    []int
}

func (c *reportBudgetClient) ReportDiscoveredDevices(ctx context.Context, req *connect.Request[pb.ReportDiscoveredDevicesRequest]) (*connect.Response[pb.ReportDiscoveredDevicesResponse], error) {
	deadline, _ := ctx.Deadline()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadlines = append(c.deadlines, deadline)
	c.counts = append(c.counts, len(req.Msg.Devices))
	return connect.NewResponse(&pb.ReportDiscoveredDevicesResponse{}), nil
}

func TestStreamReportsHasOneTotalUploadDeadline(t *testing.T) {
	client := &reportBudgetClient{}
	reports := make([]*pb.DiscoveredDeviceReport, 2*maxDevicesPerReport+1)
	for i := range reports {
		reports[i] = &pb.DiscoveredDeviceReport{}
	}
	started := time.Now()
	err := (&RunCmd{}).streamReports(t.Context(), client, "command", reports, discardLogger(t))
	require.NoError(t, err)
	assert.Equal(t, []int{1024, 1024, 1}, client.counts)
	require.Len(t, client.deadlines, 3)
	assert.WithinDuration(t, started.Add(discoveryReportTimeout), client.deadlines[0], time.Second)
	assert.Equal(t, client.deadlines[0], client.deadlines[1])
	assert.Equal(t, client.deadlines[0], client.deadlines[2])
}
