import { type ReactNode } from "react";
import { render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { settingsActions } from "./constants";
import MinerActionsMenu from "./MinerActionsMenu";

// Use vi.hoisted to properly hoist mock variable declarations
const { mockPoolSelectionPageWrapper, mockUseMinerActions } = vi.hoisted(() => ({
  mockPoolSelectionPageWrapper: vi.fn(
    (_props: {
      selectedMiners: Array<{ deviceIdentifier: string }>;
      selectionMode: string;
      poolNeededCount?: number;
      userUsername?: string;
      userPassword?: string;
      onSuccess: (batchIdentifier: string) => void;
      onError?: (error: string) => void;
      onDismiss: () => void;
    }) => null,
  ),
  mockUseMinerActions: vi.fn(
    (): {
      currentAction: string | null;
      popoverActions: unknown[];
      handleConfirmation: ReturnType<typeof vi.fn>;
      handleCancel: ReturnType<typeof vi.fn>;
      handleMiningPoolSuccess: ReturnType<typeof vi.fn>;
      handleMiningPoolError: ReturnType<typeof vi.fn>;
      showPoolSelectionPage: boolean;
      poolFilteredDeviceIds?: string[];
      fleetCredentials?: { username: string; password: string };
      showManagePowerModal: boolean;
      handleManagePowerConfirm: ReturnType<typeof vi.fn>;
      handleManagePowerDismiss: ReturnType<typeof vi.fn>;
      showCoolingModeModal: boolean;
      coolingModeCount: number;
      currentCoolingMode: unknown;
      handleCoolingModeConfirm: ReturnType<typeof vi.fn>;
      handleCoolingModeDismiss: ReturnType<typeof vi.fn>;
      showAuthenticateFleetModal: boolean;
      showUpdatePasswordModal: boolean;
      hasProtoMiners: boolean;
      handleFleetAuthenticated: ReturnType<typeof vi.fn>;
      handlePasswordConfirm: ReturnType<typeof vi.fn>;
      handlePasswordDismiss: ReturnType<typeof vi.fn>;
      handleAuthDismiss: ReturnType<typeof vi.fn>;
      unsupportedMinersInfo: unknown;
      handleUnsupportedMinersContinue: ReturnType<typeof vi.fn>;
      handleUnsupportedMinersDismiss: ReturnType<typeof vi.fn>;
    } => ({
      currentAction: null,
      popoverActions: [],
      handleConfirmation: vi.fn(),
      handleCancel: vi.fn(),
      handleMiningPoolSuccess: vi.fn(),
      handleMiningPoolError: vi.fn(),
      showPoolSelectionPage: false,
      poolFilteredDeviceIds: undefined,
      fleetCredentials: undefined,
      showManagePowerModal: false,
      handleManagePowerConfirm: vi.fn(),
      handleManagePowerDismiss: vi.fn(),
      showCoolingModeModal: false,
      coolingModeCount: 0,
      currentCoolingMode: undefined,
      handleCoolingModeConfirm: vi.fn(),
      handleCoolingModeDismiss: vi.fn(),
      showAuthenticateFleetModal: false,
      showUpdatePasswordModal: false,
      hasProtoMiners: true,
      handleFleetAuthenticated: vi.fn(),
      handlePasswordConfirm: vi.fn(),
      handlePasswordDismiss: vi.fn(),
      handleAuthDismiss: vi.fn(),
      unsupportedMinersInfo: undefined,
      handleUnsupportedMinersContinue: vi.fn(),
      handleUnsupportedMinersDismiss: vi.fn(),
    }),
  ),
}));

vi.mock("../ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: mockPoolSelectionPageWrapper,
}));

// Mock BulkActionsWidget
vi.mock("../BulkActions", () => ({
  default: vi.fn(() => null),
  BulkActionsPopover: vi.fn(() => null),
}));

// Mock CoolingModeModal
vi.mock("./CoolingModeModal", () => ({
  default: vi.fn(() => null),
}));

// Mock ManagePowerModal
vi.mock("./ManagePowerModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./useMinerActions", () => ({
  useMinerActions: mockUseMinerActions,
}));

// Mock Popover
vi.mock("@/shared/components/Popover", () => ({
  PopoverProvider: ({ children }: { children: ReactNode }) => children,
}));

// Helper function to create mock useMinerActions return value
const createMockMinerActionsReturn = (
  currentAction: string | null,
  showPoolSelectionPage = false,
  fleetCredentials?: { username: string; password: string },
) => ({
  currentAction,
  popoverActions: [],
  handleConfirmation: vi.fn(),
  handleCancel: vi.fn(),
  handleMiningPoolSuccess: vi.fn(),
  handleMiningPoolError: vi.fn(),
  showPoolSelectionPage,
  poolFilteredDeviceIds: undefined,
  fleetCredentials,
  showManagePowerModal: false,
  handleManagePowerConfirm: vi.fn(),
  handleManagePowerDismiss: vi.fn(),
  showCoolingModeModal: false,
  coolingModeCount: 0,
  currentCoolingMode: undefined,
  handleCoolingModeConfirm: vi.fn(),
  handleCoolingModeDismiss: vi.fn(),
  showAuthenticateFleetModal: false,
  showUpdatePasswordModal: false,
  hasProtoMiners: true,
  handleFleetAuthenticated: vi.fn(),
  handlePasswordConfirm: vi.fn(),
  handlePasswordDismiss: vi.fn(),
  handleAuthDismiss: vi.fn(),
  unsupportedMinersInfo: undefined,
  handleUnsupportedMinersContinue: vi.fn(),
  handleUnsupportedMinersDismiss: vi.fn(),
});

describe("MinerActionsMenu", () => {
  test("passes totalCount as poolNeededCount when rendering PoolSelectionPageWrapper", async () => {
    const selectedMiners = ["miner-1", "miner-2"];
    const totalCount = 297;

    // Mock the current action to be mining pool settings with authentication complete
    mockUseMinerActions.mockReturnValueOnce(
      createMockMinerActionsReturn(settingsActions.miningPool, true, { username: "testuser", password: "testpass" }),
    );

    render(
      <MinerActionsMenu
        selectedMiners={selectedMiners}
        selectionMode="all"
        totalCount={totalCount}
        onActionStart={vi.fn()}
        onActionComplete={vi.fn()}
      />,
    );

    // Wait for component to render
    await waitFor(() => {
      expect(mockPoolSelectionPageWrapper).toHaveBeenCalled();
    });

    // Verify PoolSelectionPageWrapper was called with totalCount as poolNeededCount
    expect(mockPoolSelectionPageWrapper).toHaveBeenCalled();
    const calls = mockPoolSelectionPageWrapper.mock.calls;
    const lastCall = calls[calls.length - 1];
    const props = lastCall[0];

    expect(props.poolNeededCount).toBe(totalCount);
    expect(props.selectionMode).toBe("all");
    expect(props.selectedMiners).toEqual([{ deviceIdentifier: "miner-1" }, { deviceIdentifier: "miner-2" }]);
    expect(props.userUsername).toBe("testuser");
    expect(props.userPassword).toBe("testpass");
  });

  test("does not render PoolSelectionPageWrapper when currentAction is not miningPool", () => {
    mockUseMinerActions.mockReturnValueOnce(createMockMinerActionsReturn(null));

    mockPoolSelectionPageWrapper.mockClear();

    render(
      <MinerActionsMenu
        selectedMiners={["miner-1"]}
        selectionMode="subset"
        totalCount={100}
        onActionStart={vi.fn()}
        onActionComplete={vi.fn()}
      />,
    );

    expect(mockPoolSelectionPageWrapper).not.toHaveBeenCalled();
  });
});
