import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Hardware from "./Hardware";

// Mock the hardware API hook
const mockUseHardware = vi.fn();
// Mock the cooling status API hook
const mockUseCoolingStatus = vi.fn();
vi.mock("@/protoOS/api", () => ({
  useHardware: () => mockUseHardware(),
  useCoolingStatus: () => mockUseCoolingStatus(),
}));

// Mock the cooling mode store hook
const mockUseCoolingMode = vi.fn();
vi.mock("@/protoOS/store", () => ({
  useCoolingMode: () => mockUseCoolingMode(),
}));

describe("Hardware", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    // Default mock implementations
    mockUseCoolingMode.mockReturnValue("Auto");
    mockUseHardware.mockReturnValue({
      hashboardsInfo: [{ board: "1234", hb_sn: "PM-123456789" }],
      controlBoardInfo: { board_id: "3", serial_number: "CB-123456789" },
      fansInfo: [{ slot: 1 }],
      psusInfo: [{ model: "1234", psu_sn: "PSU-123456789" }],
      pending: false,
      error: null,
    });
    // Default: fans are running (RPM > 0)
    mockUseCoolingStatus.mockReturnValue({
      data: {
        fans: [{ slot: 1, rpm: 1200 }],
      },
    });
  });

  describe("Fans section", () => {
    it("renders fans table when fans are connected", () => {
      mockUseHardware.mockReturnValue({
        hashboardsInfo: [],
        controlBoardInfo: null,
        fansInfo: [{ slot: 1 }, { slot: 2 }],
        psusInfo: [],
        pending: false,
        error: null,
      });
      mockUseCoolingMode.mockReturnValue("Auto");
      // Fans with RPM > 0 means they're connected
      mockUseCoolingStatus.mockReturnValue({
        data: {
          fans: [
            { slot: 1, rpm: 1200 },
            { slot: 2, rpm: 1150 },
          ],
        },
      });

      render(<Hardware />);

      expect(screen.getByText("Fan 1")).toBeInTheDocument();
      expect(screen.getByText("Fan 2")).toBeInTheDocument();
      expect(screen.queryByText("No fans connected")).not.toBeInTheDocument();
    });

    it("shows 'No component found' for individual missing fans in air cooling mode", () => {
      mockUseHardware.mockReturnValue({
        hashboardsInfo: [],
        controlBoardInfo: null,
        fansInfo: [{ slot: 1 }, null, { slot: 3 }],
        psusInfo: [],
        pending: false,
        error: null,
      });
      mockUseCoolingMode.mockReturnValue("Auto");
      // Some fans have RPM > 0, so not all disconnected
      mockUseCoolingStatus.mockReturnValue({
        data: {
          fans: [
            { slot: 1, rpm: 1200 },
            { slot: 2, rpm: 0 }, // This one is missing/disconnected
            { slot: 3, rpm: 1180 },
          ],
        },
      });

      render(<Hardware />);

      expect(screen.getByText("Fan 1")).toBeInTheDocument();
      expect(screen.getByText("Fan 3")).toBeInTheDocument();
      expect(screen.getByText("No component found")).toBeInTheDocument();
      expect(screen.queryByText("No fans connected")).not.toBeInTheDocument();
    });

    it("shows callout when no fans are connected and in immersion mode", () => {
      mockUseHardware.mockReturnValue({
        hashboardsInfo: [],
        controlBoardInfo: null,
        fansInfo: [null, null, null],
        psusInfo: [],
        pending: false,
        error: null,
      });
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

      render(<Hardware />);

      expect(screen.getByText("No fans connected")).toBeInTheDocument();
      expect(screen.getByText("This miner is set to immersion cooling")).toBeInTheDocument();
      // Fan table should not be visible (no "Fan X" entries)
      expect(screen.queryByText(/^Fan \d+$/)).not.toBeInTheDocument();
    });

    it("does not show callout when no fans but in air cooling mode", () => {
      mockUseHardware.mockReturnValue({
        hashboardsInfo: [],
        controlBoardInfo: null,
        fansInfo: [null, null, null],
        psusInfo: [],
        pending: false,
        error: null,
      });
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

      render(<Hardware />);

      expect(screen.queryByText("No fans connected")).not.toBeInTheDocument();
      expect(screen.queryByText("This miner is set to immersion cooling")).not.toBeInTheDocument();
      // Should show the table with "No component found" instead
      expect(screen.getAllByText("No component found")).toHaveLength(3);
    });

    it("does not show callout when fans are connected even in immersion mode", () => {
      mockUseHardware.mockReturnValue({
        hashboardsInfo: [],
        controlBoardInfo: null,
        fansInfo: [{ slot: 1 }, { slot: 2 }],
        psusInfo: [],
        pending: false,
        error: null,
      });
      mockUseCoolingMode.mockReturnValue("Off");
      // At least one fan has RPM > 0 → fans ARE connected, so don't show callout
      mockUseCoolingStatus.mockReturnValue({
        data: {
          fans: [
            { slot: 1, rpm: 1200 },
            { slot: 2, rpm: 1150 },
          ],
        },
      });

      render(<Hardware />);

      expect(screen.queryByText("No fans connected")).not.toBeInTheDocument();
      expect(screen.queryByText("This miner is set to immersion cooling")).not.toBeInTheDocument();
      expect(screen.getByText("Fan 1")).toBeInTheDocument();
      expect(screen.getByText("Fan 2")).toBeInTheDocument();
    });
  });

  describe("Loading and error states", () => {
    it("shows loading spinner when pending", () => {
      mockUseHardware.mockReturnValue({
        hashboardsInfo: null,
        controlBoardInfo: null,
        fansInfo: null,
        psusInfo: null,
        pending: true,
        error: null,
      });

      render(<Hardware />);

      expect(screen.getByText("Hardware")).toBeInTheDocument();
      // ProgressCircular is rendered
      expect(screen.queryByText("Control Board")).not.toBeInTheDocument();
    });

    it("shows error state when there is an error", () => {
      mockUseHardware.mockReturnValue({
        hashboardsInfo: null,
        controlBoardInfo: null,
        fansInfo: null,
        psusInfo: null,
        pending: false,
        error: { message: "Failed to load" },
      });

      render(<Hardware />);

      expect(screen.getByText("Hardware")).toBeInTheDocument();
      expect(screen.getByText("Could not load hardware details")).toBeInTheDocument();
    });
  });
});
