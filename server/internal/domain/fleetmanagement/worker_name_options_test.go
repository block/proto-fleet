package fleetmanagement

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"buf.build/go/protovalidate"
	"connectrpc.com/authn"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/internal/domain/deviceresolver"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storemocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestReviewWorkerNameOptionsForMiners(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options *pb.WorkerNameOptions
		want    string
	}{
		{"none", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_NONE}, ""},
		{"mac", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_MAC_ADDRESS}, "aa:bb:cc:dd:1a:2b"},
		{"custom", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_CUSTOM, Name: proto.String("rack-1")}, "rack-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			devices := storemocks.NewMockDeviceStore(ctrl)
			devices.EXPECT().AllDevicesBelongToOrg(gomock.Any(), []string{"miner"}, int64(2)).Return(true, nil)
			devices.EXPECT().GetDevicePropertiesForRename(gomock.Any(), int64(2), []string{"miner"}, false).Return([]interfaces.DeviceRenameProperties{{DeviceIdentifier: "miner", MacAddress: "AA:BB:CC:DD:1A:2B"}}, nil)
			pools := &workerNamePoolServiceStub{}
			service := &Service{deviceStore: devices, deviceResolver: deviceresolver.New(devices), workerNamePoolService: pools}
			_, err := service.ReviewWorkerNames(authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 2}), &pb.ReviewWorkerNamesRequest{
				DeviceSelector: &pb.DeviceSelector{SelectionType: &pb.DeviceSelector_IncludeDevices{IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"miner"}}}}, WorkerName: tc.options,
			})
			require.NoError(t, err)
			require.Equal(t, map[string]string{"miner": tc.want}, pools.lastDesiredWorkerNames)
		})
	}
}

func TestReviewBoardWorkerNameOptionsUseSharedEndpoint(t *testing.T) {
	pools := &workerNamePoolServiceStub{reviews: []poolconfigurations.Review{{ID: "review", WorkerNames: []poolconfigurations.WorkerNameChange{{DeviceIdentifier: "controller", HashboardID: 17, After: "custom"}}}}}
	service := &Service{workerNamePoolService: pools}
	result, err := service.ReviewWorkerNames(authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 2}), &pb.ReviewWorkerNamesRequest{ModuleIds: []int64{9}, HashboardIds: []int64{17}, WorkerName: &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_CUSTOM, Name: proto.String("custom")}})
	require.NoError(t, err)
	require.Equal(t, []int64{9}, pools.boardSelection.ModuleIDs)
	require.Equal(t, []int64{17}, pools.boardSelection.HashboardIDs)
	require.Equal(t, "custom", pools.boardOptions.Custom)
	require.Equal(t, int32(1), result.UnchangedCount)
	require.Equal(t, int64(17), result.Reviews[0].Changes[0].HashboardId)
}

func TestWorkerNameOptionsRejectIgnoredInputs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options *pb.WorkerNameOptions
	}{
		{"missing mode", &pb.WorkerNameOptions{}},
		{"unknown mode", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode(99)}},
		{"custom missing name", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_CUSTOM}},
		{"custom empty name", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_CUSTOM, Name: proto.String("")}},
		{"custom whitespace", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_CUSTOM, Name: proto.String("has space")}},
		{"custom control", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_CUSTOM, Name: proto.String("has\nnewline")}},
		{"custom too long", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_CUSTOM, Name: proto.String(strings.Repeat("x", 101))}},
		{"none with name", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_NONE, Name: proto.String("ignored")}},
		{"none with empty name", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_NONE, Name: proto.String("")}},
		{"mac with name", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_MAC_ADDRESS, Name: proto.String("ignored")}},
		{"mac with empty name", &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_MAC_ADDRESS, Name: proto.String("")}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Error(t, protovalidate.Validate(tc.options)) })
	}
	request := &pb.ReviewWorkerNamesRequest{HashboardIds: []int64{17}, WorkerName: &pb.WorkerNameOptions{Mode: pb.WorkerNameMode_WORKER_NAME_MODE_NONE}}
	require.NoError(t, protovalidate.Validate(request))
	request.Sort = []*commonpb.SortConfig{{}}
	require.Error(t, protovalidate.Validate(request), "sort must not be silently ignored")
	request.Sort = nil
	request.DeviceSelector = &pb.DeviceSelector{SelectionType: &pb.DeviceSelector_IncludeDevices{IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"miner"}}}}
	require.Error(t, protovalidate.Validate(request), "ambiguous target selection")
}

func TestWorkerNameOptionsJSON(t *testing.T) {
	for _, input := range []string{
		`{"mode":"WORKER_NAME_MODE_NONE"}`,
		`{"mode":"WORKER_NAME_MODE_MAC_ADDRESS"}`,
		`{"mode":"WORKER_NAME_MODE_CUSTOM","name":"rack-1"}`,
	} {
		t.Run(input, func(t *testing.T) {
			var options pb.WorkerNameOptions
			require.NoError(t, protojson.Unmarshal([]byte(input), &options))
			require.NoError(t, protovalidate.Validate(&options))
			encoded, err := protojson.Marshal(&options)
			require.NoError(t, err)
			require.JSONEq(t, input, string(encoded))
		})
	}
}
