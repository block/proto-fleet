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
	"google.golang.org/protobuf/types/known/timestamppb"
)

type haServer struct {
	mdk.UnimplementedSystemServiceServer
	mdk.UnimplementedPowerServiceServer
	mdk.UnimplementedHashboardServiceServer
	mdk.UnimplementedMiningServiceServer
	mdk.UnimplementedMiningGroupServiceServer
	id, peer       string
	peerOverride   atomic.Pointer[string]
	primary        atomic.Bool
	writes         atomic.Int32
	uncertain      atomic.Bool
	server         *grpc.Server
	endpoint       sdk.DeviceEndpoint
	haOverride     func(*mdk.HAStatus)
	statusOverride func(*mdk.SystemStatus)
	mapError       error
	containerMap   *mdk.GetContainerMapResponse
	mapDelay       time.Duration
	mapReads       atomic.Int32
	infoReads      atomic.Int32
	groupReads     atomic.Int32
	groupWrites    atomic.Int32
	haDelay        time.Duration
	haReads        atomic.Int32
	statusDelay    time.Duration
	haError        error
	haEntered      chan struct{}
}

func startHAServer(t *testing.T, id, peer string) *haServer {
	t.Helper()
	return startHAGroupServer(t, id, peer, nil)
}

func startHAGroupServer(t *testing.T, id, peer string, groups mdk.MiningGroupServiceServer) *haServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	key := "ct1:" + testClusterID
	s := &haServer{id: id, peer: peer, server: grpc.NewServer(), endpoint: sdk.DeviceEndpoint{ID: id, DeviceKey: key, Host: host, Port: 8080, URLScheme: "http", RPCPort: port}}
	mdk.RegisterSystemServiceServer(s.server, s)
	mdk.RegisterPowerServiceServer(s.server, s)
	mdk.RegisterHashboardServiceServer(s.server, s)
	mdk.RegisterMiningServiceServer(s.server, s)
	if groups == nil {
		groups = s
	}
	mdk.RegisterMiningGroupServiceServer(s.server, groups)
	go func() { _ = s.server.Serve(listener) }()
	t.Cleanup(s.server.Stop)
	return s
}
func (s *haServer) GetSystemInfo(context.Context, *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	s.infoReads.Add(1)

	return &mdk.GetSystemInfoResponse{Info: &mdk.SystemInfo{Id: s.id, Product: "Frigate", Model: "CT1", Version: "v1-" + s.id}}, nil
}

const testClusterID = "7900e1a3-5be9-4a6b-b640-eaf3d959333b"

func (s *haServer) GetHAStatus(ctx context.Context, _ *mdk.GetHAStatusRequest) (*mdk.GetHAStatusResponse, error) {
	s.haReads.Add(1)
	if s.haDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("HA status delay: %w", ctx.Err())
		case <-time.After(s.haDelay):
		}
	}
	if s.haEntered != nil {
		close(s.haEntered)
		<-ctx.Done()
		return nil, fmt.Errorf("HA status canceled: %w", ctx.Err())
	}
	if s.haError != nil {
		return nil, s.haError
	}
	peer := s.peer
	if override := s.peerOverride.Load(); override != nil {
		peer = *override
	}
	now := timestamppb.Now()
	report := &mdk.HAStatus{Enabled: true, ReportingServerId: s.id, PeerServerId: peer,
		ClusterId: testClusterID, IdentityState: mdk.HAIdentityState_HA_IDENTITY_STATE_CONFIRMED,
		InventoryReady: true, ObservedAt: now}
	for _, id := range []string{s.id, peer} {
		primary := s.primary.Load() == (id == s.id)
		role := mdk.HARole_HA_ROLE_STANDBY
		if primary {
			role = mdk.HARole_HA_ROLE_PRIMARY
		}
		report.Members = append(report.Members, &mdk.HAMemberStatus{
			Id: id, Kind: mdk.HAMemberRole_HA_MEMBER_ROLE_FRIGATE, Local: id == s.id,
			HaRole: role, ContainerWritePrimary: &primary, FrigateReachable: new(true),
			Recovery:           &mdk.HARecoveryStatus{State: mdk.HARecoveryState_HA_RECOVERY_STATE_STEADY},
			RoleObservation:    &mdk.HAObservation{SourceServerId: id, ObservedAt: now, Freshness: mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_FRESH},
			FrigateObservation: &mdk.HAObservation{SourceServerId: s.id, ObservedAt: now, Freshness: mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_FRESH},
		})
	}
	if s.haOverride != nil {
		s.haOverride(report)
	}
	return &mdk.GetHAStatusResponse{Status: report}, nil
}
func (s *haServer) GetSystemStatus(ctx context.Context, _ *mdk.GetSystemStatusRequest) (*mdk.GetSystemStatusResponse, error) {
	if s.statusDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("system status delay: %w", ctx.Err())
		case <-time.After(s.statusDelay):
		}
	}
	state := &mdk.SystemStatus{ServerId: s.id, Version: "v1-" + s.id, ContainerWritePrimary: s.primary.Load(), HaRole: mdk.HARole_HA_ROLE_STANDBY, DatabaseConnected: true, HaRecovery: &mdk.HARecoveryStatus{State: mdk.HARecoveryState_HA_RECOVERY_STATE_STEADY}}
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
func (s *haServer) GetContainerMap(ctx context.Context, _ *mdk.GetContainerMapRequest) (*mdk.GetContainerMapResponse, error) {
	s.mapReads.Add(1)
	if s.mapDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("read container map: %w", ctx.Err())
		case <-time.After(s.mapDelay):
		}
	}
	if s.mapError != nil {
		return nil, s.mapError
	}
	if s.containerMap != nil {
		return s.containerMap, nil
	}
	return &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{}}, nil
}
func (s *haServer) GetMiningStatus(context.Context, *mdk.GetMiningStatusRequest) (*mdk.GetMiningStatusResponse, error) {
	s.groupReads.Add(1)
	return &mdk.GetMiningStatusResponse{Mining: true, ActiveBoards: 3}, nil
}
func (s *haServer) CreateMiningGroup(context.Context, *mdk.CreateMiningGroupRequest) (*mdk.CreateMiningGroupResponse, error) {
	s.groupWrites.Add(1)
	if s.uncertain.Load() {
		return nil, fmt.Errorf("simulated uncertain group write: %w", status.Error(codes.Unavailable, "connection lost after dispatch"))
	}
	return &mdk.CreateMiningGroupResponse{Group: &mdk.MiningGroupRecord{Id: "group", Revision: 1}}, nil
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

func TestHAPairingAndFailoverWithoutHashboards(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	metrics, err := d.Status(t.Context())
	require.NoError(t, err)
	require.False(t, metrics.Container.HA.Degraded)
	require.Equal(t, "v1-a", metrics.FirmwareVersion)
	require.EqualValues(t, 1, a.mapReads.Load())
	require.Zero(t, b.mapReads.Load())
	a.server.Stop()
	b.primary.Store(true)
	metrics, err = d.Status(t.Context())
	require.NoError(t, err)
	require.True(t, metrics.Container.HA.Degraded)
	require.Equal(t, "v1-b", metrics.FirmwareVersion)
	require.EqualValues(t, 1, b.mapReads.Load())
	require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
	require.EqualValues(t, 1, b.writes.Load())
	// Restarting Fleet while A is unavailable must still create a usable device.
	restarted, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer restarted.Close(t.Context())
	info, _, err := restarted.DescribeDevice(t.Context())
	require.NoError(t, err)
	require.Equal(t, "v1-b", info.FirmwareVersion)
}

func TestHAReusedControllerAddressKeepsVerifiedPartnerUsable(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	b.primary.Store(true)
	info := haInfo(a, b)
	d, err := NewDevice(t.Context(), "container", info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	stale, current := d.clients[0], d.clients[1]
	aSystem, aPower := stale.system, stale.power

	// Simulate B answering at A's saved address as well as its own.
	stale.system, stale.power = current.system, current.power

	selected, ha, err := d.probeControllers(t.Context(), true)
	require.NoError(t, err)
	require.Same(t, current, selected.client)
	require.True(t, ha.Degraded)
	require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
	require.Zero(t, a.writes.Load())
	require.EqualValues(t, 1, b.writes.Load())

	// Rediscovery restores A's own address and makes the full pair healthy.
	stale.system, stale.power = aSystem, aPower
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

func TestHAStatusFallsBackAfterPrimaryCollectionFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		change      func(*haServer)
		standbyRead int32
		wantError   bool
	}{
		{name: "verified standby", standbyRead: 1},
		{name: "standby collection also fails", standbyRead: 1, wantError: true, change: func(s *haServer) {
			s.mapError = status.Error(codes.Unavailable, "standby map unavailable")
		}},
		{name: "unsettled standby", wantError: true, change: func(s *haServer) {
			s.statusOverride = func(state *mdk.SystemStatus) { state.DatabaseConnected = false }
		}},
		{name: "changed cluster identity", wantError: true, change: func(s *haServer) {
			s.haOverride = func(report *mdk.HAStatus) { report.ClusterId = "65778105-4500-4905-8268-c0320b07827e" }
		}},
		{name: "changed member identity", wantError: true, change: func(s *haServer) {
			s.statusOverride = func(state *mdk.SystemStatus) { state.ServerId = "replacement" }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			a.primary.Store(true)
			a.mapError = status.Error(codes.Internal, "primary database unavailable")
			if tc.change != nil {
				tc.change(b)
			}
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err)
			defer d.Close(t.Context())

			metrics, err := d.Status(t.Context())
			if tc.wantError {
				require.ErrorContains(t, err, "primary database unavailable")
				if tc.standbyRead == 1 {
					require.ErrorContains(t, err, "standby map unavailable")
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, "v1-b", metrics.FirmwareVersion)
				require.True(t, metrics.Container.HA.Degraded)
			}
			require.EqualValues(t, 1, a.mapReads.Load())
			require.Equal(t, tc.standbyRead, b.mapReads.Load())
			require.EqualValues(t, 1, b.haReads.Load(), "fallback reuses the verified probe")
			require.Zero(t, a.writes.Load())
			require.Zero(t, b.writes.Load())
		})
	}
}

func TestHAStatusFallbackKeepsCallerDeadline(t *testing.T) {
	for _, duringFallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("during fallback=%t", duringFallback), func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			a.primary.Store(true)
			if duringFallback {
				a.mapError = status.Error(codes.Internal, "primary database unavailable")
				b.mapDelay = time.Second
			} else {
				a.mapDelay = time.Second
			}
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err)
			defer d.Close(t.Context())
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			deadline, _ := ctx.Deadline()
			started := time.Now()
			_, err = d.Status(ctx)
			require.Error(t, err)
			// The RPC can fail at the deadline before the context's own timer
			// marks ctx.Err() on a loaded runner, so compare against the clock.
			require.False(t, time.Now().Before(deadline), "Status returned before the caller's deadline: %v", err)
			require.Less(t, time.Since(started), time.Second)
			require.EqualValues(t, 1, a.mapReads.Load())
			if duringFallback {
				require.EqualValues(t, 1, b.mapReads.Load())
			} else {
				require.Zero(t, b.mapReads.Load(), "an expired poll cannot start fallback collection")
			}
		})
	}
}

func TestHAReadsPreferWritePrimaryWhenNeitherControllerIsSettled(t *testing.T) {
	for _, delayed := range []string{"primary", "standby"} {
		t.Run(delayed+" replies last", func(t *testing.T) {
			a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
			a.primary.Store(true)
			a.statusOverride = func(s *mdk.SystemStatus) {
				s.HaRecovery.State = mdk.HARecoveryState_HA_RECOVERY_STATE_RECONCILING
			}
			b.statusOverride = func(s *mdk.SystemStatus) { s.DatabaseConnected = false }
			b.mapError = status.Error(codes.Internal, "database unavailable")
			if delayed == "primary" {
				a.statusDelay = 50 * time.Millisecond
			} else {
				b.statusDelay = 50 * time.Millisecond
			}
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err)
			defer d.Close(t.Context())
			metrics, err := d.Status(t.Context())
			require.NoError(t, err)
			require.True(t, metrics.Container.HA.Degraded)
			require.EqualValues(t, 1, a.mapReads.Load())
			require.Zero(t, b.mapReads.Load())
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

func TestHAMiningGroupsUseSelectedControllerWithoutReplay(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	_, err = d.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "mining_status"})
	require.NoError(t, err)
	require.EqualValues(t, 1, a.groupReads.Load())
	require.Zero(t, b.groupReads.Load())

	req := sdk.MiningGroupRequest{Action: "create", Key: "create", ConfigID: "config"}
	a.uncertain.Store(true)
	_, err = d.MiningGroups(t.Context(), req)
	require.Error(t, err)
	require.EqualValues(t, 1, a.groupWrites.Load())
	require.Zero(t, b.groupWrites.Load())

	a.server.Stop()
	b.primary.Store(true)
	_, err = d.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "mining_status"})
	require.NoError(t, err)
	require.EqualValues(t, 1, b.groupReads.Load())
	_, err = d.MiningGroups(t.Context(), req)
	require.NoError(t, err)
	require.EqualValues(t, 1, b.groupWrites.Load())

	b.statusOverride = func(s *mdk.SystemStatus) { s.ContainerWritePrimary = false }
	_, err = d.MiningGroups(t.Context(), req)
	require.Error(t, err)
	require.EqualValues(t, 1, b.groupWrites.Load(), "cached HA roles cannot grant write authority")
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

func TestHAClusterIdentitySurvivesPartnerReplacement(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())

	// B still belongs to this container, even when its configured partner changes.
	// Its pinned route and live write authority remain valid; the old pair is unhealthy.
	a.server.Stop()
	b.primary.Store(true)
	b.peerOverride.Store(new("replacement"))
	metrics, err := d.Status(t.Context())
	require.NoError(t, err)
	require.True(t, metrics.Container.HA.Degraded)
	require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
	require.EqualValues(t, 1, b.mapReads.Load())
	require.EqualValues(t, 1, b.writes.Load())
	// Operation routing does not adopt the new peer or modify saved routes.
	require.Equal(t, haInfo(a, b).Endpoints, d.info.Endpoints)
	verified, err := d.clients[1].Discover(t.Context(), sdk.DeviceInfo{})
	require.NoError(t, err)
	require.Equal(t, d.info.Endpoints[0].DeviceKey, verified.Endpoints[0].DeviceKey)
}

func TestHAWebViewUsesSavedEndpointWithoutProbing(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	b.primary.Store(true)
	info := haInfo(a, b)
	info.Host, info.Port, info.URLScheme = "2001:db8::1", 8443, "https"
	d, err := NewDevice(t.Context(), "container", info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())

	a.server.Stop()
	b.server.Stop()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	web, supported, err := d.TryGetWebViewURL(ctx)
	require.NoError(t, err)
	require.True(t, supported)
	require.Equal(t, "https://[2001:db8::1]:8443", web)
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

func TestHAProbesClusterWithoutRepeatingSystemInfo(t *testing.T) {
	a, b := startHAServer(t, "a", "b"), startHAServer(t, "b", "a")
	a.primary.Store(true)
	a.haDelay = 1100 * time.Millisecond
	d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(t.Context())
	require.Zero(t, a.infoReads.Load(), "saved groups use HA identity and live status")
	require.Zero(t, b.infoReads.Load())
	require.Zero(t, a.haReads.Load(), "creating a saved group must not probe controllers")
	require.Zero(t, b.haReads.Load())
	_, err = d.Status(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, a.haReads.Load(), "the first poll probes each controller only once")
	require.EqualValues(t, 1, b.haReads.Load())
	require.NoError(t, d.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: mainCircuitBreakerControl, Enabled: new(true)}))
	require.EqualValues(t, 1, a.writes.Load())
	require.EqualValues(t, 2, a.haReads.Load())
	err = d.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(true)})
	require.Error(t, err, "the empty fixture has no tank topology")
	require.EqualValues(t, 3, a.haReads.Load(), "tank commands do not repeat identity discovery")
	require.Zero(t, a.infoReads.Load())

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
					a.haDelay = 10 * time.Second
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
