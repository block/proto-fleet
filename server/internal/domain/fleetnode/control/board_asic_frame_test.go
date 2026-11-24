package control

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Every frame field survives the node's encode, the wire and fleetd's decode.
// requireEveryFieldSet fails first when a field is added to the SDK frame, so
// the new field gets a value here and then has to round-trip.
func TestBoardASICFrameRoundTripsEveryField(t *testing.T) {
	quarantined := true
	frame := sdk.BoardASICFrame{
		MACAddress: "aa:bb:cc:dd:ee:ff",
		ObservedAt: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC),
		Layout: &sdk.BoardASICLayout{
			StackCount: 2, ASICsPerStack: 3, TotalASICs: 6,
			Positions: []sdk.BoardASICPosition{{Index: 1, StackID: 2, StackIndex: 3, X: 4, Y: 5}},
		},
		ASICs: []sdk.BoardASICReading{{
			Index: 1, TemperatureC: 61.5, HashrateGHs: 123.4, VoltageV: 0.32,
			FrequencyMHz: 525, PassRatePercent: 99.1, ThermalQuarantined: &quarantined,
		}},
	}
	requireEveryFieldSet(t, reflect.ValueOf(frame), "BoardASICFrame")

	payload, err := proto.Marshal(BoardASICFrameToProto(frame))
	require.NoError(t, err)
	var decoded gatewaypb.BoardASICStreamFrame
	require.NoError(t, proto.Unmarshal(payload, &decoded))
	require.Equal(t, frame, BoardASICFrameFromProto(&decoded))
}

func requireEveryFieldSet(t *testing.T, v reflect.Value, path string) {
	t.Helper()
	require.False(t, v.IsZero(), "%s is not set; give it a value so the round trip covers it", path)
	//nolint:exhaustive // Scalars have no fields to walk.
	switch v.Kind() {
	case reflect.Pointer:
		requireEveryFieldSet(t, v.Elem(), path)
	case reflect.Slice:
		for i := range v.Len() {
			requireEveryFieldSet(t, v.Index(i), path)
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			return
		}
		for i := range v.NumField() {
			requireEveryFieldSet(t, v.Field(i), path+"."+v.Type().Field(i).Name)
		}
	}
}
