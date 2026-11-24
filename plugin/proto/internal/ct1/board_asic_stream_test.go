package ct1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var streamTarget = sdk.BoardIdentifyTarget{Tank: 1, Module: 1, Slot: 2, ModuleReportedID: "module-a", MACAddress: "aa:00:00:00:00:02"}

// newBoardStreamDevice serves a controller map whose slot 2 holds boardMAC.
func newBoardStreamDevice(t *testing.T, boardMAC string, stream func(*mdk.StreamASICHealthRequest, mdk.TelemetryService_StreamASICHealthServer) error) *Device {
	t.Helper()
	srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/system/info":
			_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
		case "/api/v2/container/map":
			require.NoError(t, json.NewEncoder(w).Encode(boardIdentifyMap(boardMAC, "module-a")))
		default:
			t.Errorf("unexpected controller request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	srv.asicHealth = stream
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(u.Port(), 10, 32)
	require.NoError(t, err)
	dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
	require.NoError(t, err)
	return dev
}

func TestBoardASICStreamRelaysTheConfirmedBoardsReadings(t *testing.T) {
	observed := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	quarantined := true
	var identifier string
	dev := newBoardStreamDevice(t, "AA:00:00:00:00:02", func(in *mdk.StreamASICHealthRequest, stream mdk.TelemetryService_StreamASICHealthServer) error {
		identifier = in.GetIdentifier()
		require.NoError(t, stream.Send(&mdk.StreamASICHealthResponse{
			MacAddress: "AA:00:00:00:00:02",
			Timestamp:  timestamppb.New(observed),
			Layout:     &mdk.HashboardASICLayout{StackCount: 1, AsicsPerStack: 2, TotalAsics: 2, Entries: []*mdk.ASICLayoutEntry{{Index: 0}, {Index: 1, StackIndex: 1, X: 1}}},
			Asics: []*mdk.ASIC{
				{Index: 0, TemperatureDc: 725, HashrateGhs10: 125, VoltageMv: 320, FrequencyMhz: 500, PassRateBp: 9950, ThermalQuarantined: &quarantined, DebugHealth: &mdk.ASICDebugHealth{VddV: 1.2}},
				{Index: 1, TemperatureDc: 700},
			},
		}))
		return stream.Send(&mdk.StreamASICHealthResponse{MacAddress: "AA:00:00:00:00:02", Timestamp: timestamppb.New(observed.Add(time.Second)), Asics: []*mdk.ASIC{{Index: 0, TemperatureDc: 730}}})
	})

	var frames []sdk.BoardASICFrame
	require.NoError(t, dev.StreamBoardASICReadings(t.Context(), streamTarget, func(f sdk.BoardASICFrame) error {
		frames = append(frames, f)
		return nil
	}))

	require.Equal(t, "AA:00:00:00:00:02", identifier, "the stream addresses the controller's own identity for the confirmed board")
	require.Len(t, frames, 2)
	require.Equal(t, observed, frames[0].ObservedAt)
	require.Equal(t, &sdk.BoardASICLayout{StackCount: 1, ASICsPerStack: 2, TotalASICs: 2, Positions: []sdk.BoardASICPosition{{Index: 0}, {Index: 1, StackIndex: 1, X: 1}}}, frames[0].Layout)
	require.Equal(t, sdk.BoardASICReading{Index: 0, TemperatureC: 72.5, HashrateGHs: 12.5, VoltageV: 0.32, FrequencyMHz: 500, PassRatePercent: 99.5, ThermalQuarantined: &quarantined}, frames[0].ASICs[0])
	require.Nil(t, frames[0].ASICs[1].ThermalQuarantined, "unreported quarantine stays unknown")
	require.Nil(t, frames[1].Layout)
	require.InDelta(t, 73.0, frames[1].ASICs[0].TemperatureC, 1e-9)
}

func TestBoardASICStreamRefusesAReplacedBoardBeforeStreaming(t *testing.T) {
	opened := false
	dev := newBoardStreamDevice(t, "aa:00:00:00:00:07", func(*mdk.StreamASICHealthRequest, mdk.TelemetryService_StreamASICHealthServer) error {
		opened = true
		return nil
	})
	err := dev.StreamBoardASICReadings(t.Context(), streamTarget, func(sdk.BoardASICFrame) error { return nil })
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.ErrorContains(t, err, "slot 2 board was replaced")
	require.False(t, opened)
}

func TestBoardASICStreamEndsOnReadingsForAnotherBoard(t *testing.T) {
	dev := newBoardStreamDevice(t, "aa:00:00:00:00:02", func(_ *mdk.StreamASICHealthRequest, stream mdk.TelemetryService_StreamASICHealthServer) error {
		return stream.Send(&mdk.StreamASICHealthResponse{MacAddress: "aa:00:00:00:00:09"})
	})
	sent := 0
	err := dev.StreamBoardASICReadings(t.Context(), streamTarget, func(sdk.BoardASICFrame) error {
		sent++
		return nil
	})
	require.Equal(t, codes.DataLoss, status.Code(err))
	require.Zero(t, sent)
}

func TestBoardASICStreamEndsOnFramesNoBoardCouldProduce(t *testing.T) {
	oversized := make([]*mdk.ASIC, 0, maxBoardASICs+1)
	for i := int32(0); i <= maxBoardASICs; i++ {
		oversized = append(oversized, &mdk.ASIC{Index: i})
	}
	for name, frame := range map[string]*mdk.StreamASICHealthResponse{
		"more readings than a board has": {Asics: oversized},
		"a repeated reading":             {Asics: []*mdk.ASIC{{Index: 3}, {Index: 3}}},
		"a repeated layout entry":        {Layout: &mdk.HashboardASICLayout{Entries: []*mdk.ASICLayoutEntry{{Index: 3}, {Index: 3}}}},
	} {
		t.Run(name, func(t *testing.T) {
			frame.MacAddress = "aa:00:00:00:00:02"
			dev := newBoardStreamDevice(t, "aa:00:00:00:00:02", func(_ *mdk.StreamASICHealthRequest, stream mdk.TelemetryService_StreamASICHealthServer) error {
				return stream.Send(frame)
			})
			sent := 0
			err := dev.StreamBoardASICReadings(t.Context(), streamTarget, func(sdk.BoardASICFrame) error {
				sent++
				return nil
			})
			require.Equal(t, codes.DataLoss, status.Code(err))
			require.Zero(t, sent)
		})
	}
}

func TestBoardASICStreamPassesControllerErrorsAndStopsOnCancel(t *testing.T) {
	dev := newBoardStreamDevice(t, "aa:00:00:00:00:02", func(*mdk.StreamASICHealthRequest, mdk.TelemetryService_StreamASICHealthServer) error {
		return status.Error(codes.Unavailable, "board offline")
	})
	err := dev.StreamBoardASICReadings(t.Context(), streamTarget, func(sdk.BoardASICFrame) error { return nil })
	require.Equal(t, codes.Unavailable, status.Code(err))

	stopped := make(chan struct{})
	dev = newBoardStreamDevice(t, "aa:00:00:00:00:02", func(_ *mdk.StreamASICHealthRequest, stream mdk.TelemetryService_StreamASICHealthServer) error {
		defer close(stopped)
		if err := stream.Send(&mdk.StreamASICHealthResponse{MacAddress: "aa:00:00:00:00:02"}); err != nil {
			return err
		}
		<-stream.Context().Done()
		return nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	err = dev.StreamBoardASICReadings(ctx, streamTarget, func(sdk.BoardASICFrame) error {
		cancel()
		return nil
	})
	require.Equal(t, codes.Canceled, status.Code(err))
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("controller stream kept running after the caller cancelled")
	}
}
