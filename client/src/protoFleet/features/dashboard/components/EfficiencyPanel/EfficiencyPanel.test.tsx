import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { EfficiencyPanel } from "./EfficiencyPanel";
import {
  AggregatedValueSchema,
  AggregationType,
  MeasurementType,
  type Metric,
  MetricSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

// Helper function to create mock Metric with device count
const createMockMetric = (avgValue: number, deviceCount: number): Metric => {
  return create(MetricSchema, {
    measurementType: MeasurementType.EFFICIENCY,
    openTime: {
      seconds: BigInt(Math.floor(Date.now() / 1000)),
      nanos: 0,
    },
    aggregatedValues: [
      create(AggregatedValueSchema, {
        aggregationType: AggregationType.AVERAGE,
        value: avgValue,
      }),
    ],
    deviceCount,
  });
};

describe("EfficiencyPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows subtitle when not all miners are reporting", () => {
    const metrics = [createMockMetric(25.5, 3)];

    render(<EfficiencyPanel duration={"1h"} metrics={metrics} totalMiners={5} />);

    expect(screen.getByText("3 of 5 miners reporting")).toBeInTheDocument();
  });

  it("hides subtitle when all miners are reporting", () => {
    const metrics = [createMockMetric(25.5, 5)];

    render(<EfficiencyPanel duration={"1h"} metrics={metrics} totalMiners={5} />);

    expect(screen.queryByText(/miners reporting/)).not.toBeInTheDocument();
  });

  it("hides subtitle when device count is null", () => {
    // No metrics, so device count will be null
    render(<EfficiencyPanel duration={"1h"} metrics={[]} totalMiners={5} />);

    expect(screen.queryByText(/miners reporting/)).not.toBeInTheDocument();
  });

  it("shows subtitle with zero miners reporting", () => {
    const metrics = [createMockMetric(0, 0)];

    render(<EfficiencyPanel duration={"1h"} metrics={metrics} totalMiners={5} />);

    expect(screen.getByText("0 of 5 miners reporting")).toBeInTheDocument();
  });

  it("uses max device count across buckets, not the last bucket", () => {
    // Arrange — first bucket has 5 devices, second (incomplete) bucket has only 3
    const metrics = [createMockMetric(25.5, 5), createMockMetric(24.0, 3)];

    // Act
    render(<EfficiencyPanel duration={"1h"} metrics={metrics} totalMiners={7} />);

    // Assert — subtitle should reflect the max (5), not the last bucket (3)
    expect(screen.getByText("5 of 7 miners reporting")).toBeInTheDocument();
  });

  it("renders loading state without subtitle", () => {
    // undefined = not loaded yet (loading state)
    render(<EfficiencyPanel duration={"1h"} metrics={undefined} totalMiners={5} />);

    expect(screen.queryByText(/miners reporting/)).not.toBeInTheDocument();
  });
});
