package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestPoolShareHistory(t *testing.T) {
	srv, _ := newTestServer(t)
	path := "/api/v2/telemetry/shares/history?poolId=1&grouping=SHARE_QUALITY_HISTORY_GROUPING_TOTAL&range.start=2026-09-16T00:00:00Z&range.end=2026-09-16T01:00:00Z&bucketSize=BUCKET_SIZE_5M"
	var result poolShareHistoryResponse
	getJSON(t, srv, path, &result)
	if len(result.Buckets) != 12 || result.Buckets[0].Accepted != 490 || result.Buckets[0].Rejected != 10 {
		t.Fatalf("unexpected share history: %+v", result)
	}
	if result.Buckets[11].Time != "2026-09-16T00:55:00Z" {
		t.Fatalf("last bucket = %s, want half-open range", result.Buckets[11].Time)
	}
	for _, backup := range []struct {
		id                 string
		accepted, rejected int64str
	}{{"2", 45, 5}, {"3", 0, 0}} {
		var history poolShareHistoryResponse
		getJSON(t, srv, strings.Replace(path, "poolId=1", "poolId="+backup.id, 1), &history)
		if len(history.Buckets) != 12 {
			t.Fatalf("backup %s: got %d buckets, want 12", backup.id, len(history.Buckets))
		}
		for _, bucket := range history.Buckets {
			if bucket.Accepted != backup.accepted || bucket.Rejected != backup.rejected || bucket.SampleCount != 5 || bucket.ReportingCoverage != 1 {
				t.Errorf("backup %s: unexpected bucket %+v", backup.id, bucket)
			}
		}
	}
	for _, query := range []string{
		strings.Replace(strings.TrimPrefix(path, "/api/v2/telemetry/shares/history?"), "poolId=1", "poolId=4", 1),
		"poolId=1", "poolId=1&grouping=SHARE_QUALITY_HISTORY_GROUPING_POOL",
		"poolId=1&grouping=SHARE_QUALITY_HISTORY_GROUPING_TOTAL&range.start=2026-09-16T00:00:00Z&range.end=2026-09-26T00:00:00Z&bucketSize=BUCKET_SIZE_1M",
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
