package ct1

import (
	"context"
	"fmt"
	"math"
	"net"
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

func feedSlot(feed, observed, expected, wiring string, board *Board) Slot {
	return Slot{Occupied: board != nil, Hashboard: board, Feed: &sdk.ContainerFeedEvidence{PowerFeedID: feed, ObservedPowerFeedID: observed, ExpectedPowerFeedID: expected, WiringStatus: wiring}}
}

func feedBoard(mac, module string) *Board {
	return &Board{MACAddress: mac, Serial: "sn-" + mac, ModuleID: module, MiningState: miningStateIdle}
}

// fillerModule is a placed module on its own feed, with every slot reported.
func fillerModule(tank, module int32) Module {
	feed := fmt.Sprintf("filler-%d-outlet-%02d", tank, module)
	board := feedBoard(fmt.Sprintf("bb:00:00:00:%02x:%02x", tank, module), fmt.Sprintf("module-%d-%d", tank, module))
	return Module{ID: module, Slots: []Slot{feedSlot(feed, "", "", "", board), feedSlot(feed, "", "", "", nil), feedSlot(feed, "", "", "", nil)}}
}

// feedMap is a complete CT1 map. Module 1.1 is supplied by outlet 01 and
// module 1.2 by outlet 02; every other position is a filler module.
func feedMap(slots ...Slot) MapResponse {
	if len(slots) == 0 {
		slots = []Slot{
			feedSlot("pdu-1-outlet-01", "pdu-1-outlet-01", "pdu-1-outlet-01", "verified", feedBoard("aa:00:00:00:00:01", "module-a")),
			feedSlot("pdu-1-outlet-01", "pdu-1-outlet-01", "pdu-1-outlet-01", "verified", feedBoard("aa:00:00:00:00:02", "module-a")),
			feedSlot("pdu-1-outlet-01", "", "", "", nil),
		}
	}
	out := &ContainerMap{}
	for tank := int32(1); tank <= sdk.CT1TankCount; tank++ {
		t := Tank{ID: tank}
		for module := int32(1); module <= sdk.CT1ModulesPerTank; module++ {
			t.Modules = append(t.Modules, fillerModule(tank, module))
		}
		out.Tanks = append(out.Tanks, t)
	}
	out.Tanks[0].Modules[0] = Module{ID: 1, Slots: slots}
	out.Tanks[0].Modules[1] = Module{ID: 2, Slots: []Slot{
		feedSlot("pdu-1-outlet-02", "pdu-1-outlet-02", "", "", feedBoard("aa:00:00:00:00:09", "module-b")),
		feedSlot("pdu-1-outlet-02", "", "", "", nil), feedSlot("pdu-1-outlet-02", "", "", "", nil),
	}}
	return MapResponse{ContainerMap: out}
}

var feedTarget = sdk.ContainerPowerFeedModule{Tank: 1, Module: 1, ReportedID: "module-a"}

func TestModuleFeedMapping(t *testing.T) {
	board := func(i byte) *Board { return feedBoard("aa:00:00:00:00:0"+string('0'+i), "module-a") }
	for _, tc := range []struct {
		name       string
		wire       MapResponse
		target     sdk.ContainerPowerFeedModule
		confidence string
		feed       string
		reasons    []string
		shared     []sdk.PowerFeedModuleRef
	}{
		{name: "every occupied board verified", wire: feedMap(), target: feedTarget, confidence: sdk.PowerFeedMappingWiringVerified, feed: "pdu-1-outlet-01"},
		{name: "configured without wiring checks", wire: feedMap(
			feedSlot("pdu-1-outlet-01", "", "", "", board(1)),
			feedSlot("pdu-1-outlet-01", "", "", "unverified", board(2)),
			feedSlot("pdu-1-outlet-01", "", "", "", nil),
		), target: feedTarget, confidence: sdk.PowerFeedMappingConfigured, feed: "pdu-1-outlet-01"},
		{name: "residual board in empty slot is not counted", wire: feedMap(
			feedSlot("pdu-1-outlet-01", "pdu-1-outlet-01", "", "verified", board(1)),
			Slot{Hashboard: board(2), Feed: &sdk.ContainerFeedEvidence{PowerFeedID: "pdu-1-outlet-01"}},
			feedSlot("pdu-1-outlet-01", "", "", "", nil),
		), target: feedTarget, confidence: sdk.PowerFeedMappingWiringVerified, feed: "pdu-1-outlet-01"},
		{name: "boards disagree", wire: feedMap(
			feedSlot("pdu-1-outlet-01", "", "", "", board(1)),
			feedSlot("pdu-1-outlet-01", "pdu-1-outlet-03", "", "", board(2)),
			feedSlot("pdu-1-outlet-01", "", "", "", nil),
		), target: feedTarget, confidence: sdk.PowerFeedMappingAmbiguous, reasons: []string{"multiple_feeds"}},
		{name: "wiring mismatch", wire: feedMap(
			feedSlot("pdu-1-outlet-01", "pdu-1-outlet-01", "", "mismatch", board(1)),
			feedSlot("pdu-1-outlet-01", "", "", "", board(2)),
			feedSlot("pdu-1-outlet-01", "", "", "", nil),
		), target: feedTarget, confidence: sdk.PowerFeedMappingAmbiguous, feed: "pdu-1-outlet-01", reasons: []string{"wiring_mismatch"}},
		{name: "feed claimed by another module", wire: feedMap(
			feedSlot("pdu-1-outlet-02", "", "", "", board(1)),
			feedSlot("pdu-1-outlet-02", "", "", "", board(2)),
			feedSlot("pdu-1-outlet-02", "", "", "", nil),
		), target: feedTarget, confidence: sdk.PowerFeedMappingAmbiguous, feed: "pdu-1-outlet-02", reasons: []string{"feed_shared"}, shared: []sdk.PowerFeedModuleRef{{Tank: 1, Module: 2}}},
		{name: "configured elsewhere", wire: feedMap(
			Slot{Occupied: true, Hashboard: board(1), Feed: &sdk.ContainerFeedEvidence{PowerFeedID: "pdu-1-outlet-01", ConfiguredTank: 1, ConfiguredModule: 4}},
			feedSlot("pdu-1-outlet-01", "", "", "", board(2)),
			feedSlot("pdu-1-outlet-01", "", "", "", nil),
		), target: feedTarget, confidence: sdk.PowerFeedMappingAmbiguous, feed: "pdu-1-outlet-01", reasons: []string{"configured_location_mismatch"}},
		{name: "occupied slot names no feed", wire: feedMap(
			feedSlot("pdu-1-outlet-01", "pdu-1-outlet-01", "", "verified", board(1)),
			Slot{Occupied: true, Hashboard: board(2), Feed: &sdk.ContainerFeedEvidence{}},
			feedSlot("pdu-1-outlet-01", "", "", "", nil),
		), target: feedTarget, confidence: sdk.PowerFeedMappingAmbiguous, feed: "pdu-1-outlet-01", reasons: []string{"feed_evidence_incomplete"}},
		{name: "occupied slot without feed evidence", wire: feedMap(
			feedSlot("pdu-1-outlet-01", "", "", "", board(1)),
			Slot{Occupied: true, Hashboard: board(2)},
			feedSlot("pdu-1-outlet-01", "", "", "", nil),
		), target: feedTarget, confidence: sdk.PowerFeedMappingAmbiguous, feed: "pdu-1-outlet-01", reasons: []string{"feed_evidence_incomplete"}},
		{name: "no feed assigned", wire: feedMap(
			Slot{Occupied: true, Hashboard: board(1), Feed: &sdk.ContainerFeedEvidence{}},
			Slot{Occupied: true, Hashboard: board(2), Feed: &sdk.ContainerFeedEvidence{}},
			Slot{Feed: &sdk.ContainerFeedEvidence{}},
		), target: feedTarget, confidence: sdk.PowerFeedMappingUnavailable, reasons: []string{"no_feed_assigned"}},
		{name: "module replaced", wire: feedMap(), target: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1, ReportedID: "module-old"}, confidence: sdk.PowerFeedMappingAmbiguous, feed: "pdu-1-outlet-01", reasons: []string{"module_identity_changed"}},
		{name: "identity unconfirmed", wire: feedMap(), target: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1}, confidence: sdk.PowerFeedMappingAmbiguous, feed: "pdu-1-outlet-01", reasons: []string{"module_identity_unconfirmed"}},
		{name: "module missing", wire: feedMap(), target: sdk.ContainerPowerFeedModule{Tank: 7, Module: 1, ReportedID: "module-a"}, confidence: sdk.PowerFeedMappingUnavailable, reasons: []string{"module_not_in_topology"}},
		{name: "no topology", wire: MapResponse{}, target: feedTarget, confidence: sdk.PowerFeedMappingUnavailable, reasons: []string{"topology_unavailable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := moduleFeedMapping(tc.wire, tc.target)
			require.Equal(t, tc.confidence, got.Confidence)
			require.Equal(t, tc.feed, got.FeedID)
			require.Equal(t, tc.reasons, got.Reasons)
			require.Equal(t, tc.shared, got.SharedWith)
		})
	}
}

// Feed records name no modules, so a map that omits hardware cannot show
// whether that hardware shares the feed. The mapping is then never unique.
func TestModuleFeedMappingNeedsCompleteTopology(t *testing.T) {
	for name, partial := range map[string]func(*ContainerMap){
		"tank missing":   func(m *ContainerMap) { m.Tanks = m.Tanks[:len(m.Tanks)-1] },
		"module missing": func(m *ContainerMap) { m.Tanks[2].Modules = m.Tanks[2].Modules[1:] },
		"module that shares the feed missing": func(m *ContainerMap) {
			m.Tanks[0].Modules[0].Slots = []Slot{feedSlot("pdu-1-outlet-02", "", "", "", feedBoard("aa:00:00:00:00:01", "module-a")), feedSlot("pdu-1-outlet-02", "", "", "", nil), feedSlot("pdu-1-outlet-02", "", "", "", nil)}
			m.Tanks[0].Modules = append(m.Tanks[0].Modules[:1], m.Tanks[0].Modules[2:]...)
		},
		"tank layout unknown":            func(m *ContainerMap) { m.Tanks[4].ExpectedModules = -1 },
		"slot unreported elsewhere":      func(m *ContainerMap) { m.Tanks[1].Modules[4].Slots[2] = Slot{unreported: true} },
		"slot unreported in this module": func(m *ContainerMap) { m.Tanks[0].Modules[0].Slots[2] = Slot{unreported: true} },
		"board elsewhere names no feed":  func(m *ContainerMap) { m.Tanks[5].Modules[15].Slots[0].Feed = &sdk.ContainerFeedEvidence{} },
		"unlocated board": func(m *ContainerMap) {
			m.Tanks = append([]Tank{{ID: 0, Modules: []Module{{Slots: []Slot{{Occupied: true, Hashboard: feedBoard("cc:00:00:00:00:01", "")}}}}}}, m.Tanks...)
		},
		"module position outside layout":  func(m *ContainerMap) { m.Tanks[3].Modules[0].ID = sdk.CT1ModulesPerTank + 1 },
		"tank reported twice":             func(m *ContainerMap) { m.Tanks[5].ID = 5 },
		"module reported with two slots":  func(m *ContainerMap) { m.Tanks[1].Modules[0].Slots = m.Tanks[1].Modules[0].Slots[:2] },
		"empty unlocated group is benign": nil,
	} {
		t.Run(name, func(t *testing.T) {
			wire := feedMap()
			if partial == nil {
				wire.ContainerMap.Tanks = append([]Tank{{ID: 0, Modules: []Module{{}}}}, wire.ContainerMap.Tanks...)
				got := moduleFeedMapping(wire, feedTarget)
				require.Equal(t, sdk.PowerFeedMappingWiringVerified, got.Confidence)
				require.Empty(t, got.Reasons)
				return
			}
			partial(wire.ContainerMap)
			got := moduleFeedMapping(wire, feedTarget)
			require.Equal(t, sdk.PowerFeedMappingAmbiguous, got.Confidence, "a partial map never yields a unique feed")
			require.Equal(t, []string{"topology_incomplete"}, got.Reasons)
			require.NotEmpty(t, got.FeedID, "the feed is still reported for inspection")
			require.Empty(t, got.SharedWith)
		})
	}
}

func TestModuleFeedMappingReportsBoardEvidence(t *testing.T) {
	got := moduleFeedMapping(feedMap(), feedTarget)
	require.Len(t, got.Boards, 3)
	require.Equal(t, sdk.PowerFeedBoardEvidence{Position: 1, Occupied: true, Serial: "sn-aa:00:00:00:00:01", MACAddress: "aa:00:00:00:00:01", SlotFeedID: "pdu-1-outlet-01", ObservedFeedID: "pdu-1-outlet-01", ExpectedFeedID: "pdu-1-outlet-01", WiringStatus: "verified"}, got.Boards[0])
	require.Equal(t, sdk.PowerFeedBoardEvidence{Position: 3, SlotFeedID: "pdu-1-outlet-01"}, got.Boards[2])
}

func TestFeedReading(t *testing.T) {
	enabled := mdk.PowerFeedState_POWER_FEED_STATE_ENABLED
	feed := func(id string) *mdk.PowerFeed {
		return &mdk.PowerFeed{PowerFeedId: id, State: &enabled, Active: new(true), Faulted: new(false), Phase: mdk.Phase_PHASE_B, VoltageV: new(230.5), CurrentA: new(0.0), PowerW: new(math.NaN()), CurrentAA: new(12.0)}
	}
	got, reason := feedReading(&mdk.ListPowerFeedsResponse{SnapshotReady: new(true), PowerFeeds: []*mdk.PowerFeed{feed("pdu-1-outlet-02"), feed("pdu-1-outlet-01")}}, "pdu-1-outlet-01")
	require.Empty(t, reason)
	require.Equal(t, &sdk.PowerFeedReading{ID: "pdu-1-outlet-01", State: sdk.PowerFeedStateEnabled, Active: new(true), Faulted: new(false), Phase: "B", VoltageV: new(230.5), CurrentA: new(0.0), CurrentAA: new(12.0)}, got, "a measured zero is kept and a non-finite reading is dropped")

	unset := feed("pdu-1-outlet-01")
	unset.State = nil
	got, _ = feedReading(&mdk.ListPowerFeedsResponse{PowerFeeds: []*mdk.PowerFeed{unset}}, "pdu-1-outlet-01")
	require.Equal(t, sdk.PowerFeedStateUnknown, got.State, "absent state is unknown, never assumed on")

	for name, tc := range map[string]struct {
		resp *mdk.ListPowerFeedsResponse
		want string
	}{
		"snapshot not ready": {&mdk.ListPowerFeedsResponse{SnapshotReady: new(false), PowerFeeds: []*mdk.PowerFeed{feed("pdu-1-outlet-01")}}, sdk.PowerFeedReadingSnapshotNotReady},
		"missing":            {&mdk.ListPowerFeedsResponse{PowerFeeds: []*mdk.PowerFeed{feed("pdu-1-outlet-02")}}, sdk.PowerFeedReadingNotInSnapshot},
		"duplicate":          {&mdk.ListPowerFeedsResponse{PowerFeeds: []*mdk.PowerFeed{feed("pdu-1-outlet-01"), feed("pdu-1-outlet-01")}}, sdk.PowerFeedReadingDuplicate},
	} {
		t.Run(name, func(t *testing.T) {
			got, reason := feedReading(tc.resp, "pdu-1-outlet-01")
			require.Nil(t, got)
			require.Equal(t, tc.want, reason)
		})
	}
}

type powerFeedServer struct {
	mdk.UnimplementedSystemServiceServer
	mdk.UnimplementedHashboardServiceServer
	mdk.UnimplementedPowerServiceServer
	containerMap *mdk.GetContainerMapResponse
	feeds        *mdk.ListPowerFeedsResponse
	feedsErr     error
	cycle        *mdk.PowerRecoveryCycle
	cycleErr     error
	primary      bool
	writes       int
	// writeErr fails every write; otherwise writes acknowledge like Frigate.
	writeErr error
	written  []string
	// ackOverride replaces the acknowledgement of a successful write.
	ackOverride func(*mdk.PowerFeed) *mdk.PowerFeed
}

func (s *powerFeedServer) GetSystemInfo(context.Context, *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	return &mdk.GetSystemInfoResponse{Info: &mdk.SystemInfo{Id: "ct1", Product: "CT1"}}, nil
}
func (s *powerFeedServer) GetSystemStatus(context.Context, *mdk.GetSystemStatusRequest) (*mdk.GetSystemStatusResponse, error) {
	return &mdk.GetSystemStatusResponse{Status: &mdk.SystemStatus{ContainerWritePrimary: s.primary}}, nil
}
func (s *powerFeedServer) GetContainerMap(context.Context, *mdk.GetContainerMapRequest) (*mdk.GetContainerMapResponse, error) {
	return s.containerMap, nil
}
func (s *powerFeedServer) ListPowerFeeds(context.Context, *mdk.ListPowerFeedsRequest) (*mdk.ListPowerFeedsResponse, error) {
	return s.feeds, s.feedsErr
}
func (s *powerFeedServer) GetActivePowerRecoveryCycle(context.Context, *mdk.GetActivePowerRecoveryCycleRequest) (*mdk.GetActivePowerRecoveryCycleResponse, error) {
	return &mdk.GetActivePowerRecoveryCycleResponse{Cycle: s.cycle}, s.cycleErr
}
func (s *powerFeedServer) ack(feedID string, state mdk.PowerFeedState, overridden bool) *mdk.PowerFeed {
	feed := &mdk.PowerFeed{PowerFeedId: feedID, State: &state, Overridden: overridden}
	if s.ackOverride != nil {
		return s.ackOverride(feed)
	}
	return feed
}
func (s *powerFeedServer) SetPowerFeedState(_ context.Context, req *mdk.SetPowerFeedStateRequest) (*mdk.SetPowerFeedStateResponse, error) {
	s.writes++
	s.written = append(s.written, fmt.Sprintf("state %s disabled=%t", req.GetPowerFeedId(), req.GetDisabled()))
	if s.writeErr != nil {
		return nil, s.writeErr
	}
	state := mdk.PowerFeedState_POWER_FEED_STATE_ENABLED
	if req.GetDisabled() {
		state = mdk.PowerFeedState_POWER_FEED_STATE_DISABLED
	}
	return &mdk.SetPowerFeedStateResponse{PowerFeed: s.ack(req.GetPowerFeedId(), state, true)}, nil
}
func (s *powerFeedServer) CyclePowerFeed(_ context.Context, req *mdk.CyclePowerFeedRequest) (*mdk.CyclePowerFeedResponse, error) {
	s.writes++
	s.written = append(s.written, "cycle "+req.GetPowerFeedId())
	if s.writeErr != nil {
		return nil, s.writeErr
	}
	// Frigate acknowledges a cycle with its restore target.
	return &mdk.CyclePowerFeedResponse{PowerFeed: s.ack(req.GetPowerFeedId(), mdk.PowerFeedState_POWER_FEED_STATE_ENABLED, true)}, nil
}
func (s *powerFeedServer) ClearPowerFeedOverride(_ context.Context, req *mdk.ClearPowerFeedOverrideRequest) (*mdk.ClearPowerFeedOverrideResponse, error) {
	s.writes++
	s.written = append(s.written, "clear "+req.GetPowerFeedId())
	if s.writeErr != nil {
		return nil, s.writeErr
	}
	return &mdk.ClearPowerFeedOverrideResponse{PowerFeed: s.ack(req.GetPowerFeedId(), mdk.PowerFeedState_POWER_FEED_STATE_ENABLED, false)}, nil
}

func newPowerFeedDevice(t *testing.T, s *powerFeedServer) *Device {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	mdk.RegisterSystemServiceServer(server, s)
	mdk.RegisterHashboardServiceServer(server, s)
	mdk.RegisterPowerServiceServer(server, s)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: "127.0.0.1", Port: 8080, RPCPort: int32(addr.Port), URLScheme: "http"}) //nolint:gosec // test port
	require.NoError(t, err)
	return dev
}

// wireFeedMap is a complete CT1 map whose module 1.1 is supplied by feed;
// every other position is a filler module on its own feed.
func wireFeedMap(feed string) *mdk.GetContainerMapResponse {
	slot := func(tank, module int32, position uint32, feed string, board *mdk.Hashboard) *mdk.Slot {
		out := &mdk.Slot{Occupied: board != nil, PowerFeedId: feed, ConfiguredLocation: &mdk.HashboardLocation{Tank: tank, Module: module, HashboardPosition: new(position)}, Hashboard: board}
		if board != nil {
			board.Location = &mdk.HashboardLocation{Tank: tank, Module: module, HashboardPosition: new(position), PowerFeedId: feed, WiringStatus: "verified"}
		}
		return out
	}
	out := &mdk.ContainerMap{}
	for tank := int32(1); tank <= sdk.CT1TankCount; tank++ {
		t := &mdk.Tank{Id: tank}
		for module := int32(1); module <= sdk.CT1ModulesPerTank; module++ {
			filler := fmt.Sprintf("filler-%d-outlet-%02d", tank, module)
			board := &mdk.Hashboard{MacAddress: fmt.Sprintf("bb:00:00:00:%02x:%02x", tank, module), ModuleId: fmt.Sprintf("module-%d-%d", tank, module)}
			t.Modules = append(t.Modules, &mdk.Module{Id: module, Slots: []*mdk.Slot{
				slot(tank, module, 1, filler, board), slot(tank, module, 2, filler, nil), slot(tank, module, 3, filler, nil),
			}})
		}
		out.Tanks = append(out.Tanks, t)
	}
	out.Tanks[0].Modules[0].Slots = []*mdk.Slot{
		slot(1, 1, 1, feed, &mdk.Hashboard{MacAddress: "aa:00:00:00:00:01", ModuleId: "module-a"}),
		slot(1, 1, 2, feed, &mdk.Hashboard{MacAddress: "aa:00:00:00:00:02", ModuleId: "module-a"}),
		slot(1, 1, 3, feed, nil),
	}
	return &mdk.GetContainerMapResponse{ContainerMap: out}
}

func TestContainerPowerFeedInspectsWithoutWriting(t *testing.T) {
	enabled := mdk.PowerFeedState_POWER_FEED_STATE_DISABLED
	restore := time.Date(2026, 11, 14, 12, 0, 30, 0, time.UTC)
	s := &powerFeedServer{
		containerMap: wireFeedMap("pdu-1-outlet-01"),
		feeds:        &mdk.ListPowerFeedsResponse{SnapshotReady: new(true), PowerFeeds: []*mdk.PowerFeed{{PowerFeedId: "pdu-1-outlet-01", State: &enabled, Overridden: true, VoltageV: new(0.0)}}},
		cycle:        &mdk.PowerRecoveryCycle{Id: "cycle-1", Scope: "outlets", Phase: "holding", OutletIds: []string{"pdu-1-outlet-01"}, RestoreAfter: timestamppb.New(restore)},
		primary:      true,
	}
	dev := newPowerFeedDevice(t, s)
	got, err := dev.ContainerPowerFeed(t.Context(), sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedInspect, Module: feedTarget})
	require.NoError(t, err)
	require.Zero(t, s.writes)
	i := got.Inspection
	require.Equal(t, sdk.PowerFeedMappingWiringVerified, i.Mapping.Confidence)
	require.Equal(t, "pdu-1-outlet-01", i.Mapping.FeedID)
	require.Equal(t, &sdk.PowerFeedReading{ID: "pdu-1-outlet-01", State: sdk.PowerFeedStateDisabled, Overridden: true, VoltageV: new(0.0)}, i.Feed)
	require.Equal(t, new(true), i.ControllerWritePrimary)
	require.True(t, i.RecoveryCycleKnown)
	require.Equal(t, &sdk.PowerRecoveryCycle{ID: "cycle-1", Scope: "outlets", Phase: "holding", OutletIDs: []string{"pdu-1-outlet-01"}, RestoreAfter: &restore}, i.RecoveryCycle)

	s.feedsErr = status.Error(codes.Unavailable, "down")
	s.cycleErr = status.Error(codes.Unavailable, "down")
	got, err = dev.ContainerPowerFeed(t.Context(), sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedInspect, Module: feedTarget})
	require.NoError(t, err, "a missing reading is reported, not fatal")
	require.Nil(t, got.Inspection.Feed)
	require.Equal(t, sdk.PowerFeedReadingReadFailed, got.Inspection.FeedUnavailableReason)
	require.False(t, got.Inspection.RecoveryCycleKnown, "an unreadable cycle is unknown, never reported as none")
	require.Nil(t, got.Inspection.RecoveryCycle)
}

func TestContainerPowerFeedRejectsInvalidRequest(t *testing.T) {
	dev := newPowerFeedDevice(t, &powerFeedServer{containerMap: wireFeedMap("pdu-1-outlet-01")})
	_, err := dev.ContainerPowerFeed(t.Context(), sdk.ContainerPowerFeedRequest{Action: "cycle", Module: feedTarget})
	require.Error(t, err)
}
