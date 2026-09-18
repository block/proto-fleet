package main

import (
	"net/http"
	"time"
)

type poolShareBucket struct {
	Time              string    `json:"time"`
	Key               string    `json:"key"`
	Accepted          int64str  `json:"accepted"`
	Rejected          int64str  `json:"rejected"`
	SampleCount       uint64str `json:"sampleCount"`
	ReportingCoverage float64   `json:"reportingCoverage"`
}

type poolShareHistoryResponse struct {
	Buckets  []poolShareBucket `json:"buckets"`
	Grouping string            `json:"grouping"`
	Source   string            `json:"source"`
}

// handlePoolShareHistory supplies deterministic demonstration history for the
// simulator's configured pools. It does not reconstruct real mining history.
func (h *RESTApiHandler) handlePoolShareHistory(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	switch query.Get("poolId") {
	case "1", "2", "3":
	default:
		writeError(w, http.StatusNotFound, "unknown pool")
		return
	}
	if query.Get("grouping") != "SHARE_QUALITY_HISTORY_GROUPING_TOTAL" || query.Get("identifier") != "" {
		writeError(w, http.StatusBadRequest, "simulator supports pool-filtered total history only")
		return
	}
	start, startErr := time.Parse(time.RFC3339Nano, query.Get("range.start"))
	end, endErr := time.Parse(time.RFC3339Nano, query.Get("range.end"))
	interval := map[string]time.Duration{
		"BUCKET_SIZE_1H": time.Hour,
		"BUCKET_SIZE_1D": 24 * time.Hour,
	}[query.Get("bucketSize")]
	if startErr != nil || endErr != nil || !end.After(start) || interval == 0 || end.Sub(start) > 7*24*time.Hour {
		writeError(w, http.StatusBadRequest, "invalid share history range or interval (maximum seven days)")
		return
	}
	if !start.Equal(start.Truncate(interval)) || !end.Equal(end.Truncate(interval)) {
		writeError(w, http.StatusBadRequest, "share history range must align to hour/day bucket boundaries")
		return
	}
	s := h.state
	s.mu.RLock()
	defer s.mu.RUnlock()
	response := poolShareHistoryResponse{Buckets: []poolShareBucket{}, Grouping: query.Get("grouping"), Source: "HISTORY_SOURCE_HOURLY"}
	for at := start; at.Before(end); at = at.Add(interval) {
		bucket := poolShareBucket{Time: at.Format(time.RFC3339Nano)}
		minutes := int64(interval / time.Minute)
		for tick := at; tick.Before(at.Add(interval)); tick = tick.Add(time.Minute) {
			accepted, rejected, collected := s.poolShares(tick, query.Get("poolId"))
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
	writeJSON(w, http.StatusOK, response)
}

// Explicit demo cadence, independent of the advertised difficulty. Both the
// live acceptance ratio and historical counts use the same 199:1 share model.
const acceptedSharesPerMinute, rejectedSharesPerMinute = 199, 1

func (s *ContainerState) poolShares(at time.Time, poolID string) (accepted, rejected int, collected bool) {
	if at.After(s.sampleTime) || at.Before(s.sampleTime.Add(-historyRetention)) {
		return 0, 0, false
	}
	minute := at.UTC().Hour()*60 + at.UTC().Minute()
	if minute >= 17 && minute < 22 {
		return 0, 0, false
	} // daily five-minute collection gap
	activePool := "1"
	mining := false
	if at.Before(s.StartTime) {
		active, _ := s.history.Baseline.boardCounts()
		mining = active > 0
		if minute >= 120 && minute < 180 {
			activePool = "2"
		} // historical 02:00–03:00 UTC failover
	} else {
		sample, ok := s.sampleAt(at)
		if !ok {
			return 0, 0, false
		}
		for _, b := range sample.Boards {
			mining = mining || b.Mining
		}
	}
	if mining && poolID == activePool {
		return acceptedSharesPerMinute, rejectedSharesPerMinute, true
	}
	return 0, 0, true
}

func (s *ContainerState) shareAcceptanceRate() float64 {
	active, _ := s.boardCounts()
	if active == 0 {
		return 0
	}
	// Most recent collected minute; collection gaps do not reset the ratio.
	return float64(acceptedSharesPerMinute) / float64(acceptedSharesPerMinute+rejectedSharesPerMinute)
}
