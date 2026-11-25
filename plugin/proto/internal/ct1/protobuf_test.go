package ct1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestProtoMapUsesPhysicalSlots(t *testing.T) {
	now := time.Now().UTC()
	board := func(pos uint32) *mdk.Slot {
		return &mdk.Slot{Occupied: true, Hashboard: &mdk.Hashboard{MacAddress: fmt.Sprintf("00:01:02:03:04:%02d", pos), ModuleId: "module", Location: &mdk.HashboardLocation{HashboardPosition: &pos}, MiningState: mdk.MiningState_MINING_STATE_MINING, Source: mdk.HashboardSource_HASHBOARD_SOURCE_LOCAL, Health: &mdk.HashboardHealth{Timestamp: timestamppb.New(now), Hashrate: 100, AverageAsicTemperatureC: new(60.0), HashrailOk: true, RefclksOk: true, PsuElectrical: &mdk.PsuElectricalHealth{InputPower: 9000}}}}
	}
	input := &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{Tanks: []*mdk.Tank{{Id: 1, Modules: []*mdk.Module{{Id: 1, Slots: []*mdk.Slot{board(3), board(1), board(2)}}}}}}}
	result, err := snapshot(mapResponse(input), now)
	require.NoError(t, err)
	m := result.Tanks[0].Modules[0]
	require.Equal(t, sdk.MinerStatusActive, m.State)
	require.Equal(t, 300e12, *m.Metrics.HashrateHS)
	require.Equal(t, 9000.0, *m.Metrics.PowerW)
	for i, b := range m.Hashboards {
		require.Equal(t, i+1, int(b.Position))
		require.Equal(t, fmt.Sprintf("00:01:02:03:04:%02d", i+1), b.MACAddress)
	}
	// Missing position cannot manufacture placement; duplicate claims veto all contributors.
	input.ContainerMap.Tanks[0].Modules[0].Slots[0].Hashboard.Location = nil
	result, err = snapshot(mapResponse(input), now)
	require.NoError(t, err)
	require.Nil(t, result.Tanks[0].Modules[0].Metrics.HashrateHS)
	for _, b := range result.Tanks[0].Modules[0].Hashboards {
		require.Equal(t, "unknown", b.Occupancy)
	}
}

func TestProtoPresenceAndUnknownEnums(t *testing.T) {
	b := boardFromProto(&mdk.Hashboard{MiningState: mdk.MiningState(999), Health: &mdk.HashboardHealth{Timestamp: timestamppb.Now(), PsuElectrical: &mdk.PsuElectricalHealth{}}})
	require.Nil(t, b.Health.Hashrate, "implicit zero has no producer validity marker")
	require.Equal(t, 0.0, *b.Health.PSUElectrical.InputPower, "present PSU message is a complete snapshot")
	require.Equal(t, "999", b.MiningState)

}

func fixtureClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := newGRPCFixture(t, handler)
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(u.Port(), 10, 32)
	require.NoError(t, err)
	c, err := NewClient(sdk.DeviceInfo{Host: u.Hostname(), Port: 8080, URLScheme: "http", RPCPort: int32(port)}, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func TestRequiredMapErrorsAndPayloadBounds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		httpCode int
		want     codes.Code
	}{{"authentication", 401, codes.Unauthenticated}, {"authorization", 403, codes.PermissionDenied}, {"unavailable", 503, codes.Unavailable}, {"missing service", 404, codes.Unimplemented}, {"oversized", 200, codes.ResourceExhausted}} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v2/container/map" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(tc.httpCode)
				if tc.httpCode == 200 {
					_, _ = fmt.Fprintf(w, `{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"serial":%q}}]}]}]}}`, strings.Repeat("x", maxReceiveBytes))
				}
			}))
			_, err := c.Collect(t.Context(), "test")
			require.Equal(t, tc.want, status.Code(err))
		})
	}
}
func TestClosedClientAndCanceledPoll(t *testing.T) {
	c := fixtureClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") }))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Collect(ctx, "test")
	require.Equal(t, codes.Canceled, status.Code(err))
	require.NoError(t, c.Close())
	_, err = c.Discover(t.Context(), sdk.DeviceInfo{})
	require.Equal(t, codes.Canceled, status.Code(err))
}

func TestProtoMapKeepsEmptyAndInvalidSlotsDistinct(t *testing.T) {
	now := time.Now().UTC()
	bad := &mdk.Hashboard{Serial: "board-1", MiningState: mdk.MiningState_MINING_STATE_MINING, Location: &mdk.HashboardLocation{HashboardPosition: new(uint32(1))}, Health: &mdk.HashboardHealth{Timestamp: &timestamppb.Timestamp{Seconds: 1 << 62}}}
	wire := &mdk.GetContainerMapResponse{ContainerMap: &mdk.ContainerMap{Tanks: []*mdk.Tank{{Id: 1, Modules: []*mdk.Module{{Id: 1, Slots: []*mdk.Slot{
		{Occupied: true, Hashboard: bad},
		{ConfiguredLocation: &mdk.HashboardLocation{HashboardPosition: new(uint32(2))}},
	}}}}}}}
	result, err := snapshot(mapResponse(wire), now)
	require.NoError(t, err)
	boards := result.Tanks[0].Modules[0].Hashboards
	require.Equal(t, "unknown", boards[0].Occupancy)
	require.Nil(t, boards[0].HashrateHS)
	require.Equal(t, "empty", boards[1].Occupancy)
	require.Equal(t, "unknown", boards[2].Occupancy)
}

func TestProtoASICMaximumSurvivesCollection(t *testing.T) {
	now := time.Now().UTC()
	for _, maximum := range []*float64{nil, new(0.0), new(73.2)} {
		board := boardFromProto(&mdk.Hashboard{
			MacAddress: "00:01:02:03:04:05", ModuleId: "module", MiningState: mdk.MiningState_MINING_STATE_MINING,
			Health: &mdk.HashboardHealth{Timestamp: timestamppb.New(now), MaximumAsicTemperatureC: maximum, AsicTemperatureObservedAt: timestamppb.New(now)},
		})
		observation := hashboardObservation(Slot{Occupied: true, Hashboard: board}, 1, now)
		require.Equal(t, "occupied", observation.Occupancy)
		var health map[string]any
		require.NoError(t, json.Unmarshal(observation.HealthJSON, &health))
		if maximum == nil {
			require.NotContains(t, health, "maximumAsicTemperatureC")
		} else {
			require.Equal(t, *maximum, health["maximumAsicTemperatureC"])
		}
		require.NotEmpty(t, health["asicTemperatureObservedAt"])
	}
}
