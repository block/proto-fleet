package main

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ---------------------------------------------------------------------------
// Operations shared by the REST, native gRPC, and Connect APIs. Each transport
// decodes its own request; callers must hold the appropriate state lock.
// ---------------------------------------------------------------------------

// apiError is a rejection that REST reports as an HTTP status and gRPC and
// Connect report as a status code.
type apiError struct {
	httpStatus int
	code       codes.Code
	message    string
}

func (e *apiError) Error() string { return e.message }

func (e *apiError) GRPCStatus() *status.Status { return status.New(e.code, e.message) }

func notFoundf(format string, args ...any) error {
	return &apiError{http.StatusNotFound, codes.NotFound, fmt.Sprintf(format, args...)}
}

func invalidArgument(message string) error {
	return &apiError{http.StatusBadRequest, codes.InvalidArgument, message}
}

func writeAPIError(w http.ResponseWriter, err error) {
	var rejected *apiError
	if errors.As(err, &rejected) {
		writeError(w, rejected.httpStatus, rejected.message)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func (s *ContainerState) systemStatusDTO() systemStatusResponse {
	active, total := s.boardCounts()
	role, writePrimary := "HA_ROLE_PRIMARY", true
	if s.ha != nil {
		role, writePrimary = s.ha.role()
	}
	return systemStatusResponse{Status: systemStatus{
		Health:                s.healthStatus(active, total),
		Mining:                active > 0,
		ActiveBoards:          active,
		TotalBoards:           total,
		ActivePool:            defaultActivePool,
		DatabaseConnected:     true,
		ServerID:              s.ServerID,
		HARole:                role,
		Version:               s.Version,
		ContainerWritePrimary: writePrimary,
	}}
}

func (s *ContainerState) poolDiagnosticsDTO() poolDiagnosticsResponse {
	activeBoards, _ := s.boardCounts()
	active := activeBoards > 0
	state := "POOL_CONN_STATE_IDLE"
	var activePoolID *int64str
	if active {
		state = "POOL_CONN_STATE_ACTIVE"
		for _, pool := range s.poolDTOs() {
			if pool.Enabled {
				id := pool.ID
				activePoolID = &id
				break
			}
		}
	}
	var diagnostics []poolDiagnostics
	for _, pool := range s.poolDTOs() {
		if !pool.Enabled {
			continue
		}
		connectionState := "POOL_CONN_STATE_IDLE"
		if activePoolID != nil && pool.ID == *activePoolID {
			connectionState = state
		}
		diagnostics = append(diagnostics, poolDiagnostics{
			PoolID:     pool.ID,
			PoolName:   pool.Name,
			ConnState:  connectionState,
			IsActive:   activePoolID != nil && pool.ID == *activePoolID,
			IsReady:    true,
			Difficulty: 2048,
		})
	}
	return poolDiagnosticsResponse{ActivePoolID: activePoolID, Pools: diagnostics}
}

type targetPowerUpdate struct {
	watts            float64
	performanceMode  string
	balanceBays      *bool
	hashOnDisconnect *bool
}

// targetPower applies update, when present, to every board and reports the
// container-wide target.
func (s *ContainerState) targetPower(update *targetPowerUpdate) (targetPowerResponse, error) {
	boards := s.eachBoard()
	if len(boards) == 0 {
		return targetPowerResponse{}, &apiError{http.StatusConflict, codes.FailedPrecondition, "container has no hashboards"}
	}
	if update != nil {
		if err := s.mappingWriteGuard(); err != nil {
			return targetPowerResponse{}, err
		}
		if err := finiteWatts(update.watts); err != nil {
			return targetPowerResponse{}, err
		}
		if update.watts <= 0 {
			return targetPowerResponse{}, invalidArgument("watts must be greater than zero")
		}
		for _, board := range boards {
			board.PowerTargetW = math.Max(board.PowerTargetMinW, math.Min(board.PowerTargetMaxW, update.watts))
		}
		s.PerformanceMode = update.performanceMode
		if update.balanceBays != nil {
			s.BalanceBays = *update.balanceBays
		}
		if update.hashOnDisconnect != nil {
			s.HashOnDisconnect = *update.hashOnDisconnect
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
	return response, nil
}

func (s *ContainerState) setContainerSetpoint(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(float64(float32(value)), 0) {
		return invalidArgument("setpoint must be representable as a finite float32 PLC value")
	}
	if _, supported := s.ContainerSetpoints[name]; !supported {
		return notFoundf("unknown container setpoint")
	}
	s.ContainerSetpoints[name] = value
	return nil
}

func (s *ContainerState) hashboardASICs(identifier string) (asicsResponse, error) {
	board := s.findBoard(identifier)
	if board == nil {
		return asicsResponse{}, notFoundf("unknown hashboard %q", identifier)
	}
	return s.asicsResponse(board), nil
}

// finiteWatts rejects NaN and infinities, which protobuf can carry but the
// JSON projection of the simulator state cannot.
func finiteWatts(watts float64) error {
	if math.IsNaN(watts) || math.IsInf(watts, 0) {
		return invalidArgument("watts must be a finite number")
	}
	return nil
}

func scopedPowerTarget(watts float64) func(*Hashboard) (bool, string) {
	return func(b *Hashboard) (bool, string) {
		clamped := math.Max(b.PowerTargetMinW, math.Min(b.PowerTargetMaxW, watts))
		b.ScopedWatts = clamped
		b.ScopedWattsSet = true
		if clamped != watts {
			return true, fmt.Sprintf("clamped to %.0fW", clamped)
		}
		return true, ""
	}
}

func clearScopedPowerTarget(b *Hashboard) (bool, string) {
	b.ScopedWatts = 0
	b.ScopedWattsSet = false
	return true, ""
}

func startBoard(b *Hashboard) (bool, string) {
	if b.Isolated {
		return false, "board isolated"
	}
	b.State = miningStateMining
	return true, ""
}

func stopBoard(b *Hashboard) (bool, string) {
	b.State = miningStateIdle
	return true, ""
}

var timeSeriesBucketSizes = map[string]time.Duration{
	"BUCKET_SIZE_1M":  time.Minute,
	"BUCKET_SIZE_5M":  5 * time.Minute,
	"BUCKET_SIZE_15M": 15 * time.Minute,
	"BUCKET_SIZE_1H":  time.Hour,
	"BUCKET_SIZE_1D":  24 * time.Hour,
}

func (s *ContainerState) timeSeries(metric, identifier string, start, end time.Time, bucketSize string) (timeSeriesResponse, error) {
	if !end.After(start) {
		return timeSeriesResponse{}, invalidArgument("range.end must be after range.start")
	}
	bucketDuration := timeSeriesBucketSizes[bucketSize]
	if bucketDuration == 0 {
		return timeSeriesResponse{}, invalidArgument("unsupported bucketSize")
	}
	unit, boardIndex, err := s.historyMetric(metric, identifier)
	if err != nil {
		if strings.HasPrefix(err.Error(), "unknown") {
			return timeSeriesResponse{}, notFoundf("%s", err)
		}
		return timeSeriesResponse{}, invalidArgument(err.Error())
	}
	if strings.Contains(metric, "_FAN_") || strings.Contains(metric, "_DISTRIBUTION_") || strings.Contains(metric, "_PHASE_") || strings.HasPrefix(metric, "TIME_SERIES_METRIC_POWER_FEED_") {
		if _, ok := s.recordSample().metric(metric, identifier, boardIndex); !ok {
			return timeSeriesResponse{}, notFoundf("unknown component identifier")
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
	return timeSeriesResponse{Metric: metric, Unit: unit, Buckets: buckets}, nil
}

// poolShareHistory supplies deterministic demonstration history for the
// simulator's configured pools. It does not reconstruct real mining history.
// A missing bound (zero or the Unix epoch) fails the range check.
func (s *ContainerState) poolShareHistory(poolID, grouping, identifier string, start, end time.Time, bucketSize string) (poolShareHistoryResponse, error) {
	switch poolID {
	case "1", "2", "3":
	default:
		return poolShareHistoryResponse{}, notFoundf("unknown pool")
	}
	if grouping != "SHARE_QUALITY_HISTORY_GROUPING_TOTAL" || identifier != "" {
		return poolShareHistoryResponse{}, invalidArgument("simulator supports pool-filtered total history only")
	}
	interval := map[string]time.Duration{
		"BUCKET_SIZE_1H": time.Hour,
		"BUCKET_SIZE_1D": 24 * time.Hour,
	}[bucketSize]
	if !end.After(start) || interval == 0 || end.Sub(start) > 7*24*time.Hour {
		return poolShareHistoryResponse{}, invalidArgument("invalid share history range or interval (maximum seven days)")
	}
	if !start.Equal(start.Truncate(interval)) || !end.Equal(end.Truncate(interval)) {
		return poolShareHistoryResponse{}, invalidArgument("share history range must align to hour/day bucket boundaries")
	}
	response := poolShareHistoryResponse{Buckets: []poolShareBucket{}, Grouping: grouping, Source: "HISTORY_SOURCE_HOURLY"}
	for at := start; at.Before(end); at = at.Add(interval) {
		bucket := poolShareBucket{Time: at.Format(time.RFC3339Nano)}
		minutes := int64(interval / time.Minute)
		for tick := at; tick.Before(at.Add(interval)); tick = tick.Add(time.Minute) {
			accepted, rejected, collected := s.poolShares(tick, poolID)
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
	return response, nil
}

func (s *ContainerState) moduleRunIn(moduleID, taskID string) (moduleRunInResponse, error) {
	module := s.modulesByID[moduleID]
	if module == nil {
		return moduleRunInResponse{}, notFoundf("unknown module %q", moduleID)
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
		boards = append(boards, moduleRunInBoard{
			MACAddress: slot.Board.MAC,
			Serial:     slot.Board.Serial,
			Status:     "RUN_IN_BOARD_STATUS_QUEUED",
		})
	}
	if taskID == "" {
		taskID = fmt.Sprintf("run-in-%d", time.Now().UnixNano())
	}
	return moduleRunInResponse{
		TaskID:           taskID,
		ModuleID:         moduleID,
		Status:           "HASHBOARD_RUN_IN_STATUS_QUEUED",
		VoltageLeaderMAC: leader,
		Boards:           boards,
	}, nil
}
