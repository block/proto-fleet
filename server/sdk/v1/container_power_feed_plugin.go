package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
)

func (s *DriverGRPCServer) ContainerPowerFeed(ctx context.Context, req *pb.ContainerPowerFeedRequest) (*pb.ContainerPowerFeedResponse, error) {
	if req == nil || req.Ref == nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError("missing device", codes.InvalidArgument, "missing device")))
	}
	payload, err := DecodeContainerPowerFeedRequest(req.Payload)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())))
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return nil, err
	}
	controller, ok := device.(DeviceContainerPowerFeed)
	if !ok {
		return nil, sdkErrorToGRPCStatus(UnsupportedWrite(CapabilityContainerPowerFeed))
	}
	result, err := controller.ContainerPowerFeed(ctx, payload)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}
	if err := result.Validate(payload); err != nil {
		return nil, grpcStatusError(err.Error(), codes.Internal, err.Error())
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode container power feed response: %w", err)
	}
	return &pb.ContainerPowerFeedResponse{Payload: data}, nil
}

func (d *DeviceGRPCClient) ContainerPowerFeed(ctx context.Context, req ContainerPowerFeedRequest) (ContainerPowerFeedResult, error) {
	var result ContainerPowerFeedResult
	if err := req.Validate(); err != nil {
		return result, InvalidWriteRequest(err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		return result, fmt.Errorf("encode container power feed request: %w", err)
	}
	response, err := d.client.ContainerPowerFeed(ctx, &pb.ContainerPowerFeedRequest{Ref: d.deviceRef(), Payload: data})
	if err != nil {
		return result, err
	}
	return DecodeContainerPowerFeedResult(response.GetPayload(), req)
}
