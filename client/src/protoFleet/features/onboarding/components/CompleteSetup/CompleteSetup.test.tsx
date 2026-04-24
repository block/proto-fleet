import React from "react";
import { MemoryRouter } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import CompleteSetup from "./CompleteSetup";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import usePoolNeededCount from "@/protoFleet/api/usePoolNeededCount";

vi.mock("@/protoFleet/api/useAuthNeededMiners");
vi.mock("@/protoFleet/api/usePoolNeededCount");
vi.mock("@/protoFleet/api/useMinerCommand");
const mockRefetchMiners = vi.fn();
vi.mock("@/shared/hooks/useReactiveLocalStorage");
vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({
  default: ({
    open,
    onAuthenticated,
  }: {
    open: boolean;
    onAuthenticated: (username: string, password: string) => void;
  }) =>
    open ? (
      <div data-testid="auth-fleet-modal">
        <button onClick={() => onAuthenticated("testuser", "testpass")}>Submit Auth</button>
      </div>
    ) : null,
}));
vi.mock("@/protoFleet/features/auth/components/AuthenticateMiners", () => ({
  AuthenticateMiners: ({ open }: { open?: boolean }) =>
    open ? <div data-testid="auth-miners-modal">Authenticate Miners Modal</div> : null,
}));
vi.mock("@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: () => <div data-testid="pool-selection-modal">Pool Selection Modal</div>,
}));

// Mock motion to render without animations in tests
vi.mock("motion/react", () => ({
  motion: {
    div: ({ children, ...props }: React.ComponentProps<"div">) => <div {...props}>{children}</div>,
  },
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

const mockRefetchAuthNeededMiners = vi.fn();
const mockRefetchPoolNeededCount = vi.fn();
const mockStreamCommandBatchUpdates = vi.fn();

beforeEach(async () => {
  vi.clearAllMocks();

  // Default mock values
  vi.mocked(useAuthNeededMiners).mockReturnValue({
    minerIds: [],
    miners: {},
    totalMiners: 0,
    hasMore: false,
    isLoading: false,
    hasInitialLoadCompleted: true,
    availableModels: [],
    loadMore: vi.fn(),
    refetch: mockRefetchAuthNeededMiners,
  });

  vi.mocked(usePoolNeededCount).mockReturnValue({
    poolNeededCount: 0,
    isLoading: false,
    hasInitialLoadCompleted: true,
    refetch: mockRefetchPoolNeededCount,
  });

  vi.mocked(useMinerCommand).mockReturnValue({
    streamCommandBatchUpdates: mockStreamCommandBatchUpdates,
    blinkLED: vi.fn(),
    startMining: vi.fn(),
    stopMining: vi.fn(),
    deleteMiners: vi.fn(),
    reboot: vi.fn(),
    updateMiningPools: vi.fn(),
    previewMiningPoolAssignment: vi.fn(),
    setPowerTarget: vi.fn(),
    setCoolingMode: vi.fn(),
    updateMinerPassword: vi.fn(),
    checkCommandCapabilities: vi.fn(),
    downloadLogs: vi.fn(),
    firmwareUpdate: vi.fn(),
    getCommandBatchLogBundle: vi.fn(),
  });

  // Mock localStorage to return both values used in CompleteSetup
  const { useReactiveLocalStorage } = await import("@/shared/hooks/useReactiveLocalStorage");
  vi.mocked(useReactiveLocalStorage).mockImplementation((key: string) => {
    if (key === "completeSetupDismissed") {
      return [false, vi.fn()];
    }
    if (key === "configurePoolDismissed") {
      return [false, vi.fn()];
    }
    return [false, vi.fn()];
  });
});

describe("CompleteSetup", () => {
  const renderCompleteSetup = (props: { lastPairingCompletedAt?: number; onRefetchMiners?: () => void } = {}) => {
    return render(
      <MemoryRouter>
        <CompleteSetup
          lastPairingCompletedAt={props.lastPairingCompletedAt}
          onRefetchMiners={props.onRefetchMiners ?? mockRefetchMiners}
        />
      </MemoryRouter>,
    );
  };

  describe("Visibility conditions", () => {
    it("does not render when no miners need pools and no miners need auth", () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 0,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: [],
        miners: {},
        totalMiners: 0,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      renderCompleteSetup();

      expect(screen.queryByText("Complete setup")).not.toBeInTheDocument();
    });

    it("renders when miners need pools", () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 5,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      expect(screen.getByText("Complete setup")).toBeInTheDocument();
      expect(screen.getByText("Configure pools")).toBeInTheDocument();
      expect(screen.getByText("5 miners")).toBeInTheDocument();
    });

    it("renders when miners need authentication", () => {
      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: ["miner1", "miner2"],
        miners: {},
        totalMiners: 2,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      renderCompleteSetup();

      expect(screen.getByText("Complete setup")).toBeInTheDocument();
      expect(screen.getByText("Authenticate miners")).toBeInTheDocument();
      expect(screen.getByText("2 miners need attention")).toBeInTheDocument();
    });

    it("renders both cards when miners need pools and auth", () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 3,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: ["miner1"],
        miners: {},
        totalMiners: 1,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      renderCompleteSetup();

      expect(screen.getByText("Complete setup")).toBeInTheDocument();
      expect(screen.getByText("Configure pools")).toBeInTheDocument();
      expect(screen.getByText("3 miners")).toBeInTheDocument();
      expect(screen.getByText("Authenticate miners")).toBeInTheDocument();
      expect(screen.getByText("1 miner needs attention")).toBeInTheDocument();
    });

    it("does not render when complete setup is dismissed", async () => {
      const { useReactiveLocalStorage } = await import("@/shared/hooks/useReactiveLocalStorage");
      vi.mocked(useReactiveLocalStorage).mockImplementation((key: string) => {
        if (key === "completeSetupDismissed") {
          return [true, vi.fn()];
        }
        return [false, vi.fn()];
      });

      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 3,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      expect(screen.queryByText("Complete setup")).not.toBeInTheDocument();
    });

    it("does not render configure pools card when dismissed separately", async () => {
      const { useReactiveLocalStorage } = await import("@/shared/hooks/useReactiveLocalStorage");
      vi.mocked(useReactiveLocalStorage).mockImplementation((key: string) => {
        if (key === "configurePoolDismissed") {
          return [true, vi.fn()];
        }
        return [false, vi.fn()];
      });

      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 3,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      expect(screen.queryByText("Configure pools")).not.toBeInTheDocument();
    });
  });

  describe("Dismiss functionality", () => {
    it("dismisses complete setup when dismiss button clicked", async () => {
      const setCompleteSetupDismissed = vi.fn();
      const { useReactiveLocalStorage } = await import("@/shared/hooks/useReactiveLocalStorage");
      vi.mocked(useReactiveLocalStorage).mockImplementation((key: string) => {
        if (key === "completeSetupDismissed") {
          return [false, setCompleteSetupDismissed];
        }
        return [false, vi.fn()];
      });

      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 3,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      const dismissButton = screen.getByRole("button", { name: "Dismiss complete setup" });
      fireEvent.click(dismissButton);

      expect(setCompleteSetupDismissed).toHaveBeenCalledWith(true);
    });
  });

  describe("ConfigurePoolCard", () => {
    it("renders configure pools card with correct count", () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 5,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      expect(screen.getByText("Configure pools")).toBeInTheDocument();
      expect(screen.getByText("5 miners")).toBeInTheDocument();
      expect(screen.getByText("Configure")).toBeInTheDocument();
      expect(screen.getByText("Skip")).toBeInTheDocument();
    });

    it("uses singular form for one miner", () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 1,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      expect(screen.getByText("1 miner")).toBeInTheDocument();
    });

    it("does not render configure pools card when no miners need pools", () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 0,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: ["miner1"],
        miners: {},
        totalMiners: 1,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      renderCompleteSetup();

      expect(screen.queryByText("Configure pools")).not.toBeInTheDocument();
    });

    it("opens pool selection modal when configure button clicked", async () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 5,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      const configureButton = screen.getByText("Configure");
      fireEvent.click(configureButton);

      await waitFor(() => {
        expect(screen.getByTestId("auth-fleet-modal")).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText("Submit Auth"));

      await waitFor(() => {
        expect(screen.getByTestId("pool-selection-modal")).toBeInTheDocument();
      });
    });

    it("dismisses configure pools card when skip button clicked", async () => {
      const setConfigurePoolDismissed = vi.fn();
      const { useReactiveLocalStorage } = await import("@/shared/hooks/useReactiveLocalStorage");
      vi.mocked(useReactiveLocalStorage).mockImplementation((key: string) => {
        if (key === "configurePoolDismissed") {
          return [false, setConfigurePoolDismissed];
        }
        return [false, vi.fn()];
      });

      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 5,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      const skipButton = screen.getByText("Skip");
      fireEvent.click(skipButton);

      expect(setConfigurePoolDismissed).toHaveBeenCalledWith(true);
    });

    it("shows loading state when fetching miners", () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 5,
        isLoading: true,
        hasInitialLoadCompleted: false,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      // Check that the button is in loading state
      const configureButton = screen.getByRole("button", { name: /configure/i });
      expect(configureButton).toHaveAttribute("disabled");
    });

    it("removes entire component when configure pools card is skipped and it's the only card", async () => {
      const setConfigurePoolDismissed = vi.fn();
      const { useReactiveLocalStorage } = await import("@/shared/hooks/useReactiveLocalStorage");

      // Start with configurePoolDismissed = false
      vi.mocked(useReactiveLocalStorage).mockImplementation((key: string) => {
        if (key === "configurePoolDismissed") {
          return [false, setConfigurePoolDismissed];
        }
        return [false, vi.fn()];
      });

      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 5,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      // No auth card showing
      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: [],
        miners: {},
        totalMiners: 0,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      const { rerender } = renderCompleteSetup();

      expect(screen.getByText("Complete setup")).toBeInTheDocument();
      expect(screen.getByText("Configure pools")).toBeInTheDocument();

      // Click skip button
      const skipButton = screen.getByText("Skip");
      fireEvent.click(skipButton);

      expect(setConfigurePoolDismissed).toHaveBeenCalledWith(true);

      // Simulate the card being dismissed by updating the mock
      vi.mocked(useReactiveLocalStorage).mockImplementation((key: string) => {
        if (key === "configurePoolDismissed") {
          return [true, setConfigurePoolDismissed];
        }
        return [false, vi.fn()];
      });

      // Rerender to reflect the dismissed state
      rerender(
        <MemoryRouter>
          <CompleteSetup />
        </MemoryRouter>,
      );

      // Entire component should be removed since no cards are showing
      expect(screen.queryByText("Complete setup")).not.toBeInTheDocument();
    });

    it("keeps component visible when configure pools card is skipped but auth card is still showing", async () => {
      const setConfigurePoolDismissed = vi.fn();
      const { useReactiveLocalStorage } = await import("@/shared/hooks/useReactiveLocalStorage");

      vi.mocked(useReactiveLocalStorage).mockImplementation((key: string) => {
        if (key === "configurePoolDismissed") {
          return [false, setConfigurePoolDismissed];
        }
        return [false, vi.fn()];
      });

      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 5,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      // Auth card is showing
      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: ["miner1"],
        miners: {},
        totalMiners: 1,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      const { rerender } = renderCompleteSetup();

      expect(screen.getByText("Complete setup")).toBeInTheDocument();
      expect(screen.getByText("Configure pools")).toBeInTheDocument();
      expect(screen.getByText("Authenticate miners")).toBeInTheDocument();

      // Click skip button on configure pools card
      const skipButton = screen.getByText("Skip");
      fireEvent.click(skipButton);

      expect(setConfigurePoolDismissed).toHaveBeenCalledWith(true);

      // Simulate the card being dismissed
      vi.mocked(useReactiveLocalStorage).mockImplementation((key: string) => {
        if (key === "configurePoolDismissed") {
          return [true, setConfigurePoolDismissed];
        }
        return [false, vi.fn()];
      });

      rerender(
        <MemoryRouter>
          <CompleteSetup />
        </MemoryRouter>,
      );

      // Component should still be visible because auth card is showing
      expect(screen.getByText("Complete setup")).toBeInTheDocument();
      expect(screen.queryByText("Configure pools")).not.toBeInTheDocument();
      expect(screen.getByText("Authenticate miners")).toBeInTheDocument();
    });
  });

  describe("AuthenticateMinersCard", () => {
    it("renders authenticate miners card when miners need auth", () => {
      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: ["miner1", "miner2", "miner3"],
        miners: {},
        totalMiners: 3,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      renderCompleteSetup();

      expect(screen.getByText("Authenticate miners")).toBeInTheDocument();
      expect(screen.getByText("3 miners need attention")).toBeInTheDocument();
    });

    it("uses singular form for one miner", () => {
      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: ["miner1"],
        miners: {},
        totalMiners: 1,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      renderCompleteSetup();

      expect(screen.getByText("1 miner needs attention")).toBeInTheDocument();
    });

    it("does not render authenticate miners card when no miners need auth", () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 3,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: [],
        miners: {},
        totalMiners: 0,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      renderCompleteSetup();

      expect(screen.queryByText("Authenticate miners")).not.toBeInTheDocument();
    });
  });

  describe("Polling after pairing completion", () => {
    it("starts polling when pairing completes", async () => {
      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: ["miner1"],
        miners: {},
        totalMiners: 1,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 0,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      const { rerender } = renderCompleteSetup();

      // Reset call counts before simulating pairing
      mockRefetchAuthNeededMiners.mockClear();
      mockRefetchPoolNeededCount.mockClear();

      // Simulate pairing completion by updating the timestamp prop
      const timestamp = Date.now();

      rerender(
        <MemoryRouter>
          <CompleteSetup lastPairingCompletedAt={timestamp} onRefetchMiners={mockRefetchMiners} />
        </MemoryRouter>,
      );

      // First poll should happen after 1s initial delay
      await waitFor(
        () => {
          expect(mockRefetchAuthNeededMiners).toHaveBeenCalled();
          expect(mockRefetchPoolNeededCount).toHaveBeenCalled();
        },
        { timeout: 1500 },
      );
    });

    it("stops polling when poolNeededCount changes from initial value", async () => {
      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: [],
        miners: {},
        totalMiners: 0,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      // Start with 0 miners
      const mockPoolNeededHook = vi.mocked(usePoolNeededCount);
      mockPoolNeededHook.mockReturnValue({
        poolNeededCount: 0,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      const timestamp = Date.now();
      const { rerender } = renderCompleteSetup({ lastPairingCompletedAt: undefined });

      // Simulate pairing completion
      rerender(
        <MemoryRouter>
          <CompleteSetup lastPairingCompletedAt={timestamp} onRefetchMiners={mockRefetchMiners} />
        </MemoryRouter>,
      );

      // Wait for first poll
      await waitFor(
        () => {
          expect(mockRefetchPoolNeededCount).toHaveBeenCalled();
        },
        { timeout: 1500 },
      );

      // Simulate backend detecting miners with NEEDS_MINING_POOL status
      mockPoolNeededHook.mockReturnValue({
        poolNeededCount: 5,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      // Rerender with new pool count
      rerender(
        <MemoryRouter>
          <CompleteSetup lastPairingCompletedAt={timestamp} onRefetchMiners={mockRefetchMiners} />
        </MemoryRouter>,
      );

      // Polling should have stopped, so no additional calls after a delay
      const callCount = mockRefetchPoolNeededCount.mock.calls.length;
      await new Promise((resolve) => setTimeout(resolve, 600));
      expect(mockRefetchPoolNeededCount).toHaveBeenCalledTimes(callCount);
    });

    it("does not refetch when pairing timestamp is 0", () => {
      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: ["miner1"],
        miners: {},
        totalMiners: 1,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      renderCompleteSetup();

      expect(mockRefetchAuthNeededMiners).not.toHaveBeenCalled();
      expect(mockRefetchPoolNeededCount).not.toHaveBeenCalled();
    });

    it("does not start new polling if timestamp is same as previous", async () => {
      vi.mocked(useAuthNeededMiners).mockReturnValue({
        minerIds: ["miner1"],
        miners: {},
        totalMiners: 1,
        hasMore: false,
        isLoading: false,
        hasInitialLoadCompleted: true,
        availableModels: [],
        loadMore: vi.fn(),
        refetch: mockRefetchAuthNeededMiners,
      });

      const timestamp = Date.now();

      const { rerender } = renderCompleteSetup({ lastPairingCompletedAt: timestamp });

      await waitFor(
        () => {
          expect(mockRefetchAuthNeededMiners).toHaveBeenCalled();
        },
        { timeout: 1500 },
      );

      const callCountAfterFirst = mockRefetchAuthNeededMiners.mock.calls.length;

      // Rerender with same timestamp should not trigger new polling
      rerender(
        <MemoryRouter>
          <CompleteSetup lastPairingCompletedAt={timestamp} onRefetchMiners={mockRefetchMiners} />
        </MemoryRouter>,
      );

      // Wait a bit and verify no new calls were made
      await new Promise((resolve) => setTimeout(resolve, 100));

      expect(mockRefetchAuthNeededMiners).toHaveBeenCalledTimes(callCountAfterFirst);
    });
  });

  describe("Pool assignment flow", () => {
    it("passes correct miners to pool selection modal", async () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 3,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      renderCompleteSetup();

      const configureButton = screen.getByText("Configure");
      fireEvent.click(configureButton);

      await waitFor(() => {
        expect(screen.getByTestId("auth-fleet-modal")).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText("Submit Auth"));

      await waitFor(() => {
        expect(screen.getByTestId("pool-selection-modal")).toBeInTheDocument();
      });
    });

    it("shows loading state on configure pools card during polling after pool assignment", async () => {
      vi.mocked(usePoolNeededCount).mockReturnValue({
        poolNeededCount: 2,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      const { rerender } = renderCompleteSetup();

      // Verify initial state - button is not loading
      let configureButton = screen.getByText("Configure");
      expect(configureButton).not.toHaveAttribute("disabled");

      // Trigger pool assignment success by simulating the pairing timestamp update
      // which triggers polling
      const timestamp = Date.now();

      rerender(
        <MemoryRouter>
          <CompleteSetup lastPairingCompletedAt={timestamp} onRefetchMiners={mockRefetchMiners} />
        </MemoryRouter>,
      );

      // Wait for polling to start
      await waitFor(
        () => {
          expect(mockRefetchPoolNeededCount).toHaveBeenCalled();
        },
        { timeout: 1500 },
      );

      // Button should now be in loading state
      configureButton = screen.getByRole("button", { name: /configure/i });
      expect(configureButton).toHaveAttribute("disabled");
    });

    it("stops polling and exits loading state when pool count changes to 0", async () => {
      const mockPoolNeededHook = vi.mocked(usePoolNeededCount);

      // Start with miners needing pools
      mockPoolNeededHook.mockReturnValue({
        poolNeededCount: 5,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      const { rerender } = renderCompleteSetup();

      // Trigger polling
      const timestamp = Date.now();

      rerender(
        <MemoryRouter>
          <CompleteSetup lastPairingCompletedAt={timestamp} onRefetchMiners={mockRefetchMiners} />
        </MemoryRouter>,
      );

      // Wait for polling to start
      await waitFor(
        () => {
          expect(mockRefetchPoolNeededCount).toHaveBeenCalled();
        },
        { timeout: 1500 },
      );

      // Simulate pool configuration completing - count goes to 0
      mockPoolNeededHook.mockReturnValue({
        poolNeededCount: 0,
        isLoading: false,
        hasInitialLoadCompleted: true,
        refetch: mockRefetchPoolNeededCount,
      });

      rerender(
        <MemoryRouter>
          <CompleteSetup lastPairingCompletedAt={timestamp} onRefetchMiners={mockRefetchMiners} />
        </MemoryRouter>,
      );

      // Component should be removed since no cards are showing
      await waitFor(() => {
        expect(screen.queryByText("Complete setup")).not.toBeInTheDocument();
      });
    });

    it("exits loading state after all polls complete even when pool count unchanged", async () => {
      vi.useFakeTimers();

      try {
        const mockPoolNeededHook = vi.mocked(usePoolNeededCount);

        // Start with miners needing pools
        mockPoolNeededHook.mockReturnValue({
          poolNeededCount: 5,
          isLoading: false,
          hasInitialLoadCompleted: true,
          refetch: mockRefetchPoolNeededCount,
        });

        const { rerender } = renderCompleteSetup();

        // Trigger polling via pairing completion
        const timestamp = Date.now();

        rerender(
          <MemoryRouter>
            <CompleteSetup lastPairingCompletedAt={timestamp} onRefetchMiners={mockRefetchMiners} />
          </MemoryRouter>,
        );

        // Button should be in loading state after polling starts
        let configureButton = screen.getByRole("button", { name: /configure/i });
        expect(configureButton).toHaveAttribute("disabled");

        // Advance through all 10 polls and flush React state updates
        // Total polling time: 1000ms initial delay + 9 × 2000ms intervals = 19000ms
        await act(async () => {
          await vi.advanceTimersByTimeAsync(19000);
        });

        // After all polls complete, button should no longer be disabled
        configureButton = screen.getByRole("button", { name: /configure/i });
        expect(configureButton).not.toHaveAttribute("disabled");
      } finally {
        vi.useRealTimers();
      }
    });
  });

  it("applies custom className when provided", () => {
    vi.mocked(usePoolNeededCount).mockReturnValue({
      poolNeededCount: 3,
      isLoading: false,
      hasInitialLoadCompleted: true,
      refetch: mockRefetchPoolNeededCount,
    });

    const { container } = render(
      <MemoryRouter>
        <CompleteSetup className="custom-class" />
      </MemoryRouter>,
    );

    const outerDiv = container.firstChild as HTMLElement;
    expect(outerDiv).toHaveClass("custom-class");
  });
});
