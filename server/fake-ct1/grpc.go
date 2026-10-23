package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
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
	mdk.UnimplementedTelemetryServiceServer
	mdk.UnimplementedPowerServiceServer
	mdk.UnimplementedPoolServiceServer
	mdk.UnimplementedRunInServiceServer
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
	mdk.RegisterPoolServiceServer(server, api)
	mdk.RegisterRunInServiceServer(server, api)
	return server
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

// Reuse the simulator's existing DTO builders and clock for both transports.
// The REST simulator retains some older diagnostic fields (e.g. breakerId)
// that aren't in the pinned native contract; protojson drops only those fields.
func (a *grpcAPI) snapshot(build func(*ContainerState) any, out proto.Message) error {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	now := a.state.now()
	a.state.recordUntil(now)
	a.state.advance(now)
	return projectProto(build(a.state), out)
}
func (a *grpcAPI) GetSystemInfo(context.Context, *mdk.GetSystemInfoRequest) (*mdk.GetSystemInfoResponse, error) {
	out := new(mdk.GetSystemInfoResponse)
	err := a.snapshot(func(s *ContainerState) any { return s.systemInfoDTO() }, out)
	return out, err
}

func (a *grpcAPI) GetSystemStatus(context.Context, *mdk.GetSystemStatusRequest) (*mdk.GetSystemStatusResponse, error) {
	out := new(mdk.GetSystemStatusResponse)
	err := a.snapshot(func(s *ContainerState) any {
		active, total := s.boardCounts()
		return systemStatusResponse{Status: systemStatus{
			Health:                s.healthStatus(active, total),
			Mining:                active > 0,
			ActiveBoards:          active,
			TotalBoards:           total,
			ActivePool:            defaultActivePool,
			DatabaseConnected:     true,
			ServerID:              s.ServerID,
			HARole:                "HA_ROLE_PRIMARY",
			Version:               s.Version,
			ContainerWritePrimary: true,
		}}
	}, out)
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

func (a *grpcAPI) SetContainerSetpoint(ctx context.Context, in *mdk.SetContainerSetpointRequest) (*mdk.SetContainerSetpointResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	if math.IsNaN(in.GetValue()) || math.IsInf(in.GetValue(), 0) || in.GetValue() < 20 || in.GetValue() > 55 {
		return nil, status.Error(codes.InvalidArgument, "temperature target must be between 20 and 55°C (simulator bounds)")
	}
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	if _, ok := a.state.ContainerSetpoints[in.Name]; !ok {
		return nil, status.Error(codes.NotFound, "unknown container setpoint")
	}
	a.state.ContainerSetpoints[in.Name] = in.Value
	return &mdk.SetContainerSetpointResponse{Setpoint: &mdk.ContainerSetpoint{Name: in.Name, Value: in.Value, Unit: "°C"}}, nil
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

func (a *grpcAPI) ListPools(context.Context, *mdk.ListPoolsRequest) (*mdk.ListPoolsResponse, error) {
	out := new(mdk.ListPoolsResponse)
	return out, projectProto(listPoolsResponse{Pools: demoPools()}, out)
}

func (a *grpcAPI) GetPoolDiagnostics(context.Context, *mdk.GetPoolDiagnosticsRequest) (*mdk.GetPoolDiagnosticsResponse, error) {
	a.state.mu.RLock()
	activeBoards, _ := a.state.boardCounts()
	a.state.mu.RUnlock()
	active := activeBoards > 0
	state := "POOL_CONN_STATE_IDLE"
	var activePoolID *int64str
	if active {
		state = "POOL_CONN_STATE_ACTIVE"
		id := int64str(1)
		activePoolID = &id
	}
	var diagnostics []poolDiagnostics
	for _, pool := range demoPools() {
		connectionState := "POOL_CONN_STATE_IDLE"
		if pool.ID == 1 {
			connectionState = state
		}
		diagnostics = append(diagnostics, poolDiagnostics{
			PoolID:     pool.ID,
			PoolName:   pool.Name,
			ConnState:  connectionState,
			IsActive:   pool.ID == 1 && active,
			IsReady:    true,
			Difficulty: 2048,
		})
	}
	out := new(mdk.GetPoolDiagnosticsResponse)
	return out, projectProto(poolDiagnosticsResponse{ActivePoolID: activePoolID, Pools: diagnostics}, out)
}

func (a *grpcAPI) GetTargetPower(context.Context, *mdk.GetTargetPowerRequest) (*mdk.GetTargetPowerResponse, error) {
	out := new(mdk.GetTargetPowerResponse)
	return out, a.targetPower(out, nil)
}

func (a *grpcAPI) SetTargetPower(_ context.Context, in *mdk.SetTargetPowerRequest) (*mdk.SetTargetPowerResponse, error) {
	out := new(mdk.SetTargetPowerResponse)
	return out, a.targetPower(out, in)
}

func (a *grpcAPI) targetPower(out proto.Message, in *mdk.SetTargetPowerRequest) error {
	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	boards := s.eachBoard()
	if len(boards) == 0 {
		return status.Error(codes.FailedPrecondition, "container has no hashboards")
	}
	if in != nil {
		if in.GetWatts() <= 0 {
			return status.Error(codes.InvalidArgument, "watts must be greater than zero")
		}
		for _, board := range boards {
			board.PowerTargetW = math.Max(board.PowerTargetMinW, math.Min(board.PowerTargetMaxW, in.GetWatts()))
		}
		s.PerformanceMode = in.GetPerformanceMode().String()
		if in.BalanceBays != nil {
			s.BalanceBays = in.GetBalanceBays()
		}
		if in.HashOnDisconnect != nil {
			s.HashOnDisconnect = in.GetHashOnDisconnect()
		}
	}
	response := targetPowerResponse{
		Watts:            boards[0].PowerTargetW,
		DefaultWatts:     boardPowerTargetW,
		MinimumWatts:     boardPowerTargetMinW,
		MaximumWatts:     boardPowerTargetMaxW,
		PerformanceMode:  s.PerformanceMode,
		BalanceBays:      s.BalanceBays,
		HashOnDisconnect: s.HashOnDisconnect,
	}
	if response.PerformanceMode == "" || response.PerformanceMode == "PERFORMANCE_MODE_UNSPECIFIED" {
		response.PerformanceMode = "PERFORMANCE_MODE_MAXIMUM_HASHRATE"
	}
	return projectProto(response, out)
}

func (a *grpcAPI) StartMining(ctx context.Context, _ *mdk.StartMiningRequest) (*mdk.StartMiningResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	for _, b := range a.state.eachBoard() {
		if !b.Isolated {
			b.State = miningStateMining
		}
	}
	a.state.mu.Unlock()
	return &mdk.StartMiningResponse{}, nil
}

func (a *grpcAPI) StopMining(ctx context.Context, _ *mdk.StopMiningRequest) (*mdk.StopMiningResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	for _, b := range a.state.eachBoard() {
		b.State = miningStateIdle
	}
	a.state.mu.Unlock()
	return &mdk.StopMiningResponse{}, nil
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

func mdkScope(in *mdk.BoardScope) scope {
	sc := scope{Identifiers: in.GetIdentifiers()}
	for _, module := range in.GetModules() {
		sc.Modules = append(sc.Modules, moduleCoord{Tank: module.GetTank(), Module: module.GetModule()})
	}
	return sc
}

func mdkBoardResults(results []boardResult) []*mdk.BoardResult {
	out := make([]*mdk.BoardResult, 0, len(results))
	for _, result := range results {
		out = append(out, &mdk.BoardResult{MacAddress: result.MACAddress, Applied: result.Applied, Message: result.Message})
	}
	return out
}

func (a *grpcAPI) SetPowerTargetScoped(ctx context.Context, in *mdk.SetPowerTargetScopedRequest) (*mdk.SetPowerTargetScopedResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	results := a.state.applyToScope(mdkScope(in.GetScope()), func(b *Hashboard) (bool, string) {
		clamped := math.Max(b.PowerTargetMinW, math.Min(b.PowerTargetMaxW, in.GetWatts()))
		b.ScopedWatts = clamped
		b.ScopedWattsSet = true
		if clamped != in.GetWatts() {
			return true, fmt.Sprintf("clamped to %.0fW", clamped)
		}
		return true, ""
	})
	a.state.mu.Unlock()
	out := mdkBoardResults(results)
	return &mdk.SetPowerTargetScopedResponse{BoardsUpdated: appliedBoardCount(out), Results: out}, nil
}

func (a *grpcAPI) ClearPowerTargetScoped(ctx context.Context, in *mdk.ClearPowerTargetScopedRequest) (*mdk.ClearPowerTargetScopedResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	results := a.state.applyToScope(mdkScope(in.GetScope()), func(b *Hashboard) (bool, string) {
		b.ScopedWatts = 0
		b.ScopedWattsSet = false
		return true, ""
	})
	a.state.mu.Unlock()
	out := mdkBoardResults(results)
	return &mdk.ClearPowerTargetScopedResponse{BoardsUpdated: appliedBoardCount(out), Results: out}, nil
}

func (a *grpcAPI) SetIsolation(ctx context.Context, in *mdk.SetIsolationRequest) (*mdk.SetIsolationResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	results := a.state.applyToScope(mdkScope(in.GetScope()), func(b *Hashboard) (bool, string) {
		b.Isolated = in.GetIsolated()
		return true, ""
	})
	a.state.mu.Unlock()
	out := mdkBoardResults(results)
	return &mdk.SetIsolationResponse{BoardsChanged: appliedBoardCount(out), Results: out}, nil
}

func (a *grpcAPI) SetModuleFixedVoltage(ctx context.Context, in *mdk.SetModuleFixedVoltageRequest) (*mdk.SetModuleFixedVoltageResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	if in.GetMillivolts() < 26000 || in.GetMillivolts() > 30000 {
		return nil, status.Error(codes.InvalidArgument, "fixed voltage must be between 26000 and 30000 mV (simulator bounds)")
	}
	a.state.mu.Lock()
	module := a.state.modulesByID[in.GetModuleId()]
	if module == nil {
		a.state.mu.Unlock()
		return nil, status.Errorf(codes.NotFound, "unknown module %q", in.GetModuleId())
	}
	module.FixedMillivolts = int64(in.GetMillivolts())
	module.FixedVoltageSet = true
	resp := a.state.moduleVoltageResponse(module)
	a.state.mu.Unlock()
	out := new(mdk.SetModuleFixedVoltageResponse)
	return out, projectProto(resp, out)
}

func (a *grpcAPI) ClearModuleFixedVoltage(ctx context.Context, in *mdk.ClearModuleFixedVoltageRequest) (*mdk.ClearModuleFixedVoltageResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	module := a.state.modulesByID[in.GetModuleId()]
	if module == nil {
		a.state.mu.Unlock()
		return nil, status.Errorf(codes.NotFound, "unknown module %q", in.GetModuleId())
	}
	module.FixedMillivolts = 0
	module.FixedVoltageSet = false
	resp := a.state.moduleVoltageResponse(module)
	a.state.mu.Unlock()
	out := new(mdk.ClearModuleFixedVoltageResponse)
	return out, projectProto(resp, out)
}

func (a *grpcAPI) GetHashboardASICs(_ context.Context, in *mdk.GetHashboardASICsRequest) (*mdk.GetHashboardASICsResponse, error) {
	a.state.mu.RLock()
	board := a.state.findBoard(in.GetIdentifier())
	if board == nil {
		a.state.mu.RUnlock()
		return nil, status.Errorf(codes.NotFound, "unknown hashboard %q", in.GetIdentifier())
	}
	resp := a.state.asicsResponse(board)
	a.state.mu.RUnlock()
	out := new(mdk.GetHashboardASICsResponse)
	return out, projectProto(resp, out)
}

func (a *grpcAPI) SetIdentifyLED(ctx context.Context, in *mdk.SetIdentifyLEDRequest) (*mdk.SetIdentifyLEDResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	a.state.mu.Lock()
	results := a.state.applyToScope(mdkScope(in.GetScope()), func(*Hashboard) (bool, string) { return true, "" })
	a.state.mu.Unlock()
	return &mdk.SetIdentifyLEDResponse{BoardsUpdated: appliedBoardCount(mdkBoardResults(results))}, nil
}

func (a *grpcAPI) QueryTimeSeries(_ context.Context, in *mdk.QueryTimeSeriesRequest) (*mdk.QueryTimeSeriesResponse, error) {
	if in.GetRange().GetStart() == nil || in.GetRange().GetEnd() == nil {
		return nil, status.Error(codes.InvalidArgument, "range.start and range.end are required")
	}
	start := in.GetRange().GetStart().AsTime()
	end := in.GetRange().GetEnd().AsTime()
	if !end.After(start) {
		return nil, status.Error(codes.InvalidArgument, "range.end must be after range.start")
	}
	metric := in.GetMetric().String()
	identifier := in.GetIdentifier()
	bucketDuration := map[mdk.BucketSize]time.Duration{
		mdk.BucketSize_BUCKET_SIZE_1M:  time.Minute,
		mdk.BucketSize_BUCKET_SIZE_5M:  5 * time.Minute,
		mdk.BucketSize_BUCKET_SIZE_15M: 15 * time.Minute,
		mdk.BucketSize_BUCKET_SIZE_1H:  time.Hour,
		mdk.BucketSize_BUCKET_SIZE_1D:  24 * time.Hour,
	}[in.GetBucketSize()]
	if bucketDuration == 0 {
		return nil, status.Error(codes.InvalidArgument, "unsupported bucketSize")
	}

	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	unit, boardIndex, err := s.historyMetric(metric, identifier)
	if err != nil {
		if strings.HasPrefix(err.Error(), "unknown") {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if strings.Contains(metric, "_FAN_") || strings.Contains(metric, "_DISTRIBUTION_") || strings.Contains(metric, "_PHASE_") || strings.HasPrefix(metric, "TIME_SERIES_METRIC_POWER_FEED_") {
		if _, ok := s.recordSample().metric(metric, identifier, boardIndex); !ok {
			return nil, status.Error(codes.NotFound, "unknown component identifier")
		}
	}
	buckets := make([]timeSeriesBucket, 0, 300)
	for at := start; at.Before(end) && len(buckets) < 300; at = at.Add(bucketDuration) {
		bucket := timeSeriesBucket{Time: at}
		sum := 0.0
		addSample := func(sample recordedSample) {
			value, ok := sample.metric(metric, identifier, boardIndex)
			if !ok {
				return
			}
			if bucket.Count == 0 {
				bucket.Min = value
				bucket.Max = value
			}
			bucket.Min = math.Min(bucket.Min, value)
			bucket.Max = math.Max(bucket.Max, value)
			sum += value
			bucket.Count++
		}
		bucketEnd := minTime(at.Add(bucketDuration), end)
		for tick := at; tick.Before(bucketEnd); tick = tick.Add(time.Minute) {
			if sample, ok := s.sampleAt(tick); ok {
				addSample(sample)
			}
		}
		first := sort.Search(len(s.history.Samples), func(i int) bool { return s.history.Samples[i].At.After(at) })
		for _, sample := range s.history.Samples[first:] {
			if !sample.At.Before(bucketEnd) {
				break
			}
			if sample.At.Sub(at)%time.Minute != 0 && !sample.At.Before(s.sampleTime.Add(-historyRetention)) {
				addSample(sample)
			}
		}
		if bucket.Count > 0 {
			bucket.Avg = sum / float64(bucket.Count)
		}
		buckets = append(buckets, bucket)
	}
	out := new(mdk.QueryTimeSeriesResponse)
	return out, projectProto(timeSeriesResponse{Metric: metric, Unit: unit, Buckets: buckets}, out)
}

func (a *grpcAPI) QueryShareQualityHistory(_ context.Context, in *mdk.QueryShareQualityHistoryRequest) (*mdk.QueryShareQualityHistoryResponse, error) {
	poolID := fmt.Sprintf("%d", in.GetPoolId())
	switch poolID {
	case "1", "2", "3":
	default:
		return nil, status.Error(codes.NotFound, "unknown pool")
	}
	if in.GetGrouping() != mdk.ShareQualityHistoryGrouping_SHARE_QUALITY_HISTORY_GROUPING_TOTAL || in.GetIdentifier() != "" {
		return nil, status.Error(codes.InvalidArgument, "simulator supports pool-filtered total history only")
	}
	if in.GetRange().GetStart() == nil || in.GetRange().GetEnd() == nil {
		return nil, status.Error(codes.InvalidArgument, "range.start and range.end are required")
	}
	start := in.GetRange().GetStart().AsTime()
	end := in.GetRange().GetEnd().AsTime()
	interval := map[mdk.BucketSize]time.Duration{
		mdk.BucketSize_BUCKET_SIZE_1H: time.Hour,
		mdk.BucketSize_BUCKET_SIZE_1D: 24 * time.Hour,
	}[in.GetBucketSize()]
	if !end.After(start) || interval == 0 || end.Sub(start) > 7*24*time.Hour {
		return nil, status.Error(codes.InvalidArgument, "invalid share history range or interval (maximum seven days)")
	}
	if !start.Equal(start.Truncate(interval)) || !end.Equal(end.Truncate(interval)) {
		return nil, status.Error(codes.InvalidArgument, "share history range must align to hour/day bucket boundaries")
	}
	a.state.mu.RLock()
	defer a.state.mu.RUnlock()
	response := poolShareHistoryResponse{Buckets: []poolShareBucket{}, Grouping: in.GetGrouping().String(), Source: "HISTORY_SOURCE_HOURLY"}
	for at := start; at.Before(end); at = at.Add(interval) {
		bucket := poolShareBucket{Time: at.Format(time.RFC3339Nano)}
		minutes := int64(interval / time.Minute)
		for tick := at; tick.Before(at.Add(interval)); tick = tick.Add(time.Minute) {
			accepted, rejected, collected := a.state.poolShares(tick, poolID)
			if !collected {
				continue
			}
			bucket.SampleCount++
			bucket.Accepted += int64str(accepted)
			bucket.Rejected += int64str(rejected)
		}
		bucket.ReportingCoverage = float64(bucket.SampleCount) / float64(minutes)
		response.Buckets = append(response.Buckets, bucket)
	}
	out := new(mdk.QueryShareQualityHistoryResponse)
	return out, projectProto(response, out)
}

func (a *grpcAPI) StartModuleRunIn(_ context.Context, in *mdk.StartModuleRunInRequest) (*mdk.StartModuleRunInResponse, error) {
	moduleID := in.GetModuleId()
	a.state.mu.RLock()
	module := a.state.modulesByID[moduleID]
	if module == nil {
		a.state.mu.RUnlock()
		return nil, status.Errorf(codes.NotFound, "unknown module %q", moduleID)
	}
	boards := make([]moduleRunInBoard, 0, len(module.Slots))
	leader := ""
	for _, slot := range module.Slots {
		if slot.Board == nil {
			continue
		}
		if leader == "" {
			leader = slot.Board.MAC
		}
		boards = append(boards, moduleRunInBoard{MACAddress: slot.Board.MAC, Serial: slot.Board.Serial, Status: "RUN_IN_BOARD_STATUS_QUEUED"})
	}
	a.state.mu.RUnlock()
	taskID := in.GetTaskId()
	if taskID == "" {
		taskID = fmt.Sprintf("run-in-%d", time.Now().UnixNano())
	}
	out := new(mdk.StartModuleRunInResponse)
	return out, projectProto(moduleRunInResponse{
		TaskID:           taskID,
		ModuleID:         moduleID,
		Status:           "HASHBOARD_RUN_IN_STATUS_QUEUED",
		VoltageLeaderMAC: leader,
		Boards:           boards,
	}, out)
}
