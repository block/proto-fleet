package models

import (
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
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
