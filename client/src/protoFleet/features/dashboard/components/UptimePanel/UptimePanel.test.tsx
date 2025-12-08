import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import { UptimePanel } from "./UptimePanel";
import {
  type GetCombinedMetricsResponse,
  GetCombinedMetricsResponseSchema,
  type StreamCombinedMetricUpdatesResponse,
  StreamCombinedMetricUpdatesResponseSchema,
  type UptimeStatusCount,
  UptimeStatusCountSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

// Mock the API hooks
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
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";

const mockUseTelemetryMetrics = vi.mocked(useTelemetryMetrics);
const mockUseStreamingTelemetryMetrics = vi.mocked(useStreamingTelemetryMetrics);

// Helper function to create proper UptimeStatusCount objects
const createMockUptimeStatusCount = (
  timestampSeconds: number,
  hashingCount: number,
  notHashingCount: number,
): UptimeStatusCount => {
  return create(UptimeStatusCountSchema, {
    timestamp: {
      seconds: BigInt(timestampSeconds),
      nanos: 0,
    },
    hashingCount,
    notHashingCount,
  });
};

// Helper function to create proper GetCombinedMetricsResponse objects
const createMockResponse = (uptimeStatusCounts: UptimeStatusCount[]): GetCombinedMetricsResponse => {
  return create(GetCombinedMetricsResponseSchema, {
    uptimeStatusCounts,
  });
};

// Helper function to create proper StreamCombinedMetricUpdatesResponse objects
const createMockStreamingResponse = (uptimeStatusCounts: UptimeStatusCount[]): StreamCombinedMetricUpdatesResponse => {
  return create(StreamCombinedMetricUpdatesResponseSchema, {
    uptimeStatusCounts,
  });
};

describe("UptimePanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockNavigate.mockClear();
  });

  it("renders loading state", () => {
    mockUseTelemetryMetrics.mockReturnValue({
      data: null,
      isLoading: true,
      error: null,
      refetch: vi.fn(),
    });

    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: null,
      isStreaming: false,
    });

    render(<UptimePanel duration="24h" />);

    // Check for skeleton loading state
    expect(screen.getByText("Uptime")).toBeInTheDocument();
  });

  it("renders with all miners hashing", () => {
    // Use timestamp from 1 hour ago to ensure it's before chart intervals
    const mockData: UptimeStatusCount[] = [createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 5, 0)];

    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(mockData),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: null,
      isStreaming: false,
    });

    render(<UptimePanel duration="24h" />);

    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();
    expect(screen.getByText("Not hashing")).toBeInTheDocument();
    expect(screen.getByText("Hashing")).toBeInTheDocument();
    expect(screen.getByText("0% of fleet")).toBeInTheDocument();
    expect(screen.getByText("100% of fleet")).toBeInTheDocument();
    // Button should not be shown when all miners are hashing
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("renders with some miners not hashing", () => {
    const mockData: UptimeStatusCount[] = [createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 4, 1)];

    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(mockData),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: null,
      isStreaming: false,
    });

    render(<UptimePanel duration="24h" />);

    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("20% not hashing")).toBeInTheDocument();
    expect(screen.getByText("Not hashing")).toBeInTheDocument();
    expect(screen.getByText("Hashing")).toBeInTheDocument();
    expect(screen.getByText("20% of fleet")).toBeInTheDocument();
    expect(screen.getByText("80% of fleet")).toBeInTheDocument();
    // Button should show with singular "miner"
    expect(screen.getByRole("button")).toBeInTheDocument();
    expect(screen.getByText("1 miner")).toBeInTheDocument();
  });

  it("renders with multiple miners not hashing", () => {
    const mockData: UptimeStatusCount[] = [createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 3, 2)];

    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(mockData),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: null,
      isStreaming: false,
    });

    render(<UptimePanel duration="24h" />);

    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("40% not hashing")).toBeInTheDocument();
    expect(screen.getByText("Not hashing")).toBeInTheDocument();
    expect(screen.getByText("Hashing")).toBeInTheDocument();
    expect(screen.getByText("40% of fleet")).toBeInTheDocument();
    expect(screen.getByText("60% of fleet")).toBeInTheDocument();
    // Button should show with plural "miners"
    expect(screen.getByRole("button")).toBeInTheDocument();
    expect(screen.getByText("2 miners")).toBeInTheDocument();
  });

  it("shows button only when not hashing count > 0", () => {
    const mockDataAllHashing: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 5, 0),
    ];

    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(mockDataAllHashing),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: null,
      isStreaming: false,
    });

    const { rerender } = render(<UptimePanel duration="24h" />);

    // Should not show button when count is 0
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    // Update with not hashing miners
    const mockDataWithNotHashing: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 4, 1),
    ];

    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(mockDataWithNotHashing),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    rerender(<UptimePanel duration="24h" />);

    // Should show button with count when not hashing > 0
    expect(screen.getByRole("button")).toBeInTheDocument();
    expect(screen.getByText("1 miner")).toBeInTheDocument();
    expect(screen.getByText("20% not hashing")).toBeInTheDocument();
  });

  it("merges streaming data with initial data", () => {
    const initialData: UptimeStatusCount[] = [createMockUptimeStatusCount(1000, 5, 0)];

    const streamingData: UptimeStatusCount[] = [createMockUptimeStatusCount(2000, 4, 1)];

    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(initialData),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: createMockStreamingResponse(streamingData),
      isStreaming: true,
    });

    render(<UptimePanel duration="24h" />);

    // Should show the latest (streaming) data
    expect(screen.getByText("20% not hashing")).toBeInTheDocument();
    expect(screen.getByText("20% of fleet")).toBeInTheDocument();
    expect(screen.getByText("80% of fleet")).toBeInTheDocument();
    expect(screen.getByText("1 miner")).toBeInTheDocument();
  });

  it("handles empty data", () => {
    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse([]),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: null,
      isStreaming: false,
    });

    render(<UptimePanel duration="24h" />);

    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("No data")).toBeInTheDocument();
  });

  it("handles different duration props", () => {
    // Use timestamp from 3 days ago to work with all durations including 5d
    const mockData: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3 * 24 * 3600, 5, 0),
    ];

    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(mockData),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: null,
      isStreaming: false,
    });

    const { rerender } = render(<UptimePanel duration="1h" />);
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration="12h" />);
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration="24h" />);
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration="48h" />);
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration="5d" />);
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();
  });

  it("navigates to miners page with filters when clicking not hashing button", async () => {
    const user = userEvent.setup();
    const mockData: UptimeStatusCount[] = [createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 4, 1)];

    mockUseTelemetryMetrics.mockReturnValue({
      data: createMockResponse(mockData),
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    });

    mockUseStreamingTelemetryMetrics.mockReturnValue({
      latestData: null,
      isStreaming: false,
    });

    render(<UptimePanel duration="24h" />);

    // Find and click the "1 miner" button
    const button = screen.getByRole("button", { name: /1 miner/i });
    await user.click(button);

    // Verify navigate was called with the correct URL
    expect(mockNavigate).toHaveBeenCalledWith("/miners?status=offline,sleeping,needs-attention");
  });
});
