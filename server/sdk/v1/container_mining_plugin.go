package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
)

func (s *DriverGRPCServer) ContainerMining(ctx context.Context, req *pb.ContainerMiningRequest) (*pb.ContainerMiningResponse, error) {
	if req == nil || req.Ref == nil {
		return nil, grpcStatusError("missing device", codes.InvalidArgument, "missing device")
	}
	payload, err := DecodeContainerMiningRequest(req.Payload)
	if err != nil {
		return nil, grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return nil, err
	}
	controller, ok := device.(DeviceContainerMining)
	if !ok {
		return nil, sdkErrorToGRPCStatus(NewErrUnsupportedCapability(CapabilityContainerMining))
	}
	result, err := controller.ContainerMining(ctx, payload)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}
	if err := result.Validate(payload); err != nil {
		return nil, grpcStatusError(err.Error(), codes.Internal, err.Error())
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode container mining response: %w", err)
	}
	return &pb.ContainerMiningResponse{Payload: data}, nil
}
func (d *DeviceGRPCClient) ContainerMining(ctx context.Context, req ContainerMiningRequest) (ContainerMiningResult, error) {
	var result ContainerMiningResult
	if err := req.Validate(); err != nil {
		return result, err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return result, fmt.Errorf("encode container mining request: %w", err)
	}
	response, err := d.client.ContainerMining(ctx, &pb.ContainerMiningRequest{Ref: d.deviceRef(), Payload: data})
	if err != nil {
		return result, err
	}
	return DecodeContainerMiningResult(response.GetPayload(), req)
}
