package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type controllerRecordsDevice struct {
	sdk.Device
	received sdk.ControllerRecordsRequest
	result   sdk.ControllerRecordsResult
}

func (d *controllerRecordsDevice) ControllerRecords(_ context.Context, r sdk.ControllerRecordsRequest) (sdk.ControllerRecordsResult, error) {
	d.received = r
	return d.result, nil
}

func TestControllerRecordsNodeDispatchPreservesTypedRequestsAndBoundsResults(t *testing.T) {
	logs := sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindLogs, ControllerID: "ct1-b", Logs: &sdk.ControllerLogQuery{Limit: sdk.ControllerRecordsLimit, MinSeverity: sdk.ControllerLogWarn, Component: "plant"}}
	small := sdk.ControllerRecordsResult{ControllerID: "ct1-b", ControllerIDs: []string{"ct1-a", "ct1-b"}, Logs: &sdk.ControllerLogSnapshot{
		Entries:  []sdk.ControllerLogEntry{{Severity: sdk.ControllerLogWarn, Component: "plant", Message: "pump slow"}},
		Coverage: sdk.ControllerLogRetained,
	}}
	// A driver that returns more than fits one response: the node bounds it
	// before acking rather than sending an oversized payload.
	large := sdk.ControllerRecordsResult{ControllerID: "ct1-b", Logs: &sdk.ControllerLogSnapshot{Coverage: sdk.ControllerLogRetained}}
	for range sdk.ControllerRecordsLimit {
		large.Logs.Entries = append(large.Logs.Entries, sdk.ControllerLogEntry{Severity: sdk.ControllerLogWarn, Message: strings.Repeat("m", 8<<10)})
	}

	for name, tc := range map[string]struct {
		result sdk.ControllerRecordsResult
		check  func(t *testing.T, decoded sdk.ControllerRecordsResult)
	}{
		"typed result": {small, func(t *testing.T, decoded sdk.ControllerRecordsResult) {
			require.Equal(t, small, decoded)
		}},
		"over budget": {large, func(t *testing.T, decoded sdk.ControllerRecordsResult) {
			require.Equal(t, sdk.ControllerLogLimitReached, decoded.Logs.Coverage)
			require.Less(t, len(decoded.Logs.Entries), sdk.ControllerRecordsLimit)
			for _, e := range decoded.Logs.Entries {
				require.True(t, e.MessageTruncated)
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			driver := mocks.NewMockDriver(ctrl)
			base := mocks.NewMockDevice(ctrl)
			base.EXPECT().Close(gomock.Any()).Return(nil)
			device := &controllerRecordsDevice{Device: base, result: tc.result}
			driver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil)
			run := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}}
			data, err := sdk.EncodeControllerRecords(logs)
			require.NoError(t, err)
			capture := &captureAcker{}

			run.handleMinerCommand(t.Context(), nil, capture, "container", withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_ControllerRecords{ControllerRecords: &pb.ControllerRecordsAction{Request: data}}}), slog.Default())

			ack := capture.only(t)
			require.True(t, ack.Succeeded, ack.ErrorMessage)
			require.Equal(t, logs, device.received)
			decoded, err := sdk.DecodeControllerRecordsResult(ack.Payload, logs)
			require.NoError(t, err)
			tc.check(t, decoded)
		})
	}
}
