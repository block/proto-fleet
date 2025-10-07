package sdk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Helper function to convert SDK errors to gRPC status errors
func sdkErrorToGRPCStatus(err error) error {
	var sdkErr SDKError
	if errors.As(err, &sdkErr) {
		switch sdkErr.Code {
		case ErrCodeDeviceNotFound:
			return fmt.Errorf("device not found: %w", status.Error(codes.NotFound, sdkErr.Message))
		case ErrCodeUnsupportedCapability:
			return fmt.Errorf("unsupported capability: %w", status.Error(codes.Unimplemented, sdkErr.Message))
		case ErrCodeInvalidConfig:
			return fmt.Errorf("invalid config: %w", status.Error(codes.InvalidArgument, sdkErr.Message))
		case ErrCodeDeviceUnavailable:
			return fmt.Errorf("device unavailable: %w", status.Error(codes.Unavailable, sdkErr.Message))
		case ErrCodeDriverShutdown:
			return fmt.Errorf("driver shutdown: %w", status.Error(codes.Aborted, sdkErr.Message))
		default:
			return fmt.Errorf("internal error: %w", status.Error(codes.Internal, sdkErr.Message))
		}
	}
	return err
}

// Helper function to safely convert int to int32
func safeIntToInt32(i int) int32 {
	if i > 2147483647 || i < -2147483648 {
		return 0 // Return 0 for out-of-range values
	}
	return int32(i)
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
		return nil, err
	}

	return &pb.DiscoverDeviceResponse{
		Device: deviceInfoToProto(deviceInfo),
	}, nil
}

func (s *DriverGRPCServer) PairDevice(ctx context.Context, req *pb.PairDeviceRequest) (*pb.PairDeviceResponse, error) {
	deviceInfo := deviceInfoFromProto(req.Device)
	access := secretBundleFromProto(req.Access)

	message, err := s.Impl.PairDevice(ctx, deviceInfo, access)
	if err != nil {
		return nil, err
	}

	return &pb.PairDeviceResponse{
		Message: message,
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
		return nil, err
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
		return nil, err
	}

	return &pb.DescribeDeviceResponse{
		Device: deviceInfoToProto(deviceInfo),
		Caps:   &pb.Capabilities{Flags: caps},
	}, nil
}

func (s *DriverGRPCServer) DeviceStatus(ctx context.Context, req *pb.DeviceRef) (*pb.DeviceStatusResponse, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	statusResp, err := device.Status(ctx)
	if err != nil {
		return nil, err
	}

	return statusResponseToProto(statusResp), nil
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
	return &emptypb.Empty{}, err
}

func (s *DriverGRPCServer) StartMining(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.StartMining(ctx)
	return &emptypb.Empty{}, err
}

func (s *DriverGRPCServer) StopMining(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.StopMining(ctx)
	return &emptypb.Empty{}, err
}

func (s *DriverGRPCServer) SetCoolingMode(ctx context.Context, req *pb.SetCoolingModeRequest) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}

	err := device.SetCoolingMode(ctx, CoolingMode(req.Mode))
	return &emptypb.Empty{}, err
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
	return &emptypb.Empty{}, err
}

func (s *DriverGRPCServer) BlinkLED(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.BlinkLED(ctx)
	return &emptypb.Empty{}, err
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
	return &emptypb.Empty{}, err
}

func (s *DriverGRPCServer) UpdateFirmware(ctx context.Context, req *pb.DeviceRef) (*emptypb.Empty, error) {
	s.mu.RLock()
	device, exists := s.devices[req.DeviceId]
	s.mu.RUnlock()

	if !exists {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.DeviceId))
	}

	err := device.FirmwareUpdate(ctx)
	return &emptypb.Empty{}, err
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

	pbSeries := make([]*pb.DeviceStatusResponse, len(series))
	for i, s := range series {
		pbSeries[i] = statusResponseToProto(s)
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
		Items: make([]*pb.DeviceStatusResponse, 0, len(results)),
	}

	for _, statusResp := range results {
		batch.Items = append(batch.Items, statusResponseToProto(statusResp))
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
		case status, ok := <-statusChan:
			if !ok {
				return nil // Stream closed
			}

			if err := stream.Send(statusResponseToProto(status)); err != nil {
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

func (c *DriverGRPCClient) PairDevice(ctx context.Context, device DeviceInfo, access SecretBundle) (string, error) {
	resp, err := c.client.PairDevice(ctx, &pb.PairDeviceRequest{
		Device: deviceInfoToProto(device),
		Access: secretBundleToProto(access),
	})
	if err != nil {
		return "", err
	}

	return resp.Message, nil
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

func (d *DeviceGRPCClient) Status(ctx context.Context) (DeviceStatusResponse, error) {
	resp, err := d.client.DeviceStatus(ctx, &pb.DeviceRef{
		DeviceId: d.deviceID,
	})
	if err != nil {
		return DeviceStatusResponse{}, err
	}

	return statusResponseFromProto(resp), nil
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

func (d *DeviceGRPCClient) FirmwareUpdate(ctx context.Context) error {
	_, err := d.client.UpdateFirmware(ctx, &pb.DeviceRef{
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

func (d *DeviceGRPCClient) TryGetTimeSeriesData(ctx context.Context, metricNames []string, startTime, endTime time.Time, granularity *time.Duration, maxPoints int32, pageToken string) ([]DeviceStatusResponse, string, bool, error) {
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

	series := make([]DeviceStatusResponse, len(resp.Series))
	for i, pbStatus := range resp.Series {
		series[i] = statusResponseFromProto(pbStatus)
	}

	return series, resp.NextPageToken, true, nil
}

func (d *DeviceGRPCClient) TryBatchStatus(ctx context.Context, ids []string) (map[string]DeviceStatusResponse, bool, error) {
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

	results := make(map[string]DeviceStatusResponse)
	for _, item := range resp.Items {
		results[item.DeviceId] = statusResponseFromProto(item)
	}

	return results, true, nil
}

func (d *DeviceGRPCClient) TrySubscribe(ctx context.Context, ids []string) (<-chan DeviceStatusResponse, bool, error) {
	stream, err := d.client.Subscribe(ctx, &pb.SubscribeRequest{
		DeviceIds: ids,
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, false, nil
		}
		return nil, false, err
	}

	statusChan := make(chan DeviceStatusResponse)

	go func() {
		defer close(statusChan)

		for {
			statusResp, err := stream.Recv()
			if err != nil {
				return
			}

			status := statusResponseFromProto(statusResp)

			select {
			case statusChan <- status:
			case <-ctx.Done():
				return
			}
		}
	}()

	return statusChan, true, nil
}

// Helper functions for proto conversion
func statusResponseToProto(s DeviceStatusResponse) *pb.DeviceStatusResponse {
	timestamp := timestamppb.New(s.Timestamp)
	resp := &pb.DeviceStatusResponse{
		DeviceId:  s.DeviceID,
		Timestamp: timestamp,
		Summary:   s.Summary,
		Health:    pb.HealthStatus(safeIntToInt32(int(s.Health))),
		Metadata:  s.Metadata,
	}

	if s.HashrateHS != nil {
		resp.HashrateHs = &(*s.HashrateHS)
	}
	if s.PowerWatts != nil {
		resp.PowerWatts = &(*s.PowerWatts)
	}
	if s.TemperatureCelsius != nil {
		resp.TemperatureCelsius = &(*s.TemperatureCelsius)
	}
	if s.EfficiencyJPerHash != nil {
		resp.EfficiencyJPerHash = &(*s.EfficiencyJPerHash)
	}
	if s.FanRPM != nil {
		resp.FanRpm = s.FanRPM
	}

	// Convert sample semantics
	if s.Sample != nil {
		resp.Sample = &pb.SampleSemantics{
			Aggregation:     pb.Aggregation(safeIntToInt32(int(s.Sample.Aggregation))),
			AveragingWindow: durationpb.New(s.Sample.AveragingWindow),
			StartOfWindow:   timestamppb.New(s.Sample.StartOfWindow),
		}
	}

	// Convert metric details
	if s.MetricDetails != nil {
		resp.MetricDetails = make(map[string]*pb.MetricDetail)
		for key, detail := range s.MetricDetails {
			pbDetail := &pb.MetricDetail{
				Aggregation:     pb.Aggregation(safeIntToInt32(int(detail.Aggregation))),
				AveragingWindow: durationpb.New(detail.AveragingWindow),
			}
			if detail.Min != nil {
				pbDetail.Min = detail.Min
			}
			if detail.Max != nil {
				pbDetail.Max = detail.Max
			}
			if detail.StdDev != nil {
				pbDetail.Stddev = detail.StdDev
			}
			if detail.SensorID != nil {
				pbDetail.SensorId = detail.SensorID
			}
			resp.MetricDetails[key] = pbDetail
		}
	}

	// Convert extra metrics
	if s.ExtraMetrics != nil {
		resp.ExtraMetrics = make([]*pb.Metric, len(s.ExtraMetrics))
		for i, metric := range s.ExtraMetrics {
			pbMetric := &pb.Metric{
				Name:       metric.Name,
				Unit:       pb.Unit(safeIntToInt32(int(metric.Unit))),
				Kind:       pb.MetricKind(safeIntToInt32(int(metric.Kind))),
				ObservedAt: timestamppb.New(metric.ObservedAt),
				Window:     durationpb.New(metric.Window),
				Labels:     metric.Labels,
			}

			// Handle different value types using MetricValue interface
			if metric.Value != nil {
				switch metric.Value.Type() {
				case ValueTypeFloat64:
					if val, ok := metric.Value.AsFloat64(); ok {
						pbMetric.Value = &pb.Metric_DoubleValue{DoubleValue: val}
					}
				case ValueTypeInt:
					if val, ok := metric.Value.AsInt(); ok {
						pbMetric.Value = &pb.Metric_IntValue{IntValue: int64(val)}
					}
				case ValueTypeBool:
					if val, ok := metric.Value.AsBool(); ok {
						pbMetric.Value = &pb.Metric_BoolValue{BoolValue: val}
					}
				case ValueTypeString:
					if val, ok := metric.Value.AsString(); ok {
						pbMetric.Value = &pb.Metric_StringValue{StringValue: val}
					}
				}
			}

			resp.ExtraMetrics[i] = pbMetric
		}
	}

	return resp
}

func statusResponseFromProto(p *pb.DeviceStatusResponse) DeviceStatusResponse {
	resp := DeviceStatusResponse{
		DeviceID:  p.DeviceId,
		Timestamp: p.Timestamp.AsTime(),
		Summary:   p.Summary,
		Health:    HealthStatus(p.Health),
		Metadata:  p.Metadata,
	}

	if p.HashrateHs != nil {
		resp.HashrateHS = p.HashrateHs
	}
	if p.PowerWatts != nil {
		resp.PowerWatts = p.PowerWatts
	}
	if p.TemperatureCelsius != nil {
		resp.TemperatureCelsius = p.TemperatureCelsius
	}
	if p.EfficiencyJPerHash != nil {
		resp.EfficiencyJPerHash = p.EfficiencyJPerHash
	}
	if p.FanRpm != nil {
		resp.FanRPM = p.FanRpm
	}

	// Convert sample semantics
	if p.Sample != nil {
		resp.Sample = &SampleSemantics{
			Aggregation:     Aggregation(p.Sample.Aggregation),
			AveragingWindow: p.Sample.AveragingWindow.AsDuration(),
			StartOfWindow:   p.Sample.StartOfWindow.AsTime(),
		}
	}

	// Convert metric details
	if p.MetricDetails != nil {
		resp.MetricDetails = make(map[string]MetricDetail)
		for key, pbDetail := range p.MetricDetails {
			detail := MetricDetail{
				Aggregation:     Aggregation(pbDetail.Aggregation),
				AveragingWindow: pbDetail.AveragingWindow.AsDuration(),
			}
			if pbDetail.Min != nil {
				detail.Min = pbDetail.Min
			}
			if pbDetail.Max != nil {
				detail.Max = pbDetail.Max
			}
			if pbDetail.Stddev != nil {
				detail.StdDev = pbDetail.Stddev
			}
			if pbDetail.SensorId != nil {
				detail.SensorID = pbDetail.SensorId
			}
			resp.MetricDetails[key] = detail
		}
	}

	// Convert extra metrics
	if p.ExtraMetrics != nil {
		resp.ExtraMetrics = make([]Metric, len(p.ExtraMetrics))
		for i, pbMetric := range p.ExtraMetrics {
			metric := Metric{
				Name:       pbMetric.Name,
				Unit:       Unit(pbMetric.Unit),
				Kind:       MetricKind(pbMetric.Kind),
				ObservedAt: pbMetric.ObservedAt.AsTime(),
				Window:     pbMetric.Window.AsDuration(),
				Labels:     pbMetric.Labels,
			}

			// Handle different value types and convert to MetricValue interface
			switch v := pbMetric.Value.(type) {
			case *pb.Metric_DoubleValue:
				metric.Value = NewMetricValue(v.DoubleValue)
			case *pb.Metric_IntValue:
				metric.Value = NewMetricValue(int(v.IntValue))
			case *pb.Metric_BoolValue:
				metric.Value = NewMetricValue(v.BoolValue)
			case *pb.Metric_StringValue:
				metric.Value = NewMetricValue(v.StringValue)
			}

			resp.ExtraMetrics[i] = metric
		}
	}

	return resp
}

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
	// Convert DeviceType enum to protobuf enum
	var deviceType pb.DeviceType
	switch d.Type {
	case DeviceTypeASIC:
		deviceType = pb.DeviceType_DEVICE_TYPE_ASIC
	case DeviceTypeGPU:
		deviceType = pb.DeviceType_DEVICE_TYPE_GPU
	case DeviceTypeFPGA:
		deviceType = pb.DeviceType_DEVICE_TYPE_FPGA
	case DeviceTypeUnspecified:
		deviceType = pb.DeviceType_DEVICE_TYPE_UNSPECIFIED
	default:
		deviceType = pb.DeviceType_DEVICE_TYPE_UNSPECIFIED
	}

	return &pb.DeviceInfo{
		Host:         d.Host,
		Port:         d.Port,
		UrlScheme:    d.URLScheme,
		SerialNumber: d.SerialNumber,
		Model:        d.Model,
		Manufacturer: d.Manufacturer,
		Type:         deviceType,
		MacAddress:   d.MacAddress,
	}
}

func deviceInfoFromProto(p *pb.DeviceInfo) DeviceInfo {
	// Convert protobuf DeviceType enum to Go enum
	var deviceType DeviceType
	switch p.Type {
	case pb.DeviceType_DEVICE_TYPE_UNSPECIFIED:
		deviceType = DeviceTypeUnspecified
	case pb.DeviceType_DEVICE_TYPE_ASIC:
		deviceType = DeviceTypeASIC
	case pb.DeviceType_DEVICE_TYPE_GPU:
		deviceType = DeviceTypeGPU
	case pb.DeviceType_DEVICE_TYPE_FPGA:
		deviceType = DeviceTypeFPGA
	default:
		deviceType = DeviceTypeUnspecified
	}

	return DeviceInfo{
		Host:         p.Host,
		Port:         p.Port,
		URLScheme:    p.UrlScheme,
		SerialNumber: p.SerialNumber,
		Model:        p.Model,
		Manufacturer: p.Manufacturer,
		Type:         deviceType,
		MacAddress:   p.MacAddress,
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
