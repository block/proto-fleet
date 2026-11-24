package miner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type streamingDelegate struct {
	interfaces.Miner
	board sdk.BoardIdentifyTarget
}

func (d *streamingDelegate) StreamBoardASICReadings(_ context.Context, board sdk.BoardIdentifyTarget, send func(sdk.BoardASICFrame) error) error {
	d.board = board
	return send(sdk.BoardASICFrame{MACAddress: board.MACAddress})
}

// The command service holds the wrapper as an interfaces.Miner and asserts the
// streamer interface on it, so the wrapper itself has to stream.
func TestRemoteFleetNodeMinerStreamsBoardASICReadingsThroughDelegate(t *testing.T) {
	delegate := &streamingDelegate{}
	var routed interfaces.Miner = &RemoteFleetNodeMiner{delegate: delegate}
	streamer, ok := routed.(sdk.DeviceBoardASICStreamer)
	require.True(t, ok)

	board := sdk.BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedID: "module-a", MACAddress: "02:00:00:00:00:0a"}
	var frames []sdk.BoardASICFrame
	err := streamer.StreamBoardASICReadings(t.Context(), board, func(frame sdk.BoardASICFrame) error {
		frames = append(frames, frame)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, board, delegate.board)
	assert.Equal(t, []sdk.BoardASICFrame{{MACAddress: board.MACAddress}}, frames)
}

func TestRemoteFleetNodeMinerBoardASICStreamWithoutStreamingDelegateIsUnimplemented(t *testing.T) {
	for name, miner := range map[string]*RemoteFleetNodeMiner{
		"no delegate":            {},
		"non-streaming delegate": {delegate: &recordingCurtailmentConfigMiner{}},
	} {
		t.Run(name, func(t *testing.T) {
			err := miner.StreamBoardASICReadings(t.Context(), sdk.BoardIdentifyTarget{}, func(sdk.BoardASICFrame) error { return nil })
			assert.Equal(t, codes.Unimplemented, status.Code(err))
		})
	}
}
