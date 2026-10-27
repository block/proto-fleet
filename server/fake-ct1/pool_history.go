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

func (h *RESTApiHandler) handlePoolShareHistory(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	// An unparsable bound stays zero, which poolShareHistory rejects.
	start, _ := time.Parse(time.RFC3339Nano, query.Get("range.start"))
	end, _ := time.Parse(time.RFC3339Nano, query.Get("range.end"))
	h.state.mu.RLock()
	resp, err := h.state.poolShareHistory(query.Get("poolId"), query.Get("grouping"), query.Get("identifier"), start, end, query.Get("bucketSize"))
	h.state.mu.RUnlock()
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
