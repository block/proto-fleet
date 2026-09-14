package pairing

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storemocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type discoverFunc func(context.Context, string, string) (*discoverymodels.DiscoveredDevice, error)

func (f discoverFunc) Discover(ctx context.Context, ip, port string) (*discoverymodels.DiscoveredDevice, error) {
	return f(ctx, ip, port)
}

func newScanTestService(t *testing.T, discover discoverFunc) *Service {
	t.Helper()
	ctrl := gomock.NewController(t)
	caps := pairingmocks.NewMockCapabilitiesProvider(ctrl)
	caps.EXPECT().GetMinerCapabilitiesForDevice(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	store := storemocks.NewMockDiscoveredDeviceStore(ctrl)
	store.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, id discoverymodels.DeviceOrgIdentifier, d *discoverymodels.DiscoveredDevice) (*discoverymodels.DiscoveredDevice, error) {
			d.DeviceIdentifier = id.DeviceIdentifier
			return d, nil
		}).AnyTimes()
	return NewService(store, nil, nil, nil, discover, caps, nil, nil)
}

func testIdentifiedDevice() *discoverymodels.DiscoveredDevice {
	return &discoverymodels.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: "miner-1", FirmwareVersion: "1"}}
}

// Explicit discovery must reach plugins that do not use TCP, such as virtual
// miners. Use a closed loopback port with the real scanner left in place.
func TestExplicitDiscoveryWithoutTCPListener(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	for _, mode := range []string{"list", "range"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			s := newScanTestService(t, func(_ context.Context, ip, gotPort string) (*discoverymodels.DiscoveredDevice, error) {
				calls.Add(1)
				if ip != "127.0.0.1" || gotPort != port {
					return nil, errors.New("plugin received unexpected endpoint")
				}
				return testIdentifiedDevice(), nil
			})
			ctx := mockSessionContext(t.Context(), 1, 1)
			var results <-chan *pb.DiscoverResponse
			var err error
			if mode == "list" {
				results, err = s.DiscoverWithIPList(ctx, &pb.IPListModeRequest{IpAddresses: []string{"127.0.0.1", "127.0.0.1"}, Ports: []string{port, port}})
			} else {
				results, err = s.DiscoverWithIPRange(ctx, &pb.IPRangeModeRequest{StartIp: "127.0.0.1", EndIp: "127.0.0.1", Ports: []string{port, port}})
			}
			require.NoError(t, err)
			var devices []*pb.Device
			for result := range results {
				require.Empty(t, result.Error)
				devices = append(devices, result.Devices...)
			}
			require.Len(t, devices, 1)
			require.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestNetworkScanTCPPrefilter(t *testing.T) {
	const openPort = "4028"
	ports := []string{openPort, "443"}

	var calls atomic.Int32
	s := newScanTestService(t, func(_ context.Context, ip, port string) (*discoverymodels.DiscoveredDevice, error) {
		calls.Add(1)
		if ip != "192.168.1.2" || port != openPort {
			return nil, errors.New("plugin received a closed endpoint")
		}
		return testIdentifiedDevice(), nil
	})
	s.scanner = scanFunc(func(_ context.Context, addrs iter.Seq[netip.Addr], scanPorts []uint16, emit func(netscan.HostResult) error) error {
		for addr := range addrs {
			if addr.String() != "192.168.1.2" || len(scanPorts) != 2 || scanPorts[0] != 443 || scanPorts[1] != 4028 {
				return errors.New("scanner received unexpected endpoints")
			}
			if err := emit(netscan.HostResult{Addr: addr, OpenPorts: []uint16{4028}}); err != nil {
				return err
			}
		}
		return nil
	})
	s.localNetworkInfo = func(context.Context) (*NetworkInfo, error) { return nil, errors.New("no local subnet") }
	ctx := mockSessionContext(t.Context(), 1, 1)
	results, err := s.DiscoverWithNmap(ctx, &pb.NmapModeRequest{Target: "192.168.1.2/32", Ports: ports})
	require.NoError(t, err)
	var devices []*pb.Device
	for result := range results {
		require.Empty(t, result.Error)
		devices = append(devices, result.Devices...)
	}
	require.Len(t, devices, 1)
	require.Equal(t, int32(1), calls.Load())
}

func TestNetworkScanInterleavesSubnetsBeforeTimeout(t *testing.T) {
	s := newScanTestService(t, nil)
	first, err := netscan.ParseTarget("10.0.0.0/16")
	require.NoError(t, err)
	second, err := netscan.ParseTarget("192.168.0.0/16")
	require.NoError(t, err)
	var checked []netip.Addr
	s.scanner = scanFunc(func(_ context.Context, addrs iter.Seq[netip.Addr], _ []uint16, _ func(netscan.HostResult) error) error {
		for addr := range addrs {
			checked = append(checked, addr)
			if len(checked) == 4 {
				break
			}
		}
		return context.DeadlineExceeded
	})
	results := s.discoverTargets(t.Context(), []netscan.Target{first, first, second}, []uint16{4028}, true)
	var messages []string
	for result := range results {
		messages = append(messages, result.Error)
	}
	require.Len(t, checked, 4)
	for i, addr := range checked {
		target := []netscan.Target{first, second}[i%2]
		require.True(t, target.Contains(addr), "subnet did not get its turn: %s", addr)
	}
	require.Equal(t, []string{"Server scan timed out. Retry to check other addresses, or narrow the range."}, messages)
}

func TestNetworkScanStreamsBeforeTerminalFailure(t *testing.T) {
	for _, terminal := range []error{errors.New("socket resource exhausted"), context.DeadlineExceeded} {
		t.Run(terminal.Error(), func(t *testing.T) {
			s := newScanTestService(t, func(context.Context, string, string) (*discoverymodels.DiscoveredDevice, error) {
				return testIdentifiedDevice(), nil
			})
			finishScan := make(chan struct{})
			s.scanner = scanFunc(func(_ context.Context, _ iter.Seq[netip.Addr], _ []uint16, emit func(netscan.HostResult) error) error {
				if err := emit(netscan.HostResult{Addr: netip.MustParseAddr("192.168.1.1"), OpenPorts: []uint16{80}}); err != nil {
					return err
				}
				<-finishScan
				return terminal
			})
			ctx := mockSessionContext(t.Context(), 1, 1)
			results, err := s.DiscoverWithNmap(ctx, &pb.NmapModeRequest{Target: "192.168.1.1", Ports: []string{"80"}})
			require.NoError(t, err)
			select {
			case result := <-results:
				require.Len(t, result.Devices, 1)
				require.Empty(t, result.Error)
			case <-time.After(time.Second):
				t.Fatal("device was held until the scan completed")
			}
			close(finishScan)
			result := <-results
			require.NotEmpty(t, result.Error)
			_, open := <-results
			require.False(t, open)
		})
	}
}

func TestNetworkScanCancellationDoesNotWaitForNoncooperativePlugin(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s := newScanTestService(t, func(context.Context, string, string) (*discoverymodels.DiscoveredDevice, error) {
		close(started)
		<-release
		return testIdentifiedDevice(), nil
	})
	ctx, cancel := context.WithCancel(mockSessionContext(t.Context(), 1, 1))
	results, err := s.DiscoverWithIPList(ctx, &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.1"}, Ports: []string{"80"}})
	require.NoError(t, err)
	<-started
	cancel()
	select {
	case _, open := <-results:
		require.False(t, open, "user cancellation must be quiet")
	case <-time.After(time.Second):
		t.Fatal("discovery waited for a plugin that ignored cancellation")
	}
	require.Len(t, s.probeSemaphore, 1, "cancelled plugins retain permits until they return")
	close(release)
	require.Eventually(t, func() bool { return len(s.probeSemaphore) == 0 }, time.Second, time.Millisecond)
}

func TestNetworkScanExplicitRangeIncludesZeroAndOne(t *testing.T) {
	var mu sync.Mutex
	var addresses []string
	s := newScanTestService(t, func(_ context.Context, ip, _ string) (*discoverymodels.DiscoveredDevice, error) {
		mu.Lock()
		addresses = append(addresses, ip)
		mu.Unlock()
		return nil, errors.New("not a miner")
	})
	results, err := s.DiscoverWithIPRange(t.Context(), &pb.IPRangeModeRequest{StartIp: "192.168.1.0", EndIp: "192.168.1.1", Ports: []string{"80"}})
	require.NoError(t, err)
	for result := range results {
		require.Empty(t, result.Error)
	}
	require.ElementsMatch(t, []string{"192.168.1.0", "192.168.1.1"}, addresses)
}

func TestNetworkScanProbeDeadlineBoundsNoncooperativePlugin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		s := newScanTestService(t, func(context.Context, string, string) (*discoverymodels.DiscoveredDevice, error) {
			<-release
			return testIdentifiedDevice(), nil
		})
		done := make(chan struct{})
		go func() {
			s.discoverAllPortsForIP(t.Context(), "192.168.1.1", []string{"80"}, make(chan *pb.DiscoverResponse))
			close(done)
		}()
		synctest.Wait()
		time.Sleep(perDeviceDiscoveryTimeout)
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("host identification outlived the plugin deadline")
		}
		require.Len(t, s.probeSemaphore, 1)
		close(release)
		synctest.Wait()
		require.Empty(t, s.probeSemaphore)
	})
}

func TestNetworkScanRetainsFallbackAfterCollisionSkip(t *testing.T) {
	collisionSkipped := make(chan struct{})
	s := newScanTestService(t, func(ctx context.Context, _, port string) (*discoverymodels.DiscoveredDevice, error) {
		if port == "4028" {
			return &discoverymodels.DiscoveredDevice{Device: pb.Device{MacAddress: "AA:BB:CC:DD:EE:FF", FirmwareVersion: "1"}}, nil
		}
		select {
		case <-collisionSkipped:
			return testIdentifiedDevice(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	ctrl := gomock.NewController(t)
	devices := storemocks.NewMockDeviceStore(ctrl)
	s.deviceStore = devices
	devices.EXPECT().GetPairedDeviceByMACAddress(gomock.Any(), "AA:BB:CC:DD:EE:FF", int64(1)).Return(
		&interfaces.PairedDeviceInfo{DeviceIdentifier: "reconciled", DiscoveredDeviceIdentifier: "reconciled"}, nil)
	store, ok := s.discoveredDeviceStore.(*storemocks.MockDiscoveredDeviceStore)
	require.True(t, ok)
	store.EXPECT().GetByIPAndPort(gomock.Any(), int64(1), "192.168.1.1", "4028").Return(
		&discoverymodels.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: "occupant"}}, nil)
	devices.EXPECT().GetDeviceByDeviceIdentifier(gomock.Any(), "occupant", int64(1)).DoAndReturn(
		func(context.Context, string, int64) (*pb.Device, error) {
			close(collisionSkipped)
			return &pb.Device{DeviceIdentifier: "occupant"}, nil
		})
	ctx := mockSessionContext(t.Context(), 1, 1)
	results, err := s.DiscoverWithIPList(ctx, &pb.IPListModeRequest{IpAddresses: []string{"192.168.1.1"}, Ports: []string{"4028", "443"}})
	require.NoError(t, err)
	var found []*pb.Device
	for result := range results {
		require.Empty(t, result.Error)
		found = append(found, result.Devices...)
	}
	require.Len(t, found, 1)
	require.Equal(t, "443", found[0].Port)
}

type scanFunc func(context.Context, iter.Seq[netip.Addr], []uint16, func(netscan.HostResult) error) error

func (f scanFunc) Scan(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, emit func(netscan.HostResult) error) error {
	return f(ctx, addrs, ports, emit)
}

func TestExplicitDiscoveryRetainsResultsWhenHostnameFails(t *testing.T) {
	s := newScanTestService(t, func(_ context.Context, ip, _ string) (*discoverymodels.DiscoveredDevice, error) {
		require.Equal(t, "192.168.1.1", ip)
		return testIdentifiedDevice(), nil
	})
	s.resolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("DNS unavailable")
	}}
	results, err := s.DiscoverWithIPList(mockSessionContext(t.Context(), 1, 1), &pb.IPListModeRequest{
		IpAddresses: []string{"192.168.1.1", "miner.invalid"}, Ports: []string{"4028"},
	})
	require.NoError(t, err)
	var devices []*pb.Device
	for result := range results {
		require.Empty(t, result.Error)
		devices = append(devices, result.Devices...)
	}
	require.Len(t, devices, 1)
}

type scanResolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f scanResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

func TestExplicitDiscoverySlowHostnameDoesNotBlockOtherTargets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		probed := make(chan string, 2)
		s := newScanTestService(t, func(_ context.Context, ip, _ string) (*discoverymodels.DiscoveredDevice, error) {
			probed <- ip
			return nil, errors.New("not a miner")
		})
		var slowActive atomic.Bool
		s.resolver = scanResolverFunc(func(ctx context.Context, hostname string) ([]net.IPAddr, error) {
			if hostname == "slow.invalid" {
				slowActive.Store(true)
				defer slowActive.Store(false)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []net.IPAddr{{IP: net.ParseIP("192.168.1.2")}}, nil
		})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		results, err := s.DiscoverWithIPList(ctx, &pb.IPListModeRequest{
			IpAddresses: []string{"slow.invalid", "192.168.1.1", "healthy.invalid"}, Ports: []string{"4028"},
		})
		require.NoError(t, err)
		synctest.Wait()
		require.True(t, slowActive.Load())
		require.Len(t, probed, 2, "literal and healthy hostname must progress while the first lookup is blocked")
		require.ElementsMatch(t, []string{"192.168.1.1", "192.168.1.2"}, []string{<-probed, <-probed})
		cancel()
		for result := range results {
			require.Empty(t, result.Error)
		}
		synctest.Wait()
		require.False(t, slowActive.Load())
	})
}

func TestResolveTargetsBoundsLookupsWithoutBlockingLiterals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var active atomic.Int32
		s := &Service{resolver: scanResolverFunc(func(ctx context.Context, _ string) ([]net.IPAddr, error) {
			active.Add(1)
			defer active.Add(-1)
			<-ctx.Done()
			return nil, ctx.Err()
		})}
		var targets []netscan.Target
		for i := range concurrentDiscoveryLimit + 10 {
			target, err := netscan.ParseAddrTarget(fmt.Sprintf("miner-%d.invalid", i))
			require.NoError(t, err)
			targets = append(targets, target)
		}
		literal, err := netscan.ParseAddrTarget("192.168.1.1")
		require.NoError(t, err)
		targets = append(targets, literal)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		seen := make(chan netscan.Target, 1)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for target, err := range s.resolveTargets(ctx, targets, false) {
				if err == nil {
					seen <- target
				}
			}
		}()
		synctest.Wait()
		require.EqualValues(t, concurrentDiscoveryLimit, active.Load())
		require.Len(t, seen, 1, "saturated lookup workers must not hold back a literal")
		require.Equal(t, literal, <-seen)
		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("resolution did not stop after cancellation")
		}
		require.Zero(t, active.Load(), "resolution must wait for its workers")
	})
}

func TestExplicitDiscoveryDeduplicatesResolvedTargets(t *testing.T) {
	var probes, lookups atomic.Int32
	s := newScanTestService(t, func(context.Context, string, string) (*discoverymodels.DiscoveredDevice, error) {
		probes.Add(1)
		return nil, errors.New("not a miner")
	})
	s.resolver = scanResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		lookups.Add(1)
		return []net.IPAddr{{IP: net.ParseIP("192.168.1.1")}}, nil
	})
	results, err := s.DiscoverWithIPList(t.Context(), &pb.IPListModeRequest{
		IpAddresses: []string{"one.invalid", "one.invalid", "two.invalid", "192.168.1.1"}, Ports: []string{"4028"},
	})
	require.NoError(t, err)
	for result := range results {
		require.Empty(t, result.Error)
	}
	require.EqualValues(t, 2, lookups.Load(), "duplicate hostname inputs should be resolved only once")
	require.EqualValues(t, 1, probes.Load(), "aliases and literals for one target should be probed only once")
}
