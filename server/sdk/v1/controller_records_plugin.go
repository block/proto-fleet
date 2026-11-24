package sdk

import (
	"context"
	"fmt"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
)

func (s *DriverGRPCServer) ControllerRecords(ctx context.Context, req *pb.ControllerRecordsRequest) (*pb.ControllerRecordsResponse, error) {
	if req == nil || req.Ref == nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError("missing device", codes.InvalidArgument, "missing device")))
	}
	payload, err := DecodeControllerRecordsRequest(req.Payload)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())))
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return nil, err
	}
	reader, ok := device.(DeviceControllerRecords)
	if !ok {
		return nil, sdkErrorToGRPCStatus(UnsupportedWrite(CapabilityControllerRecords))
	}
	result, err := reader.ControllerRecords(ctx, payload)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}
	result = BoundControllerRecords(result)
	if err := result.Validate(payload); err != nil {
		return nil, grpcStatusError(err.Error(), codes.Internal, err.Error())
	}
	data, err := EncodeControllerRecords(result)
	if err != nil {
		return nil, fmt.Errorf("encode controller records response: %w", err)
	}
	return &pb.ControllerRecordsResponse{Payload: data}, nil
}

func (d *DeviceGRPCClient) ControllerRecords(ctx context.Context, req ControllerRecordsRequest) (ControllerRecordsResult, error) {
	var result ControllerRecordsResult
	if err := req.Validate(); err != nil {
		return result, InvalidWriteRequest(err)
	}
	data, err := EncodeControllerRecords(req)
	if err != nil {
		return result, fmt.Errorf("encode controller records request: %w", err)
	}
	response, err := d.client.ControllerRecords(ctx, &pb.ControllerRecordsRequest{Ref: d.deviceRef(), Payload: data})
	if err != nil {
		return result, err
	}
	return DecodeControllerRecordsResult(response.GetPayload(), req)
}
