package main

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

func TestInjectResourceMetricsLabelsAddsMissingKeys(t *testing.T) {
	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{Resource: &resourcepb.Resource{}},
		},
	}
	injectResourceMetricsLabels(req, map[string]string{"hostname": "rig-1", "site": "lab"})
	attrs := req.ResourceMetrics[0].Resource.Attributes
	if len(attrs) != 2 {
		t.Fatalf("got %d attributes, want 2", len(attrs))
	}
}

func TestPushMetricsBatchGzipsBody(t *testing.T) {
	payload := mustMarshalMetrics(t, "mcdd", nil)
	var gotEncoding string
	var gotReq colmetricspb.ExportMetricsServiceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		body := readRequestBody(t, r, true)
		if err := proto.Unmarshal(body, &gotReq); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := decodeAndPushMetricsBatch(context.Background(), srv.Client(), srv.URL, payload, map[string]string{"hostname": "rig-1"}, true)
	if err != nil {
		t.Fatalf("decodeAndPushMetricsBatch: %v", err)
	}
	if gotEncoding != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", gotEncoding)
	}
	if got := stringAttr(gotReq.ResourceMetrics[0].Resource.Attributes, "hostname"); got != "rig-1" {
		t.Fatalf("hostname = %q, want rig-1", got)
	}
}

func TestPushMetricsBatchCanDisableGzip(t *testing.T) {
	payload := mustMarshalMetrics(t, "mcdd", nil)
	var gotEncoding string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		gotBody = readRequestBody(t, r, false)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := decodeAndPushMetricsBatch(context.Background(), srv.Client(), srv.URL, payload, map[string]string{"hostname": "rig-1"}, false)
	if err != nil {
		t.Fatalf("decodeAndPushMetricsBatch: %v", err)
	}
	if gotEncoding != "" {
		t.Fatalf("Content-Encoding = %q, want empty", gotEncoding)
	}
	var req colmetricspb.ExportMetricsServiceRequest
	if err := proto.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
}

func TestPushLogsBatchGzipsBody(t *testing.T) {
	payload := mustMarshalLogs(t, "telemetry-service")
	var gotEncoding string
	var gotReq collogspb.ExportLogsServiceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		body := readRequestBody(t, r, true)
		if err := proto.Unmarshal(body, &gotReq); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := pushLogsBatch(context.Background(), srv.Client(), srv.URL, payload, map[string]string{"hostname": "rig-1"}, true)
	if err != nil {
		t.Fatalf("pushLogsBatch: %v", err)
	}
	if gotEncoding != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", gotEncoding)
	}
	if got := stringAttr(gotReq.ResourceLogs[0].Resource.Attributes, "hostname"); got != "rig-1" {
		t.Fatalf("hostname = %q, want rig-1", got)
	}
}

func TestPushLogsBatchCanDisableGzip(t *testing.T) {
	payload := mustMarshalLogs(t, "telemetry-service")
	var gotEncoding string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		gotBody = readRequestBody(t, r, false)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := pushLogsBatch(context.Background(), srv.Client(), srv.URL, payload, map[string]string{"hostname": "rig-1"}, false)
	if err != nil {
		t.Fatalf("pushLogsBatch: %v", err)
	}
	if gotEncoding != "" {
		t.Fatalf("Content-Encoding = %q, want empty", gotEncoding)
	}
	var req collogspb.ExportLogsServiceRequest
	if err := proto.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
}

func TestCombinedLogBatchesFlushAsOneRequest(t *testing.T) {
	var requestCount int
	var gotReq collogspb.ExportLogsServiceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body := readRequestBody(t, r, true)
		if err := proto.Unmarshal(body, &gotReq); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stats, err := pushCombinedLogBatches(
		context.Background(),
		srv.Client(),
		srv.URL,
		[]queuedLogBatch{
			{rigAddress: "rig-1:2123", payload: mustMarshalLogs(t, "telemetry-service"), labels: map[string]string{"hostname": "rig-1"}},
			{rigAddress: "rig-2:2123", payload: mustMarshalLogs(t, "mcdd"), labels: map[string]string{"hostname": "rig-2"}},
		},
		true,
		time.Second,
		0,
		256,
	)
	if err != nil {
		t.Fatalf("pushCombinedLogBatches: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("requestCount = %d, want 1", requestCount)
	}
	if len(gotReq.ResourceLogs) != 2 {
		t.Fatalf("ResourceLogs = %d, want 2", len(gotReq.ResourceLogs))
	}
	if stats.inputBatches != 2 || stats.resourceLogs != 2 || stats.uncompressedBytes == 0 || stats.wireBytes == 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if got := stringAttr(gotReq.ResourceLogs[0].Resource.Attributes, "hostname"); got != "rig-1" {
		t.Fatalf("first hostname = %q, want rig-1", got)
	}
	if got := stringAttr(gotReq.ResourceLogs[1].Resource.Attributes, "hostname"); got != "rig-2" {
		t.Fatalf("second hostname = %q, want rig-2", got)
	}
}

func TestCombinedMetricsBatchesFlushAsOneRequest(t *testing.T) {
	var requestCount int
	var gotReq colmetricspb.ExportMetricsServiceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body := readRequestBody(t, r, true)
		if err := proto.Unmarshal(body, &gotReq); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stats, err := pushCombinedMetricsBatches(
		context.Background(),
		srv.Client(),
		srv.URL,
		[]queuedMetricBatch{
			{rigAddress: "rig-1:2123", payload: mustMarshalMetrics(t, "mcdd", nil), labels: map[string]string{"hostname": "rig-1"}},
			{rigAddress: "rig-2:2123", payload: mustMarshalMetrics(t, "host", nil), labels: map[string]string{"hostname": "rig-2"}},
		},
		true,
		time.Second,
		0,
		256,
	)
	if err != nil {
		t.Fatalf("pushCombinedMetricsBatches: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("requestCount = %d, want 1", requestCount)
	}
	if len(gotReq.ResourceMetrics) != 2 {
		t.Fatalf("ResourceMetrics = %d, want 2", len(gotReq.ResourceMetrics))
	}
	if stats.inputBatches != 2 || stats.resourceMetrics != 2 || stats.uncompressedBytes == 0 || stats.wireBytes == 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if got := stringAttr(gotReq.ResourceMetrics[0].Resource.Attributes, "hostname"); got != "rig-1" {
		t.Fatalf("first hostname = %q, want rig-1", got)
	}
	if got := stringAttr(gotReq.ResourceMetrics[1].Resource.Attributes, "hostname"); got != "rig-2" {
		t.Fatalf("second hostname = %q, want rig-2", got)
	}
}

func TestCombinedMetricsSplitRetryIsolatesFailedBatch(t *testing.T) {
	var requestCount int
	var goodUploads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body := readRequestBody(t, r, r.Header.Get("Content-Encoding") == "gzip")
		var req colmetricspb.ExportMetricsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if len(req.ResourceMetrics) > 1 {
			http.Error(w, "combined rejected", http.StatusBadRequest)
			return
		}
		hostname := stringAttr(req.ResourceMetrics[0].Resource.Attributes, "hostname")
		if hostname == "rig-bad" {
			http.Error(w, "out of order sample", http.StatusBadRequest)
			return
		}
		goodUploads++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := pushCombinedMetricsBatches(
		context.Background(),
		srv.Client(),
		srv.URL,
		[]queuedMetricBatch{
			{rigAddress: "rig-good:2123", payload: mustMarshalMetrics(t, "mcdd", nil), labels: map[string]string{"hostname": "rig-good"}},
			{rigAddress: "rig-bad:2123", payload: mustMarshalMetrics(t, "mcdd", nil), labels: map[string]string{"hostname": "rig-bad"}},
		},
		true,
		time.Second,
		0,
		256,
	)
	if err == nil {
		t.Fatal("pushCombinedMetricsBatches succeeded with one bad split batch")
	}
	if requestCount != 3 {
		t.Fatalf("requestCount = %d, want combined attempt plus two split retries", requestCount)
	}
	if goodUploads != 1 {
		t.Fatalf("goodUploads = %d, want 1", goodUploads)
	}
	for _, want := range []string{"combined metrics upload failed", "rig-bad:2123", "out of order sample"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestCombinedLogSplitRetryIsolatesFailedBatch(t *testing.T) {
	var requestCount int
	var goodUploads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body := readRequestBody(t, r, r.Header.Get("Content-Encoding") == "gzip")
		var req collogspb.ExportLogsServiceRequest
		if err := proto.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if len(req.ResourceLogs) > 1 {
			http.Error(w, "combined rejected", http.StatusBadRequest)
			return
		}
		hostname := stringAttr(req.ResourceLogs[0].Resource.Attributes, "hostname")
		if hostname == "rig-bad" {
			http.Error(w, "bad log batch", http.StatusBadRequest)
			return
		}
		goodUploads++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := pushCombinedLogBatches(
		context.Background(),
		srv.Client(),
		srv.URL,
		[]queuedLogBatch{
			{rigAddress: "rig-good:2123", payload: mustMarshalLogs(t, "telemetry-service"), labels: map[string]string{"hostname": "rig-good"}},
			{rigAddress: "rig-bad:2123", payload: mustMarshalLogs(t, "telemetry-service"), labels: map[string]string{"hostname": "rig-bad"}},
		},
		true,
		time.Second,
		0,
		256,
	)
	if err == nil {
		t.Fatal("pushCombinedLogBatches succeeded with one bad split batch")
	}
	if requestCount != 3 {
		t.Fatalf("requestCount = %d, want combined attempt plus two split retries", requestCount)
	}
	if goodUploads != 1 {
		t.Fatalf("goodUploads = %d, want 1", goodUploads)
	}
	for _, want := range []string{"combined logs upload failed", "rig-bad:2123", "bad log batch"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestCombinedMetricsOverwritesExistingResourceAttributes(t *testing.T) {
	var gotReq colmetricspb.ExportMetricsServiceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readRequestBody(t, r, false)
		if err := proto.Unmarshal(body, &gotReq); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := mustMarshalMetrics(t, "mcdd", map[string]string{"hostname": "set-by-rig"})
	_, err := pushCombinedMetricsBatches(
		context.Background(),
		srv.Client(),
		srv.URL,
		[]queuedMetricBatch{{rigAddress: "rig-1:2123", payload: payload, labels: map[string]string{"hostname": "from-bridge"}}},
		false,
		time.Second,
		0,
		256,
	)
	if err != nil {
		t.Fatalf("pushCombinedMetricsBatches: %v", err)
	}
	// Bridge labels are authoritative: rig payloads must not spoof identity keys.
	if got := stringAttr(gotReq.ResourceMetrics[0].Resource.Attributes, "hostname"); got != "from-bridge" {
		t.Fatalf("hostname = %q, want from-bridge", got)
	}
}

func TestMetricFlushStatsAccountForUncompressedUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readRequestBody(t, r, false)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stats, err := pushCombinedMetricsBatches(
		context.Background(),
		srv.Client(),
		srv.URL,
		[]queuedMetricBatch{
			{rigAddress: "rig-1:2123", payload: mustMarshalMetrics(t, "mcdd", nil), labels: map[string]string{"hostname": "rig-1"}},
			{rigAddress: "rig-2:2123", payload: mustMarshalMetrics(t, "host", nil), labels: map[string]string{"hostname": "rig-2"}},
		},
		false,
		time.Second,
		7,
		256,
	)
	if err != nil {
		t.Fatalf("pushCombinedMetricsBatches: %v", err)
	}
	if stats.inputBatches != 2 {
		t.Errorf("inputBatches = %d, want 2", stats.inputBatches)
	}
	if stats.resourceMetrics != 2 {
		t.Errorf("resourceMetrics = %d, want 2", stats.resourceMetrics)
	}
	if stats.uncompressedBytes == 0 {
		t.Error("uncompressedBytes = 0, want > 0")
	}
	if stats.wireBytes != stats.uncompressedBytes {
		t.Errorf("wireBytes = %d, want uncompressedBytes %d", stats.wireBytes, stats.uncompressedBytes)
	}
	if stats.gzipEnabled {
		t.Error("gzipEnabled = true, want false")
	}
	if stats.queueDepth != 7 || stats.queueCapacity != 256 {
		t.Errorf("queue stats = %d/%d, want 7/256", stats.queueDepth, stats.queueCapacity)
	}
}

func TestMetricsUploaderDropsNewestWhenQueueFull(t *testing.T) {
	uploader := newMetricsUploader("http://example.invalid", 1, false)
	info := &rigInfo{address: "rig-1:2123", labels: map[string]string{"hostname": "rig-1"}}
	first := []byte("first")
	second := []byte("second")

	uploader.enqueue(info, first)
	uploader.enqueue(info, second)

	if got := len(uploader.queue); got != 1 {
		t.Fatalf("queue len = %d, want 1", got)
	}
	got := <-uploader.queue
	if string(got.payload) != "first" {
		t.Fatalf("queued payload = %q, want first", string(got.payload))
	}
}

func TestLogsUploaderDropsNewestWhenQueueFull(t *testing.T) {
	uploader := newLogsUploader("http://example.invalid", 1, false)
	info := &rigInfo{address: "rig-1:2123", labels: map[string]string{"hostname": "rig-1"}}
	first := []byte("first")
	second := []byte("second")

	uploader.enqueue(info, first)
	uploader.enqueue(info, second)

	if got := len(uploader.queue); got != 1 {
		t.Fatalf("queue len = %d, want 1", got)
	}
	got := <-uploader.queue
	if string(got.payload) != "first" {
		t.Fatalf("queued payload = %q, want first", string(got.payload))
	}
}

func TestPushMetricsBatchIncludesResourceIdentitiesOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "out of order sample", http.StatusBadRequest)
	}))
	defer srv.Close()

	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{
							Key: "service.name",
							Value: &commonpb.AnyValue{
								Value: &commonpb.AnyValue_StringValue{StringValue: "mcdd"},
							},
						},
						{
							Key: "service.instance.id",
							Value: &commonpb.AnyValue{
								Value: &commonpb.AnyValue_StringValue{StringValue: "0"},
							},
						},
					},
				},
			},
		},
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	err = pushMetricsBatch(context.Background(), srv.Client(), srv.URL, payload, map[string]string{"hostname": "rig-204"})
	if err == nil {
		t.Fatal("pushMetricsBatch succeeded against 400 response")
	}
	got := err.Error()
	for _, want := range []string{
		"status 400: out of order sample",
		`hostname="rig-204"`,
		`service_name="mcdd"`,
		`service_instance_id="0"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("error %q missing %q", got, want)
		}
	}
}

func TestInjectResourceLogsLabelsOverridesExisting(t *testing.T) {
	existing := &commonpb.KeyValue{
		Key:   "hostname",
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "set-by-rig"}},
	}
	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{existing}}},
		},
	}
	injectResourceLogsLabels(req, map[string]string{"hostname": "from-bridge"})
	attrs := req.ResourceLogs[0].Resource.Attributes
	if len(attrs) != 1 {
		t.Fatalf("got %d attributes, want 1 (no duplicate key)", len(attrs))
	}
	if attrs[0].GetValue().GetStringValue() != "from-bridge" {
		t.Errorf("bridge label did not win: got %q", attrs[0].GetValue().GetStringValue())
	}
}

func readRequestBody(t *testing.T, r *http.Request, gzipped bool) []byte {
	t.Helper()
	reader := io.Reader(r.Body)
	if gzipped {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatalf("gzip.NewReader: %v", err)
		}
		defer zr.Close()
		reader = zr
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return body
}

func mustMarshalMetrics(t *testing.T, serviceName string, attrs map[string]string) []byte {
	t.Helper()
	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{Resource: &resourcepb.Resource{Attributes: resourceAttrs(serviceName, attrs)}},
		},
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	return payload
}

func mustMarshalLogs(t *testing.T, serviceName string) []byte {
	t.Helper()
	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{Resource: &resourcepb.Resource{Attributes: resourceAttrs(serviceName, nil)}},
		},
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal logs: %v", err)
	}
	return payload
}

func resourceAttrs(serviceName string, extra map[string]string) []*commonpb.KeyValue {
	attrs := []*commonpb.KeyValue{
		stringKeyValue("service.name", serviceName),
	}
	for key, value := range extra {
		attrs = append(attrs, stringKeyValue(key, value))
	}
	return attrs
}

func stringKeyValue(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: value},
		},
	}
}
