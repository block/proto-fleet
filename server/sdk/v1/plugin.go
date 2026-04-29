package sdk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"
	"time"

	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const errDeviceDoesNotSupportCurtailment = "device does not support curtailment"

func grpcStatusError(label string, code codes.Code, message string) error {
	return fmt.Errorf("%s: %w", label, status.Error(code, message))
}

// Helper function to convert SDK errors to gRPC status errors
func sdkErrorToGRPCStatus(err error) error {
	if err == nil {
		return nil
	}

	var sdkErr SDKError
	if errors.As(err, &sdkErr) {
		switch sdkErr.Code {
		case ErrCodeDeviceNotFound:
			return grpcStatusError("device not found", codes.NotFound, sdkErr.Message)
		case ErrCodeUnsupportedCapability:
			return grpcStatusError("unsupported capability", codes.Unimplemented, sdkErr.Message)
		case ErrCodeCurtailCapabilityNotSupported:
			// Permanent: report as Unimplemented to avoid retries.
			return grpcStatusError("curtail capability not supported", codes.Unimplemented, sdkErr.Message)
		case ErrCodeInvalidConfig:
			return grpcStatusError("invalid config", codes.InvalidArgument, sdkErr.Message)
		case ErrCodeDeviceUnavailable:
			return grpcStatusError("device unavailable", codes.Unavailable, sdkErr.Message)
		case ErrCodeCurtailTransient:
			// Retryable: map to Unavailable.
			return grpcStatusError("curtail transient failure", codes.Unavailable, sdkErr.Message)
		case ErrCodeDriverShutdown:
			return grpcStatusError("driver shutdown", codes.Aborted, sdkErr.Message)
		case ErrCodeAuthenticationFailed:
			return grpcStatusError("authentication failed", codes.Unauthenticated, sdkErr.Message)
		default:
			return grpcStatusError("internal error", codes.Internal, sdkErr.Message)
		}
	}
	return err
}

// Helper function to safely convert int to int32
func safeIntToInt32(i int) int32 {
	if i > math.MaxInt32 || i < math.MinInt32 {
		return 0 // Return 0 for out-of-range values
	}
	return int32(i)
}

// ============================================================================
// Conversion Helpers - Rule of 3 Refactoring
// ============================================================================

// convertOptionalMetricToProto converts an optional MetricValue pointer to protobuf format
func convertOptionalMetricToProto(src *MetricValue) *pb.MetricValue {
	if src == nil {
		return nil
	}
	return metricValueToProto(*src)
}

// convertOptionalMetricFromProto converts a protobuf MetricValue to an optional pointer
func convertOptionalMetricFromProto(src *pb.MetricValue) *MetricValue {
	if src == nil {
		return nil
	}
	mv := metricValueFromProto(src)
	return &mv
}

// convertSliceToProto converts a slice of SDK types to protobuf using the provided converter
func convertSliceToProto[T any, P any](src []T, converter func(T) *P) []*P {
	if len(src) == 0 {
		return nil
	}
	result := make([]*P, len(src))
	for i, item := range src {
		result[i] = converter(item)
	}
	return result
}

// convertSliceFromProto converts a slice of protobuf pointers to SDK types using the provided converter
func convertSliceFromProto[T any, P any](src []*P, converter func(*P) T) []T {
	if len(src) == 0 {
		return nil
	}
	result := make([]T, len(src))
	for i, item := range src {
		result[i] = converter(item)
	}
	return result
}

// convertOptionalDuration converts an optional duration pointer to protobuf format
func convertOptionalDuration(src *time.Duration) *durationpb.Duration {
	if src == nil {
		return nil
	}
	return durationpb.New(*src)
}

// convertOptionalDurationFromProto converts a protobuf duration to an optional pointer
func convertOptionalDurationFromProto(src *durationpb.Duration) *time.Duration {
	if src == nil {
		return nil
	}
	d := src.AsDuration()
	return &d
}

// convertOptionalTimestamp converts an optional time pointer to protobuf format
func convertOptionalTimestamp(src *time.Time) *timestamppb.Timestamp {
	if src == nil {
		return nil
	}
	return timestamppb.New(*src)
}

// convertOptionalTimestampFromProto converts a protobuf timestamp to an optional pointer
func convertOptionalTimestampFromProto(src *timestamppb.Timestamp) *time.Time {
	if src == nil {
		return nil
	}
	t := src.AsTime()
	return &t
}

// DriverPlugin implements the go-plugin interface for gRPC
type DriverPlugin struct {
	plugin.Plugin
	Impl Driver
}

func (p *DriverPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterDriverServer(s, &DriverGRPCServer{
		Impl:    p.Impl,
		devices: make(map[string]Device),
	})
	return nil
}

func (p *DriverPlugin) GRPCClient(ctx context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &DriverGRPCClient{client: pb.NewDriverClient(c)}, nil
}

// DriverGRPCServer implements the gRPC server side (runs in plugin process)
type DriverGRPCServer struct {
	pb.UnimplementedDriverServer
	Impl    Driver
	devices map[string]Device
	mu      sync.RWMutex
}

func (s *DriverGRPCServer) Handshake(ctx context.Context, _ *emptypb.Empty) (*pb.HandshakeResponse, error) {
	handshake, err := s.Impl.Handshake(ctx)
	if err != nil {
		return nil, err
	}

	return &pb.HandshakeResponse{
		DriverName: handshake.DriverName,
		ApiVersion: handshake.APIVersion,
	}, nil
}

func (s *DriverGRPCServer) DescribeDriver(ctx context.Context, _ *emptypb.Empty) (*pb.DescribeDriverResponse, error) {
	handshake, caps, err := s.Impl.DescribeDriver(ctx)
	if err != nil {
		return nil, err
	}

	return &pb.DescribeDriverResponse{
		DriverName: handshake.DriverName,
		ApiVersion: handshake.APIVersion,
		Caps:       &pb.Capabilities{Flags: caps},
	}, nil
}

func (s *DriverGRPCServer) DiscoverDevice(ctx context.Context, req *pb.DiscoverDeviceRequest) (*pb.DiscoverDeviceResponse, error) {
	deviceInfo, err := s.Impl.DiscoverDevice(ctx, req.IpAddress, req.Port)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}

	return &pb.DiscoverDeviceResponse{
		Device: deviceInfoToProto(deviceInfo),
	}, nil
}

func (s *DriverGRPCServer) PairDevice(ctx context.Context, req *pb.PairDeviceRequest) (*pb.PairDeviceResponse, error) {
	deviceInfo := deviceInfoFromProto(req.Device)
	access := secretBundleFromProto(req.Access)

	updatedDeviceInfo, err := s.Impl.PairDevice(ctx, deviceInfo, access)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}

	return &pb.PairDeviceResponse{
		Device: deviceInfoToProto(updatedDeviceInfo),
	}, nil
}

func (s *DriverGRPCServer) GetDefaultCredentials(ctx context.Context, req *pb.GetDefaultCredentialsRequest) (*pb.GetDefaultCredentialsResponse, error) {
	// Check if the driver implements DefaultCredentialsProvider
	provider, ok := s.Impl.(DefaultCredentialsProvider)
	if !ok {
		// Return empty credentials if not implemented
		return &pb.GetDefaultCredentialsResponse{}, nil
	}

	creds := provider.GetDefaultCredentials(ctx, req.GetManufacturer(), req.GetFirmwareVersion())
	pbCreds := make([]*pb.UsernamePassword, len(creds))
	for i, c := range creds {
		pbCreds[i] = &pb.UsernamePassword{
			Username: c.Username,
			Password: c.Password,
		}
	}

	return &pb.GetDefaultCredentialsResponse{
		Credentials: pbCreds,
	}, nil
}

func (s *DriverGRPCServer) GetCapabilitiesForModel(ctx context.Context, req *pb.GetCapabilitiesForModelRequest) (*pb.GetCapabilitiesForModelResponse, error) {
	// Check if the driver implements ModelCapabilitiesProvider
	provider, ok := s.Impl.(ModelCapabilitiesProvider)
	if !ok {
		// Return empty capabilities if not implemented
		return &pb.GetCapabilitiesForModelResponse{}, nil
	}

	caps := provider.GetCapabilitiesForModel(ctx, req.Model)
	return &pb.GetCapabilitiesForModelResponse{
		Caps: &pb.Capabilities{Flags: caps},
	}, nil
}

func (s *DriverGRPCServer) GetDiscoveryPorts(ctx context.Context, _ *emptypb.Empty) (*pb.GetDiscoveryPortsResponse, error) {
	provider, ok := s.Impl.(DiscoveryPortsProvider)
	if !ok {
		return &pb.GetDiscoveryPortsResponse{}, nil
	}

	return &pb.GetDiscoveryPortsResponse{
		Ports: provider.GetDiscoveryPorts(ctx),
	}, nil
}

func (s *DriverGRPCServer) NewDevice(ctx context.Context, req *pb.NewDeviceRequest) (*pb.NewDeviceResponse, error) {
	// Convert the secret bundle from proto
	secret := secretBundleFromProto(req.Secret)

	// Convert DeviceInfo from proto
	deviceInfo := deviceInfoFromProto(req.Info)

	// Use the provided device ID from the request
	result, err := s.Impl.NewDevice(ctx, req.DeviceId, deviceInfo, secret)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}

	// Verify the device uses the provided ID
	deviceID := result.Device.ID()
	if deviceID != req.DeviceId {
		return nil, fmt.Errorf("device ID mismatch: expected %s, got %s", req.DeviceId, deviceID)
	}

	s.mu.Lock()
	s.devices[deviceID] = result.Device
	s.mu.Unlock()

	return &pb.NewDeviceResponse{
		DeviceId: deviceID,
	}, nil
}

func (s *DriverGRPCServer) DescribeDevice(ctx context.Context, req *pb.DescribeDeviceRequest) (*pb.DescribeDeviceResponse, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	deviceInfo, caps, err := device.DescribeDevice(ctx)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}

	return &pb.DescribeDeviceResponse{
		Device: deviceInfoToProto(deviceInfo),
		Caps:   &pb.Capabilities{Flags: caps},
	}, nil
}

func (s *DriverGRPCServer) DeviceStatus(ctx context.Context, req *pb.DeviceRef) (*pb.DeviceMetrics, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	metrics, err := device.Status(ctx)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}

	return deviceMetricsToProto(metrics), nil
}

func (s *DriverGRPCServer) GetErrors(ctx context.Context, req *pb.DeviceRef) (*pb.DeviceErrors, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	deviceErrors, err := device.GetErrors(ctx)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}

	pbErrors := make([]*pb.DeviceError, len(deviceErrors.Errors))
	for i, devErr := range deviceErrors.Errors {
		pbErrors[i] = devErr.ToProto()
	}

	return &pb.DeviceErrors{
		DeviceId: deviceErrors.DeviceID,
		Errors:   pbErrors,
	}, nil
}

func (s *DriverGRPCServer) CloseDevice(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.Lock()
	device, exists := s.devices[req.DeviceId]
	if exists {
		delete(s.devices, req.DeviceId)
	}
	s.mu.Unlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.Close(ctx)
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) StartMining(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.StartMining(ctx)
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) StopMining(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.StopMining(ctx)
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) SetCoolingMode(ctx context.Context, req *pb.SetCoolingModeRequest) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	err := device.SetCoolingMode(ctx, CoolingMode(req.Mode))
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) GetCoolingMode(ctx context.Context, req *pb.DeviceRef) (*pb.GetCoolingModeResponse, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	mode, err := device.GetCoolingMode(ctx)
	if err != nil {
		return nil, err
	}

	// #nosec G115 -- CoolingMode enum values are small constants (0-3), safe for int32
	return &pb.GetCoolingModeResponse{Mode: pb.CoolingMode(mode)}, nil
}

func (s *DriverGRPCServer) SetPowerTarget(ctx context.Context, req *pb.SetPowerTargetRequest) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	err := device.SetPowerTarget(ctx, PerformanceMode(req.PerformanceMode))
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) UpdateMiningPools(ctx context.Context, req *pb.UpdateMiningPoolsRequest) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	pools := make([]MiningPoolConfig, len(req.Pools))
	for i, pool := range req.Pools {
		pools[i] = MiningPoolConfig{
			Priority:   pool.Priority,
			URL:        pool.Url,
			WorkerName: pool.WorkerName,
		}
	}

	err := device.UpdateMiningPools(ctx, pools)
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) GetMiningPools(ctx context.Context, req *pb.GetMiningPoolsRequest) (*pb.GetMiningPoolsResponse, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	pools, err := device.GetMiningPools(ctx)
	if err != nil {
		return nil, err
	}

	pbPools := make([]*pb.ConfiguredPool, len(pools))
	for i, pool := range pools {
		pbPools[i] = &pb.ConfiguredPool{
			Priority: pool.Priority,
			Url:      pool.URL,
			Username: pool.Username,
		}
	}

	return &pb.GetMiningPoolsResponse{Pools: pbPools}, nil
}

func (s *DriverGRPCServer) BlinkLED(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.BlinkLED(ctx)
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) DownloadLogs(ctx context.Context, req *pb.DownloadLogsRequest) (*pb.DownloadLogsResponse, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	var since *time.Time
	if req.Since != nil {
		t := req.Since.AsTime()
		since = &t
	}

	logData, moreData, err := device.DownloadLogs(ctx, since, req.BatchLogUuid)
	if err != nil {
		return nil, err
	}

	return &pb.DownloadLogsResponse{
		LogData:  logData,
		MoreData: moreData,
	}, nil
}

func (s *DriverGRPCServer) Reboot(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.Reboot(ctx)
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) UpdateFirmware(ctx context.Context, req *pb.UpdateFirmwareRequest) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	fw := req.Firmware
	if fw == nil || fw.FilePath == "" {
		return nil, sdkErrorToGRPCStatus(NewErrorInvalidConfig(req.Ref.DeviceId, fmt.Errorf("firmware file info is required")))
	}

	file, err := os.Open(fw.FilePath)
	if err != nil {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceUnavailable(req.Ref.DeviceId, fmt.Errorf("failed to open firmware file: %w", err)))
	}
	defer file.Close()

	firmware := FirmwareFile{
		Reader:   file,
		Filename: fw.OriginalFilename,
		Size:     fw.FileSize,
		FilePath: fw.FilePath,
	}

	if err := device.FirmwareUpdate(ctx, firmware); err != nil {
		return nil, sdkErrorToGRPCStatus(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *DriverGRPCServer) GetFirmwareUpdateStatus(ctx context.Context, req *pb.DeviceRef) (*pb.GetFirmwareUpdateStatusResponse, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	provider, ok := device.(FirmwareUpdateStatusProvider)
	if !ok {
		return nil, status.Errorf(codes.Unimplemented, "device does not support firmware update status")
	}

	fwStatus, err := provider.GetFirmwareUpdateStatus(ctx)
	if err != nil {
		return nil, err
	}
	if fwStatus == nil {
		return &pb.GetFirmwareUpdateStatusResponse{}, nil
	}

	resp := &pb.GetFirmwareUpdateStatusResponse{
		State: fwStatus.State,
	}
	if fwStatus.Progress != nil {
		p := safeIntToInt32(*fwStatus.Progress)
		resp.Progress = &p
	}
	if fwStatus.Error != nil {
		resp.Error = fwStatus.Error
	}
	return resp, nil
}

func (s *DriverGRPCServer) Unpair(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.Unpair(ctx)
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) UpdateMinerPassword(ctx context.Context, req *pb.UpdateMinerPasswordRequest) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	err := device.UpdateMinerPassword(ctx, req.CurrentPassword, req.NewPassword)
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) Curtail(ctx context.Context, req *pb.CurtailRequest) (*emptypb.Empty, error) {
	if req.Ref == nil {
		return nil, grpcStatusError("missing device ref", codes.InvalidArgument, "missing device ref")
	}
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	curtailer, ok := device.(DeviceCurtailment)
	if !ok {
		return nil, grpcStatusError(errDeviceDoesNotSupportCurtailment, codes.Unimplemented, errDeviceDoesNotSupportCurtailment)
	}

	err := curtailer.Curtail(ctx, CurtailLevel(req.Level))
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) Uncurtail(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	curtailer, ok := device.(DeviceCurtailment)
	if !ok {
		return nil, grpcStatusError(errDeviceDoesNotSupportCurtailment, codes.Unimplemented, errDeviceDoesNotSupportCurtailment)
	}

	err := curtailer.Uncurtail(ctx)
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(err)
}

func (s *DriverGRPCServer) GetTimeSeriesData(ctx context.Context, req *pb.GetTimeSeriesDataRequest) (*pb.GetTimeSeriesDataResponse, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	var granularity *time.Duration
	if req.Granularity != nil {
		g := req.Granularity.AsDuration()
		granularity = &g
	}

	series, nextPageToken, supported, err := device.TryGetTimeSeriesData(
		ctx,
		req.MetricNames,
		req.StartTime.AsTime(),
		req.EndTime.AsTime(),
		granularity,
		req.MaxPoints,
		req.PageToken,
	)
	if !supported {
		return nil, sdkErrorToGRPCStatus(NewErrUnsupportedCapability("time_series_data"))
	}
	if err != nil {
		return nil, err
	}

	pbSeries := make([]*pb.DeviceMetrics, len(series))
	for i, s := range series {
		pbSeries[i] = deviceMetricsToProto(s)
	}

	return &pb.GetTimeSeriesDataResponse{
		Series:        pbSeries,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *DriverGRPCServer) GetDeviceWebViewURL(ctx context.Context, req *pb.GetDeviceWebViewURLRequest) (*pb.GetDeviceWebViewURLResponse, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	url, supported, err := device.TryGetWebViewURL(ctx)
	if !supported {
		return nil, sdkErrorToGRPCStatus(NewErrUnsupportedCapability("web_view_url"))
	}
	if err != nil {
		return nil, err
	}

	return &pb.GetDeviceWebViewURLResponse{
		Url: url,
	}, nil
}

func (s *DriverGRPCServer) BatchStatus(ctx context.Context, req *pb.BatchStatusRequest) (*pb.StatusBatchResponse, error) {
	// Try to find a device that supports batch status
	s.mu.RLock()
	var batchDevice Device
	for _, device := range s.devices {
		batchDevice = device
		break
	}
	s.mu.RUnlock()

	if batchDevice == nil {
		return nil, sdkErrorToGRPCStatus(NewErrUnsupportedCapability("batch_status"))
	}

	deviceIDs := make([]string, len(req.Refs))
	for i, ref := range req.Refs {
		deviceIDs[i] = ref.DeviceId
	}

	results, supported, err := batchDevice.TryBatchStatus(ctx, deviceIDs)
	if !supported {
		return nil, sdkErrorToGRPCStatus(NewErrUnsupportedCapability("batch_status"))
	}
	if err != nil {
		return nil, err
	}

	batch := &pb.StatusBatchResponse{
		Items: make([]*pb.DeviceMetrics, 0, len(results)),
	}

	for _, metrics := range results {
		batch.Items = append(batch.Items, deviceMetricsToProto(metrics))
	}

	return batch, nil
}

func (s *DriverGRPCServer) Subscribe(req *pb.SubscribeRequest, stream pb.Driver_SubscribeServer) error {
	// Try to find a device that supports streaming
	s.mu.RLock()
	var streamDevice Device
	for _, device := range s.devices {
		streamDevice = device
		break
	}
	s.mu.RUnlock()

	if streamDevice == nil {
		return sdkErrorToGRPCStatus(NewErrUnsupportedCapability("streaming"))
	}

	statusChan, supported, err := streamDevice.TrySubscribe(stream.Context(), req.DeviceIds)
	if !supported {
		return sdkErrorToGRPCStatus(NewErrUnsupportedCapability("streaming"))
	}
	if err != nil {
		return err
	}

	for {
		select {
		case metrics, ok := <-statusChan:
			if !ok {
				return nil // Stream closed
			}

			if err := stream.Send(deviceMetricsToProto(metrics)); err != nil {
				return err
			}

		case <-stream.Context().Done():
			return fmt.Errorf("stream context cancelled: %w", stream.Context().Err())
		}
	}
}

// DriverGRPCClient implements the gRPC client side (runs in host process)
type DriverGRPCClient struct {
	client pb.DriverClient
}

// Compile-time interface checks
var _ Driver = (*DriverGRPCClient)(nil)
var _ DefaultCredentialsProvider = (*DriverGRPCClient)(nil)
var _ ModelCapabilitiesProvider = (*DriverGRPCClient)(nil)
var _ DiscoveryPortsProvider = (*DriverGRPCClient)(nil)

func (c *DriverGRPCClient) Handshake(ctx context.Context) (DriverIdentifier, error) {
	resp, err := c.client.Handshake(ctx, &emptypb.Empty{})
	if err != nil {
		return DriverIdentifier{}, err
	}

	return DriverIdentifier{
		DriverName: resp.DriverName,
		APIVersion: resp.ApiVersion,
	}, nil
}

func (c *DriverGRPCClient) DescribeDriver(ctx context.Context) (DriverIdentifier, Capabilities, error) {
	resp, err := c.client.DescribeDriver(ctx, &emptypb.Empty{})
	if err != nil {
		return DriverIdentifier{}, nil, err
	}

	handshake := DriverIdentifier{
		DriverName: resp.DriverName,
		APIVersion: resp.ApiVersion,
	}

	var caps Capabilities
	if resp.Caps != nil {
		caps = resp.Caps.Flags
	}

	return handshake, caps, nil
}

func (c *DriverGRPCClient) DiscoverDevice(ctx context.Context, ipAddress, port string) (DeviceInfo, error) {
	resp, err := c.client.DiscoverDevice(ctx, &pb.DiscoverDeviceRequest{
		IpAddress: ipAddress,
		Port:      port,
	})
	if err != nil {
		return DeviceInfo{}, err
	}

	return deviceInfoFromProto(resp.Device), nil
}

func (c *DriverGRPCClient) PairDevice(ctx context.Context, device DeviceInfo, access SecretBundle) (DeviceInfo, error) {
	resp, err := c.client.PairDevice(ctx, &pb.PairDeviceRequest{
		Device: deviceInfoToProto(device),
		Access: secretBundleToProto(access),
	})
	if err != nil {
		return DeviceInfo{}, err
	}

	return deviceInfoFromProto(resp.Device), nil
}

// GetDefaultCredentials implements DefaultCredentialsProvider for the gRPC client.
// This allows the server to get default credentials from plugins over gRPC.
// Returns nil if the plugin doesn't implement the method (not an error condition).
func (c *DriverGRPCClient) GetDefaultCredentials(ctx context.Context, manufacturer, firmwareVersion string) []UsernamePassword {
	resp, err := c.client.GetDefaultCredentials(ctx, &pb.GetDefaultCredentialsRequest{
		Manufacturer:    manufacturer,
		FirmwareVersion: firmwareVersion,
	})
	if err != nil {
		// If the plugin doesn't implement this method, return nil (not an error)
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return nil
		}
		// For other errors, log a warning but return nil to maintain backwards compatibility.
		slog.Warn("Failed to get default credentials from plugin", "error", err)
		return nil
	}

	if resp == nil || len(resp.Credentials) == 0 {
		return nil
	}

	creds := make([]UsernamePassword, len(resp.Credentials))
	for i, c := range resp.Credentials {
		creds[i] = UsernamePassword{
			Username: c.Username,
			Password: c.Password,
		}
	}

	return creds
}

// GetCapabilitiesForModel implements ModelCapabilitiesProvider for the gRPC client.
// This allows the server to get model-specific capabilities from plugins over gRPC.
// Returns nil if the plugin doesn't implement the method (not an error condition).
func (c *DriverGRPCClient) GetCapabilitiesForModel(ctx context.Context, model string) Capabilities {
	resp, err := c.client.GetCapabilitiesForModel(ctx, &pb.GetCapabilitiesForModelRequest{
		Model: model,
	})
	if err != nil {
		// If the plugin doesn't implement this method, return nil (not an error)
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return nil
		}
		// For other errors, log a warning but return nil to maintain backwards compatibility.
		slog.Warn("Failed to get capabilities for model from plugin", "error", err, "model", model)
		return nil
	}

	if resp == nil || resp.Caps == nil {
		return nil
	}

	return resp.Caps.Flags
}

// GetDiscoveryPorts implements DiscoveryPortsProvider for the gRPC client.
// Returns nil if the plugin doesn't implement the method.
func (c *DriverGRPCClient) GetDiscoveryPorts(ctx context.Context) []string {
	resp, err := c.client.GetDiscoveryPorts(ctx, &emptypb.Empty{})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return nil
		}
		slog.Warn("Failed to get discovery ports from plugin", "error", err)
		return nil
	}

	if resp == nil || len(resp.Ports) == 0 {
		return nil
	}

	return resp.Ports
}

func (c *DriverGRPCClient) NewDevice(ctx context.Context, deviceID string, deviceInfo DeviceInfo, secret SecretBundle) (NewDeviceResult, error) {
	resp, err := c.client.NewDevice(ctx, &pb.NewDeviceRequest{
		DeviceId: deviceID,
		Info:     deviceInfoToProto(deviceInfo),
		Secret:   secretBundleToProto(secret),
	})
	if err != nil {
		return NewDeviceResult{}, err
	}

	device := &DeviceGRPCClient{
		client:   c.client,
		deviceID: resp.DeviceId,
	}

	return NewDeviceResult{
		Device: device,
	}, nil
}

// DeviceGRPCClient implements Device interface as a proxy to the plugin
type DeviceGRPCClient struct {
	client   pb.DriverClient
	deviceID string
}

var _ FirmwareUpdateStatusProvider = (*DeviceGRPCClient)(nil)

func (d *DeviceGRPCClient) ID() string {
	return d.deviceID
}

func (d *DeviceGRPCClient) DescribeDevice(ctx context.Context) (DeviceInfo, Capabilities, error) {
	resp, err := d.client.DescribeDevice(ctx, &pb.DescribeDeviceRequest{
		DeviceId: d.deviceID,
	})
	if err != nil {
		return DeviceInfo{}, nil, err
	}

	deviceInfo := DeviceInfo{}
	if resp.Device != nil {
		deviceInfo = deviceInfoFromProto(resp.Device)
	}

	caps := Capabilities{}
	if resp.Caps != nil {
		caps = resp.Caps.Flags
	}

	return deviceInfo, caps, nil
}

func (d *DeviceGRPCClient) Status(ctx context.Context) (DeviceMetrics, error) {
	resp, err := d.client.DeviceStatus(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	if err != nil {
		return DeviceMetrics{}, err
	}

	return deviceMetricsFromProto(resp), nil
}

func (d *DeviceGRPCClient) GetErrors(ctx context.Context) (DeviceErrors, error) {
	resp, err := d.client.GetErrors(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	if err != nil {
		return DeviceErrors{}, err
	}

	return sdkerrors.DeviceErrorsFromProto(resp), nil
}

func (d *DeviceGRPCClient) Close(ctx context.Context) error {
	_, err := d.client.CloseDevice(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	return err
}

func (d *DeviceGRPCClient) StartMining(ctx context.Context) error {
	_, err := d.client.StartMining(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	return err
}

func (d *DeviceGRPCClient) StopMining(ctx context.Context) error {
	_, err := d.client.StopMining(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	return err
}

func (d *DeviceGRPCClient) SetCoolingMode(ctx context.Context, mode CoolingMode) error {
	_, err := d.client.SetCoolingMode(ctx, &pb.SetCoolingModeRequest{
		Ref:  &pb.DeviceRef{DeviceId: d.deviceID},
		Mode: pb.CoolingMode(safeIntToInt32(int(mode))),
	})
	return err
}

func (d *DeviceGRPCClient) GetCoolingMode(ctx context.Context) (CoolingMode, error) {
	resp, err := d.client.GetCoolingMode(ctx, &pb.DeviceRef{DeviceId: d.deviceID})
	if err != nil {
		return CoolingModeUnspecified, err
	}
	return CoolingMode(resp.Mode), nil
}

func (d *DeviceGRPCClient) SetPowerTarget(ctx context.Context, performanceMode PerformanceMode) error {
	_, err := d.client.SetPowerTarget(ctx, &pb.SetPowerTargetRequest{
		Ref:             &pb.DeviceRef{DeviceId: d.deviceID},
		PerformanceMode: pb.PerformanceMode(safeIntToInt32(int(performanceMode))),
	})
	return err
}

func (d *DeviceGRPCClient) UpdateMiningPools(ctx context.Context, pools []MiningPoolConfig) error {
	pbPools := make([]*pb.MiningPool, len(pools))
	for i, pool := range pools {
		pbPools[i] = &pb.MiningPool{
			Priority:   pool.Priority,
			Url:        pool.URL,
			WorkerName: pool.WorkerName,
		}
	}

	_, err := d.client.UpdateMiningPools(ctx, &pb.UpdateMiningPoolsRequest{
		Ref:   &pb.DeviceRef{DeviceId: d.deviceID},
		Pools: pbPools,
	})
	return err
}

func (d *DeviceGRPCClient) GetMiningPools(ctx context.Context) ([]ConfiguredPool, error) {
	resp, err := d.client.GetMiningPools(ctx, &pb.GetMiningPoolsRequest{
		Ref: &pb.DeviceRef{DeviceId: d.deviceID},
	})
	if err != nil {
		return nil, err
	}

	pools := make([]ConfiguredPool, len(resp.Pools))
	for i, pool := range resp.Pools {
		pools[i] = ConfiguredPool{
			Priority: pool.Priority,
			URL:      pool.Url,
			Username: pool.Username,
		}
	}

	return pools, nil
}

func (d *DeviceGRPCClient) BlinkLED(ctx context.Context) error {
	_, err := d.client.BlinkLED(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	return err
}

func (d *DeviceGRPCClient) DownloadLogs(ctx context.Context, since *time.Time, batchLogUUID string) (string, bool, error) {
	req := &pb.DownloadLogsRequest{
		Ref:          &pb.DeviceRef{DeviceId: d.deviceID},
		BatchLogUuid: batchLogUUID,
	}

	if since != nil {
		req.Since = timestamppb.New(*since)
	}

	resp, err := d.client.DownloadLogs(ctx, req)
	if err != nil {
		return "", false, err
	}

	return resp.LogData, resp.MoreData, nil
}

func (d *DeviceGRPCClient) Reboot(ctx context.Context) error {
	_, err := d.client.Reboot(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	return err
}

func (d *DeviceGRPCClient) FirmwareUpdate(ctx context.Context, firmware FirmwareFile) error {
	_, err := d.client.UpdateFirmware(ctx, &pb.UpdateFirmwareRequest{
		Ref: &pb.DeviceRef{DeviceId: d.deviceID},
		Firmware: &pb.FirmwareFileInfo{
			FilePath:         firmware.FilePath,
			OriginalFilename: firmware.Filename,
			FileSize:         firmware.Size,
		},
	})
	return err
}

func (d *DeviceGRPCClient) GetFirmwareUpdateStatus(ctx context.Context) (*FirmwareUpdateStatus, error) {
	resp, err := d.client.GetFirmwareUpdateStatus(ctx, &pb.DeviceRef{DeviceId: d.deviceID})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			return nil, nil
		}
		return nil, err
	}
	if resp.State == "" {
		return nil, nil
	}
	result := &FirmwareUpdateStatus{
		State: resp.State,
	}
	if resp.Progress != nil {
		p := int(*resp.Progress)
		result.Progress = &p
	}
	if resp.Error != nil {
		result.Error = resp.Error
	}
	return result, nil
}

func (d *DeviceGRPCClient) Unpair(ctx context.Context) error {
	_, err := d.client.Unpair(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	return err
}

func (d *DeviceGRPCClient) UpdateMinerPassword(ctx context.Context, currentPassword string, newPassword string) error {
	_, err := d.client.UpdateMinerPassword(ctx, &pb.UpdateMinerPasswordRequest{
		Ref:             &pb.DeviceRef{DeviceId: d.deviceID},
		CurrentPassword: currentPassword,
		NewPassword:     newPassword,
	})
	return err
}

func (d *DeviceGRPCClient) Curtail(ctx context.Context, level CurtailLevel) error {
	_, err := d.client.Curtail(ctx, &pb.CurtailRequest{
		Ref:   &pb.DeviceRef{DeviceId: d.deviceID},
		Level: pb.CurtailLevel(level),
	})
	return err
}

func (d *DeviceGRPCClient) Uncurtail(ctx context.Context) error {
	_, err := d.client.Uncurtail(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	return err
}

func (d *DeviceGRPCClient) TryGetWebViewURL(ctx context.Context) (string, bool, error) {
	resp, err := d.client.GetDeviceWebViewURL(ctx, &pb.GetDeviceWebViewURLRequest{
		Ref: &pb.DeviceRef{DeviceId: d.deviceID},
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return "", false, nil
		}
		return "", false, err
	}

	return resp.Url, true, nil
}

func (d *DeviceGRPCClient) TryGetTimeSeriesData(ctx context.Context, metricNames []string, startTime, endTime time.Time, granularity *time.Duration, maxPoints int32, pageToken string) ([]DeviceMetrics, string, bool, error) {
	req := &pb.GetTimeSeriesDataRequest{
		Ref:         &pb.DeviceRef{DeviceId: d.deviceID},
		MetricNames: metricNames,
		StartTime:   timestamppb.New(startTime),
		EndTime:     timestamppb.New(endTime),
		MaxPoints:   maxPoints,
		PageToken:   pageToken,
	}

	if granularity != nil {
		req.Granularity = durationpb.New(*granularity)
	}

	resp, err := d.client.GetTimeSeriesData(ctx, req)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}

	series := make([]DeviceMetrics, len(resp.Series))
	for i, pbMetrics := range resp.Series {
		series[i] = deviceMetricsFromProto(pbMetrics)
	}

	return series, resp.NextPageToken, true, nil
}

func (d *DeviceGRPCClient) TryBatchStatus(ctx context.Context, ids []string) (map[string]DeviceMetrics, bool, error) {
	refs := make([]*pb.DeviceRef, len(ids))
	for i, id := range ids {
		refs[i] = &pb.DeviceRef{DeviceId: id}
	}

	resp, err := d.client.BatchStatus(ctx, &pb.BatchStatusRequest{Refs: refs})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, false, nil
		}
		return nil, false, err
	}

	results := make(map[string]DeviceMetrics)
	for _, item := range resp.Items {
		results[item.DeviceId] = deviceMetricsFromProto(item)
	}

	return results, true, nil
}

func (d *DeviceGRPCClient) TrySubscribe(ctx context.Context, ids []string) (<-chan DeviceMetrics, bool, error) {
	stream, err := d.client.Subscribe(ctx, &pb.SubscribeRequest{
		DeviceIds: ids,
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, false, nil
		}
		return nil, false, err
	}

	metricsChan := make(chan DeviceMetrics)

	go func() {
		defer close(metricsChan)

		for {
			metricsResp, err := stream.Recv()
			if err != nil {
				return
			}

			metrics := deviceMetricsFromProto(metricsResp)

			select {
			case metricsChan <- metrics:
			case <-ctx.Done():
				return
			}
		}
	}()

	return metricsChan, true, nil
}

// ============================================================================
// V2 Telemetry Model - Conversion Functions
// ============================================================================

// deviceMetricsToProto converts SDK DeviceMetrics to protobuf DeviceMetrics
func deviceMetricsToProto(dm DeviceMetrics) *pb.DeviceMetrics {
	pbMetrics := &pb.DeviceMetrics{
		DeviceId:        dm.DeviceID,
		Timestamp:       timestamppb.New(dm.Timestamp),
		Health:          pb.HealthStatus(safeIntToInt32(int(dm.Health))),
		FirmwareVersion: dm.FirmwareVersion,
	}

	if dm.HealthReason != nil {
		pbMetrics.HealthReason = dm.HealthReason
	}

	// Device-level aggregated metrics - using helper for optional metrics
	pbMetrics.HashrateHs = convertOptionalMetricToProto(dm.HashrateHS)
	pbMetrics.TempC = convertOptionalMetricToProto(dm.TempC)
	pbMetrics.FanRpm = convertOptionalMetricToProto(dm.FanRPM)
	pbMetrics.PowerW = convertOptionalMetricToProto(dm.PowerW)
	pbMetrics.EfficiencyJh = convertOptionalMetricToProto(dm.EfficiencyJH)

	// Component-level metrics - using generic slice converters
	pbMetrics.HashBoards = convertSliceToProto(dm.HashBoards, hashBoardMetricsToProto)
	pbMetrics.PsuMetrics = convertSliceToProto(dm.PSUMetrics, psuMetricsToProto)
	pbMetrics.ControlBoardMetrics = convertSliceToProto(dm.ControlBoardMetrics, controlBoardMetricsToProto)
	pbMetrics.FanMetrics = convertSliceToProto(dm.FanMetrics, fanMetricsToProto)
	pbMetrics.SensorMetrics = convertSliceToProto(dm.SensorMetrics, sensorMetricsToProto)

	return pbMetrics
}

// deviceMetricsFromProto converts protobuf DeviceMetrics to SDK DeviceMetrics
func deviceMetricsFromProto(pb *pb.DeviceMetrics) DeviceMetrics {
	dm := DeviceMetrics{
		DeviceID:        pb.DeviceId,
		Timestamp:       pb.Timestamp.AsTime(),
		Health:          HealthStatus(pb.Health),
		FirmwareVersion: pb.FirmwareVersion,
	}

	if pb.HealthReason != nil {
		dm.HealthReason = pb.HealthReason
	}

	// Device-level aggregated metrics - using helper for optional metrics
	dm.HashrateHS = convertOptionalMetricFromProto(pb.HashrateHs)
	dm.TempC = convertOptionalMetricFromProto(pb.TempC)
	dm.FanRPM = convertOptionalMetricFromProto(pb.FanRpm)
	dm.PowerW = convertOptionalMetricFromProto(pb.PowerW)
	dm.EfficiencyJH = convertOptionalMetricFromProto(pb.EfficiencyJh)

	// Component-level metrics - using generic slice converters
	dm.HashBoards = convertSliceFromProto(pb.HashBoards, hashBoardMetricsFromProto)
	dm.PSUMetrics = convertSliceFromProto(pb.PsuMetrics, psuMetricsFromProto)
	dm.ControlBoardMetrics = convertSliceFromProto(pb.ControlBoardMetrics, controlBoardMetricsFromProto)
	dm.FanMetrics = convertSliceFromProto(pb.FanMetrics, fanMetricsFromProto)
	dm.SensorMetrics = convertSliceFromProto(pb.SensorMetrics, sensorMetricsFromProto)

	return dm
}

// metricValueToProto converts SDK MetricValue to protobuf MetricValue
func metricValueToProto(mv MetricValue) *pb.MetricValue {
	pbMV := &pb.MetricValue{
		Value: mv.Value,
		Kind:  pb.MetricKind(safeIntToInt32(int(mv.Kind))),
	}

	if mv.MetaData != nil {
		pbMD := &pb.MetricValueMetaData{}

		// Using helper functions for optional time-based fields
		pbMD.Window = convertOptionalDuration(mv.MetaData.Window)
		pbMD.Min = mv.MetaData.Min
		pbMD.Max = mv.MetaData.Max
		pbMD.Avg = mv.MetaData.Avg
		pbMD.StdDev = mv.MetaData.StdDev
		pbMD.Timestamp = convertOptionalTimestamp(mv.MetaData.Timestamp)

		pbMV.Metadata = pbMD
	}

	return pbMV
}

// metricValueFromProto converts protobuf MetricValue to SDK MetricValue
func metricValueFromProto(pbMV *pb.MetricValue) MetricValue {
	mv := MetricValue{
		Value: pbMV.Value,
		Kind:  MetricKind(pbMV.Kind),
	}

	if pbMV.Metadata != nil {
		md := &MetricValueMetaData{}

		// Using helper functions for optional time-based fields
		md.Window = convertOptionalDurationFromProto(pbMV.Metadata.Window)
		md.Min = pbMV.Metadata.Min
		md.Max = pbMV.Metadata.Max
		md.Avg = pbMV.Metadata.Avg
		md.StdDev = pbMV.Metadata.StdDev
		md.Timestamp = convertOptionalTimestampFromProto(pbMV.Metadata.Timestamp)

		mv.MetaData = md
	}

	return mv
}

// componentInfoToProto converts SDK ComponentInfo to protobuf ComponentInfo
func componentInfoToProto(ci ComponentInfo) *pb.ComponentInfo {
	pbCI := &pb.ComponentInfo{
		Index:  ci.Index,
		Name:   ci.Name,
		Status: pb.ComponentStatus(safeIntToInt32(int(ci.Status))),
	}

	if ci.StatusReason != nil {
		pbCI.StatusReason = ci.StatusReason
	}
	if ci.Timestamp != nil {
		pbCI.Timestamp = timestamppb.New(*ci.Timestamp)
	}

	return pbCI
}

// componentInfoFromProto converts protobuf ComponentInfo to SDK ComponentInfo
func componentInfoFromProto(pbCI *pb.ComponentInfo) ComponentInfo {
	ci := ComponentInfo{
		Index:  pbCI.Index,
		Name:   pbCI.Name,
		Status: ComponentStatus(pbCI.Status),
	}

	if pbCI.StatusReason != nil {
		ci.StatusReason = pbCI.StatusReason
	}
	if pbCI.Timestamp != nil {
		timestamp := pbCI.Timestamp.AsTime()
		ci.Timestamp = &timestamp
	}

	return ci
}

// hashBoardMetricsToProto converts SDK HashBoardMetrics to protobuf HashBoardMetrics
func hashBoardMetricsToProto(hb HashBoardMetrics) *pb.HashBoardMetrics {
	pbHB := &pb.HashBoardMetrics{
		ComponentInfo: componentInfoToProto(hb.ComponentInfo),
	}

	if hb.SerialNumber != nil {
		pbHB.SerialNumber = hb.SerialNumber
	}

	// Using helper for optional metrics
	pbHB.HashRateHs = convertOptionalMetricToProto(hb.HashRateHS)
	pbHB.TempC = convertOptionalMetricToProto(hb.TempC)
	pbHB.VoltageV = convertOptionalMetricToProto(hb.VoltageV)
	pbHB.CurrentA = convertOptionalMetricToProto(hb.CurrentA)
	pbHB.InletTempC = convertOptionalMetricToProto(hb.InletTempC)
	pbHB.OutletTempC = convertOptionalMetricToProto(hb.OutletTempC)
	pbHB.AmbientTempC = convertOptionalMetricToProto(hb.AmbientTempC)
	pbHB.ChipFrequencyMhz = convertOptionalMetricToProto(hb.ChipFrequencyMHz)

	if hb.ChipCount != nil {
		pbHB.ChipCount = hb.ChipCount
	}

	// Using generic slice converters
	pbHB.Asics = convertSliceToProto(hb.ASICs, asicMetricsToProto)
	pbHB.FanMetrics = convertSliceToProto(hb.FanMetrics, fanMetricsToProto)

	return pbHB
}

// hashBoardMetricsFromProto converts protobuf HashBoardMetrics to SDK HashBoardMetrics
func hashBoardMetricsFromProto(pbHB *pb.HashBoardMetrics) HashBoardMetrics {
	hb := HashBoardMetrics{
		ComponentInfo: componentInfoFromProto(pbHB.ComponentInfo),
	}

	if pbHB.SerialNumber != nil {
		hb.SerialNumber = pbHB.SerialNumber
	}

	// Using helper for optional metrics
	hb.HashRateHS = convertOptionalMetricFromProto(pbHB.HashRateHs)
	hb.TempC = convertOptionalMetricFromProto(pbHB.TempC)
	hb.VoltageV = convertOptionalMetricFromProto(pbHB.VoltageV)
	hb.CurrentA = convertOptionalMetricFromProto(pbHB.CurrentA)
	hb.InletTempC = convertOptionalMetricFromProto(pbHB.InletTempC)
	hb.OutletTempC = convertOptionalMetricFromProto(pbHB.OutletTempC)
	hb.AmbientTempC = convertOptionalMetricFromProto(pbHB.AmbientTempC)
	hb.ChipFrequencyMHz = convertOptionalMetricFromProto(pbHB.ChipFrequencyMhz)

	if pbHB.ChipCount != nil {
		hb.ChipCount = pbHB.ChipCount
	}

	// Using generic slice converters
	hb.ASICs = convertSliceFromProto(pbHB.Asics, asicMetricsFromProto)
	hb.FanMetrics = convertSliceFromProto(pbHB.FanMetrics, fanMetricsFromProto)

	return hb
}

// asicMetricsToProto converts SDK ASICMetrics to protobuf ASICMetrics
func asicMetricsToProto(asic ASICMetrics) *pb.ASICMetrics {
	pbASIC := &pb.ASICMetrics{
		ComponentInfo: componentInfoToProto(asic.ComponentInfo),
	}

	// Using helper for optional metrics
	pbASIC.TempC = convertOptionalMetricToProto(asic.TempC)
	pbASIC.FrequencyMhz = convertOptionalMetricToProto(asic.FrequencyMHz)
	pbASIC.VoltageV = convertOptionalMetricToProto(asic.VoltageV)
	pbASIC.HashrateHs = convertOptionalMetricToProto(asic.HashrateHS)

	return pbASIC
}

// asicMetricsFromProto converts protobuf ASICMetrics to SDK ASICMetrics
func asicMetricsFromProto(pbASIC *pb.ASICMetrics) ASICMetrics {
	asic := ASICMetrics{
		ComponentInfo: componentInfoFromProto(pbASIC.ComponentInfo),
	}

	// Using helper for optional metrics
	asic.TempC = convertOptionalMetricFromProto(pbASIC.TempC)
	asic.FrequencyMHz = convertOptionalMetricFromProto(pbASIC.FrequencyMhz)
	asic.VoltageV = convertOptionalMetricFromProto(pbASIC.VoltageV)
	asic.HashrateHS = convertOptionalMetricFromProto(pbASIC.HashrateHs)

	return asic
}

// psuMetricsToProto converts SDK PSUMetrics to protobuf PSUMetrics
func psuMetricsToProto(psu PSUMetrics) *pb.PSUMetrics {
	pbPSU := &pb.PSUMetrics{
		ComponentInfo: componentInfoToProto(psu.ComponentInfo),
	}

	// Using helper for optional metrics
	pbPSU.OutputPowerW = convertOptionalMetricToProto(psu.OutputPowerW)
	pbPSU.OutputVoltageV = convertOptionalMetricToProto(psu.OutputVoltageV)
	pbPSU.OutputCurrentA = convertOptionalMetricToProto(psu.OutputCurrentA)
	pbPSU.InputPowerW = convertOptionalMetricToProto(psu.InputPowerW)
	pbPSU.InputVoltageV = convertOptionalMetricToProto(psu.InputVoltageV)
	pbPSU.InputCurrentA = convertOptionalMetricToProto(psu.InputCurrentA)
	pbPSU.HotspotTempC = convertOptionalMetricToProto(psu.HotSpotTempC)
	pbPSU.EfficiencyPercent = convertOptionalMetricToProto(psu.EfficiencyPercent)

	// Using generic slice converter
	pbPSU.FanMetrics = convertSliceToProto(psu.FanMetrics, fanMetricsToProto)

	return pbPSU
}

// psuMetricsFromProto converts protobuf PSUMetrics to SDK PSUMetrics
func psuMetricsFromProto(pbPSU *pb.PSUMetrics) PSUMetrics {
	psu := PSUMetrics{
		ComponentInfo: componentInfoFromProto(pbPSU.ComponentInfo),
	}

	// Using helper for optional metrics
	psu.OutputPowerW = convertOptionalMetricFromProto(pbPSU.OutputPowerW)
	psu.OutputVoltageV = convertOptionalMetricFromProto(pbPSU.OutputVoltageV)
	psu.OutputCurrentA = convertOptionalMetricFromProto(pbPSU.OutputCurrentA)
	psu.InputPowerW = convertOptionalMetricFromProto(pbPSU.InputPowerW)
	psu.InputVoltageV = convertOptionalMetricFromProto(pbPSU.InputVoltageV)
	psu.InputCurrentA = convertOptionalMetricFromProto(pbPSU.InputCurrentA)
	psu.HotSpotTempC = convertOptionalMetricFromProto(pbPSU.HotspotTempC)
	psu.EfficiencyPercent = convertOptionalMetricFromProto(pbPSU.EfficiencyPercent)

	// Using generic slice converter
	psu.FanMetrics = convertSliceFromProto(pbPSU.FanMetrics, fanMetricsFromProto)

	return psu
}

// fanMetricsToProto converts SDK FanMetrics to protobuf FanMetrics
func fanMetricsToProto(fan FanMetrics) *pb.FanMetrics {
	pbFan := &pb.FanMetrics{
		ComponentInfo: componentInfoToProto(fan.ComponentInfo),
	}

	// Using helper for optional metrics
	pbFan.Rpm = convertOptionalMetricToProto(fan.RPM)
	pbFan.TempC = convertOptionalMetricToProto(fan.TempC)
	pbFan.Percent = convertOptionalMetricToProto(fan.Percent)

	return pbFan
}

// fanMetricsFromProto converts protobuf FanMetrics to SDK FanMetrics
func fanMetricsFromProto(pbFan *pb.FanMetrics) FanMetrics {
	fan := FanMetrics{
		ComponentInfo: componentInfoFromProto(pbFan.ComponentInfo),
	}

	// Using helper for optional metrics
	fan.RPM = convertOptionalMetricFromProto(pbFan.Rpm)
	fan.TempC = convertOptionalMetricFromProto(pbFan.TempC)
	fan.Percent = convertOptionalMetricFromProto(pbFan.Percent)

	return fan
}

// controlBoardMetricsToProto converts SDK ControlBoardMetrics to protobuf ControlBoardMetrics
func controlBoardMetricsToProto(cb ControlBoardMetrics) *pb.ControlBoardMetrics {
	return &pb.ControlBoardMetrics{
		ComponentInfo: componentInfoToProto(cb.ComponentInfo),
	}
}

// controlBoardMetricsFromProto converts protobuf ControlBoardMetrics to SDK ControlBoardMetrics
func controlBoardMetricsFromProto(pbCB *pb.ControlBoardMetrics) ControlBoardMetrics {
	return ControlBoardMetrics{
		ComponentInfo: componentInfoFromProto(pbCB.ComponentInfo),
	}
}

// sensorMetricsToProto converts SDK SensorMetrics to protobuf SensorMetrics
func sensorMetricsToProto(sensor SensorMetrics) *pb.SensorMetrics {
	pbSensor := &pb.SensorMetrics{
		ComponentInfo: componentInfoToProto(sensor.ComponentInfo),
	}

	if sensor.Type != "" {
		pbSensor.Type = &sensor.Type
	}
	if sensor.Unit != "" {
		pbSensor.Unit = &sensor.Unit
	}
	if sensor.Value != nil {
		pbSensor.Value = metricValueToProto(*sensor.Value)
	}

	return pbSensor
}

// sensorMetricsFromProto converts protobuf SensorMetrics to SDK SensorMetrics
func sensorMetricsFromProto(pbSensor *pb.SensorMetrics) SensorMetrics {
	sensor := SensorMetrics{
		ComponentInfo: componentInfoFromProto(pbSensor.ComponentInfo),
	}

	if pbSensor.Type != nil {
		sensor.Type = *pbSensor.Type
	}
	if pbSensor.Unit != nil {
		sensor.Unit = *pbSensor.Unit
	}
	if pbSensor.Value != nil {
		mv := metricValueFromProto(pbSensor.Value)
		sensor.Value = &mv
	}

	return sensor
}

// ============================================================================
// Other Conversion Functions
// ============================================================================

// SecretBundle conversion functions
func secretBundleToProto(s SecretBundle) *pb.SecretBundle {
	pbSecret := &pb.SecretBundle{
		Version: s.Version,
	}

	if s.TTL != nil {
		pbSecret.Ttl = durationpb.New(*s.TTL)
	}

	switch kind := s.Kind.(type) {
	case APIKey:
		pbSecret.Kind = &pb.SecretBundle_ApiKey{
			ApiKey: &pb.APIKey{
				Key: kind.Key,
			},
		}
	case UsernamePassword:
		pbSecret.Kind = &pb.SecretBundle_UserPass{
			UserPass: &pb.UsernamePassword{
				Username: kind.Username,
				Password: kind.Password,
			},
		}
	case BearerToken:
		pbSecret.Kind = &pb.SecretBundle_BearerToken{
			BearerToken: &pb.BearerToken{
				Token: kind.Token,
			},
		}
	case TLSClientCert:
		pbSecret.Kind = &pb.SecretBundle_TlsClientCert{
			TlsClientCert: &pb.TlsClientCert{
				ClientCertPem: kind.ClientCertPEM,
				KeyPem:        kind.KeyPEM,
				CaCertPem:     kind.CACertPEM,
			},
		}
	}

	return pbSecret
}

func secretBundleFromProto(p *pb.SecretBundle) SecretBundle {
	secret := SecretBundle{
		Version: p.Version,
	}

	if p.Ttl != nil {
		ttl := p.Ttl.AsDuration()
		secret.TTL = &ttl
	}

	switch kind := p.Kind.(type) {
	case *pb.SecretBundle_ApiKey:
		secret.Kind = APIKey{
			Key: kind.ApiKey.Key,
		}
	case *pb.SecretBundle_UserPass:
		secret.Kind = UsernamePassword{
			Username: kind.UserPass.Username,
			Password: kind.UserPass.Password,
		}
	case *pb.SecretBundle_BearerToken:
		secret.Kind = BearerToken{
			Token: kind.BearerToken.Token,
		}
	case *pb.SecretBundle_TlsClientCert:
		secret.Kind = TLSClientCert{
			ClientCertPEM: kind.TlsClientCert.ClientCertPem,
			KeyPEM:        kind.TlsClientCert.KeyPem,
			CACertPEM:     kind.TlsClientCert.CaCertPem,
		}
	}

	return secret
}

// DeviceInfo conversion functions
func deviceInfoToProto(d DeviceInfo) *pb.DeviceInfo {
	return &pb.DeviceInfo{
		Host:            d.Host,
		Port:            d.Port,
		UrlScheme:       d.URLScheme,
		SerialNumber:    d.SerialNumber,
		Model:           d.Model,
		Manufacturer:    d.Manufacturer,
		MacAddress:      d.MacAddress,
		FirmwareVersion: d.FirmwareVersion,
	}
}

func deviceInfoFromProto(p *pb.DeviceInfo) DeviceInfo {
	return DeviceInfo{
		Host:            p.Host,
		Port:            p.Port,
		URLScheme:       p.UrlScheme,
		SerialNumber:    p.SerialNumber,
		Model:           p.Model,
		Manufacturer:    p.Manufacturer,
		MacAddress:      p.MacAddress,
		FirmwareVersion: p.FirmwareVersion,
	}
}

// HandshakeConfig contains the plugin handshake configuration
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "MINER_DRIVER_PLUGIN",
	MagicCookieValue: "fleet-miner-driver",
}

// PluginMap for go-plugin
var PluginMap = map[string]plugin.Plugin{
	"driver": &DriverPlugin{},
}
