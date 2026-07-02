package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

const maxMetricErrorIdentities = 20

type otlpUploadStats struct {
	uncompressedBytes int
	wireBytes         int
	gzipEnabled       bool
}

type metricsFlushStats struct {
	inputBatches      int
	resourceMetrics   int
	uncompressedBytes int
	wireBytes         int
	gzipEnabled       bool
	flushInterval     time.Duration
	queueDepth        int
	queueCapacity     int
}

type logsFlushStats struct {
	inputBatches      int
	resourceLogs      int
	uncompressedBytes int
	wireBytes         int
	gzipEnabled       bool
	flushInterval     time.Duration
	queueDepth        int
	queueCapacity     int
}

type decodedMetricBatch struct {
	rigAddress string
	req        *colmetricspb.ExportMetricsServiceRequest
}

type decodedLogBatch struct {
	rigAddress string
	req        *collogspb.ExportLogsServiceRequest
}

// injectResourceMetricsLabels stamps discovery labels onto every Resource,
// overriding same-key attributes: rigs cannot spoof identity labels.
func injectResourceMetricsLabels(req *colmetricspb.ExportMetricsServiceRequest, labels map[string]string) {
	if len(labels) == 0 {
		return
	}
	for _, rm := range req.ResourceMetrics {
		if rm.Resource == nil {
			rm.Resource = &resourcepb.Resource{}
		}
		rm.Resource.Attributes = mergeLabels(rm.Resource.Attributes, labels)
	}
}

func injectResourceLogsLabels(req *collogspb.ExportLogsServiceRequest, labels map[string]string) {
	if len(labels) == 0 {
		return
	}
	for _, rl := range req.ResourceLogs {
		if rl.Resource == nil {
			rl.Resource = &resourcepb.Resource{}
		}
		rl.Resource.Attributes = mergeLabels(rl.Resource.Attributes, labels)
	}
}

// bridgeOwnedLabelKeys is the full identity/placement namespace the bridge
// controls: rig attributes with these keys are dropped even when the bridge
// has no value for one (an unracked rig must not supply its own placement).
var bridgeOwnedLabelKeys = map[string]bool{
	"hostname": true, "device_identifier": true, "rig_ip": true,
	"site": true, "building": true, "rack": true, "zone": true,
}

func mergeLabels(existing []*commonpb.KeyValue, labels map[string]string) []*commonpb.KeyValue {
	// Bridge labels are authoritative: drop every rig-supplied instance of
	// a bridge-owned key (duplicates included), then append exactly one.
	merged := make([]*commonpb.KeyValue, 0, len(existing)+len(labels))
	for _, kv := range existing {
		if _, owned := labels[kv.GetKey()]; owned || bridgeOwnedLabelKeys[kv.GetKey()] {
			continue
		}
		merged = append(merged, kv)
	}
	for k, v := range labels {
		merged = append(merged, &commonpb.KeyValue{
			Key: k,
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{StringValue: v},
			},
		})
	}
	return merged
}

// pushOTLP POSTs protobuf-encoded OTLP bytes to a receiver. 2xx is success.
func pushOTLP(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	body []byte,
	gzipEnabled bool,
) (otlpUploadStats, error) {
	stats, _, err := pushOTLPWithResponse(ctx, client, endpoint, body, gzipEnabled)
	return stats, err
}

func pushOTLPWithResponse(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	body []byte,
	gzipEnabled bool,
) (otlpUploadStats, []byte, error) {
	wireBody, err := encodeOTLPBody(body, gzipEnabled)
	if err != nil {
		return otlpUploadStats{}, nil, err
	}
	stats := otlpUploadStats{
		uncompressedBytes: len(body),
		wireBytes:         len(wireBody),
		gzipEnabled:       gzipEnabled,
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(wireBody))
	if err != nil {
		return stats, nil, err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	if gzipEnabled {
		req.Header.Set("Content-Encoding", "gzip")
	}
	resp, err := client.Do(req)
	if err != nil {
		return stats, nil, err
	}
	defer resp.Body.Close()
	// OTLP responses are tiny; bound the read against a misbehaving receiver.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return stats, nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return stats, respBody, nil
}

func encodeOTLPBody(body []byte, gzipEnabled bool) ([]byte, error) {
	if !gzipEnabled {
		return body, nil
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pushMetricsBatch decodes, re-stamps labels, and forwards an OTLP
// metrics batch to the configured endpoint.
func pushMetricsBatch(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	payload []byte,
	labels map[string]string,
) error {
	_, err := pushCombinedMetricsBatches(
		ctx,
		client,
		endpoint,
		[]queuedMetricBatch{{payload: payload, labels: labels}},
		true,
		0,
		0,
		0,
	)
	return err
}

func pushCombinedMetricsBatches(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	batches []queuedMetricBatch,
	gzipEnabled bool,
	flushInterval time.Duration,
	queueDepth int,
	queueCapacity int,
) (metricsFlushStats, error) {
	stats := metricsFlushStats{
		inputBatches:  len(batches),
		gzipEnabled:   gzipEnabled,
		flushInterval: flushInterval,
		queueDepth:    queueDepth,
		queueCapacity: queueCapacity,
	}
	decoded := make([]decodedMetricBatch, 0, len(batches))
	combined := &colmetricspb.ExportMetricsServiceRequest{}
	for _, batch := range batches {
		req := &colmetricspb.ExportMetricsServiceRequest{}
		if err := proto.Unmarshal(batch.payload, req); err != nil {
			log.Printf("metrics batch decode %s payload_bytes=%d: %v", batch.rigAddress, len(batch.payload), err)
			continue
		}
		injectResourceMetricsLabels(req, batch.labels)
		decoded = append(decoded, decodedMetricBatch{rigAddress: batch.rigAddress, req: req})
		combined.ResourceMetrics = append(combined.ResourceMetrics, req.ResourceMetrics...)
	}
	stats.resourceMetrics = len(combined.ResourceMetrics)
	if stats.resourceMetrics == 0 {
		return stats, nil
	}
	out, err := proto.Marshal(combined)
	if err != nil {
		return stats, fmt.Errorf("re-encode metrics payload: %w", err)
	}
	stats.uncompressedBytes = len(out)
	uploadStats, err := pushOTLP(ctx, client, endpoint, out, gzipEnabled)
	stats.wireBytes = uploadStats.wireBytes
	if err != nil {
		if len(decoded) > 1 {
			if retryErr := retryMetricBatchesIndividually(ctx, client, endpoint, decoded, gzipEnabled); retryErr == nil {
				return stats, nil
			} else {
				return stats, fmt.Errorf(
					"combined metrics upload failed: %w; split retry failed: %w; %s; metric resource identities: %s",
					err,
					retryErr,
					formatMetricsFlushStats(stats),
					metricResourceIdentities(combined),
				)
			}
		}
		return stats, fmt.Errorf(
			"%w; %s; metric resource identities: %s",
			err,
			formatMetricsFlushStats(stats),
			metricResourceIdentities(combined),
		)
	}
	return stats, nil
}

// splitRetryBudget bounds the whole per-batch fallback: without it a wedged
// receiver stalls the single uploader loop for queue-length × client-timeout.
const splitRetryBudget = 30 * time.Second

func retryMetricBatchesIndividually(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	batches []decodedMetricBatch,
	gzipEnabled bool,
) error {
	ctx, cancel := context.WithTimeout(ctx, splitRetryBudget)
	defer cancel()
	var failures []string
	for i, batch := range batches {
		if ctx.Err() != nil {
			failures = append(failures, fmt.Sprintf("split retry budget exhausted with %d batches unsent", len(batches)-i))
			break
		}
		if len(batch.req.GetResourceMetrics()) == 0 {
			continue
		}
		out, err := proto.Marshal(batch.req)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s re-encode metrics payload: %v", batch.rigAddress, err))
			continue
		}
		if _, err := pushOTLP(ctx, client, endpoint, out, gzipEnabled); err != nil {
			failures = append(failures, fmt.Sprintf(
				"%s: %v; metric resource identities: %s",
				batch.rigAddress,
				err,
				metricResourceIdentities(batch.req),
			))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, " | "))
	}
	return nil
}

// decodeAndPushMetricsBatch preserves the old single-batch path for focused
// tests; streaming metrics use pushCombinedMetricsBatches through metricsUploader.
func decodeAndPushMetricsBatch(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	payload []byte,
	labels map[string]string,
	gzipEnabled bool,
) error {
	req := &colmetricspb.ExportMetricsServiceRequest{}
	if err := proto.Unmarshal(payload, req); err != nil {
		return fmt.Errorf("decode metrics payload: %w", err)
	}
	injectResourceMetricsLabels(req, labels)
	out, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("re-encode metrics payload: %w", err)
	}
	if _, err := pushOTLP(ctx, client, endpoint, out, gzipEnabled); err != nil {
		return fmt.Errorf("%w; metric resource identities: %s", err, metricResourceIdentities(req))
	}
	return nil
}

// pushLogsBatch preserves the old single-batch path for focused tests; streaming
// logs use pushCombinedLogBatches through logsUploader.
func pushLogsBatch(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	payload []byte,
	labels map[string]string,
	gzipEnabled bool,
) error {
	_, err := pushCombinedLogBatches(
		ctx,
		client,
		endpoint,
		[]queuedLogBatch{{payload: payload, labels: labels}},
		gzipEnabled,
		0,
		0,
		0,
	)
	return err
}

func pushCombinedLogBatches(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	batches []queuedLogBatch,
	gzipEnabled bool,
	flushInterval time.Duration,
	queueDepth int,
	queueCapacity int,
) (logsFlushStats, error) {
	stats := logsFlushStats{
		inputBatches:  len(batches),
		gzipEnabled:   gzipEnabled,
		flushInterval: flushInterval,
		queueDepth:    queueDepth,
		queueCapacity: queueCapacity,
	}
	decoded := make([]decodedLogBatch, 0, len(batches))
	combined := &collogspb.ExportLogsServiceRequest{}
	for _, batch := range batches {
		req := &collogspb.ExportLogsServiceRequest{}
		if err := proto.Unmarshal(batch.payload, req); err != nil {
			log.Printf("logs batch decode %s payload_bytes=%d: %v", batch.rigAddress, len(batch.payload), err)
			continue
		}
		injectResourceLogsLabels(req, batch.labels)
		decoded = append(decoded, decodedLogBatch{rigAddress: batch.rigAddress, req: req})
		combined.ResourceLogs = append(combined.ResourceLogs, req.ResourceLogs...)
	}
	stats.resourceLogs = len(combined.ResourceLogs)
	if stats.resourceLogs == 0 {
		return stats, nil
	}
	out, err := proto.Marshal(combined)
	if err != nil {
		return stats, fmt.Errorf("re-encode logs payload: %w", err)
	}
	stats.uncompressedBytes = len(out)
	uploadStats, err := pushOTLP(ctx, client, endpoint, out, gzipEnabled)
	stats.wireBytes = uploadStats.wireBytes
	if err != nil {
		if len(decoded) > 1 {
			if retryErr := retryLogBatchesIndividually(ctx, client, endpoint, decoded, gzipEnabled); retryErr == nil {
				return stats, nil
			} else {
				return stats, fmt.Errorf(
					"combined logs upload failed: %w; split retry failed: %w; %s; log resource identities: %s",
					err,
					retryErr,
					formatLogsFlushStats(stats),
					logResourceIdentities(combined),
				)
			}
		}
		return stats, fmt.Errorf(
			"%w; %s; log resource identities: %s",
			err,
			formatLogsFlushStats(stats),
			logResourceIdentities(combined),
		)
	}
	return stats, nil
}

func retryLogBatchesIndividually(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	batches []decodedLogBatch,
	gzipEnabled bool,
) error {
	ctx, cancel := context.WithTimeout(ctx, splitRetryBudget)
	defer cancel()
	var failures []string
	for i, batch := range batches {
		if ctx.Err() != nil {
			failures = append(failures, fmt.Sprintf("split retry budget exhausted with %d batches unsent", len(batches)-i))
			break
		}
		if len(batch.req.GetResourceLogs()) == 0 {
			continue
		}
		out, err := proto.Marshal(batch.req)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s re-encode logs payload: %v", batch.rigAddress, err))
			continue
		}
		if _, err := pushOTLP(ctx, client, endpoint, out, gzipEnabled); err != nil {
			failures = append(failures, fmt.Sprintf(
				"%s: %v; log resource identities: %s",
				batch.rigAddress,
				err,
				logResourceIdentities(batch.req),
			))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, " | "))
	}
	return nil
}

func formatMetricsFlushStats(stats metricsFlushStats) string {
	return fmt.Sprintf(
		"input_batches=%d resource_metrics=%d uncompressed_bytes=%d wire_bytes=%d gzip=%t flush_interval=%s queue_depth=%d queue_capacity=%d compression_ratio=%.3f",
		stats.inputBatches,
		stats.resourceMetrics,
		stats.uncompressedBytes,
		stats.wireBytes,
		stats.gzipEnabled,
		stats.flushInterval,
		stats.queueDepth,
		stats.queueCapacity,
		compressionRatio(stats.uncompressedBytes, stats.wireBytes),
	)
}

func formatLogsFlushStats(stats logsFlushStats) string {
	return fmt.Sprintf(
		"input_batches=%d resource_logs=%d uncompressed_bytes=%d wire_bytes=%d gzip=%t flush_interval=%s queue_depth=%d queue_capacity=%d compression_ratio=%.3f",
		stats.inputBatches,
		stats.resourceLogs,
		stats.uncompressedBytes,
		stats.wireBytes,
		stats.gzipEnabled,
		stats.flushInterval,
		stats.queueDepth,
		stats.queueCapacity,
		compressionRatio(stats.uncompressedBytes, stats.wireBytes),
	)
}

func compressionRatio(uncompressedBytes, wireBytes int) float64 {
	if uncompressedBytes == 0 {
		return 0
	}
	return float64(wireBytes) / float64(uncompressedBytes)
}

func metricResourceIdentities(req *colmetricspb.ExportMetricsServiceRequest) string {
	seen := make(map[string]struct{})
	identities := make([]string, 0, len(req.ResourceMetrics))
	for _, rm := range req.ResourceMetrics {
		attrs := []*commonpb.KeyValue(nil)
		if rm.GetResource() != nil {
			attrs = rm.GetResource().GetAttributes()
		}
		identity := fmt.Sprintf(
			"hostname=%q service_name=%q service_instance_id=%q",
			stringAttr(attrs, "hostname"),
			stringAttr(attrs, "service.name"),
			stringAttr(attrs, "service.instance.id"),
		)
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		identities = append(identities, identity+" "+metricTimestampSummary(rm))
	}
	sort.Strings(identities)
	if len(identities) > maxMetricErrorIdentities {
		hidden := len(identities) - maxMetricErrorIdentities
		identities = append(identities[:maxMetricErrorIdentities], fmt.Sprintf("... %d more", hidden))
	}
	return strings.Join(identities, "; ")
}

func logResourceIdentities(req *collogspb.ExportLogsServiceRequest) string {
	seen := make(map[string]struct{})
	identities := make([]string, 0, len(req.ResourceLogs))
	for _, rl := range req.ResourceLogs {
		attrs := []*commonpb.KeyValue(nil)
		if rl.GetResource() != nil {
			attrs = rl.GetResource().GetAttributes()
		}
		identity := fmt.Sprintf(
			"hostname=%q service_name=%q service_instance_id=%q",
			stringAttr(attrs, "hostname"),
			stringAttr(attrs, "service.name"),
			stringAttr(attrs, "service.instance.id"),
		)
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	if len(identities) > maxMetricErrorIdentities {
		hidden := len(identities) - maxMetricErrorIdentities
		identities = append(identities[:maxMetricErrorIdentities], fmt.Sprintf("... %d more", hidden))
	}
	return strings.Join(identities, "; ")
}

func metricTimestampSummary(rm *metricspb.ResourceMetrics) string {
	var count int
	var minTs uint64
	var maxTs uint64
	for _, sm := range rm.GetScopeMetrics() {
		for _, metric := range sm.GetMetrics() {
			for _, ts := range metricDataPointTimestamps(metric) {
				if ts == 0 {
					continue
				}
				if count == 0 || ts < minTs {
					minTs = ts
				}
				if ts > maxTs {
					maxTs = ts
				}
				count++
			}
		}
	}
	if count == 0 {
		return "points=0"
	}
	return fmt.Sprintf("points=%d min_ts=%s max_ts=%s", count, formatUnixNanos(minTs), formatUnixNanos(maxTs))
}

func metricDataPointTimestamps(metric *metricspb.Metric) []uint64 {
	switch data := metric.GetData().(type) {
	case *metricspb.Metric_Gauge:
		out := make([]uint64, 0, len(data.Gauge.GetDataPoints()))
		for _, dp := range data.Gauge.GetDataPoints() {
			out = append(out, dp.GetTimeUnixNano())
		}
		return out
	case *metricspb.Metric_Sum:
		out := make([]uint64, 0, len(data.Sum.GetDataPoints()))
		for _, dp := range data.Sum.GetDataPoints() {
			out = append(out, dp.GetTimeUnixNano())
		}
		return out
	case *metricspb.Metric_Histogram:
		out := make([]uint64, 0, len(data.Histogram.GetDataPoints()))
		for _, dp := range data.Histogram.GetDataPoints() {
			out = append(out, dp.GetTimeUnixNano())
		}
		return out
	case *metricspb.Metric_ExponentialHistogram:
		out := make([]uint64, 0, len(data.ExponentialHistogram.GetDataPoints()))
		for _, dp := range data.ExponentialHistogram.GetDataPoints() {
			out = append(out, dp.GetTimeUnixNano())
		}
		return out
	case *metricspb.Metric_Summary:
		out := make([]uint64, 0, len(data.Summary.GetDataPoints()))
		for _, dp := range data.Summary.GetDataPoints() {
			out = append(out, dp.GetTimeUnixNano())
		}
		return out
	default:
		return nil
	}
}

// unixNanosToTime converts OTLP unix-nanos to a UTC time. ok is false when ts
// overflows int64 (time.Unix takes a signed offset), letting callers skip or
// fall back instead of wrapping into a bogus date.
func unixNanosToTime(ts uint64) (t time.Time, ok bool) {
	if ts > uint64(1<<63-1) {
		return time.Time{}, false
	}
	return time.Unix(0, int64(ts)).UTC(), true
}

func formatUnixNanos(ts uint64) string {
	t, ok := unixNanosToTime(ts)
	if !ok {
		return fmt.Sprintf("%d", ts)
	}
	return t.Format(time.RFC3339Nano)
}

func stringAttr(attrs []*commonpb.KeyValue, key string) string {
	for _, kv := range attrs {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}
