package main

import (
	"context"
	"encoding/json"
	"fmt"

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
	mdk.UnimplementedTelemetryServiceServer
	mdk.UnimplementedPowerServiceServer
	state *ContainerState
}

func newGRPCServer(state *ContainerState) *grpc.Server {
	state.initHistory()
	api := &grpcAPI{state: state}
	server := grpc.NewServer()
	mdk.RegisterSystemServiceServer(server, api)
	mdk.RegisterHashboardServiceServer(server, api)
	mdk.RegisterMiningServiceServer(server, api)
	mdk.RegisterTelemetryServiceServer(server, api)
	mdk.RegisterPowerServiceServer(server, api)
	return server
}

// Reuse the simulator's existing DTO builders and clock for both transports.
// The REST simulator retains some older diagnostic fields (e.g. breakerId)
// that aren't in the pinned native contract; protojson drops only those fields.
func (a *grpcAPI) snapshot(build func(*ContainerState) any, out proto.Message) error {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	now := a.state.now()
	a.state.recordUntil(now)
	a.state.advance(now)
	data, err := json.Marshal(build(a.state))
	if err != nil {
		return status.Errorf(codes.Internal, "encode simulation: %v", err)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, out); err != nil {
		return status.Errorf(codes.Internal, "project simulation: %v", err)
	}
	return nil
}
func (a *grpcAPI) GetSystemInfo(context.Context, *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	out := new(mdk.GetSystemInfoResponse)
	err := a.snapshot(func(s *ContainerState) any { return s.systemInfoDTO() }, out)
	return out, err
}
func (a *grpcAPI) GetContainerMap(context.Context, *mdk.GetContainerMapRequest) (*mdk.GetContainerMapResponse, error) {
	out := new(mdk.GetContainerMapResponse)
	err := a.snapshot(func(s *ContainerState) any { return s.containerMapDTO() }, out)
	return out, err
}
func (a *grpcAPI) GetMiningStatus(context.Context, *mdk.GetMiningStatusRequest) (*mdk.GetMiningStatusResponse, error) {
	out := new(mdk.GetMiningStatusResponse)
	err := a.snapshot(func(s *ContainerState) any { return s.miningStatusDTO() }, out)
	if err == nil {
		out.CurrentMeasurements = &mdk.MiningCurrentMeasurements{Source: mdk.MiningMeasurementSource_MINING_MEASUREMENT_SOURCE_PERSISTED_BOARD_HEALTH, Scope: mdk.MiningMeasurementScope_MINING_MEASUREMENT_SCOPE_SHARED_ASSIGNMENTS, HashboardPowerW: &mdk.MiningCurrentMeasurement{Value: &out.Power, Complete: true, ExpectedBoards: uint32(out.TotalBoards), ReportingBoards: uint32(out.TotalBoards)}}
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
		return containerControlsResponse{Supported: true, Controls: v.Controls, Setpoints: v.Setpoints, MainCircuitBreakerSupported: false}
	}, out)
	return out, err
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
	a.state.recordUntil(now)
	a.state.advance(now)
	a.state.ContainerControls[in.Name] = in.Enabled
	a.state.advance(now)
	a.state.recordTransition()
	return &mdk.SetContainerControlResponse{Control: &mdk.ContainerControl{Name: in.Name, Enabled: in.Enabled, StateKnown: true}, Confirmation: &mdk.ContainerControlConfirmation{RequestedEnabled: in.Enabled, ObservedEnabled: new(a.state.ContainerControls[in.Name]), ObservedAt: timestamppb.New(now), State: mdk.PlantWriteConfirmationState_PLANT_WRITE_CONFIRMATION_STATE_CONFIRMED, Source: mdk.PlantWriteConfirmationSource_PLANT_WRITE_CONFIRMATION_SOURCE_CONTROLLER_REGISTER}}, nil
}
func (a *grpcAPI) SetMainCircuitBreakerState(context.Context, *mdk.SetMainCircuitBreakerStateRequest) (*mdk.SetMainCircuitBreakerStateResponse, error) {
	return nil, status.Error(codes.Unimplemented, fmt.Sprintf("%s simulator has no dedicated breaker readback", product))
}

func (a *grpcAPI) StartMiningScoped(ctx context.Context, in *mdk.StartMiningScopedRequest) (*mdk.StartMiningScopedResponse, error) {
	results, err := a.setScopedMining(ctx, in.GetScope(), true)
	if err != nil {
		return nil, err
	}
	return &mdk.StartMiningScopedResponse{BoardsStarted: appliedBoardCount(results), Results: results}, nil
}
func (a *grpcAPI) StopMiningScoped(ctx context.Context, in *mdk.StopMiningScopedRequest) (*mdk.StopMiningScopedResponse, error) {
	results, err := a.setScopedMining(ctx, in.GetScope(), false)
	if err != nil {
		return nil, err
	}
	return &mdk.StopMiningScopedResponse{BoardsStopped: appliedBoardCount(results), Results: results}, nil
}
func (a *grpcAPI) setScopedMining(ctx context.Context, in *mdk.BoardScope, mining bool) ([]*mdk.BoardResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	sc := scope{Identifiers: in.GetIdentifiers()}
	for _, module := range in.GetModules() {
		sc.Modules = append(sc.Modules, moduleCoord{Tank: module.GetTank(), Module: module.GetModule()})
	}
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	results := a.state.applyToScope(sc, func(b *Hashboard) (bool, string) {
		if mining && b.Isolated {
			return false, "board isolated"
		}
		b.State = miningStateIdle
		if mining {
			b.State = miningStateMining
		}
		return true, ""
	})
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
