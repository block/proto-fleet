import { Fragment, type ReactNode } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { deviceActions, groupActions, performanceActions, settingsActions } from "./constants";
import MinerActionsMenu from "./MinerActionsMenu";
import {
  MinerListFilterSchema,
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// Use vi.hoisted to properly hoist mock variable declarations
const {
  mockAddToGroupModal,
  mockAuthenticateFleetModal,
  mockBulkActionsWidget,
  mockBulkRenameModal,
  mockBulkWorkerNameModal,
  mockWithCapabilityCheck,
  mockPoolSelectionPageWrapper,
  mockUseBatchActions,
  mockUseMinerActions,
  mockUseWindowDimensions,
} = vi.hoisted(() => {
  const mockWithCapabilityCheck = vi.fn(async (_action: string, onProceed: (...args: unknown[]) => void) => {
    onProceed(undefined, undefined);
  });

  return {
    mockAddToGroupModal: vi.fn(() => null),
    mockAuthenticateFleetModal: vi.fn(() => null),
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
    mockBulkWorkerNameModal: vi.fn(() => null),
    mockWithCapabilityCheck,
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
    mockUseBatchActions: vi.fn(() => ({
      startBatchOperation: vi.fn(),
      completeBatchOperation: vi.fn(),
      removeDevicesFromBatch: vi.fn(),
    })),
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
        withCapabilityCheck: ReturnType<typeof vi.fn>;
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
        withCapabilityCheck: mockWithCapabilityCheck,
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
  };
});

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

vi.mock("./BulkWorkerNameModal", () => ({
  default: mockBulkWorkerNameModal,
}));

vi.mock("@/protoFleet/components/ParentPickerModal", () => ({
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
  default: mockAuthenticateFleetModal,
}));

vi.mock("./useMinerActions", () => ({
  useMinerActions: mockUseMinerActions,
}));

// Pass through the permission filter so rendering tests don't need to
// seed the auth store with every miner permission key. The filter
// behavior itself is covered by actionPermissions.test.tsx.
vi.mock("./actionPermissions", () => ({
  usePermittedActions: <T,>(actions: T[]): T[] => actions,
}));

vi.mock("@/protoFleet/features/fleetManagement/hooks/useBatchOperations", () => ({
  useBatchActions: mockUseBatchActions,
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
  withCapabilityCheck: mockWithCapabilityCheck,
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

  test("injects update worker names after pools and rename before add to group", () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });
    mockUseMinerActions.mockReturnValueOnce({
      ...createMockMinerActionsReturn(null),
      popoverActions: [
        {
          action: settingsActions.miningPool,
          title: "Edit pool",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
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
      settingsActions.miningPool,
      settingsActions.updateWorkerNames,
      settingsActions.coolingMode,
      settingsActions.rename,
      groupActions.addToSite,
      groupActions.addToBuilding,
      groupActions.addToRack,
      groupActions.addToGroup,
      settingsActions.security,
    ]);
    expect(actions[2].showGroupDivider).toBe(true);
    expect(actions[3].showGroupDivider).toBeUndefined();
    // addToGroup remains the trailing entry; its showGroupDivider
    // closes the whole site → building → rack → group re-parent cluster.
    expect(actions[7].showGroupDivider).toBe(true);
  });

  test("disables add-to-group and manage-security under a filtered/scoped Select all", () => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: false, isTablet: false });
    mockUseMinerActions.mockReturnValueOnce({
      ...createMockMinerActionsReturn(null),
      popoverActions: [
        {
          action: deviceActions.reboot,
          title: "Reboot",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
        {
          action: groupActions.addToGroup,
          title: "Add to group",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
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
        selectedMiners={["miner-1"]}
        selectionMode="all"
        totalCount={500}
        currentFilter={create(MinerListFilterSchema, { rackIds: [7n] })}
        onActionStart={vi.fn()}
        onActionComplete={vi.fn()}
      />,
    );

    const widgetCalls = mockBulkActionsWidget.mock.calls as unknown as Array<
      [{ actions: Array<{ action: string; disabled?: boolean }> }]
    >;
    const actions = widgetCalls[0][0].actions;
    const byAction = (a: string) => actions.find((x) => x.action === a);

    // Filter-unsafe all-mode actions are disabled...
    expect(byAction(groupActions.addToGroup)?.disabled).toBe(true);
    expect(byAction(settingsActions.security)?.disabled).toBe(true);
    // ...while filter-aware command actions stay enabled.
    expect(byAction(deviceActions.reboot)?.disabled).toBeFalsy();
  });

  test("keeps add-to-group and manage-security enabled for an unscoped Select all", () => {
    mockUseWindowDimensions.mockReturnValue({ isPhone: false, isTablet: false });
    mockUseMinerActions.mockReturnValueOnce({
      ...createMockMinerActionsReturn(null),
      popoverActions: [
        {
          action: groupActions.addToGroup,
          title: "Add to group",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
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
        selectedMiners={["miner-1"]}
        selectionMode="all"
        totalCount={500}
        // No currentFilter — whole-fleet select all; allDevices is correct here.
        onActionStart={vi.fn()}
        onActionComplete={vi.fn()}
      />,
    );

    const widgetCalls = mockBulkActionsWidget.mock.calls as unknown as Array<
      [{ actions: Array<{ action: string; disabled?: boolean }> }]
    >;
    const actions = widgetCalls[0][0].actions;
    expect(actions.find((x) => x.action === groupActions.addToGroup)?.disabled).toBeFalsy();
    expect(actions.find((x) => x.action === settingsActions.security)?.disabled).toBeFalsy();
  });

  test("requests credentials before opening update worker names modal", async () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });
    mockUseMinerActions.mockReturnValueOnce({
      ...createMockMinerActionsReturn(null),
      popoverActions: [
        {
          action: settingsActions.miningPool,
          title: "Edit pool",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
        {
          action: groupActions.addToGroup,
          title: "Add to group",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
      ],
    });

    mockBulkActionsWidget.mockClear();
    mockAuthenticateFleetModal.mockClear();
    mockBulkWorkerNameModal.mockClear();

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
      [{ actions: Array<{ action: string; actionHandler: () => void }> }]
    >;
    const authenticateCalls = mockAuthenticateFleetModal.mock.calls as unknown as Array<
      [
        {
          purpose?: string;
          open: boolean;
          onAuthenticated: (username: string, password: string) => void;
        },
      ]
    >;
    const bulkWorkerNameModalCalls = mockBulkWorkerNameModal.mock.calls as unknown as Array<
      [
        {
          open: boolean;
          getWorkerNameCredentials?: () => { username: string; password: string } | undefined;
        },
      ]
    >;
    const updateWorkerNamesAction = widgetCalls[0]?.[0].actions.find(
      (action) => action.action === settingsActions.updateWorkerNames,
    );

    expect(updateWorkerNamesAction).toBeDefined();

    await act(async () => {
      updateWorkerNamesAction?.actionHandler();
    });

    await waitFor(() => {
      expect(mockWithCapabilityCheck).toHaveBeenCalledWith(settingsActions.updateWorkerNames, expect.any(Function));
      expect(authenticateCalls.some(([props]) => props.purpose === "workerNames" && props.open)).toBe(true);
    });

    const latestHiddenWorkerNameModalProps = bulkWorkerNameModalCalls[bulkWorkerNameModalCalls.length - 1]?.[0];
    expect(latestHiddenWorkerNameModalProps?.open).toBe(false);

    const workerNameAuthProps = authenticateCalls
      .map(([props]) => props)
      .find((props) => props.purpose === "workerNames" && props.open === true);

    expect(workerNameAuthProps).toBeDefined();

    await act(async () => {
      workerNameAuthProps?.onAuthenticated("testuser", "testpass");
    });

    await waitFor(() => {
      const latestBulkWorkerNameModalProps = bulkWorkerNameModalCalls[bulkWorkerNameModalCalls.length - 1]?.[0];
      expect(latestBulkWorkerNameModalProps?.open).toBe(true);
      expect(latestBulkWorkerNameModalProps?.getWorkerNameCredentials?.()).toEqual({
        username: "testuser",
        password: "testpass",
      });
    });
  });

  test("opens the bulk worker-name modal with the capability-filtered target set", async () => {
    mockWithCapabilityCheck.mockImplementationOnce(async () => {});
    mockUseMinerActions.mockReturnValueOnce({
      ...createMockMinerActionsReturn(null),
      popoverActions: [
        {
          action: settingsActions.miningPool,
          title: "Edit pool",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
      ],
    });

    render(
      <MinerActionsMenu
        selectedMiners={["miner-1", "miner-2", "miner-3"]}
        selectionMode="all"
        totalCount={3}
        onActionStart={vi.fn()}
        onActionComplete={vi.fn()}
      />,
    );

    const widgetCalls = mockBulkActionsWidget.mock.calls as unknown as Array<
      [{ actions: Array<{ action: string; actionHandler: () => void }> }]
    >;
    const updateWorkerNamesAction = widgetCalls[0]?.[0].actions.find(
      (action) => action.action === settingsActions.updateWorkerNames,
    );

    await act(async () => {
      updateWorkerNamesAction?.actionHandler();
    });

    const capabilityCheckCallback = mockWithCapabilityCheck.mock.calls[0]?.[1] as
      ((filteredSelector?: unknown, filteredDeviceIds?: string[]) => void) | undefined;

    await act(async () => {
      capabilityCheckCallback?.(
        { selectionType: { case: "includeDevices", value: { deviceIdentifiers: ["miner-2"] } } },
        ["miner-2"],
      );
    });

    const workerNameAuthProps = (
      mockAuthenticateFleetModal.mock.calls as unknown as Array<
        [{ purpose?: string; open: boolean; onAuthenticated: (username: string, password: string) => void }]
      >
    )
      .map(([props]) => props)
      .find((props) => props.purpose === "workerNames" && props.open === true);

    expect(workerNameAuthProps).toBeDefined();

    await act(async () => {
      workerNameAuthProps?.onAuthenticated("testuser", "testpass");
    });

    await waitFor(() => {
      const latestBulkWorkerNameModalProps = (
        mockBulkWorkerNameModal.mock.calls as unknown as Array<
          [
            {
              open: boolean;
              selectedMinerIds: string[];
              selectionMode: string;
              originalSelectionMode?: string;
              totalCount?: number;
            },
          ]
        >
      )[mockBulkWorkerNameModal.mock.calls.length - 1]?.[0];

      expect(latestBulkWorkerNameModalProps?.open).toBe(true);
      expect(latestBulkWorkerNameModalProps?.selectedMinerIds).toEqual(["miner-2"]);
      expect(latestBulkWorkerNameModalProps?.selectionMode).toBe("subset");
      expect(latestBulkWorkerNameModalProps?.originalSelectionMode).toBe("all");
      expect(latestBulkWorkerNameModalProps?.totalCount).toBe(1);
    });
  });

  describe("when the selection includes a miner that needs authentication", () => {
    const popoverActionsFixture = [
      {
        action: deviceActions.reboot,
        title: "Reboot",
        icon: null,
        actionHandler: vi.fn(),
        requiresConfirmation: true,
      },
      {
        action: deviceActions.unpair,
        title: "Unpair",
        icon: null,
        actionHandler: vi.fn(),
        requiresConfirmation: true,
      },
      {
        action: settingsActions.miningPool,
        title: "Edit pool",
        icon: null,
        actionHandler: vi.fn(),
        requiresConfirmation: false,
      },
    ];

    const readActions = () => {
      const widgetCalls = mockBulkActionsWidget.mock.calls as unknown as Array<
        [{ actions: Array<{ action: string; disabled?: boolean; disabledReason?: string }> }]
      >;
      const lastCall = widgetCalls[widgetCalls.length - 1];
      if (lastCall === undefined) {
        throw new Error("BulkActionsWidget was not called with props");
      }
      return lastCall[0].actions;
    };

    test("falls back to the miners map and disables every non-unpair action", () => {
      mockUseMinerActions.mockReturnValueOnce({
        ...createMockMinerActionsReturn(null),
        popoverActions: popoverActionsFixture,
      });

      render(
        <MinerActionsMenu
          selectedMiners={["miner-1", "miner-2"]}
          selectionMode="subset"
          totalCount={2}
          miners={{
            "miner-1": create(MinerStateSnapshotSchema, { pairingStatus: PairingStatus.AUTHENTICATION_NEEDED }),
            "miner-2": create(MinerStateSnapshotSchema, { pairingStatus: PairingStatus.PAIRED }),
          }}
        />,
      );

      const actions = readActions();
      const unpair = actions.find((a) => a.action === deviceActions.unpair);
      const reboot = actions.find((a) => a.action === deviceActions.reboot);
      const editPool = actions.find((a) => a.action === settingsActions.miningPool);

      expect(unpair?.disabled).not.toBe(true);
      expect(reboot?.disabled).toBe(true);
      expect(reboot?.disabledReason).toContain("authentication");
      expect(editPool?.disabled).toBe(true);
      expect(editPool?.disabledReason).toContain("authentication");
    });

    test("uses the parent override even when the miners map is empty (all-mode)", () => {
      mockUseMinerActions.mockReturnValueOnce({
        ...createMockMinerActionsReturn(null),
        popoverActions: popoverActionsFixture,
      });

      render(
        <MinerActionsMenu
          selectedMiners={["miner-1"]}
          selectionMode="all"
          totalCount={5}
          miners={{}}
          selectionIncludesUnauthenticatedMiner
        />,
      );

      const actions = readActions();
      expect(actions.find((a) => a.action === deviceActions.unpair)?.disabled).not.toBe(true);
      expect(actions.find((a) => a.action === deviceActions.reboot)?.disabled).toBe(true);
      expect(actions.find((a) => a.action === settingsActions.miningPool)?.disabled).toBe(true);
    });

    test("leaves every action enabled when nothing in the selection needs authentication", () => {
      mockUseMinerActions.mockReturnValueOnce({
        ...createMockMinerActionsReturn(null),
        popoverActions: popoverActionsFixture,
      });

      render(
        <MinerActionsMenu
          selectedMiners={["miner-1"]}
          selectionMode="subset"
          totalCount={1}
          miners={{
            "miner-1": create(MinerStateSnapshotSchema, { pairingStatus: PairingStatus.PAIRED }),
          }}
        />,
      );

      const actions = readActions();
      expect(actions.every((a) => a.disabled !== true)).toBe(true);
    });

    test("disables the matching quick-action button on desktop", () => {
      mockUseWindowDimensions.mockReturnValueOnce({ isPhone: false, isTablet: false });
      mockUseMinerActions.mockReturnValueOnce({
        ...createMockMinerActionsReturn(null),
        popoverActions: popoverActionsFixture,
      });

      render(
        <MinerActionsMenu
          selectedMiners={["miner-1"]}
          selectionMode="subset"
          totalCount={1}
          miners={{
            "miner-1": create(MinerStateSnapshotSchema, { pairingStatus: PairingStatus.AUTHENTICATION_NEEDED }),
          }}
        />,
      );

      const rebootButton = screen.getByTestId(`actions-menu-quick-action-${deviceActions.reboot}`);
      expect(rebootButton).toBeDisabled();
    });
  });
});
