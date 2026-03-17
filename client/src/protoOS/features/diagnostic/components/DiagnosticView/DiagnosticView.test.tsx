import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DiagnosticView from "./DiagnosticView";

// Mock the telemetry hook
const mockUseTelemetry = vi.fn();
// Mock the cooling status API hook
const mockUseCoolingStatus = vi.fn();
const mockUseMiningStart = vi.fn();
const mockUseMiningStatus = vi.fn();
vi.mock("@/protoOS/api", () => ({
  useTelemetry: () => mockUseTelemetry(),
  useCoolingStatus: () => mockUseCoolingStatus(),
  useMiningStart: () => mockUseMiningStart(),
  useMiningStatus: () => mockUseMiningStatus(),
  TOTAL_FAN_SLOTS: 3,
  TOTAL_PSU_SLOTS: 3,
}));

// Mock the store hooks
const mockUseFanIds = vi.fn();
const mockUseCoolingMode = vi.fn();
const mockUseBayCount = vi.fn();
const mockUseHashboardSerialsByBay = vi.fn();
const mockUsePsuIds = vi.fn();
const mockUseSlotsPerBay = vi.fn();
const mockUseControlBoard = vi.fn();

vi.mock("@/protoOS/store", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/protoOS/store")>();
  return {
    ...actual,
    useFanIds: () => mockUseFanIds(),
    useCoolingMode: () => mockUseCoolingMode(),
    useBayCount: () => mockUseBayCount(),
    useHashboardSerialsByBay: () => mockUseHashboardSerialsByBay(),
    usePsuIds: () => mockUsePsuIds(),
    useSlotsPerBay: () => mockUseSlotsPerBay(),
    useControlBoard: () => mockUseControlBoard(),
  };
});

describe("DiagnosticView - Fans Section", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    // Default mock implementations
    mockUseTelemetry.mockReturnValue({});
    mockUseMiningStart.mockReturnValue({ startMining: vi.fn() });
    mockUseMiningStatus.mockReturnValue({ fetchData: vi.fn() });
    mockUseFanIds.mockReturnValue([1, 2, 3]);
    mockUseCoolingMode.mockReturnValue("Auto");
    mockUseBayCount.mockReturnValue(3);
    mockUseHashboardSerialsByBay.mockReturnValue({});
    mockUsePsuIds.mockReturnValue([1, 2, 3]);
    mockUseSlotsPerBay.mockReturnValue(3);
    mockUseControlBoard.mockReturnValue({ board_id: "3" });
    // Default: fans are running (RPM > 0)
    mockUseCoolingStatus.mockReturnValue({
      data: {
        fans: [
          { slot: 1, rpm: 1200 },
          { slot: 2, rpm: 1150 },
          { slot: 3, rpm: 1180 },
        ],
      },
    });
  });

  describe("No fans to display state", () => {
    it("shows 'No fans to display' when no fans are connected and in immersion mode", () => {
      mockUseFanIds.mockReturnValue([]);
      mockUseCoolingMode.mockReturnValue("Off");
      // All fans have RPM = 0 → no fans connected
      mockUseCoolingStatus.mockReturnValue({
        data: {
          fans: [
            { slot: 1, rpm: 0 },
            { slot: 2, rpm: 0 },
            { slot: 3, rpm: 0 },
          ],
        },
      });

      render(<DiagnosticView />);

      expect(screen.getByText("No fans to display")).toBeInTheDocument();
      expect(screen.getByText("This miner is set to immersion cooling.")).toBeInTheDocument();
      // Should not show fan cards
      expect(screen.queryByText(/^Fan \d+$/)).not.toBeInTheDocument();
    });

    it("does not show 'No fans to display' when fans are connected in immersion mode", () => {
      mockUseFanIds.mockReturnValue([1, 2, 3]);
      mockUseCoolingMode.mockReturnValue("Off");
      // At least one fan has RPM > 0 → fans ARE connected
      mockUseCoolingStatus.mockReturnValue({
        data: {
          fans: [
            { slot: 1, rpm: 1200 },
            { slot: 2, rpm: 1150 },
            { slot: 3, rpm: 1180 },
          ],
        },
      });

      render(<DiagnosticView />);

      expect(screen.queryByText("No fans to display")).not.toBeInTheDocument();
      expect(screen.queryByText("This miner is set to immersion cooling.")).not.toBeInTheDocument();
    });

    it("does not show 'No fans to display' when no fans are connected but in air cooling mode", () => {
      mockUseFanIds.mockReturnValue([]);
      mockUseCoolingMode.mockReturnValue("Auto");
      // All fans have RPM = 0 but in air cooling mode (not immersion)
      mockUseCoolingStatus.mockReturnValue({
        data: {
          fans: [
            { slot: 1, rpm: 0 },
            { slot: 2, rpm: 0 },
            { slot: 3, rpm: 0 },
          ],
        },
      });

      render(<DiagnosticView />);

      expect(screen.queryByText("No fans to display")).not.toBeInTheDocument();
      expect(screen.queryByText("This miner is set to immersion cooling.")).not.toBeInTheDocument();
      // Should show empty slots instead
      expect(screen.getByText("Fan 1")).toBeInTheDocument();
      expect(screen.getByText("Fan 2")).toBeInTheDocument();
    });

    it("shows fan cards when fans are connected in air cooling mode", () => {
      mockUseFanIds.mockReturnValue([1, 2, 3]);
      mockUseCoolingMode.mockReturnValue("Auto");
      // Fans with RPM > 0 means they're connected
      mockUseCoolingStatus.mockReturnValue({
        data: {
          fans: [
            { slot: 1, rpm: 1200 },
            { slot: 2, rpm: 1150 },
            { slot: 3, rpm: 1180 },
          ],
        },
      });

      render(<DiagnosticView />);

      expect(screen.queryByText("No fans to display")).not.toBeInTheDocument();
      expect(screen.queryByText("This miner is set to immersion cooling.")).not.toBeInTheDocument();
    });
  });
});
