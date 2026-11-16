package miner

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (m *RemoteFleetNodeMiner) ControllerRecords(ctx context.Context, req sdk.ControllerRecordsRequest) (sdk.ControllerRecordsResult, error) {
	if reader, ok := m.delegate.(sdk.DeviceControllerRecords); ok {
		return reader.ControllerRecords(ctx, req)
	}
	return sdk.ControllerRecordsResult{}, m.unsupported("controller records")
}
