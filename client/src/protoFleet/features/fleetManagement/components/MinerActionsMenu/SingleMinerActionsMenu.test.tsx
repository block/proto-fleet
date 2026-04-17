import { Fragment, type ReactNode } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { deviceActions, settingsActions } from "./constants";
import SingleMinerActionsMenu from "./SingleMinerActionsMenu";

const mockWindowOpen = vi.fn();
vi.stubGlobal("open", mockWindowOpen);

const {
  mockAuthenticateFleetModal,
  mockBulkActionConfirmDialog,
  mockWithCapabilityCheck,
  mockPushToast,
  mockRemoveToast,
  mockStreamCommandBatchUpdates,
  mockUpdateSingleWorkerName,
  mockUpdateToast,
  mockUpdateWorkerNameDialog,
  mockUseMinerCommand,
  mockUseMinerActions,
  mockUseUpdateWorkerNames,
} = vi.hoisted(() => {
  const mockWithCapabilityCheck = vi.fn(async (_action: string, onProceed: (...args: unknown[]) => void) => {
    onProceed(undefined, undefined);
  });
  const mockUpdateSingleWorkerName = vi.fn();
  const mockStreamCommandBatchUpdates = vi.fn();

  return {
    mockAuthenticateFleetModal: vi.fn(() => null),
    mockBulkActionConfirmDialog: vi.fn(() => null),
    mockWithCapabilityCheck,
    mockPushToast: vi.fn(() => 1),
    mockRemoveToast: vi.fn(),
    mockStreamCommandBatchUpdates,
    mockUpdateSingleWorkerName,
    mockUpdateToast: vi.fn(),
    mockUpdateWorkerNameDialog: vi.fn(() => null),
    mockUseMinerCommand: vi.fn(() => ({
      streamCommandBatchUpdates: mockStreamCommandBatchUpdates,
    })),
    mockUseMinerActions: vi.fn(() => ({
      currentAction: null,
      popoverActions: [] as any[],
      handleConfirmation: vi.fn(),
      handleCancel: vi.fn(),
      handleMiningPoolSuccess: vi.fn(),
      handleMiningPoolError: vi.fn(),
      showPoolSelectionPage: false,
      fleetCredentials: undefined,
      showManagePowerModal: false,
      handleManagePowerConfirm: vi.fn(),
      handleManagePowerDismiss: vi.fn(),
      showFirmwareUpdateModal: false,
      handleFirmwareUpdateConfirm: vi.fn(),
      handleFirmwareUpdateDismiss: vi.fn(),
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
      unsupportedMinersInfo: {
        visible: false,
        unsupportedGroups: [],
        totalUnsupportedCount: 0,
        noneSupported: false,
      },
      handleUnsupportedMinersContinue: vi.fn(),
      handleUnsupportedMinersDismiss: vi.fn(),
      showManageSecurityModal: false,
      minerGroups: [],
      handleUpdateGroup: vi.fn(),
      handleSecurityModalClose: vi.fn(),
      showRenameDialog: false,
      handleRenameOpen: vi.fn(),
      handleRenameConfirm: vi.fn(),
      handleRenameDismiss: vi.fn(),
      showAddToGroupModal: false,
      handleAddToGroupDismiss: vi.fn(),
    })),
    mockUseUpdateWorkerNames: vi.fn(() => ({
      updateSingleWorkerName: mockUpdateSingleWorkerName,
    })),
  };
});

vi.mock("./useMinerActions", () => ({
  useMinerActions: mockUseMinerActions,
}));

vi.mock("@/protoFleet/api/useUpdateWorkerNames", () => ({
  default: mockUseUpdateWorkerNames,
}));

vi.mock("@/protoFleet/api/useMinerCommand", () => ({
  useMinerCommand: mockUseMinerCommand,
}));

vi.mock("@/protoFleet/store/hooks/useFleet", () => ({
  useMinerDeviceStatus: vi.fn(() => undefined),
}));

vi.mock("@/shared/components/Popover", () => ({
  PopoverProvider: ({ children }: { children: ReactNode }) => <Fragment>{children}</Fragment>,
  usePopover: () => ({
    triggerRef: { current: null },
    setPopoverRenderMode: vi.fn(),
  }),
  popoverSizes: { small: "small" },
  default: ({ children, testId }: { children: ReactNode; testId?: string }) => (
    <div data-testid={testId}>{children}</div>
  ),
}));

vi.mock("@/shared/hooks/useClickOutside", () => ({
  useClickOutside: vi.fn(),
}));

vi.mock("../ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./RenameMinerDialog", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./ManagePowerModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./FirmwareUpdateModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./CoolingModeModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({
  default: mockAuthenticateFleetModal,
}));

vi.mock("./ManageSecurity", () => ({
  ManageSecurityModal: vi.fn(() => null),
  UpdateMinerPasswordModal: vi.fn(() => null),
}));

vi.mock("../BulkActions/UnsupportedMinersModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("../BulkActions/BulkActionConfirmDialog", () => ({
  default: mockBulkActionConfirmDialog,
}));

vi.mock("./AddToGroupModal", () => ({
  default: vi.fn(() => null),
}));

vi.mock("./UpdateWorkerNameDialog", () => ({
  default: mockUpdateWorkerNameDialog,
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: mockPushToast,
  removeToast: mockRemoveToast,
  updateToast: mockUpdateToast,
  STATUSES: {
    loading: "loading",
    success: "success",
    error: "error",
  },
}));

describe("SingleMinerActionsMenu", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockPushToast.mockReturnValue(1);
    mockStreamCommandBatchUpdates.mockResolvedValue(undefined);
  });

  it("renders 'Update worker name' when pool editing is available", () => {
    mockUseMinerActions.mockReturnValue({
      currentAction: null,
      popoverActions: [
        {
          action: settingsActions.miningPool,
          title: "Edit pool",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
      ] as any[],
      handleConfirmation: vi.fn(),
      handleCancel: vi.fn(),
      handleMiningPoolSuccess: vi.fn(),
      handleMiningPoolError: vi.fn(),
      showPoolSelectionPage: false,
      fleetCredentials: undefined,
      showManagePowerModal: false,
      handleManagePowerConfirm: vi.fn(),
      handleManagePowerDismiss: vi.fn(),
      showFirmwareUpdateModal: false,
      handleFirmwareUpdateConfirm: vi.fn(),
      handleFirmwareUpdateDismiss: vi.fn(),
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
      unsupportedMinersInfo: {
        visible: false,
        unsupportedGroups: [],
        totalUnsupportedCount: 0,
        noneSupported: false,
      },
      handleUnsupportedMinersContinue: vi.fn(),
      handleUnsupportedMinersDismiss: vi.fn(),
      showManageSecurityModal: false,
      minerGroups: [],
      handleUpdateGroup: vi.fn(),
      handleSecurityModalClose: vi.fn(),
      showRenameDialog: false,
      handleRenameOpen: vi.fn(),
      handleRenameConfirm: vi.fn(),
      handleRenameDismiss: vi.fn(),
      showAddToGroupModal: false,
      handleAddToGroupDismiss: vi.fn(),
    });

    render(<SingleMinerActionsMenu deviceIdentifier="test-device-123" workerName="worker-old" />);

    fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));

    expect(screen.getByText("Update worker name")).toBeInTheDocument();
    expect(screen.getByTestId("update-worker-names-popover-button")).toBeInTheDocument();
  });

  it("does not render 'View miner' menu item when minerUrl is not provided", () => {
    render(<SingleMinerActionsMenu deviceIdentifier="test-device-123" />);

    fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));

    expect(screen.queryByText("View miner")).not.toBeInTheDocument();
  });

  it("renders 'View miner' menu item when minerUrl is provided", () => {
    render(<SingleMinerActionsMenu deviceIdentifier="test-device-123" minerUrl="http://192.168.1.1" />);

    fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));

    expect(screen.getByText("View miner")).toBeInTheDocument();
    expect(screen.getByTestId("viewMiner-popover-button")).toBeInTheDocument();
  });

  it("opens miner URL in new tab when 'View miner' is clicked", () => {
    const minerUrl = "http://192.168.1.42";
    render(<SingleMinerActionsMenu deviceIdentifier="my-device-abc" minerUrl={minerUrl} />);

    fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));
    fireEvent.click(screen.getByTestId("viewMiner-popover-button"));

    expect(mockWindowOpen).toHaveBeenCalledWith(minerUrl, "_blank", "noopener,noreferrer");
  });

  it("authenticates before updating a single worker name", async () => {
    mockUpdateSingleWorkerName.mockResolvedValue({
      updatedCount: 1,
      unchangedCount: 0,
      failedCount: 0,
      batchIdentifier: "batch-1",
    });
    mockStreamCommandBatchUpdates.mockImplementation(async ({ onStreamData }) => {
      onStreamData({
        status: {
          commandBatchDeviceCount: {
            total: 1,
            success: 1,
            failure: 0,
          },
        },
      });
    });

    const onActionComplete = vi.fn();
    const onRefetchMiners = vi.fn();
    const onWorkerNameUpdated = vi.fn();

    mockUseMinerActions.mockReturnValue({
      currentAction: null,
      popoverActions: [
        {
          action: settingsActions.miningPool,
          title: "Edit pool",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
      ] as any[],
      handleConfirmation: vi.fn(),
      handleCancel: vi.fn(),
      handleMiningPoolSuccess: vi.fn(),
      handleMiningPoolError: vi.fn(),
      showPoolSelectionPage: false,
      fleetCredentials: undefined,
      showManagePowerModal: false,
      handleManagePowerConfirm: vi.fn(),
      handleManagePowerDismiss: vi.fn(),
      showFirmwareUpdateModal: false,
      handleFirmwareUpdateConfirm: vi.fn(),
      handleFirmwareUpdateDismiss: vi.fn(),
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
      unsupportedMinersInfo: {
        visible: false,
        unsupportedGroups: [],
        totalUnsupportedCount: 0,
        noneSupported: false,
      },
      handleUnsupportedMinersContinue: vi.fn(),
      handleUnsupportedMinersDismiss: vi.fn(),
      showManageSecurityModal: false,
      minerGroups: [],
      handleUpdateGroup: vi.fn(),
      handleSecurityModalClose: vi.fn(),
      showRenameDialog: false,
      handleRenameOpen: vi.fn(),
      handleRenameConfirm: vi.fn(),
      handleRenameDismiss: vi.fn(),
      showAddToGroupModal: false,
      handleAddToGroupDismiss: vi.fn(),
    });

    render(
      <SingleMinerActionsMenu
        deviceIdentifier="test-device-123"
        workerName="worker-old"
        onActionComplete={onActionComplete}
        onRefetchMiners={onRefetchMiners}
        onWorkerNameUpdated={onWorkerNameUpdated}
      />,
    );

    fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));
    fireEvent.click(screen.getByTestId("update-worker-names-popover-button"));

    expect(mockWithCapabilityCheck).toHaveBeenCalledWith(settingsActions.updateWorkerNames, expect.any(Function));

    const workerNameAuthProps = (
      mockAuthenticateFleetModal.mock.calls as unknown as Array<
        [{ purpose?: string; open: boolean; onAuthenticated: (username: string, password: string) => void }]
      >
    )
      .map(([props]) => props)
      .filter((props) => props.purpose === "workerNames");
    const latestWorkerNameAuthProps = workerNameAuthProps[workerNameAuthProps.length - 1];

    expect(latestWorkerNameAuthProps?.open).toBe(true);

    await act(async () => {
      latestWorkerNameAuthProps?.onAuthenticated("testuser", "testpass");
    });

    const updateWorkerNameDialogProps = (
      mockUpdateWorkerNameDialog.mock.calls as unknown as Array<
        [{ open: boolean; currentWorkerName?: string; onConfirm: (name: string) => void }]
      >
    ).map(([props]) => props);
    const latestUpdateWorkerNameDialogProps = updateWorkerNameDialogProps[updateWorkerNameDialogProps.length - 1];

    expect(latestUpdateWorkerNameDialogProps?.open).toBe(true);
    expect(latestUpdateWorkerNameDialogProps?.currentWorkerName).toBe("worker-old");

    await act(async () => {
      latestUpdateWorkerNameDialogProps?.onConfirm("worker-new");
    });

    await waitFor(() => {
      expect(mockUpdateSingleWorkerName).toHaveBeenCalledWith("test-device-123", "worker-new", "testuser", "testpass");
    });
    expect(mockStreamCommandBatchUpdates).toHaveBeenCalled();

    expect(mockPushToast).toHaveBeenCalledWith({
      message: "Updating worker name",
      status: "loading",
      longRunning: true,
    });
    expect(mockUpdateToast).toHaveBeenCalledWith(1, {
      message: "Worker name updated",
      status: "success",
    });
    expect(onWorkerNameUpdated).toHaveBeenCalledWith("test-device-123", "worker-new");
    expect(onRefetchMiners).toHaveBeenCalledTimes(1);
    expect(onActionComplete).toHaveBeenCalledTimes(1);
  });

  it("shows an unchanged toast when an async worker-name update makes no changes", async () => {
    mockUpdateSingleWorkerName.mockResolvedValue({
      updatedCount: 0,
      unchangedCount: 1,
      failedCount: 0,
      batchIdentifier: "batch-1",
    });

    const onActionComplete = vi.fn();
    const onRefetchMiners = vi.fn();
    const onWorkerNameUpdated = vi.fn();

    mockUseMinerActions.mockReturnValue({
      currentAction: null,
      popoverActions: [
        {
          action: settingsActions.miningPool,
          title: "Edit pool",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
      ] as any[],
      handleConfirmation: vi.fn(),
      handleCancel: vi.fn(),
      handleMiningPoolSuccess: vi.fn(),
      handleMiningPoolError: vi.fn(),
      showPoolSelectionPage: false,
      fleetCredentials: undefined,
      showManagePowerModal: false,
      handleManagePowerConfirm: vi.fn(),
      handleManagePowerDismiss: vi.fn(),
      showFirmwareUpdateModal: false,
      handleFirmwareUpdateConfirm: vi.fn(),
      handleFirmwareUpdateDismiss: vi.fn(),
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
      unsupportedMinersInfo: {
        visible: false,
        unsupportedGroups: [],
        totalUnsupportedCount: 0,
        noneSupported: false,
      },
      handleUnsupportedMinersContinue: vi.fn(),
      handleUnsupportedMinersDismiss: vi.fn(),
      showManageSecurityModal: false,
      minerGroups: [],
      handleUpdateGroup: vi.fn(),
      handleSecurityModalClose: vi.fn(),
      showRenameDialog: false,
      handleRenameOpen: vi.fn(),
      handleRenameConfirm: vi.fn(),
      handleRenameDismiss: vi.fn(),
      showAddToGroupModal: false,
      handleAddToGroupDismiss: vi.fn(),
    });

    render(
      <SingleMinerActionsMenu
        deviceIdentifier="test-device-123"
        workerName="worker-old"
        onActionComplete={onActionComplete}
        onRefetchMiners={onRefetchMiners}
        onWorkerNameUpdated={onWorkerNameUpdated}
      />,
    );

    fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));
    fireEvent.click(screen.getByTestId("update-worker-names-popover-button"));

    const workerNameAuthProps = (
      mockAuthenticateFleetModal.mock.calls as unknown as Array<
        [{ purpose?: string; open: boolean; onAuthenticated: (username: string, password: string) => void }]
      >
    )
      .map(([props]) => props)
      .filter((props) => props.purpose === "workerNames");
    const latestWorkerNameAuthProps = workerNameAuthProps[workerNameAuthProps.length - 1];

    await act(async () => {
      latestWorkerNameAuthProps?.onAuthenticated("testuser", "testpass");
    });

    const updateWorkerNameDialogProps = (
      mockUpdateWorkerNameDialog.mock.calls as unknown as Array<
        [{ open: boolean; currentWorkerName?: string; onConfirm: (name: string) => void }]
      >
    ).map(([props]) => props);
    const latestUpdateWorkerNameDialogProps = updateWorkerNameDialogProps[updateWorkerNameDialogProps.length - 1];

    await act(async () => {
      latestUpdateWorkerNameDialogProps?.onConfirm("worker-old");
    });

    await waitFor(() => {
      expect(mockUpdateSingleWorkerName).toHaveBeenCalledWith("test-device-123", "worker-old", "testuser", "testpass");
    });

    expect(mockUpdateToast).toHaveBeenCalledWith(1, {
      message: "Worker name unchanged",
      status: "success",
    });
    expect(onWorkerNameUpdated).not.toHaveBeenCalled();
    expect(onRefetchMiners).toHaveBeenCalledTimes(1);
    expect(onActionComplete).toHaveBeenCalledTimes(1);
  });

  describe("needsAuthentication filtering", () => {
    const allPopoverActions = [
      {
        action: deviceActions.reboot,
        title: "Reboot",
        icon: null,
        actionHandler: vi.fn(),
        requiresConfirmation: true,
        confirmation: {
          title: "Reboot 1 miner?",
          subtitle: "",
          confirmAction: { title: "Reboot" },
          testId: "reboot-confirm",
        },
      },
      {
        action: deviceActions.blinkLEDs,
        title: "Blink LEDs",
        icon: null,
        actionHandler: vi.fn(),
        requiresConfirmation: false,
      },
      {
        action: settingsActions.miningPool,
        title: "Edit pool",
        icon: null,
        actionHandler: vi.fn(),
        requiresConfirmation: false,
      },
      {
        action: deviceActions.unpair,
        title: "Unpair",
        icon: null,
        actionHandler: vi.fn(),
        requiresConfirmation: true,
        confirmation: { title: "Unpair?", subtitle: "", confirmAction: { title: "Unpair" }, testId: "unpair-confirm" },
      },
    ] as any[];

    function renderWithActions(
      props: Partial<Parameters<typeof SingleMinerActionsMenu>[0]> = {},
      mockOverrides: Record<string, unknown> = {},
    ) {
      mockUseMinerActions.mockReturnValue({
        currentAction: null,
        popoverActions: allPopoverActions,
        handleConfirmation: vi.fn(),
        handleCancel: vi.fn(),
        handleMiningPoolSuccess: vi.fn(),
        handleMiningPoolError: vi.fn(),
        showPoolSelectionPage: false,
        fleetCredentials: undefined,
        showManagePowerModal: false,
        handleManagePowerConfirm: vi.fn(),
        handleManagePowerDismiss: vi.fn(),
        showFirmwareUpdateModal: false,
        handleFirmwareUpdateConfirm: vi.fn(),
        handleFirmwareUpdateDismiss: vi.fn(),
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
        unsupportedMinersInfo: {
          visible: false,
          unsupportedGroups: [],
          totalUnsupportedCount: 0,
          noneSupported: false,
        },
        handleUnsupportedMinersContinue: vi.fn(),
        handleUnsupportedMinersDismiss: vi.fn(),
        showManageSecurityModal: false,
        minerGroups: [],
        handleUpdateGroup: vi.fn(),
        handleSecurityModalClose: vi.fn(),
        showRenameDialog: false,
        handleRenameOpen: vi.fn(),
        handleRenameConfirm: vi.fn(),
        handleRenameDismiss: vi.fn(),
        showAddToGroupModal: false,
        handleAddToGroupDismiss: vi.fn(),
        ...mockOverrides,
      });

      return render(<SingleMinerActionsMenu deviceIdentifier="test-device" {...props} />);
    }

    it("shows only Unpair when needsAuthentication is true and no minerUrl", () => {
      renderWithActions({ needsAuthentication: true });

      fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));

      expect(screen.getByText("Unpair")).toBeInTheDocument();
      expect(screen.queryByText("Reboot")).not.toBeInTheDocument();
      expect(screen.queryByText("Blink LEDs")).not.toBeInTheDocument();
      expect(screen.queryByText("Edit pool")).not.toBeInTheDocument();
      expect(screen.queryByText("View miner")).not.toBeInTheDocument();
    });

    it("shows Unpair and View miner when needsAuthentication is true and minerUrl is set", () => {
      renderWithActions({ needsAuthentication: true, minerUrl: "http://192.168.1.1" });

      fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));

      expect(screen.getByText("View miner")).toBeInTheDocument();
      expect(screen.getByText("Unpair")).toBeInTheDocument();
      expect(screen.queryByText("Reboot")).not.toBeInTheDocument();
      expect(screen.queryByText("Blink LEDs")).not.toBeInTheDocument();
      expect(screen.queryByText("Edit pool")).not.toBeInTheDocument();
    });

    it("does not disable the menu button when needsAuthentication is true", () => {
      renderWithActions({ needsAuthentication: true });

      const button = screen.getByTestId("single-miner-actions-menu-button");
      expect(button).not.toBeDisabled();
    });

    it("opens Unpair confirmation dialog when Unpair is clicked for an unauthenticated miner", () => {
      renderWithActions({ needsAuthentication: true }, { currentAction: deviceActions.unpair });

      fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));
      fireEvent.click(screen.getByTestId("unpair-popover-button"));

      const dialogCalls = mockBulkActionConfirmDialog.mock.calls as unknown as Array<
        [{ open: boolean; actionConfirmation: { title: string } }]
      >;
      const unpairDialogCall = dialogCalls.find(([props]) => props.open);
      expect(unpairDialogCall).toBeDefined();
      expect(unpairDialogCall![0].actionConfirmation.title).toBe("Unpair?");
    });

    it("preserves pending confirmation dialog when auth status hides the triggering action", () => {
      renderWithActions({ needsAuthentication: true }, { currentAction: deviceActions.reboot });

      const dialogCalls = mockBulkActionConfirmDialog.mock.calls as unknown as Array<
        [{ open: boolean; actionConfirmation: { title: string } }]
      >;
      const rebootDialogCall = dialogCalls.find(([props]) => props.actionConfirmation?.title?.includes("Reboot"));
      expect(rebootDialogCall).toBeDefined();
    });

    it("shows all actions when needsAuthentication is false", () => {
      renderWithActions({ needsAuthentication: false });

      fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));

      expect(screen.getByText("Reboot")).toBeInTheDocument();
      expect(screen.getByText("Blink LEDs")).toBeInTheDocument();
      expect(screen.getByText("Edit pool")).toBeInTheDocument();
      expect(screen.getByText("Unpair")).toBeInTheDocument();
    });
  });

  it("shows an error toast when a streamed worker-name update reports an immediate failure", async () => {
    mockUpdateSingleWorkerName.mockResolvedValue({
      updatedCount: 0,
      unchangedCount: 0,
      failedCount: 1,
      batchIdentifier: "batch-1",
    });

    const onActionComplete = vi.fn();
    const onRefetchMiners = vi.fn();
    const onWorkerNameUpdated = vi.fn();

    mockUseMinerActions.mockReturnValue({
      currentAction: null,
      popoverActions: [
        {
          action: settingsActions.miningPool,
          title: "Edit pool",
          icon: null,
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
      ] as any[],
      handleConfirmation: vi.fn(),
      handleCancel: vi.fn(),
      handleMiningPoolSuccess: vi.fn(),
      handleMiningPoolError: vi.fn(),
      showPoolSelectionPage: false,
      fleetCredentials: undefined,
      showManagePowerModal: false,
      handleManagePowerConfirm: vi.fn(),
      handleManagePowerDismiss: vi.fn(),
      showFirmwareUpdateModal: false,
      handleFirmwareUpdateConfirm: vi.fn(),
      handleFirmwareUpdateDismiss: vi.fn(),
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
      unsupportedMinersInfo: {
        visible: false,
        unsupportedGroups: [],
        totalUnsupportedCount: 0,
        noneSupported: false,
      },
      handleUnsupportedMinersContinue: vi.fn(),
      handleUnsupportedMinersDismiss: vi.fn(),
      showManageSecurityModal: false,
      minerGroups: [],
      handleUpdateGroup: vi.fn(),
      handleSecurityModalClose: vi.fn(),
      showRenameDialog: false,
      handleRenameOpen: vi.fn(),
      handleRenameConfirm: vi.fn(),
      handleRenameDismiss: vi.fn(),
      showAddToGroupModal: false,
      handleAddToGroupDismiss: vi.fn(),
    });

    render(
      <SingleMinerActionsMenu
        deviceIdentifier="test-device-123"
        workerName="worker-old"
        onActionComplete={onActionComplete}
        onRefetchMiners={onRefetchMiners}
        onWorkerNameUpdated={onWorkerNameUpdated}
      />,
    );

    fireEvent.click(screen.getByTestId("single-miner-actions-menu-button"));
    fireEvent.click(screen.getByTestId("update-worker-names-popover-button"));

    const workerNameAuthProps = (
      mockAuthenticateFleetModal.mock.calls as unknown as Array<
        [{ purpose?: string; open: boolean; onAuthenticated: (username: string, password: string) => void }]
      >
    )
      .map(([props]) => props)
      .filter((props) => props.purpose === "workerNames");
    const latestWorkerNameAuthProps = workerNameAuthProps[workerNameAuthProps.length - 1];

    await act(async () => {
      latestWorkerNameAuthProps?.onAuthenticated("testuser", "testpass");
    });

    const updateWorkerNameDialogProps = (
      mockUpdateWorkerNameDialog.mock.calls as unknown as Array<
        [{ open: boolean; currentWorkerName?: string; onConfirm: (name: string) => void }]
      >
    ).map(([props]) => props);
    const latestUpdateWorkerNameDialogProps = updateWorkerNameDialogProps[updateWorkerNameDialogProps.length - 1];

    await act(async () => {
      latestUpdateWorkerNameDialogProps?.onConfirm("worker-new");
    });

    await waitFor(() => {
      expect(mockUpdateSingleWorkerName).toHaveBeenCalledWith("test-device-123", "worker-new", "testuser", "testpass");
    });

    expect(mockUpdateToast).toHaveBeenCalledWith(1, {
      message: "Failed to update worker name",
      status: "error",
    });
    expect(onWorkerNameUpdated).not.toHaveBeenCalled();
    expect(onRefetchMiners).not.toHaveBeenCalled();
    expect(onActionComplete).toHaveBeenCalledTimes(1);
  });
});
