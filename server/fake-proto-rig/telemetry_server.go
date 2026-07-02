// Fake of the on-rig telemetry-service (MinerTelemetryApi): streams jittered
// OTLP batches on its own port (2123) so REST-only consumers are unaffected.
package main

import (
	"fmt"
	"log"
	"net"
	"time"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/block/proto-fleet/server/fake-proto-rig/internal/rigapi/minertelemetry"
)

const (
	defaultTelemetryGRPCPort       = 2123
	defaultTelemetryPublishSeconds = 10
)

type telemetryServer struct {
	minertelemetry.UnimplementedMinerTelemetryApiServer

	state           *MinerState
	publishInterval time.Duration
}

// StreamMetrics emits an immediate snapshot, then a fresh batch every
// publish interval, matching the real telemetry-service contract.
func (s *telemetryServer) StreamMetrics(
	_ *minertelemetry.StreamMetricsRequest,
	stream minertelemetry.MinerTelemetryApi_StreamMetricsServer,
) error {
	send := func() error {
		payload, err := s.buildOTLPPayload()
		if err != nil {
			return err
		}
		return stream.Send(&minertelemetry.MetricsBatch{
			OtlpPayload: payload,
			EmittedAt:   timestamppb.Now(),
		})
	}

	if err := send(); err != nil {
		return err
	}
	ticker := time.NewTicker(s.publishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return fmt.Errorf("stream context: %w", stream.Context().Err())
		case <-ticker.C:
			if err := send(); err != nil {
				return err
			}
		}
	}
}

// StreamLogs holds the stream open without emitting, like a real rig with
// no matching entries; Unimplemented would reconnect-loop log workers.
func (s *telemetryServer) StreamLogs(
	_ *minertelemetry.StreamLogsRequest,
	stream minertelemetry.MinerTelemetryApi_StreamLogsServer,
) error {
	<-stream.Context().Done()
	return fmt.Errorf("stream context: %w", stream.Context().Err())
}

// buildOTLPPayload encodes a representative subset of a real rig's metrics:
// mcdd mining gauges plus per-hashboard ASIC aggregates.
func (s *telemetryServer) buildOTLPPayload() ([]byte, error) {
	nanos := time.Now().UnixNano()
	if nanos < 0 {
		nanos = 0
	}
	now := uint64(nanos) // #nosec G115 -- clamped non-negative above.

	// Honor the ERROR_TEMPERATURE injection so error scenarios are visible
	// through the telemetry stream, not just the REST API.
	asicTemp := defaultASICTemperature
	if s.state != nil && s.state.ErrorConfig.OverrideTemperature > 0 {
		asicTemp = s.state.ErrorConfig.OverrideTemperature
	}

	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			resourceGauges("mcdd", "", now, map[string]float64{
				"miner_hashrate":                 applyVariation(defaultHashrateTHS, 0.03),
				"miner_power_watts":              applyVariation(defaultPowerW, 0.02),
				"miner_efficiency":               applyVariation(defaultEfficiencyJTH, 0.02),
				"miner_asic_temperature_celsius": applyVariation(asicTemp, 0.05),
				"miner_state":                    3, // Mining
			}),
		},
	}
	for slot := 1; slot <= defaultHashboardCount; slot++ {
		req.ResourceMetrics = append(req.ResourceMetrics, resourceGauges(
			"hashboard-service", fmt.Sprintf("%d", slot), now, map[string]float64{
				"hashboard_asic_temperature_celsius": applyVariation(asicTemp, 0.05),
				"hashboard_asic_hashrate":            applyVariation(defaultHashboardHashrate, 0.03),
			}))
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal OTLP payload: %w", err)
	}
	return payload, nil
}

func resourceGauges(serviceName, instanceID string, tsNanos uint64, gauges map[string]float64) *metricspb.ResourceMetrics {
	attrs := []*commonpb.KeyValue{strAttr("service.name", serviceName)}
	if instanceID != "" {
		attrs = append(attrs, strAttr("service.instance.id", instanceID))
	}
	metrics := make([]*metricspb.Metric, 0, len(gauges))
	for name, value := range gauges {
		metrics = append(metrics, &metricspb.Metric{
			Name: name,
			Data: &metricspb.Metric_Gauge{
				Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{{
						TimeUnixNano: tsNanos,
						Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: value},
					}},
				},
			},
		})
	}
	return &metricspb.ResourceMetrics{
		Resource:     &resourcepb.Resource{Attributes: attrs},
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: metrics}},
	}
}

func strAttr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}},
	}
}

// startTelemetryGRPCServer starts the fake telemetry listener; it lives
// for the process lifetime, like a real rig's until power-off.
func startTelemetryGRPCServer(state *MinerState, port int, publishInterval time.Duration) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on telemetry port %d: %w", port, err)
	}

	server := grpc.NewServer()
	minertelemetry.RegisterMinerTelemetryApiServer(server, &telemetryServer{
		state:           state,
		publishInterval: publishInterval,
	})

	go func() {
		if err := server.Serve(listener); err != nil {
			log.Printf("telemetry gRPC server error: %v", err)
		}
	}()
	log.Printf("Telemetry gRPC server listening on :%d (publish interval %s)", port, publishInterval)
	return nil
}
