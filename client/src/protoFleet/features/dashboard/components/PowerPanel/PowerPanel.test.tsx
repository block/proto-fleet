import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { PowerPanel } from "./PowerPanel";
import {
  AggregatedValueSchema,
  AggregationType,
  MeasurementType,
  type Metric,
  MetricSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import useFleetCounts from "@/protoFleet/api/useFleetCounts";
import { usePanelMetrics } from "@/protoFleet/store";

// Mock the API hooks
vi.mock("@/protoFleet/api/useFleetCounts", () => ({
  default: vi.fn(),
}));

// Mock the store hooks
vi.mock("@/protoFleet/store", () => ({
  usePanelMetrics: vi.fn(),
}));

const mockUseFleetCounts = vi.mocked(useFleetCounts);
const mockUsePanelMetrics = vi.mocked(usePanelMetrics);

// Helper function to create mock Metric with device count
const createMockMetric = (avgValue: number, deviceCount: number): Metric => {
  return create(MetricSchema, {
    measurementType: MeasurementType.POWER,
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

describe("PowerPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows subtitle when not all miners are reporting", () => {
    const metrics = [createMockMetric(1500, 3)];

    mockUsePanelMetrics.mockReturnValue(metrics);

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<PowerPanel duration={"1h"} />);

    expect(screen.getByText("3 of 5 miners reporting")).toBeInTheDocument();
  });

  it("hides subtitle when all miners are reporting", () => {
    const metrics = [createMockMetric(1500, 5)];

    mockUsePanelMetrics.mockReturnValue(metrics);

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<PowerPanel duration={"1h"} />);

    expect(screen.queryByText(/miners reporting/)).not.toBeInTheDocument();
  });

  it("hides subtitle when device count is null", () => {
    // No metrics, so device count will be null
    mockUsePanelMetrics.mockReturnValue([]);

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<PowerPanel duration={"1h"} />);

    expect(screen.queryByText(/miners reporting/)).not.toBeInTheDocument();
  });

  it("shows subtitle with zero miners reporting", () => {
    const metrics = [createMockMetric(0, 0)];

    mockUsePanelMetrics.mockReturnValue(metrics);

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<PowerPanel duration={"1h"} />);

    expect(screen.getByText("0 of 5 miners reporting")).toBeInTheDocument();
  });

  it("renders loading state without subtitle", () => {
    // undefined = not loaded yet (loading state)
    mockUsePanelMetrics.mockReturnValue(undefined);

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<PowerPanel duration={"1h"} />);

    expect(screen.queryByText(/miners reporting/)).not.toBeInTheDocument();
  });
});
