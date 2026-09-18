package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestPoolShareHistory(t *testing.T) {
	srv, _ := newTestServer(t)
	path := "/api/v2/telemetry/shares/history?poolId=1&grouping=SHARE_QUALITY_HISTORY_GROUPING_TOTAL&range.start=2026-09-16T00:00:00Z&range.end=2026-09-16T02:00:00Z&bucketSize=BUCKET_SIZE_1H"
	var result poolShareHistoryResponse
	getJSON(t, srv, path, &result)
	if len(result.Buckets) != 2 || result.Buckets[0].Accepted != 199*55 || result.Buckets[0].Rejected != 55 || result.Source != "HISTORY_SOURCE_HOURLY" {
		t.Fatalf("unexpected share history: %+v", result)
	}
	if result.Buckets[1].Time != "2026-09-16T01:00:00Z" {
		t.Fatalf("last bucket = %s, want half-open range", result.Buckets[1].Time)
	}
	for _, backup := range []struct {
		id                 string
		accepted, rejected int64str
	}{{"2", 0, 0}, {"3", 0, 0}} {
		var history poolShareHistoryResponse
		getJSON(t, srv, strings.Replace(path, "poolId=1", "poolId="+backup.id, 1), &history)
		if len(history.Buckets) != 2 {
			t.Fatalf("backup %s: got %d buckets, want 2", backup.id, len(history.Buckets))
		}
		for _, bucket := range history.Buckets {
			if bucket.Accepted != backup.accepted || bucket.Rejected != backup.rejected || bucket.SampleCount < 55 || bucket.SampleCount > 60 || bucket.ReportingCoverage != float64(bucket.SampleCount)/60 {
				t.Errorf("backup %s: unexpected bucket %+v", backup.id, bucket)
			}
		}
	}
	for _, query := range []string{
		strings.Replace(strings.TrimPrefix(path, "/api/v2/telemetry/shares/history?"), "poolId=1", "poolId=4", 1),
		strings.Replace(strings.TrimPrefix(path, "/api/v2/telemetry/shares/history?"), "BUCKET_SIZE_1H", "BUCKET_SIZE_15M", 1),
		strings.Replace(strings.TrimPrefix(path, "/api/v2/telemetry/shares/history?"), "T00:00:00Z", "T00:15:00Z", 1),
		"poolId=1", "poolId=1&grouping=SHARE_QUALITY_HISTORY_GROUPING_POOL",
		"poolId=1&grouping=SHARE_QUALITY_HISTORY_GROUPING_TOTAL&range.start=2026-09-16T00:00:00Z&range.end=2026-09-26T00:00:00Z&bucketSize=BUCKET_SIZE_1H",
	} {
		resp, err := http.Get(srv.URL + "/api/v2/telemetry/shares/history?" + query)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("query %s unexpectedly accepted", query)
		}
	}
}
