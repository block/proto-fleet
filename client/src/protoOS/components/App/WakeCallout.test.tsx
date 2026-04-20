import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import WakeCallout from "./WakeCallout";

// Mock the cooling status API hook
const mockSetCooling = vi.fn();
const mockUseCoolingStatus = vi.fn();
vi.mock("@/protoOS/api/hooks/useCoolingStatus", () => ({
  useCoolingStatus: () => mockUseCoolingStatus(),
}));

// Mock the wake miner hook - use vi.hoisted to make variables available in factory
const { mockWakeMiner, mockUseWakeMiner } = vi.hoisted(() => ({
  mockWakeMiner: vi.fn(),
  mockUseWakeMiner: vi.fn(),
}));

vi.mock("@/protoOS/hooks/useWakeMiner", () => ({
  useWakeMiner: (params: any) => mockUseWakeMiner(params),
}));

// Mock the store hooks
const mockUseIsSleeping = vi.fn();
const mockUseCoolingMode = vi.fn();
const mockUseFansTelemetry = vi.fn();
vi.mock("@/protoOS/store", () => ({
  useIsSleeping: () => mockUseIsSleeping(),
  useCoolingMode: () => mockUseCoolingMode(),
  useFansTelemetry: () => mockUseFansTelemetry(),
}));

describe("WakeCallout", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    // Default mock implementations
    mockUseCoolingStatus.mockReturnValue({
      setCooling: mockSetCooling,
    });

    mockUseWakeMiner.mockImplementation(() => {
      return {
        wakeMiner: mockWakeMiner,
        shouldWake: false,
      };
    });

    mockUseIsSleeping.mockReturnValue(false);
    mockUseCoolingMode.mockReturnValue("Auto");
    mockUseFansTelemetry.mockReturnValue([]);
  });

  describe("Sleep Callout", () => {
    it("renders wake callout when miner is sleeping", () => {
      mockUseIsSleeping.mockReturnValue(true);

      render(<WakeCallout />);

      expect(screen.getByText("This miner is asleep and is not hashing.")).toBeInTheDocument();
      expect(screen.getByText("Wake up miner")).toBeInTheDocument();
    });

    it("does not render wake callout when miner is not sleeping", () => {
      mockUseIsSleeping.mockReturnValue(false);

      render(<WakeCallout />);

      expect(screen.queryByText("This miner is asleep and is not hashing.")).not.toBeInTheDocument();
    });

    it("calls wakeMiner when wake button is clicked and in air cooled mode", async () => {
      const user = userEvent.setup();
      mockUseIsSleeping.mockReturnValue(true);
      mockUseCoolingMode.mockReturnValue("Auto");
      mockUseFansTelemetry.mockReturnValue([]);

      render(<WakeCallout />);

      const wakeButton = screen.getByText("Wake up miner");
      await user.click(wakeButton);

      expect(mockWakeMiner).toHaveBeenCalledTimes(1);
    });
  });

  describe("FansDisabledDialog", () => {
    it("shows fans disabled dialog after waking in immersion mode with fans running", async () => {
      const user = userEvent.setup();
      mockUseIsSleeping.mockReturnValue(true);
      mockUseCoolingMode.mockReturnValue("Off");
      mockUseFansTelemetry.mockReturnValue([{ slot: 1, rpm: { latest: { value: 1000 } } }]);

      // Start with shouldWake false
      mockUseWakeMiner.mockImplementation(() => ({
        wakeMiner: mockWakeMiner,
        shouldWake: false,
      }));

      const { rerender } = render(<WakeCallout />);

      const wakeButton = screen.getByText("Wake up miner");
      await user.click(wakeButton);

      // wakeMiner should be called
      expect(mockWakeMiner).toHaveBeenCalledTimes(1);

      // Simulate wake in progress
      mockUseWakeMiner.mockImplementation(() => ({
        wakeMiner: mockWakeMiner,
        shouldWake: true,
      }));
      rerender(<WakeCallout />);

      // Simulate wake completing
      mockUseWakeMiner.mockImplementation(() => ({
        wakeMiner: mockWakeMiner,
        shouldWake: false,
      }));
      mockUseIsSleeping.mockReturnValue(false); // Miner is now awake
      rerender(<WakeCallout />);

      // Dialog should now be visible
      expect(screen.getByText("Fans are disabled")).toBeInTheDocument();
    });

    it("dismisses dialog when clicking 'Continue'", async () => {
      const user = userEvent.setup();
      mockUseCoolingMode.mockReturnValue("Off");
      mockUseFansTelemetry.mockReturnValue([{ slot: 1, rpm: { latest: { value: 1000 } } }]);

      // Start with miner sleeping
      mockUseIsSleeping.mockReturnValue(true);
      const { rerender } = render(<WakeCallout />);

      // Simulate miner waking up (isSleeping: true → false)
      mockUseIsSleeping.mockReturnValue(false);
      rerender(<WakeCallout />);

      // Dialog should be visible
      expect(screen.getByText("Fans are disabled")).toBeInTheDocument();

      const continueButton = screen.getByText("Continue");
      await user.click(continueButton);

      // Dialog should be dismissed
      await waitFor(() => {
        expect(screen.queryByText("Fans are disabled")).not.toBeInTheDocument();
      });
    });

    it("switches to air cooled mode and dismisses dialog when clicking 'Switch to air cooling'", async () => {
      const user = userEvent.setup();
      mockUseCoolingMode.mockReturnValue("Off");
      mockUseFansTelemetry.mockReturnValue([{ slot: 1, rpm: { latest: { value: 1000 } } }]);

      // Start with miner sleeping
      mockUseIsSleeping.mockReturnValue(true);
      const { rerender } = render(<WakeCallout />);

      // Simulate miner waking up (isSleeping: true → false)
      mockUseIsSleeping.mockReturnValue(false);
      rerender(<WakeCallout />);

      // Dialog should be visible
      expect(screen.getByText("Fans are disabled")).toBeInTheDocument();

      // Mock setCooling to call onSuccess
      mockSetCooling.mockImplementation(({ onSuccess }) => {
        onSuccess?.({ mode: "Auto" });
      });

      const switchButton = screen.getByRole("button", { name: "Switch to air cooling" });
      await user.click(switchButton);

      expect(mockSetCooling).toHaveBeenCalledWith(
        expect.objectContaining({
          mode: "Auto",
        }),
      );

      // Dialog should be dismissed after successful mode change
      await waitFor(() => {
        expect(screen.queryByText("Fans are disabled")).not.toBeInTheDocument();
      });
    });

    it("stops loading on error when switching to air cooling", async () => {
      const user = userEvent.setup();
      mockUseCoolingMode.mockReturnValue("Off");
      mockUseFansTelemetry.mockReturnValue([{ slot: 1, rpm: { latest: { value: 1000 } } }]);

      // Start with miner sleeping
      mockUseIsSleeping.mockReturnValue(true);
      const { rerender } = render(<WakeCallout />);

      // Simulate miner waking up (isSleeping: true → false)
      mockUseIsSleeping.mockReturnValue(false);
      rerender(<WakeCallout />);

      // Dialog should be visible
      expect(screen.getByText("Fans are disabled")).toBeInTheDocument();

      const switchButton = screen.getByRole("button", { name: "Switch to air cooling" });

      // Simulate error
      mockSetCooling.mockImplementation(({ onError }) => {
        onError?.({ error: { message: "Failed to switch" } });
      });

      await user.click(switchButton);

      // Loading should stop on error
      await waitFor(() => {
        expect(screen.getByRole("button", { name: "Switch to air cooling" })).not.toBeDisabled();
      });

      // Dialog should still be visible
      expect(screen.getByText("Fans are disabled")).toBeInTheDocument();
    });

    it("shows dialog on multiple wake cycles", async () => {
      mockUseCoolingMode.mockReturnValue("Off");
      mockUseFansTelemetry.mockReturnValue([{ slot: 1, rpm: { latest: { value: 1000 } } }]);

      // First wake cycle
      mockUseIsSleeping.mockReturnValue(true);
      const { rerender } = render(<WakeCallout />);

      mockUseIsSleeping.mockReturnValue(false);
      rerender(<WakeCallout />);

      expect(screen.getByText("Fans are disabled")).toBeInTheDocument();

      // Dismiss dialog
      const continueButton = screen.getByText("Continue");
      await userEvent.setup().click(continueButton);

      await waitFor(() => {
        expect(screen.queryByText("Fans are disabled")).not.toBeInTheDocument();
      });

      // Second wake cycle - miner goes to sleep and wakes up again
      mockUseIsSleeping.mockReturnValue(true);
      rerender(<WakeCallout />);

      mockUseIsSleeping.mockReturnValue(false);
      rerender(<WakeCallout />);

      // Dialog should show again
      expect(screen.getByText("Fans are disabled")).toBeInTheDocument();
    });

    it("does not show dialog when waking in air cooled mode even with fans running", async () => {
      mockUseCoolingMode.mockReturnValue("Auto");
      mockUseFansTelemetry.mockReturnValue([{ slot: 1, rpm: { latest: { value: 1000 } } }]);

      mockUseIsSleeping.mockReturnValue(true);
      const { rerender } = render(<WakeCallout />);

      mockUseIsSleeping.mockReturnValue(false);
      rerender(<WakeCallout />);

      // Dialog should NOT be visible (air cooled mode)
      expect(screen.queryByText("Fans are disabled")).not.toBeInTheDocument();
    });

    it("does not show dialog if miner is already awake (no isSleeping transition)", async () => {
      mockUseCoolingMode.mockReturnValue("Off");
      mockUseFansTelemetry.mockReturnValue([{ slot: 1, rpm: { latest: { value: 1000 } } }]);

      // Miner is already awake
      mockUseIsSleeping.mockReturnValue(false);
      const { rerender } = render(<WakeCallout />);

      // Stays awake (no transition)
      mockUseIsSleeping.mockReturnValue(false);
      rerender(<WakeCallout />);

      // Dialog should NOT be visible (no wake transition occurred)
      expect(screen.queryByText("Fans are disabled")).not.toBeInTheDocument();
    });

    it("does not show dialog after waking in immersion mode without fans running", async () => {
      const user = userEvent.setup();
      mockUseIsSleeping.mockReturnValue(true);
      mockUseCoolingMode.mockReturnValue("Off");
      mockUseFansTelemetry.mockReturnValue([]); // No fans running

      // Start with shouldWake false
      mockUseWakeMiner.mockImplementation(() => ({
        wakeMiner: mockWakeMiner,
        shouldWake: false,
      }));

      const { rerender } = render(<WakeCallout />);

      const wakeButton = screen.getByText("Wake up miner");
      await user.click(wakeButton);

      // wakeMiner should be called
      expect(mockWakeMiner).toHaveBeenCalledTimes(1);

      // Simulate wake in progress
      mockUseWakeMiner.mockImplementation(() => ({
        wakeMiner: mockWakeMiner,
        shouldWake: true,
      }));
      rerender(<WakeCallout />);

      // Simulate wake completing
      mockUseWakeMiner.mockImplementation(() => ({
        wakeMiner: mockWakeMiner,
        shouldWake: false,
      }));
      mockUseIsSleeping.mockReturnValue(false); // Miner is now awake
      rerender(<WakeCallout />);

      // Dialog should NOT be visible (no fans running)
      expect(screen.queryByText("Fans are disabled")).not.toBeInTheDocument();
    });
  });
});
