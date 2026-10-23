package ct1

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type haServer struct {
	mdk.UnimplementedSystemServiceServer
	mdk.UnimplementedServerUpdateServiceServer
	mdk.UnimplementedPowerServiceServer
	mdk.UnimplementedHashboardServiceServer
	id, peer       string
	peerOverride   atomic.Pointer[string]
	primary        atomic.Bool
	writes         atomic.Int32
	uncertain      atomic.Bool
	server         *grpc.Server
	endpoint       sdk.DeviceEndpoint
	statusOverride func(*mdk.SystemStatus)
	mapError       error
	mapReads       atomic.Int32
	infoReads      atomic.Int32
	infoDelay      time.Duration
	statusDelay    time.Duration
	supportError   error
	supportEntered chan struct{}
}

func startHAServer(t *testing.T, id, peer string) *haServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	key, err := pairKey(id, peer)
	require.NoError(t, err)
	s := &haServer{id: id, peer: peer, server: grpc.NewServer(), endpoint: sdk.DeviceEndpoint{ID: id, DeviceKey: key, Host: host, Port: 8080, URLScheme: "http", RPCPort: port}}
	mdk.RegisterSystemServiceServer(s.server, s)
	mdk.RegisterServerUpdateServiceServer(s.server, s)
	mdk.RegisterPowerServiceServer(s.server, s)
	mdk.RegisterHashboardServiceServer(s.server, s)
	go func() { _ = s.server.Serve(listener) }()
	t.Cleanup(s.server.Stop)
	return s
}
func (s *haServer) GetSystemInfo(ctx context.Context, _ *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	s.infoReads.Add(1)
	if s.infoDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("system info delay: %w", ctx.Err())
		case <-time.After(s.infoDelay):
		}
	}
	return &mdk.GetSystemInfoResponse{Info: &mdk.SystemInfo{Id: s.id, Product: "Frigate", Model: "CT1"}}, nil
}
func (s *haServer) GetServerUpdateSupport(ctx context.Context, _ *mdk.GetServerUpdateSupportRequest) (*mdk.GetServerUpdateSupportResponse, error) {
	if s.supportEntered != nil {
		close(s.supportEntered)
		<-ctx.Done()
		return nil, fmt.Errorf("server update support canceled: %w", ctx.Err())
	}
	if s.supportError != nil {
		return nil, fmt.Errorf("server update support: %w", s.supportError)
	}
	peer := s.peer
	if override := s.peerOverride.Load(); override != nil {
		peer = *override
	}
	return &mdk.GetServerUpdateSupportResponse{InstanceId: s.id, PeerInstanceId: peer}, nil
}
func (s *haServer) GetSystemStatus(ctx context.Context, _ *mdk.GetSystemStatusRequest) (*mdk.GetSystemStatusResponse, error) {
	if s.statusDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("system status delay: %w", ctx.Err())
		case <-time.After(s.statusDelay):
		}
	}
	state := &mdk.SystemStatus{ServerId: s.id, ContainerWritePrimary: s.primary.Load(), HaRole: mdk.HARole_HA_ROLE_STANDBY, DatabaseConnected: true, HaRecovery: &mdk.HARecoveryStatus{State: mdk.HARecoveryState_HA_RECOVERY_STATE_STEADY}}
	if state.ContainerWritePrimary {
		state.HaRole = mdk.HARole_HA_ROLE_PRIMARY
	}
	if s.statusOverride != nil {
		s.statusOverride(state)
	}
	return &mdk.GetSystemStatusResponse{Status: state}, nil
}
func (s *haServer) GetContainerControls(context.Context, *mdk.GetContainerControlsRequest) (*mdk.GetContainerControlsResponse, error) {
	return &mdk.GetContainerControlsResponse{MainCircuitBreakerSupported: new(true), MainCircuitBreakerClosed: new(false)}, nil
}
func (s *haServer) GetContainerMap(context.Context, *mdk.GetContainerMapRequest) (*mdk.GetContainerMapResponse, error) {
	s.mapReads.Add(1)
	if s.mapError != nil {
		return nil, s.mapError
	}
	return &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{}}, nil
}
func (s *haServer) SetMainCircuitBreakerState(_ context.Context, req *mdk.SetMainCircuitBreakerStateRequest) (*mdk.SetMainCircuitBreakerStateResponse, error) {
	s.writes.Add(1)
	if s.uncertain.Load() {
		return nil, fmt.Errorf("simulated uncertain write: %w", status.Error(codes.Unavailable, "connection lost after dispatch"))
	}
	return &mdk.SetMainCircuitBreakerStateResponse{Closed: req.Closed}, nil
}
func haInfo(a, b *haServer) sdk.DeviceInfo {
	return sdk.DeviceInfo{Host: a.endpoint.Host, Port: 8080, URLScheme: "http", RPCPort: a.endpoint.RPCPort, SerialNumber: a.id, Endpoints: []sdk.DeviceEndpoint{a.endpoint, b.endpoint}}
}

func TestHALegacyDefaultRPCPort(t *testing.T) {
	a := startHAServer(t, "a", "b")
	a.primary.Store(true)
	info := sdk.DeviceInfo{Host: a.endpoint.Host, Port: 8080, URLScheme: "http", SerialNumber: a.id}
	c, err := NewClient(info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer c.Close()

	// Keep the logical default-port endpoint while sending fixture RPCs to an
	// ephemeral listener, so the test does not need exclusive use of port 9095.
	transportInfo := info
	transportInfo.RPCPort = a.endpoint.RPCPort
	transport, err := NewClient(transportInfo, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer transport.Close()
	c.system, c.updates, c.power = transport.system, transport.updates, transport.power

	verified, err := c.Discover(t.Context(), info)
	require.NoError(t, err)
	require.Len(t, verified.Endpoints, 1)
	require.Equal(t, DefaultRPCPort, verified.Endpoints[0].RPCPort)
	require.Equal(t, []string{"a", "b"}, verified.Endpoints[0].MemberIDs)
	d := &Device{id: "legacy", info: verified, clients: []*Client{c}}
	selected, ha, err := d.probeControllers(t.Context(), false)
	require.NoError(t, err)
	require.Same(t, c, selected.client)
	require.True(t, ha.Degraded)
	require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
	require.EqualValues(t, 1, a.writes.Load())
}

func TestHASavedDefaultRPCPortSelectsPrimary(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	info := haInfo(a, b)
	info.RPCPort, info.Endpoints[0].RPCPort = 0, 0
	c, err := NewClient(info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer c.Close()

	// The saved primary uses its default port. Route only its test transport to
	// the ephemeral listener, preserving the saved zero-port configuration.
	transportInfo := info
	transportInfo.RPCPort = a.endpoint.RPCPort
	transport, err := NewClient(transportInfo, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer transport.Close()
	c.system, c.updates, c.power = transport.system, transport.updates, transport.power
	partnerInfo := info
	partnerInfo.RPCPort = b.endpoint.RPCPort
	partner, err := NewClient(partnerInfo, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer partner.Close()

	d := &Device{id: "legacy", info: info, clients: []*Client{c, partner}}
	selected, ha, err := d.probeControllers(t.Context(), true)
	require.NoError(t, err)
	require.Same(t, c, selected.client)
	require.False(t, ha.Degraded)
	require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
	require.EqualValues(t, 1, a.writes.Load())
	require.Zero(t, b.writes.Load())
}

func TestHAPairingAndFailoverWithoutHashboards(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	c, ha, err := d.probeControllers(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, a.endpoint.RPCPort, c.client.endpoint.RPCPort)
	require.False(t, ha.Degraded)
	a.server.Stop()
	b.primary.Store(true)
	c, ha, err = d.probeControllers(t.Context(), true)
	require.NoError(t, err)
	require.Equal(t, b.endpoint.RPCPort, c.client.endpoint.RPCPort)
	require.True(t, ha.Degraded)
	// Restarting Fleet while A is unavailable must still create a usable device.
	restarted, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer restarted.Close(t.Context())
}

func TestHAReusedDefaultRPCAddressKeepsVerifiedPartnerUsable(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	b.primary.Store(true)
	info := haInfo(a, b)
	d, err := NewDevice(t.Context(), "container", info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	stale, current := d.clients[0], d.clients[1]
	aSystem, aUpdates, aPower := stale.system, stale.updates, stale.power

	// Keep the saved logical ports while routing both clients to B's ephemeral
	// listener. The old zero port and explicit 9095 name the same CT1 socket.
	d.info.Endpoints[0].RPCPort = 0
	d.info.Endpoints[1].RPCPort = DefaultRPCPort
	stale.endpoint.RPCPort, current.endpoint.RPCPort = DefaultRPCPort, DefaultRPCPort
	stale.system, stale.updates, stale.power = current.system, current.updates, current.power

	_, err = stale.Discover(t.Context(), d.info)
	require.ErrorContains(t, err, "partner identity changed")
	selected, ha, err := d.probeControllers(t.Context(), true)
	require.NoError(t, err)
	require.Same(t, current, selected.client)
	require.True(t, ha.Degraded)
	require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
	require.Zero(t, a.writes.Load())
	require.EqualValues(t, 1, b.writes.Load())

	// Rediscovery restores A's own address and makes the full pair healthy.
	stale.system, stale.updates, stale.power = aSystem, aUpdates, aPower
	d.info.Endpoints[0] = a.endpoint
	stale.endpoint.RPCPort = a.endpoint.RPCPort
	selected, ha, err = d.probeControllers(t.Context(), true)
	require.NoError(t, err)
	require.Same(t, current, selected.client)
	require.False(t, ha.Degraded)
}

func TestHAReadsPreferSettledStandby(t *testing.T) {
	for _, delayed := range []string{"primary", "standby"} {
		t.Run(delayed+" replies last", func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			a.primary.Store(true)
			a.mapError = status.Error(codes.Internal, "querying locations: database unavailable")
			a.statusOverride = func(s *mdk.SystemStatus) {
				s.DatabaseConnected = false
				s.HaRecovery.State = mdk.HARecoveryState_HA_RECOVERY_STATE_RECONCILING
				if delayed == "primary" {
					time.Sleep(50 * time.Millisecond)
				}
			}
			if delayed == "standby" {
				b.statusOverride = func(*mdk.SystemStatus) { time.Sleep(50 * time.Millisecond) }
			}
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err)
			defer d.Close(t.Context())

			metrics, err := d.Status(t.Context())
			require.NoError(t, err)
			require.NotNil(t, metrics.Container)
			require.NotNil(t, metrics.Container.HA)
			require.True(t, metrics.Container.HA.Degraded)
			require.Zero(t, a.mapReads.Load())
			require.EqualValues(t, 1, b.mapReads.Load())

			err = d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)})
			// Read health does not override the primary's explicit write authority.
			require.NoError(t, err)
			require.EqualValues(t, 1, a.writes.Load())
			require.Zero(t, b.writes.Load())
		})
	}
}

func TestHAWriteIsNotReplayedAfterUncertainResult(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	a.uncertain.Store(true)
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	err = d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)})
	require.ErrorContains(t, err, "unconfirmed")
	require.EqualValues(t, 1, a.writes.Load())
	require.Zero(t, b.writes.Load())
	a.primary.Store(false)
	b.primary.Store(true)
	require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
	require.EqualValues(t, 1, b.writes.Load())
}

func TestHARefusesConflictingWriteAuthority(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	b.primary.Store(true)
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	_, ha, err := d.probeControllers(t.Context(), false)
	require.NoError(t, err)
	require.True(t, ha.Degraded)
	err = d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)})
	require.ErrorContains(t, err, "both CT1 controllers")
	require.Zero(t, a.writes.Load())
	require.Zero(t, b.writes.Load())
}

func TestHARejectsReplacementServer(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "replacement", "other")
	info := haInfo(a, b)
	info.Endpoints[1].ID = "b"
	info.Endpoints[1].DeviceKey = info.Endpoints[0].DeviceKey
	a.server.Stop()
	b.primary.Store(true)
	_, err := NewDevice(t.Context(), "container", info, sdk.UsernamePassword{})
	require.Error(t, err)
	require.Zero(t, b.writes.Load())
}

func TestHARechecksPartnerIdentityBeforeReadsAndWrites(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())

	// B keeps its own ID but now belongs to another pair. Its own status ID
	// cannot prove that this endpoint still belongs to the saved container.
	a.server.Stop()
	b.primary.Store(true)
	b.peerOverride.Store(new("replacement"))
	_, err = d.Status(t.Context())
	require.ErrorContains(t, err, "partner identity changed")
	err = d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)})
	require.ErrorContains(t, err, "partner identity changed")
	require.Zero(t, b.mapReads.Load())
	require.Zero(t, b.writes.Load())
}

func TestHAWebViewUsesSavedEndpointWithoutProbing(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	b.primary.Store(true)
	info := haInfo(a, b)
	info.Host, info.Port, info.URLScheme = "browser.example", 8443, "https"
	d, err := NewDevice(t.Context(), "container", info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	require.EqualValues(t, 1, a.infoReads.Load())
	require.EqualValues(t, 1, b.infoReads.Load())

	a.server.Stop()
	b.server.Stop()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	web, supported, err := d.TryGetWebViewURL(ctx)
	require.NoError(t, err)
	require.True(t, supported)
	require.Equal(t, "https://browser.example:8443", web)
	require.EqualValues(t, 1, a.infoReads.Load())
	require.EqualValues(t, 1, b.infoReads.Load())
}

func TestWebViewFormatsSavedIPv6Endpoint(t *testing.T) {
	d := &Device{info: sdk.DeviceInfo{Host: "2001:db8::1", Port: 8080, URLScheme: "http"}}
	web, supported, err := d.TryGetWebViewURL(t.Context())
	require.NoError(t, err)
	require.True(t, supported)
	require.Equal(t, "http://[2001:db8::1]:8080", web)
}

func TestHADegradedUnlessPairIsSettled(t *testing.T) {
	cases := []struct {
		name   string
		change func(*mdk.SystemStatus)
	}{
		{"database disconnected", func(s *mdk.SystemStatus) { s.DatabaseConnected = false }},
		{"reconciling", func(s *mdk.SystemStatus) { s.HaRecovery.State = mdk.HARecoveryState_HA_RECOVERY_STATE_RECONCILING }},
		{"recovery degraded", func(s *mdk.SystemStatus) { s.HaRecovery.State = mdk.HARecoveryState_HA_RECOVERY_STATE_DEGRADED }},
		{"recovery absent", func(s *mdk.SystemStatus) { s.HaRecovery = nil }},
		{"recovery unknown", func(s *mdk.SystemStatus) { s.HaRecovery.State = mdk.HARecoveryState_HA_RECOVERY_STATE_UNSPECIFIED }},
		{"fenced", func(s *mdk.SystemStatus) { s.HaRole = mdk.HARole_HA_ROLE_FENCED }},
		{"role unknown", func(s *mdk.SystemStatus) { s.HaRole = mdk.HARole_HA_ROLE_UNSPECIFIED }},
		{"DCS unhealthy", func(s *mdk.SystemStatus) { s.DcsHealthy = new(false) }},
		{"wrong write authority", func(s *mdk.SystemStatus) { s.ContainerWritePrimary = !s.ContainerWritePrimary }},
	}
	for _, tc := range cases {
		for _, target := range []string{"primary", "standby"} {
			t.Run(tc.name+"/"+target, func(t *testing.T) {
				a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
				a.primary.Store(true)
				if target == "primary" {
					a.statusOverride = tc.change
				} else {
					b.statusOverride = tc.change
				}
				d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
				require.NoError(t, err)
				defer d.Close(t.Context())
				_, ha, err := d.probeControllers(t.Context(), false)
				require.NoError(t, err)
				require.True(t, ha.Degraded)
			})
		}
	}
}

func TestHAAllowsSlowIdentityProbeWithoutDuplicateDiscovery(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	a.infoDelay = 1100 * time.Millisecond
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	require.EqualValues(t, 1, a.infoReads.Load(), "creation reuses the verified controller info")
	require.EqualValues(t, 1, b.infoReads.Load())
	require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
	require.EqualValues(t, 1, a.writes.Load())
	require.EqualValues(t, 2, a.infoReads.Load())
	err = d.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(true)})
	require.Error(t, err, "the empty fixture has no tank topology")
	require.EqualValues(t, 3, a.infoReads.Load(), "tank commands do not repeat identity discovery")

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, _, err = d.probeControllers(ctx, true)
	require.Error(t, err, "the caller's shorter deadline still applies")
	require.EqualValues(t, 1, a.writes.Load())
}

func TestHAStatusReservesCollectionTimeWhenPeerStalls(t *testing.T) {
	for _, timeout := range []time.Duration{5 * time.Second, 200 * time.Millisecond} {
		for _, stalledRPC := range []string{"identity", "status"} {
			t.Run(fmt.Sprintf("%s/%s", timeout, stalledRPC), func(t *testing.T) {
				a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
				a.primary.Store(true)
				d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
				require.NoError(t, err)
				defer d.Close(t.Context())
				if stalledRPC == "identity" {
					a.infoDelay = 10 * time.Second
				} else {
					a.statusDelay = 10 * time.Second
				}

				ctx, cancel := context.WithTimeout(t.Context(), timeout)
				defer cancel()
				metrics, err := d.Status(ctx)
				require.NoError(t, err)
				require.NoError(t, ctx.Err())
				require.NotNil(t, metrics.Container)
				require.NotNil(t, metrics.Container.HA)
				require.True(t, metrics.Container.HA.Degraded)
				require.Zero(t, a.mapReads.Load())
				require.EqualValues(t, 1, b.mapReads.Load(), "the verified standby must still collect telemetry")
				deadline, _ := ctx.Deadline()
				require.Greater(t, time.Until(deadline), timeout/4, "probing must leave a usable collection budget")
			})
		}
	}
}

func TestHAWritesRequirePrimaryRoleAndAuthority(t *testing.T) {
	for _, tc := range []struct {
		name     string
		role     mdk.HARole
		writable bool
	}{
		{"standby claim", mdk.HARole_HA_ROLE_STANDBY, true},
		{"fenced claim", mdk.HARole_HA_ROLE_FENCED, true},
		{"unknown role", mdk.HARole_HA_ROLE_UNSPECIFIED, true},
		{"authority absent", mdk.HARole_HA_ROLE_PRIMARY, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			a.statusOverride = func(s *mdk.SystemStatus) {
				s.HaRole, s.ContainerWritePrimary = tc.role, tc.writable
			}
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err)
			defer d.Close(t.Context())
			require.Error(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
			require.Zero(t, a.writes.Load())
			require.Zero(t, b.writes.Load())
		})
	}
}

func TestHAWriteAuthorityIsIndependentOfRecoveryHealth(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recovery *mdk.HARecoveryStatus
	}{
		{"missing", nil},
		{"reconciling", &mdk.HARecoveryStatus{State: mdk.HARecoveryState_HA_RECOVERY_STATE_RECONCILING}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			a.primary.Store(true)
			a.statusOverride = func(s *mdk.SystemStatus) { s.HaRecovery = tc.recovery }
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err)
			defer d.Close(t.Context())
			selected, ha, err := d.probeControllers(t.Context(), true)
			require.NoError(t, err)
			require.Equal(t, a.endpoint.RPCPort, selected.client.endpoint.RPCPort)
			require.True(t, ha.Degraded)
			require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
			require.EqualValues(t, 1, a.writes.Load())
			require.Zero(t, b.writes.Load())
		})
	}
}
