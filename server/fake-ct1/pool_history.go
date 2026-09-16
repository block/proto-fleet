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
}

// handlePoolShareHistory supplies deterministic demonstration history for the
// simulator's configured pools. It does not reconstruct real mining history.
func (h *RESTApiHandler) handlePoolShareHistory(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	var acceptedPerMinute, rejectedPerMinute int64
	switch query.Get("poolId") {
	case "1":
		acceptedPerMinute, rejectedPerMinute = 98, 2
	case "2":
		// Illustrate earlier failover activity despite being on standby now.
		acceptedPerMinute, rejectedPerMinute = 9, 1
	case "3":
		// A configured backup with observed zero shares.
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
		"BUCKET_SIZE_1M":  time.Minute,
		"BUCKET_SIZE_5M":  5 * time.Minute,
		"BUCKET_SIZE_15M": 15 * time.Minute,
		"BUCKET_SIZE_1H":  time.Hour,
		"BUCKET_SIZE_1D":  24 * time.Hour,
	}[query.Get("bucketSize")]
	if startErr != nil || endErr != nil || !end.After(start) || interval == 0 || end.Sub(start) > 7*24*time.Hour {
		writeError(w, http.StatusBadRequest, "invalid share history range or interval (maximum seven days)")
		return
	}
	response := poolShareHistoryResponse{Buckets: []poolShareBucket{}, Grouping: query.Get("grouping")}
	for at := start; at.Before(end); at = at.Add(interval) {
		minutes := int64(min(interval, end.Sub(at)) / time.Minute)
		response.Buckets = append(response.Buckets, poolShareBucket{
			Time:     at.Format(time.RFC3339Nano),
			Accepted: int64str(acceptedPerMinute * minutes), Rejected: int64str(rejectedPerMinute * minutes),
			SampleCount: uint64str(minutes), ReportingCoverage: 1,
		})
	}
	writeJSON(w, http.StatusOK, response)
}
