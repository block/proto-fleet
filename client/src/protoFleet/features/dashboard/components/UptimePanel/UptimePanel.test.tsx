import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import { UptimePanel } from "./UptimePanel";
import { type UptimeStatusCount, UptimeStatusCountSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

const mockNavigate = vi.fn();
vi.mock("react-router-dom", () => ({
  useNavigate: () => mockNavigate,
}));

const createMockUptimeStatusCount = (
  timestampSeconds: number,
  hashingCount: number,
  notHashingCount: number,
  brokenCount = 0,
): UptimeStatusCount => {
  return create(UptimeStatusCountSchema, {
    timestamp: {
      seconds: BigInt(timestampSeconds),
      nanos: 0,
    },
    hashingCount,
    notHashingCount,
    brokenCount,
  });
};

describe("UptimePanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockNavigate.mockClear();
  });

  it("renders loading state", () => {
    // Arrange / Act
    render(<UptimePanel duration={"24h"} uptimeStatusCounts={undefined} />);

    // Assert
    expect(screen.getByText("Uptime")).toBeInTheDocument();
  });

  it("renders with all miners hashing", () => {
    // Arrange
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 5, 0, 0),
    ];

    // Act
    render(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);

    // Assert
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();
    expect(screen.getByText("Not hashing")).toBeInTheDocument();
    expect(screen.getByText("Degraded")).toBeInTheDocument();
    expect(screen.getByText("Healthy")).toBeInTheDocument();
    expect(screen.getByText("100% of fleet")).toBeInTheDocument();
    // Buttons should not be shown when their counts are 0.
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("renders with some miners not hashing", () => {
    // Arrange
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 4, 1, 0),
    ];

    // Act
    render(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);

    // Assert
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("20% not hashing")).toBeInTheDocument();
    expect(screen.getByText("Not hashing")).toBeInTheDocument();
    expect(screen.getByText("Healthy")).toBeInTheDocument();
    // Only the non-zero "not hashing" button should be shown.
    expect(screen.getByRole("button")).toBeInTheDocument();
    expect(screen.getByText("1 miner")).toBeInTheDocument();
  });

  it("renders a degraded (needs-attention) drill-through button", async () => {
    // Arrange
    const user = userEvent.setup();
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 7, 0, 3),
    ];

    render(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);

    // Act
    const button = screen.getByRole("button", { name: /3 miners/i });
    await user.click(button);

    // Assert
    expect(screen.getByText("30% need attention")).toBeInTheDocument();
    expect(mockNavigate).toHaveBeenCalledTimes(1);
    expect(mockNavigate.mock.calls[0][0]).toMatch(/^\/miners\?.*status=needs-attention/);
  });

  it("handles empty data", () => {
    // Act
    render(<UptimePanel duration={"24h"} uptimeStatusCounts={[]} />);

    // Assert
    expect(screen.getByText("Uptime")).toBeInTheDocument();
    expect(screen.getByText("No data")).toBeInTheDocument();
  });

  it("handles different duration props", () => {
    // Arrange
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3 * 24 * 3600, 5, 0, 0),
    ];

    // Act / Assert
    const { rerender } = render(<UptimePanel duration={"1h"} uptimeStatusCounts={uptimeStatusCounts} />);
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration={"7d"} uptimeStatusCounts={uptimeStatusCounts} />);
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();

    rerender(<UptimePanel duration={"30d"} uptimeStatusCounts={uptimeStatusCounts} />);
    expect(screen.getByText("All miners hashing")).toBeInTheDocument();
  });

  it("navigates to the not-hashing miner filter from the not-hashing button", async () => {
    // Arrange
    const user = userEvent.setup();
    const uptimeStatusCounts: UptimeStatusCount[] = [
      createMockUptimeStatusCount(Math.floor(Date.now() / 1000) - 3600, 4, 1, 0),
    ];

    render(<UptimePanel duration={"24h"} uptimeStatusCounts={uptimeStatusCounts} />);

    // Act
    const button = screen.getByRole("button", { name: /1 miner/i });
    await user.click(button);

    // Assert
    expect(mockNavigate).toHaveBeenCalledTimes(1);
    const url = new URL(mockNavigate.mock.calls[0][0], "http://dummy");
    expect(url.pathname).toBe("/miners");
    expect(url.searchParams.getAll("status").sort()).toEqual(["offline", "sleeping"]);
  });
});
