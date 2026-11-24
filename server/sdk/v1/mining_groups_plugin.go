package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
)

func (s *DriverGRPCServer) MiningGroups(ctx context.Context, req *pb.MiningGroupsRequest) (*pb.MiningGroupsResponse, error) {
	if req == nil || req.Ref == nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError("missing device", codes.InvalidArgument, "missing device")))
	}
	payload, err := DecodeMiningGroupRequest(req.Payload)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())))
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return nil, err
	}
	controller, ok := device.(DeviceMiningGroups)
	if !ok {
		return nil, sdkErrorToGRPCStatus(UnsupportedWrite(CapabilityMiningGroups))
	}
	result, err := controller.MiningGroups(ctx, payload)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}
	if err := result.Validate(payload); err != nil {
		return nil, grpcStatusError(err.Error(), codes.Internal, err.Error())
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode mining group response: %w", err)
	}
	return &pb.MiningGroupsResponse{Payload: data}, nil
}
func (d *DeviceGRPCClient) MiningGroups(ctx context.Context, req MiningGroupRequest) (MiningGroupResult, error) {
	var result MiningGroupResult
	if err := req.Validate(); err != nil {
		return result, InvalidWriteRequest(err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		return result, fmt.Errorf("encode mining group request: %w", err)
	}
	response, err := d.client.MiningGroups(ctx, &pb.MiningGroupsRequest{Ref: d.deviceRef(), Payload: data})
	if err != nil {
		return result, err
	}
	return DecodeMiningGroupResult(response.GetPayload(), req)
}
