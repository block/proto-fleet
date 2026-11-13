package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/grpc/status"
)

type connectAPI struct {
	mdkv2connect.UnimplementedSystemServiceHandler
	mdkv2connect.UnimplementedHashboardServiceHandler
	mdkv2connect.UnimplementedMiningServiceHandler
	mdkv2connect.UnimplementedTelemetryServiceHandler
	mdkv2connect.UnimplementedPowerServiceHandler
	mdkv2connect.UnimplementedPoolServiceHandler
	mdkv2connect.UnimplementedRunInServiceHandler
	mdkv2connect.UnimplementedFirmwareServiceHandler
	native *grpcAPI
}

func registerConnectRoutes(mux *http.ServeMux, state *ContainerState) {
	api := &connectAPI{native: &grpcAPI{state: state}}
	options := connect.WithInterceptors(simulationInterceptor(state))
	mux.Handle(mdkv2connect.NewSystemServiceHandler(api, options))
	mux.Handle(mdkv2connect.NewHashboardServiceHandler(api, options))
	mux.Handle(mdkv2connect.NewMiningServiceHandler(api, options))
	mux.Handle(mdkv2connect.NewTelemetryServiceHandler(api, options))
	mux.Handle(mdkv2connect.NewPowerServiceHandler(api, options))
	mux.Handle(mdkv2connect.NewPoolServiceHandler(api, options))
	mux.Handle(mdkv2connect.NewRunInServiceHandler(api, options))
	mux.Handle(mdkv2connect.NewFirmwareServiceHandler(api, options))
}

// simulationInterceptor gives each Connect call the shared request lifecycle
// and reports native status codes as Connect codes.
func simulationInterceptor(state *ContainerState) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (res connect.AnyResponse, err error) {
			started := time.Now()
			defer func() { state.metrics.record(req.Spec().Procedure, started, err != nil) }()
			if serveErr := state.serve(ctx, !boundToGET(req.Spec().Procedure), func() { res, err = next(ctx, req) }); serveErr != nil {
				err = status.FromContextError(serveErr).Err()
			}
			if err == nil {
				return res, nil
			}
			if st, ok := status.FromError(err); ok {
				return nil, connect.NewError(connect.Code(st.Code()), errors.New(st.Message()))
			}
			return nil, err
		}
	}
}

func connectResult[T any](msg *T, err error) (*connect.Response[T], error) {
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(msg), nil
}

func (a *connectAPI) GetSystemInfo(ctx context.Context, req *connect.Request[mdk.GetSystemInfoRequest]) (*connect.Response[mdk.GetSystemInfoResponse], error) {
	return connectResult(a.native.GetSystemInfo(ctx, req.Msg))
}

func (a *connectAPI) GetSystemStatus(ctx context.Context, req *connect.Request[mdk.GetSystemStatusRequest]) (*connect.Response[mdk.GetSystemStatusResponse], error) {
	return connectResult(a.native.GetSystemStatus(ctx, req.Msg))
}

func (a *connectAPI) GetContainerMap(ctx context.Context, req *connect.Request[mdk.GetContainerMapRequest]) (*connect.Response[mdk.GetContainerMapResponse], error) {
	return connectResult(a.native.GetContainerMap(ctx, req.Msg))
}

func (a *connectAPI) GetHashboardASICs(ctx context.Context, req *connect.Request[mdk.GetHashboardASICsRequest]) (*connect.Response[mdk.GetHashboardASICsResponse], error) {
	return connectResult(a.native.GetHashboardASICs(ctx, req.Msg))
}

func (a *connectAPI) SetIdentifyLED(ctx context.Context, req *connect.Request[mdk.SetIdentifyLEDRequest]) (*connect.Response[mdk.SetIdentifyLEDResponse], error) {
	return connectResult(a.native.SetIdentifyLED(ctx, req.Msg))
}

func (a *connectAPI) GetMiningStatus(ctx context.Context, req *connect.Request[mdk.GetMiningStatusRequest]) (*connect.Response[mdk.GetMiningStatusResponse], error) {
	return connectResult(a.native.GetMiningStatus(ctx, req.Msg))
}

func (a *connectAPI) GetTargetPower(ctx context.Context, req *connect.Request[mdk.GetTargetPowerRequest]) (*connect.Response[mdk.GetTargetPowerResponse], error) {
	return connectResult(a.native.GetTargetPower(ctx, req.Msg))
}

func (a *connectAPI) SetTargetPower(ctx context.Context, req *connect.Request[mdk.SetTargetPowerRequest]) (*connect.Response[mdk.SetTargetPowerResponse], error) {
	return connectResult(a.native.SetTargetPower(ctx, req.Msg))
}

func (a *connectAPI) StartMining(ctx context.Context, req *connect.Request[mdk.StartMiningRequest]) (*connect.Response[mdk.StartMiningResponse], error) {
	return connectResult(a.native.StartMining(ctx, req.Msg))
}

func (a *connectAPI) StopMining(ctx context.Context, req *connect.Request[mdk.StopMiningRequest]) (*connect.Response[mdk.StopMiningResponse], error) {
	return connectResult(a.native.StopMining(ctx, req.Msg))
}

func (a *connectAPI) StartMiningScoped(ctx context.Context, req *connect.Request[mdk.StartMiningScopedRequest]) (*connect.Response[mdk.StartMiningScopedResponse], error) {
	return connectResult(a.native.StartMiningScoped(ctx, req.Msg))
}

func (a *connectAPI) StopMiningScoped(ctx context.Context, req *connect.Request[mdk.StopMiningScopedRequest]) (*connect.Response[mdk.StopMiningScopedResponse], error) {
	return connectResult(a.native.StopMiningScoped(ctx, req.Msg))
}

func (a *connectAPI) SetPowerTargetScoped(ctx context.Context, req *connect.Request[mdk.SetPowerTargetScopedRequest]) (*connect.Response[mdk.SetPowerTargetScopedResponse], error) {
	return connectResult(a.native.SetPowerTargetScoped(ctx, req.Msg))
}

func (a *connectAPI) ClearPowerTargetScoped(ctx context.Context, req *connect.Request[mdk.ClearPowerTargetScopedRequest]) (*connect.Response[mdk.ClearPowerTargetScopedResponse], error) {
	return connectResult(a.native.ClearPowerTargetScoped(ctx, req.Msg))
}

func (a *connectAPI) SetIsolation(ctx context.Context, req *connect.Request[mdk.SetIsolationRequest]) (*connect.Response[mdk.SetIsolationResponse], error) {
	return connectResult(a.native.SetIsolation(ctx, req.Msg))
}

func (a *connectAPI) SetModuleFixedVoltage(ctx context.Context, req *connect.Request[mdk.SetModuleFixedVoltageRequest]) (*connect.Response[mdk.SetModuleFixedVoltageResponse], error) {
	return connectResult(a.native.SetModuleFixedVoltage(ctx, req.Msg))
}

func (a *connectAPI) ClearModuleFixedVoltage(ctx context.Context, req *connect.Request[mdk.ClearModuleFixedVoltageRequest]) (*connect.Response[mdk.ClearModuleFixedVoltageResponse], error) {
	return connectResult(a.native.ClearModuleFixedVoltage(ctx, req.Msg))
}

func (a *connectAPI) QueryTimeSeries(ctx context.Context, req *connect.Request[mdk.QueryTimeSeriesRequest]) (*connect.Response[mdk.QueryTimeSeriesResponse], error) {
	return connectResult(a.native.QueryTimeSeries(ctx, req.Msg))
}

func (a *connectAPI) QueryShareQualityHistory(ctx context.Context, req *connect.Request[mdk.QueryShareQualityHistoryRequest]) (*connect.Response[mdk.QueryShareQualityHistoryResponse], error) {
	return connectResult(a.native.QueryShareQualityHistory(ctx, req.Msg))
}

func (a *connectAPI) GetContainerHealth(ctx context.Context, req *connect.Request[mdk.GetContainerHealthRequest]) (*connect.Response[mdk.GetContainerHealthResponse], error) {
	return connectResult(a.native.GetContainerHealth(ctx, req.Msg))
}

func (a *connectAPI) GetContainerControls(ctx context.Context, req *connect.Request[mdk.GetContainerControlsRequest]) (*connect.Response[mdk.GetContainerControlsResponse], error) {
	return connectResult(a.native.GetContainerControls(ctx, req.Msg))
}

func (a *connectAPI) SetContainerControl(ctx context.Context, req *connect.Request[mdk.SetContainerControlRequest]) (*connect.Response[mdk.SetContainerControlResponse], error) {
	return connectResult(a.native.SetContainerControl(ctx, req.Msg))
}

func (a *connectAPI) SetContainerSetpoint(ctx context.Context, req *connect.Request[mdk.SetContainerSetpointRequest]) (*connect.Response[mdk.SetContainerSetpointResponse], error) {
	return connectResult(a.native.SetContainerSetpoint(ctx, req.Msg))
}

func (a *connectAPI) ListPools(ctx context.Context, req *connect.Request[mdk.ListPoolsRequest]) (*connect.Response[mdk.ListPoolsResponse], error) {
	return connectResult(a.native.ListPools(ctx, req.Msg))
}

func (a *connectAPI) GetPoolDiagnostics(ctx context.Context, req *connect.Request[mdk.GetPoolDiagnosticsRequest]) (*connect.Response[mdk.GetPoolDiagnosticsResponse], error) {
	return connectResult(a.native.GetPoolDiagnostics(ctx, req.Msg))
}

func (a *connectAPI) StartModuleRunIn(ctx context.Context, req *connect.Request[mdk.StartModuleRunInRequest]) (*connect.Response[mdk.StartModuleRunInResponse], error) {
	return connectResult(a.native.StartModuleRunIn(ctx, req.Msg))
}

func (a *connectAPI) CreatePool(ctx context.Context, req *connect.Request[mdk.CreatePoolRequest]) (*connect.Response[mdk.CreatePoolResponse], error) {
	return connectResult(a.native.CreatePool(ctx, req.Msg))
}
func (a *connectAPI) UpdatePool(ctx context.Context, req *connect.Request[mdk.UpdatePoolRequest]) (*connect.Response[mdk.UpdatePoolResponse], error) {
	return connectResult(a.native.UpdatePool(ctx, req.Msg))
}
func (a *connectAPI) DeletePool(ctx context.Context, req *connect.Request[mdk.DeletePoolRequest]) (*connect.Response[mdk.DeletePoolResponse], error) {
	return connectResult(a.native.DeletePool(ctx, req.Msg))
}

func (a *connectAPI) BeginFirmwareArtifactUpload(ctx context.Context, req *connect.Request[mdk.BeginFirmwareArtifactUploadRequest]) (*connect.Response[mdk.BeginFirmwareArtifactUploadResponse], error) {
	return connectResult(a.native.BeginFirmwareArtifactUpload(ctx, req.Msg))
}

func (a *connectAPI) WriteFirmwareArtifactUploadChunk(ctx context.Context, req *connect.Request[mdk.WriteFirmwareArtifactUploadChunkRequest]) (*connect.Response[mdk.WriteFirmwareArtifactUploadChunkResponse], error) {
	return connectResult(a.native.WriteFirmwareArtifactUploadChunk(ctx, req.Msg))
}

func (a *connectAPI) CommitFirmwareArtifactUpload(ctx context.Context, req *connect.Request[mdk.CommitFirmwareArtifactUploadRequest]) (*connect.Response[mdk.CommitFirmwareArtifactUploadResponse], error) {
	return connectResult(a.native.CommitFirmwareArtifactUpload(ctx, req.Msg))
}

func (a *connectAPI) AbortFirmwareArtifactUpload(ctx context.Context, req *connect.Request[mdk.AbortFirmwareArtifactUploadRequest]) (*connect.Response[mdk.AbortFirmwareArtifactUploadResponse], error) {
	return connectResult(a.native.AbortFirmwareArtifactUpload(ctx, req.Msg))
}

func (a *connectAPI) StartFirmwareUpdate(ctx context.Context, req *connect.Request[mdk.StartFirmwareUpdateRequest]) (*connect.Response[mdk.StartFirmwareUpdateResponse], error) {
	return connectResult(a.native.StartFirmwareUpdate(ctx, req.Msg))
}

func (a *connectAPI) GetFirmwareUpdate(ctx context.Context, req *connect.Request[mdk.GetFirmwareUpdateRequest]) (*connect.Response[mdk.GetFirmwareUpdateResponse], error) {
	return connectResult(a.native.GetFirmwareUpdate(ctx, req.Msg))
}

func (a *connectAPI) ListFirmwareUpdates(ctx context.Context, req *connect.Request[mdk.ListFirmwareUpdatesRequest]) (*connect.Response[mdk.ListFirmwareUpdatesResponse], error) {
	return connectResult(a.native.ListFirmwareUpdates(ctx, req.Msg))
}

func (a *connectAPI) CancelFirmwareUpdate(ctx context.Context, req *connect.Request[mdk.CancelFirmwareUpdateRequest]) (*connect.Response[mdk.CancelFirmwareUpdateResponse], error) {
	return connectResult(a.native.CancelFirmwareUpdate(ctx, req.Msg))
}
