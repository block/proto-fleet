package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type grpcAPI struct {
	mdk.UnimplementedSystemServiceServer
	mdk.UnimplementedHashboardServiceServer
	mdk.UnimplementedMiningServiceServer
	mdk.UnimplementedMiningGroupServiceServer
	mdk.UnimplementedTelemetryServiceServer
	mdk.UnimplementedPowerServiceServer
	mdk.UnimplementedPoolServiceServer
	mdk.UnimplementedRunInServiceServer
	mdk.UnimplementedFirmwareServiceServer
	mdk.UnimplementedFeedMappingServiceServer
	mdk.UnimplementedPSUFirmwareServiceServer
	state *ContainerState
}

func newGRPCServer(state *ContainerState, options ...grpc.ServerOption) *grpc.Server {
	state.initHistory()
	api := &grpcAPI{state: state}
	unary := serveUnary(state)
	serverOptions := []grpc.ServerOption{grpc.WaitForHandlers(true)}
	if state.ha != nil {
		unary = pairUnary(state, state.ha.sim)
		serverOptions = append(serverOptions, grpc.StreamInterceptor(pairStream(state.ha.sim)))
	}
	serverOptions = append(serverOptions, grpc.UnaryInterceptor(unary))
	server := grpc.NewServer(append(serverOptions, options...)...)
	mdk.RegisterSystemServiceServer(server, api)
	mdk.RegisterHashboardServiceServer(server, api)
	mdk.RegisterMiningServiceServer(server, api)
	mdk.RegisterMiningGroupServiceServer(server, api)
	mdk.RegisterTelemetryServiceServer(server, api)
	mdk.RegisterPowerServiceServer(server, api)
	mdk.RegisterPoolServiceServer(server, api)
	mdk.RegisterRunInServiceServer(server, api)
	mdk.RegisterFirmwareServiceServer(server, api)
	mdk.RegisterFeedMappingServiceServer(server, api)
	mdk.RegisterPSUFirmwareServiceServer(server, api)
	if state.ha != nil {
		mdk.RegisterServerUpdateServiceServer(server, &serverUpdateAPI{sim: state.ha.sim, self: state.ha.self})
	}
	return server
}

func stopGRPCServer(server *grpc.Server) {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	timer := time.NewTimer(serverShutdownTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		server.Stop()
		<-done
	}
}

func projectProto(data any, out proto.Message) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return status.Errorf(codes.Internal, "encode simulation: %v", err)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(encoded, out); err != nil {
		return status.Errorf(codes.Internal, "project simulation: %v", err)
	}
	return nil
}

// project converts a shared operation's REST DTO into its native response.
func project[T any, M proto.Message](dto T, err error, out M) (M, error) {
	if err == nil {
		err = projectProto(dto, out)
	}
	if err != nil {
		var none M
		return none, err
	}
	return out, nil
}

// The REST simulator retains some older diagnostic fields (e.g. breakerId)
// that aren't in the pinned native contract; protojson drops only those fields.
func (a *grpcAPI) snapshot(build func(*ContainerState) any, out proto.Message) error {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	return projectProto(build(a.state), out)
}
func (a *grpcAPI) GetSystemInfo(context.Context, *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	out := new(mdk.GetSystemInfoResponse)
	err := a.snapshot(func(s *ContainerState) any { return s.systemInfoDTO() }, out)
	return out, err
}

func (a *grpcAPI) GetSystemStatus(context.Context, *mdk.GetSystemStatusRequest) (*mdk.GetSystemStatusResponse, error) {
	out := new(mdk.GetSystemStatusResponse)
	err := a.snapshot(func(s *ContainerState) any { return s.systemStatusDTO() }, out)
	return out, err
}

func (a *grpcAPI) GetContainerMap(context.Context, *mdk.GetContainerMapRequest) (*mdk.GetContainerMapResponse, error) {
	out := new(mdk.GetContainerMapResponse)
	err := a.snapshot(func(s *ContainerState) any {
		// Boards stop and resume mining, and PSUs change version, as module updates run.
		s.advancePSUFirmware()
		return s.containerMapDTO()
	}, out)
	return out, err
}
func (a *grpcAPI) GetMiningStatus(context.Context, *mdk.GetMiningStatusRequest) (*mdk.GetMiningStatusResponse, error) {
	out := new(mdk.GetMiningStatusResponse)
	err := a.snapshot(func(s *ContainerState) any { return s.miningStatusDTO() }, out)
	if err == nil {
		measurements := out.CurrentMeasurements
		measurements.Source = mdk.MiningMeasurementSource_MINING_MEASUREMENT_SOURCE_PERSISTED_BOARD_HEALTH
		measurements.Scope = mdk.MiningMeasurementScope_MINING_MEASUREMENT_SCOPE_SHARED_ASSIGNMENTS
		measurements.HashboardPowerW = &mdk.MiningCurrentMeasurement{Value: &out.Power, Complete: true, ExpectedBoards: uint32(out.TotalBoards), ReportingBoards: uint32(out.TotalBoards)}
	}
	return out, err
}
func (a *grpcAPI) GetContainerHealth(context.Context, *mdk.GetContainerHealthRequest) (*mdk.GetContainerHealthResponse, error) {
	out := new(mdk.GetContainerHealthResponse)
	err := a.snapshot(func(s *ContainerState) any { return s.containerTelemetry() }, out)
	return out, err
}
func (a *grpcAPI) GetContainerControls(context.Context, *mdk.GetContainerControlsRequest) (*mdk.GetContainerControlsResponse, error) {
	out := new(mdk.GetContainerControlsResponse)
	err := a.snapshot(func(s *ContainerState) any {
		v := s.containerTelemetry()
		fanReverse, fanReverseSupported := s.fanReverseControls()
		return containerControlsResponse{
			DryCoolerFanReverse: fanReverse, DryCoolerFanReverseSupported: fanReverseSupported,
			Supported: true, Controls: v.Controls, Setpoints: v.Setpoints, MainCircuitBreakerSupported: false,
			AlarmMuteSupported: true, AlarmResetSupported: !s.Config.AlarmResetUnsupported,
		}
	}, out)
	return out, err
}

func (a *grpcAPI) SetContainerSetpoint(ctx context.Context, in *mdk.SetContainerSetpointRequest) (*mdk.SetContainerSetpointResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	if err := a.state.setContainerSetpoint(in.GetName(), in.GetValue()); err != nil {
		return nil, err
	}
	point := a.state.containerSetpoint(in.GetName())
	return &mdk.SetContainerSetpointResponse{Setpoint: &mdk.ContainerSetpoint{Name: point.Name, Value: point.Value, Unit: point.Unit}}, nil
}

func (a *grpcAPI) SetContainerControl(ctx context.Context, in *mdk.SetContainerControlRequest) (*mdk.SetContainerControlResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	if _, ok := a.state.ContainerControls[in.Name]; !ok {
		return nil, status.Error(codes.NotFound, "unknown container control")
	}
	now := a.state.now()
	a.state.ContainerControls[in.Name] = in.Enabled
	return &mdk.SetContainerControlResponse{Control: &mdk.ContainerControl{Name: in.Name, Enabled: in.Enabled, StateKnown: true}, Confirmation: &mdk.ContainerControlConfirmation{RequestedEnabled: in.Enabled, ObservedEnabled: new(a.state.ContainerControls[in.Name]), ObservedAt: timestamppb.New(now), State: mdk.PlantWriteConfirmationState_PLANT_WRITE_CONFIRMATION_STATE_CONFIRMED, Source: mdk.PlantWriteConfirmationSource_PLANT_WRITE_CONFIRMATION_SOURCE_CONTROLLER_REGISTER}}, nil
}
func (a *grpcAPI) MuteContainerAlarm(ctx context.Context, _ *mdk.MuteContainerAlarmRequest) (*mdk.MuteContainerAlarmResponse, error) {
	if err := a.containerAlarm(ctx, (*ContainerState).muteContainerAlarm); err != nil {
		return nil, err
	}
	return &mdk.MuteContainerAlarmResponse{}, nil
}

func (a *grpcAPI) ResetContainerAlarm(ctx context.Context, _ *mdk.ResetContainerAlarmRequest) (*mdk.ResetContainerAlarmResponse, error) {
	if err := a.containerAlarm(ctx, (*ContainerState).resetContainerAlarm); err != nil {
		return nil, err
	}
	return &mdk.ResetContainerAlarmResponse{}, nil
}

func (a *grpcAPI) containerAlarm(ctx context.Context, command func(*ContainerState) error) error {
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	return command(a.state)
}

func (a *grpcAPI) SetMainCircuitBreakerState(context.Context, *mdk.SetMainCircuitBreakerStateRequest) (*mdk.SetMainCircuitBreakerStateResponse, error) {
	return nil, status.Error(codes.Unimplemented, fmt.Sprintf("%s simulator has no dedicated breaker readback", product))
}

func (a *grpcAPI) GetPoolDiagnostics(context.Context, *mdk.GetPoolDiagnosticsRequest) (*mdk.GetPoolDiagnosticsResponse, error) {
	a.state.mu.RLock()
	dto := a.state.poolDiagnosticsDTO()
	a.state.mu.RUnlock()
	return project(dto, nil, new(mdk.GetPoolDiagnosticsResponse))
}

func (a *grpcAPI) GetTargetPower(context.Context, *mdk.GetTargetPowerRequest) (*mdk.GetTargetPowerResponse, error) {
	a.state.mu.Lock()
	dto, err := a.state.targetPower(nil)
	a.state.mu.Unlock()
	return project(dto, err, new(mdk.GetTargetPowerResponse))
}

func (a *grpcAPI) SetTargetPower(_ context.Context, in *mdk.SetTargetPowerRequest) (*mdk.SetTargetPowerResponse, error) {
	a.state.mu.Lock()
	dto, err := a.state.targetPower(&targetPowerUpdate{
		watts:            in.GetWatts(),
		performanceMode:  in.GetPerformanceMode().String(),
		balanceBays:      in.BalanceBays,
		hashOnDisconnect: in.HashOnDisconnect,
	})
	a.state.mu.Unlock()
	return project(dto, err, new(mdk.SetTargetPowerResponse))
}

func (a *grpcAPI) StartMining(ctx context.Context, _ *mdk.StartMiningRequest) (*mdk.StartMiningResponse, error) {
	if err := a.setGlobalMining(ctx, true); err != nil {
		return nil, err
	}
	return &mdk.StartMiningResponse{}, nil
}
func (a *grpcAPI) StopMining(ctx context.Context, _ *mdk.StopMiningRequest) (*mdk.StopMiningResponse, error) {
	if err := a.setGlobalMining(ctx, false); err != nil {
		return nil, err
	}
	return &mdk.StopMiningResponse{}, nil
}
func (a *grpcAPI) setGlobalMining(ctx context.Context, mining bool) error {
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	return a.state.setGlobalMining(mining)
}

func (a *grpcAPI) StartMiningScoped(ctx context.Context, in *mdk.StartMiningScopedRequest) (*mdk.StartMiningScopedResponse, error) {
	results, err := a.applyToScope(ctx, in.GetScope(), startBoard)
	if err != nil {
		return nil, err
	}
	return &mdk.StartMiningScopedResponse{BoardsStarted: appliedBoardCount(results), Results: results}, nil
}
func (a *grpcAPI) StopMiningScoped(ctx context.Context, in *mdk.StopMiningScopedRequest) (*mdk.StopMiningScopedResponse, error) {
	results, err := a.applyToScope(ctx, in.GetScope(), stopBoard)
	if err != nil {
		return nil, err
	}
	return &mdk.StopMiningScopedResponse{BoardsStopped: appliedBoardCount(results), Results: results}, nil
}

func (a *grpcAPI) SetPowerTargetScoped(ctx context.Context, in *mdk.SetPowerTargetScopedRequest) (*mdk.SetPowerTargetScopedResponse, error) {
	if err := finiteWatts(in.GetWatts()); err != nil {
		return nil, err
	}
	results, err := a.applyToScope(ctx, in.GetScope(), scopedPowerTarget(in.GetWatts()))
	if err != nil {
		return nil, err
	}
	return &mdk.SetPowerTargetScopedResponse{BoardsUpdated: appliedBoardCount(results), Results: results}, nil
}

func (a *grpcAPI) ClearPowerTargetScoped(ctx context.Context, in *mdk.ClearPowerTargetScopedRequest) (*mdk.ClearPowerTargetScopedResponse, error) {
	results, err := a.applyToScope(ctx, in.GetScope(), clearScopedPowerTarget)
	if err != nil {
		return nil, err
	}
	return &mdk.ClearPowerTargetScopedResponse{BoardsUpdated: appliedBoardCount(results), Results: results}, nil
}

func (a *grpcAPI) SetIsolation(ctx context.Context, in *mdk.SetIsolationRequest) (*mdk.SetIsolationResponse, error) {
	results, err := a.applyToScope(ctx, in.GetScope(), func(b *Hashboard) (bool, string) {
		b.Isolated = in.GetIsolated()
		return true, ""
	})
	if err != nil {
		return nil, err
	}
	return &mdk.SetIsolationResponse{BoardsChanged: appliedBoardCount(results), Results: results}, nil
}

// identifyBlinkDuration matches Frigate's blink window, which Fleet tells
// operators is about 15 minutes.
const identifyBlinkDuration = 15 * time.Minute

func (a *grpcAPI) SetIdentifyLED(ctx context.Context, in *mdk.SetIdentifyLEDRequest) (*mdk.SetIdentifyLEDResponse, error) {
	mode := in.GetMode()
	if mode != mdk.HashboardIdentifyLEDMode_HASHBOARD_IDENTIFY_LED_MODE_BLINK && mode != mdk.HashboardIdentifyLEDMode_HASHBOARD_IDENTIFY_LED_MODE_OFF {
		return nil, status.Error(codes.InvalidArgument, "identify LED mode must be BLINK or OFF")
	}
	var expiresAt time.Time
	if mode == mdk.HashboardIdentifyLEDMode_HASHBOARD_IDENTIFY_LED_MODE_BLINK {
		expiresAt = time.Now().UTC().Add(identifyBlinkDuration)
	}
	results, err := a.applyToScope(ctx, in.GetScope(), func(b *Hashboard) (bool, string) {
		b.IdentifyLED = mode
		b.IdentifyExpiresAt = expiresAt
		return true, ""
	})
	if err != nil {
		return nil, err
	}
	out := &mdk.SetIdentifyLEDResponse{BoardsUpdated: appliedBoardCount(results)}
	if !expiresAt.IsZero() {
		out.ExpiresAt = timestamppb.New(expiresAt)
	}
	return out, nil
}

func (a *grpcAPI) applyToScope(ctx context.Context, in *mdk.BoardScope, apply func(*Hashboard) (bool, string)) ([]*mdk.BoardResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	sc := scope{Identifiers: in.GetIdentifiers()}
	for _, module := range in.GetModules() {
		sc.Modules = append(sc.Modules, moduleCoord{Tank: module.GetTank(), Module: module.GetModule()})
	}
	a.state.mu.Lock()
	results := a.state.applyToScope(sc, apply)
	a.state.mu.Unlock()
	out := make([]*mdk.BoardResult, 0, len(results))
	for _, result := range results {
		out = append(out, &mdk.BoardResult{MacAddress: result.MACAddress, Applied: result.Applied, Message: result.Message})
	}
	return out, nil
}
func appliedBoardCount(results []*mdk.BoardResult) int32 {
	var count int32
	for _, result := range results {
		if result.GetApplied() {
			count++
		}
	}
	return count
}

func (a *grpcAPI) ListModuleVoltageSettings(context.Context, *mdk.ListModuleVoltageSettingsRequest) (*mdk.ListModuleVoltageSettingsResponse, error) {
	out := new(mdk.ListModuleVoltageSettingsResponse)
	return out, a.snapshot(func(s *ContainerState) any { return s.listModuleVoltageSettings() }, out)
}

func (a *grpcAPI) SetModuleFixedVoltage(ctx context.Context, in *mdk.SetModuleFixedVoltageRequest) (*mdk.SetModuleFixedVoltageResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	dto, err := a.state.setModuleFixedVoltage(in.GetModuleId(), int64(in.GetMillivolts()), true, headerValue(ctx, actorHeader))
	a.state.mu.Unlock()
	return project(dto, err, new(mdk.SetModuleFixedVoltageResponse))
}

func (a *grpcAPI) ClearModuleFixedVoltage(ctx context.Context, in *mdk.ClearModuleFixedVoltageRequest) (*mdk.ClearModuleFixedVoltageResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	dto, err := a.state.setModuleFixedVoltage(in.GetModuleId(), 0, false, headerValue(ctx, actorHeader))
	a.state.mu.Unlock()
	return project(dto, err, new(mdk.ClearModuleFixedVoltageResponse))
}

func (a *grpcAPI) GetHashboardASICs(_ context.Context, in *mdk.GetHashboardASICsRequest) (*mdk.GetHashboardASICsResponse, error) {
	a.state.mu.RLock()
	dto, err := a.state.hashboardASICs(in.GetIdentifier())
	a.state.mu.RUnlock()
	return project(dto, err, new(mdk.GetHashboardASICsResponse))
}

func (a *grpcAPI) QueryTimeSeries(_ context.Context, in *mdk.QueryTimeSeriesRequest) (*mdk.QueryTimeSeriesResponse, error) {
	if in.GetRange().GetStart() == nil || in.GetRange().GetEnd() == nil {
		return nil, status.Error(codes.InvalidArgument, "range.start and range.end are required")
	}
	a.state.mu.Lock()
	dto, err := a.state.timeSeries(in.GetMetric().String(), in.GetIdentifier(), in.GetRange().GetStart().AsTime(), in.GetRange().GetEnd().AsTime(), in.GetBucketSize().String())
	a.state.mu.Unlock()
	return project(dto, err, new(mdk.QueryTimeSeriesResponse))
}

func (a *grpcAPI) QueryShareQualityHistory(_ context.Context, in *mdk.QueryShareQualityHistoryRequest) (*mdk.QueryShareQualityHistoryResponse, error) {
	poolID := ""
	if in.PoolId != nil {
		poolID = fmt.Sprint(in.GetPoolId())
	}
	a.state.mu.RLock()
	dto, err := a.state.poolShareHistory(poolID, in.GetGrouping().String(), in.GetIdentifier(), in.GetRange().GetStart().AsTime(), in.GetRange().GetEnd().AsTime(), in.GetBucketSize().String())
	a.state.mu.RUnlock()
	return project(dto, err, new(mdk.QueryShareQualityHistoryResponse))
}

func (a *grpcAPI) StartModuleRunIn(_ context.Context, in *mdk.StartModuleRunInRequest) (*mdk.StartModuleRunInResponse, error) {
	if in.GetConfig() == nil {
		return nil, status.Error(codes.InvalidArgument, "config is required")
	}
	a.state.mu.RLock()
	dto, err := a.state.moduleRunIn(in.GetModuleId(), in.GetTaskId())
	a.state.mu.RUnlock()
	return project(dto, err, new(mdk.StartModuleRunInResponse))
}
