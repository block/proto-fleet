import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { EfficiencyPanel } from "./EfficiencyPanel";
import {
  AggregatedValueSchema,
  AggregationType,
  type GetCombinedMetricsResponse,
  GetCombinedMetricsResponseSchema,
  MeasurementType,
  type Metric,
  MetricSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

// Mock the API hooks
vi.mock("@/protoFleet/api/useFleetCounts", () => ({
  default: vi.fn(),
}));

vi.mock("@/protoFleet/api/useStreamingTelemetryMetrics", () => ({
  useStreamingTelemetryMetrics: vi.fn(),
}));

vi.mock("@/protoFleet/api/useTelemetryMetrics", () => ({
  useTelemetryMetrics: vi.fn(),
}));

// Mock react-router-dom
const mockNavigate = vi.fn();
vi.mock("react-router-dom", () => ({
  useNavigate: () => mockNavigate,
}));

// Import mocked modules
import useFleetCounts from "@/protoFleet/api/useFleetCounts";
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";

const mockUseTelemetryMetrics = vi.mocked(useTelemetryMetrics);
const mockUseStreamingTelemetryMetrics = vi.mocked(useStreamingTelemetryMetrics);
const mockUseFleetCounts = vi.mocked(useFleetCounts);

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

const createMockResponse = (metrics: Metric[]): GetCombinedMetricsResponse => {
  return create(GetCombinedMetricsResponseSchema, {
    metrics,
  });
};

describe("EfficiencyPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockNavigate.mockClear();

    // Default mock for streaming (no data)
    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: null,
      isStreaming: false,
    });
  });

  it("shows subtitle when not all miners are reporting", () => {
    const metrics = [createMockMetric(25.5, 3)];
    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(metrics),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<EfficiencyPanel duration="1h" />);

    expect(screen.getByText("3 of 5 miners reporting")).toBeInTheDocument();
  });

  it("hides subtitle when all miners are reporting", () => {
    const metrics = [createMockMetric(25.5, 5)];
    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(metrics),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<EfficiencyPanel duration="1h" />);

    expect(screen.queryByText(/miners reporting/)).not.toBeInTheDocument();
  });

  it("hides subtitle when device count is null", () => {
    // No metrics, so device count will be null
    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse([]),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<EfficiencyPanel duration="1h" />);

    expect(screen.queryByText(/miners reporting/)).not.toBeInTheDocument();
  });

  it("shows subtitle with zero miners reporting", () => {
    const metrics = [createMockMetric(0, 0)];
    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(metrics),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<EfficiencyPanel duration="1h" />);

    expect(screen.getByText("0 of 5 miners reporting")).toBeInTheDocument();
  });

  it("renders loading state without subtitle", () => {
    mockUseTelemetryMetrics.mockReturnValue({
      data: null,
      isLoading: true,
      error: null,
      refetch: vi.fn(),
    });

    mockUseFleetCounts.mockReturnValue({
      totalMiners: 5,
      stateCounts: undefined,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: vi.fn(),
    });

    render(<EfficiencyPanel duration="1h" />);

    expect(screen.queryByText(/miners reporting/)).not.toBeInTheDocument();
  });
});
