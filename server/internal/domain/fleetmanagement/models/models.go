package models

import (
	"log/slog"

	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MinerTelemetry represents the telemetry data for a miner
type MinerTelemetry struct {
	PowerUsage  []*commonpb.Measurement
	Temperature []*commonpb.Measurement
	Hashrate    []*commonpb.Measurement
	Efficiency  []*commonpb.Measurement
	Timestamp   *timestamppb.Timestamp
}

// SetMeasurements sets measurements for a given measurement type.
func (t *MinerTelemetry) SetMeasurements(measurementType pb.MeasurementConfig_MeasurementType, measurements []*commonpb.Measurement) {
	switch measurementType {
	case pb.MeasurementConfig_MEASUREMENT_TYPE_POWER_USAGE:
		t.PowerUsage = measurements
	case pb.MeasurementConfig_MEASUREMENT_TYPE_TEMPERATURE:
		t.Temperature = measurements
	case pb.MeasurementConfig_MEASUREMENT_TYPE_HASHRATE:
		t.Hashrate = measurements
	case pb.MeasurementConfig_MEASUREMENT_TYPE_EFFICIENCY:
		t.Efficiency = measurements
	case pb.MeasurementConfig_MEASUREMENT_TYPE_UNSPECIFIED:
		// Unspecified type is not mapped - ignore
	default:
		slog.Warn("Unknown measurement type", "measurement_type", measurementType)
		// Unknown measurement type - ignore for forward compatibility
	}
}
