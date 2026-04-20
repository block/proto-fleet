import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import { UptimePanel } from "./UptimePanel";
import { type UptimeStatusCount, UptimeStatusCountSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

// Mock react-router-dom
const mockNavigate = vi.fn();
vi.mock("react-router-dom", () => ({
  useNavigate: () => mockNavigate,
}));

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

describe("UptimePanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockNavigate.mockClear();
  });

  it("renders loading state", () => {
    // undefined = not loaded yet (loading state)
    render(<UptimePanel duration={"24h"} uptimeStatusCounts={undefined} />);

    // Check for skeleton loading state
    expect(screen.getByText("Uptime")).toBeInTheDocument();
  });

  it("renders with all miners hashing", () => {
    // Use timestamp from 1 hour ago to ensure it's before chart intervals
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 5, 0),
    ];

    render(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);

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
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 4, 1),
    ];

    render(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);

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
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 3, 2),
    ];

    render(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);

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
    const uptimeStatusCountsAllHashing: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 5, 0),
    ];

    const { rerender } = render(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCountsAllHashing} />);

    // Should not show button when count is 0
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    // Update with not hashing miners
    const uptimeStatusCountsWithNotHashing: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 4, 1),
    ];

    rerender(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCountsWithNotHashing} />);

    // Should show button with count when not hashing > 0
    expect(screen.getByRole("button")).toBeInTheDocument();
    expect(screen.getByText("1 miner")).toBeInTheDocument();
    expect(screen.getByText("20% not hashing")).toBeInTheDocument();
  });

  it("handles empty data", () => {
    render(<UptimePanel duration={"24h"} uptimeStatusCounts={[]} />);

    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("No data")).toBeInTheDocument();
  });

  it("handles different duration props", () => {
    // Use timestamp from 3 days ago to work with all durations including 5d
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3 * 24 * 3600, 5, 0),
    ];

    const { rerender } = render(<UptimePanel duration={"1h"} uptimeStatusCounts={uptimeStatusCounts} />);
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration={"7d"} uptimeStatusCounts={uptimeStatusCounts} />);
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration={"30d"} uptimeStatusCounts={uptimeStatusCounts} />);
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();
  });

  it("navigates to miners page with filters when clicking not hashing button", async () => {
    const user = userEvent.setup();
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 4, 1),
    ];

    render(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);

    // Find and click the "1 miner" button
    const button = screen.getByRole("button", { name: /1 miner/i });
    await user.click(button);

    // Verify navigate was called with the correct URL
    expect(mockNavigate).toHaveBeenCalledWith("/miners?status=offline,sleeping,needs-attention");
  });
});
