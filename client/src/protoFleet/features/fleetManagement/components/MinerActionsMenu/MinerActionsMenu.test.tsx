import { Fragment, type ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { deviceActions, groupActions, performanceActions, settingsActions } from "./constants";
import MinerActionsMenu from "./MinerActionsMenu";

// Use vi.hoisted to properly hoist mock variable declarations
const {
  mockAddToGroupModal,
  mockBulkActionsWidget,
  mockBulkRenameModal,
  mockPoolSelectionPageWrapper,
  mockUseMinerActions,
  mockUseWindowDimensions,
} = vi.hoisted(() => ({
  mockAddToGroupModal: vi.fn(() => null),
  mockBulkActionsWidget: vi.fn(
    (props: {
      buttonTitle: string;
      renderQuickActions?: (onAction: (action: { actionHandler: () => void }) => void) => ReactNode;
    }) => (
      <>
        {props.renderQuickActions?.((action) => action.actionHandler())}
        <div>{props.buttonTitle}</div>
      </>
    ),
  ),
  mockBulkRenameModal: vi.fn(() => null),
  mockPoolSelectionPageWrapper: vi.fn(
    (_props: {
      open?: boolean;
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
      authenticationPurpose: string | null;
      showUpdatePasswordModal: boolean;
      hasThirdPartyMiners: boolean;
      handleFleetAuthenticated: ReturnType<typeof vi.fn>;
      handlePasswordConfirm: ReturnType<typeof vi.fn>;
      handlePasswordDismiss: ReturnType<typeof vi.fn>;
      handleAuthDismiss: ReturnType<typeof vi.fn>;
      unsupportedMinersInfo: unknown;
      handleUnsupportedMinersContinue: ReturnType<typeof vi.fn>;
      handleUnsupportedMinersDismiss: ReturnType<typeof vi.fn>;
      showManageSecurityModal: boolean;
      minerGroups: unknown[];
      handleUpdateGroup: ReturnType<typeof vi.fn>;
      handleSecurityModalClose: ReturnType<typeof vi.fn>;
      showAddToGroupModal: boolean;
      handleAddToGroupDismiss: ReturnType<typeof vi.fn>;
      displayCount: number;
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
      authenticationPurpose: null,
      showUpdatePasswordModal: false,
      hasThirdPartyMiners: false,
      handleFleetAuthenticated: vi.fn(),
      handlePasswordConfirm: vi.fn(),
      handlePasswordDismiss: vi.fn(),
      handleAuthDismiss: vi.fn(),
      unsupportedMinersInfo: undefined,
      handleUnsupportedMinersContinue: vi.fn(),
      handleUnsupportedMinersDismiss: vi.fn(),
      showManageSecurityModal: false,
      minerGroups: [],
      handleUpdateGroup: vi.fn(),
      handleSecurityModalClose: vi.fn(),
      showAddToGroupModal: false,
      handleAddToGroupDismiss: vi.fn(),
      displayCount: 0,
    }),
  ),
  mockUseWindowDimensions: vi.fn(() => ({
    isPhone: false,
    isTablet: false,
  })),
}));

vi.mock("../ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: mockPoolSelectionPageWrapper,
}));

// Mock BulkActionsWidget
vi.mock("../BulkActions", () => ({
  default: mockBulkActionsWidget,
  BulkActionsPopover: vi.fn(() => null),
}));

vi.mock("./BulkRenameModal", () => ({
  default: mockBulkRenameModal,
}));

vi.mock("./AddToGroupModal", () => ({
  default: mockAddToGroupModal,
}));

// Mock CoolingModeModal
vi.mock("./CoolingModeModal", () => ({
  default: vi.fn(() => null),
}));

// Mock ManagePowerModal
vi.mock("./ManagePowerModal", () => ({
  default: vi.fn(() => null),
}));

// Mock ManageSecurity
vi.mock("./ManageSecurity", () => ({
  ManageSecurityModal: vi.fn(() => null),
  UpdateMinerPasswordModal: vi.fn(() => null),
}));

// Mock AuthenticateFleetModal
vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./useMinerActions", () => ({
  useMinerActions: mockUseMinerActions,
}));

// Mock Popover
vi.mock("@/shared/components/Popover", () => ({
  PopoverProvider: ({ children }: { children: ReactNode }) => <Fragment>{children}</Fragment>,
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: mockUseWindowDimensions,
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
  authenticationPurpose: null,
  showUpdatePasswordModal: false,
  hasThirdPartyMiners: false,
  handleFleetAuthenticated: vi.fn(),
  handlePasswordConfirm: vi.fn(),
  handlePasswordDismiss: vi.fn(),
  handleAuthDismiss: vi.fn(),
  unsupportedMinersInfo: undefined,
  handleUnsupportedMinersContinue: vi.fn(),
  handleUnsupportedMinersDismiss: vi.fn(),
  showManageSecurityModal: false,
  minerGroups: [],
  handleUpdateGroup: vi.fn(),
  handleSecurityModalClose: vi.fn(),
  showAddToGroupModal: false,
  handleAddToGroupDismiss: vi.fn(),
  displayCount: 2,
});

describe("MinerActionsMenu", () => {
  test.beforeEach(() => {
    vi.clearAllMocks();
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });
  });

  test("renders desktop quick actions and switches overflow trigger copy to More", () => {
    const blinkLEDsActionHandler = vi.fn();
    const rebootActionHandler = vi.fn();
    const managePowerActionHandler = vi.fn();

    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });
    mockUseMinerActions.mockReturnValueOnce({
      ...createMockMinerActionsReturn(null),
      popoverActions: [
        {
          action: deviceActions.reboot,
          title: "Reboot",
          icon: null,
          actionHandler: rebootActionHandler,
          requiresConfirmation: true,
        },
        {
          action: deviceActions.blinkLEDs,
          title: "Blink LEDs",
          icon: null,
          actionHandler: blinkLEDsActionHandler,
          requiresConfirmation: false,
        },
        {
          action: performanceActions.managePower,
          title: "Manage power",
          icon: null,
          actionHandler: managePowerActionHandler,
          requiresConfirmation: false,
        },
      ],
    });

    render(
      <MinerActionsMenu
        selectedMiners={["miner-1", "miner-2"]}
        selectionMode="subset"
        totalCount={2}
        onActionStart={vi.fn()}
        onActionComplete={vi.fn()}
      />,
    );

    expect(screen.getByTestId("actions-menu-quick-action-blink-leds")).toHaveTextContent("Blink LEDs");
    expect(screen.getByTestId("actions-menu-quick-action-reboot")).toHaveTextContent("Reboot");
    expect(screen.getByTestId("actions-menu-quick-action-manage-power")).toHaveTextContent("Manage power");

    fireEvent.click(screen.getByTestId("actions-menu-quick-action-blink-leds"));
    fireEvent.click(screen.getByTestId("actions-menu-quick-action-reboot"));
    fireEvent.click(screen.getByTestId("actions-menu-quick-action-manage-power"));

    expect(blinkLEDsActionHandler).toHaveBeenCalledTimes(1);
    expect(rebootActionHandler).toHaveBeenCalledTimes(1);
    expect(managePowerActionHandler).toHaveBeenCalledTimes(1);

    const widgetCalls = mockBulkActionsWidget.mock.calls as unknown as Array<[{ buttonTitle: string }]>;
    const widgetCall = widgetCalls[widgetCalls.length - 1];
    expect(widgetCall?.[0].buttonTitle).toBe("More");
  });

  test("hides quick actions on mobile and keeps the actions trigger copy", () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: true,
      isTablet: false,
    });
    mockUseMinerActions.mockReturnValueOnce({
      ...createMockMinerActionsReturn(null),
      popoverActions: [
        {
          action: deviceActions.blinkLEDs,
          title: "Blink LEDs",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
      ],
    });

    render(
      <MinerActionsMenu
        selectedMiners={["miner-1"]}
        selectionMode="subset"
        totalCount={1}
        onActionStart={vi.fn()}
        onActionComplete={vi.fn()}
      />,
    );

    expect(screen.queryByTestId("actions-menu-quick-action-blink-leds")).not.toBeInTheDocument();
    const widgetCalls = mockBulkActionsWidget.mock.calls as unknown as Array<[{ buttonTitle: string }]>;
    const widgetCall = widgetCalls[widgetCalls.length - 1];
    expect(widgetCall?.[0].buttonTitle).toBe("Actions");
  });

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

  test("renders PoolSelectionPageWrapper with open=false when currentAction is not miningPool", () => {
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

    expect(mockPoolSelectionPageWrapper).toHaveBeenCalled();
    const props = mockPoolSelectionPageWrapper.mock.calls[0][0];
    expect(props.open).toBe(false);
  });

  test("injects rename into bulk actions before add to group and keeps them in the same divider group", () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });
    mockUseMinerActions.mockReturnValueOnce({
      ...createMockMinerActionsReturn(null),
      popoverActions: [
        {
          action: settingsActions.coolingMode,
          title: "Change cooling mode",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
          showGroupDivider: true,
        },
        {
          action: groupActions.addToGroup,
          title: "Add to group",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
          showGroupDivider: true,
        },
        {
          action: settingsActions.security,
          title: "Manage security",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
      ],
    });

    mockBulkActionsWidget.mockClear();

    render(
      <MinerActionsMenu
        selectedMiners={["miner-1", "miner-2"]}
        selectionMode="subset"
        totalCount={2}
        onActionStart={vi.fn()}
        onActionComplete={vi.fn()}
      />,
    );

    const widgetCalls = mockBulkActionsWidget.mock.calls as unknown as Array<
      [{ actions: Array<{ action: string; showGroupDivider?: boolean }> }]
    >;
    const widgetCall = widgetCalls[0];
    expect(widgetCall).toBeDefined();

    if (widgetCall === undefined) {
      throw new Error("BulkActionsWidget was not called with props");
    }

    const actions = widgetCall[0].actions;

    expect(actions.map((action: { action: string }) => action.action)).toEqual([
      settingsActions.coolingMode,
      settingsActions.rename,
      groupActions.addToGroup,
      settingsActions.security,
    ]);
    expect(actions[0].showGroupDivider).toBe(true);
    expect(actions[1].showGroupDivider).toBeUndefined();
    expect(actions[2].showGroupDivider).toBe(true);
  });
});
