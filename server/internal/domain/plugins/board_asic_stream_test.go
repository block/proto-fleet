package plugins

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// boardStreamSDKDevice adds the ASIC stream to the shared mock device.
type boardStreamSDKDevice struct {
	*mockSDKDevice
	frames []sdk.BoardASICFrame
	err    error
	boards []sdk.BoardIdentifyTarget
}

func (d *boardStreamSDKDevice) StreamBoardASICReadings(_ context.Context, board sdk.BoardIdentifyTarget, send func(sdk.BoardASICFrame) error) error {
	d.boards = append(d.boards, board)
	for _, frame := range d.frames {
		if err := send(frame); err != nil {
			return err
		}
	}
	return d.err
}

func TestPluginMinerStreamBoardASICReadings(t *testing.T) {
	board := sdk.BoardIdentifyTarget{Tank: 1, Module: 1, Slot: 2, ModuleReportedID: "module-a", MACAddress: "aa:00:00:00:00:02"}
	collect := func(pm *PluginMiner, target sdk.BoardIdentifyTarget) ([]sdk.BoardASICFrame, error) {
		var frames []sdk.BoardASICFrame
		err := pm.StreamBoardASICReadings(t.Context(), target, func(f sdk.BoardASICFrame) error {
			frames = append(frames, f)
			return nil
		})
		return frames, err
	}
	streaming := func(device sdk.Device) *PluginMiner {
		pm := createTestPluginMinerWithDevice(device)
		pm.caps = sdk.Capabilities{sdk.CapabilityBoardASICStream: true}
		return pm
	}

	device := &boardStreamSDKDevice{
		mockSDKDevice: &mockSDKDevice{id: "ct1"},
		frames:        []sdk.BoardASICFrame{{MACAddress: board.MACAddress}, {MACAddress: board.MACAddress}},
		err:           status.Error(codes.Unavailable, "board offline"),
	}
	frames, err := collect(streaming(device), board)
	require.Len(t, frames, 2)
	require.Equal(t, codes.Unavailable, status.Code(err), "the plugin's status reaches the caller unchanged")
	require.Equal(t, []sdk.BoardIdentifyTarget{board}, device.boards)

	_, err = collect(streaming(device), sdk.BoardIdentifyTarget{Tank: 1})
	require.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = collect(createTestPluginMinerWithDevice(device), board)
	require.True(t, fleeterror.IsUnimplementedError(err), "the capability gates the stream")

	_, err = collect(streaming(&mockSDKDevice{id: "rig"}), board)
	require.True(t, fleeterror.IsUnimplementedError(err), "a device without the stream is unsupported")
	require.Len(t, device.boards, 1, "refused streams never reach the device")
}
