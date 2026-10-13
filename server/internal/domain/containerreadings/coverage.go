// Package containerreadings projects saved physical measurement coverage.
package containerreadings

import (
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func Coverage(r *sdk.ContainerRollup) *commonpb.EquipmentCoverage {
	if r == nil {
		return nil
	}
	return &commonpb.EquipmentCoverage{Hashrate: metricCoverage(r.Hashrate), Power: metricCoverage(r.Power), Temperature: metricCoverage(r.Temperature), PowerSource: r.PowerSource, Attribution: r.Attribution}
}
func metricCoverage(c sdk.MetricCoverage) *commonpb.MeasurementCoverage {
	out := &commonpb.MeasurementCoverage{Reporting: c.Reporting, Expected: c.Expected, TopologyComplete: c.TopologyComplete}
	if !c.OldestAt.IsZero() {
		out.OldestAt = timestamppb.New(c.OldestAt)
	}
	if !c.NewestAt.IsZero() {
		out.NewestAt = timestamppb.New(c.NewestAt)
	}
	return out
}
