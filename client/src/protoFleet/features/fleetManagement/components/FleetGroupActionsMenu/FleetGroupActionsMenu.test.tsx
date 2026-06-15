import { Fragment, type ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { deviceActions, performanceActions, settingsActions } from "../MinerActionsMenu/constants";

// Hoisted mocks — vi.mock factories are pulled above the file's top
// level, so the mock fns they capture must come from vi.hoisted.
const {
  mockListMinerStateSnapshots,
  mockUseMinerActions,
  mockHandleConfirmation,
  mockHandleCancel,
  mockHandleManagePowerConfirm,
  mockHandleFirmwareUpdateConfirm,
  mockHandleMiningPoolSuccess,
  mockHandleUnsupportedMinersContinue,
  mockHandleUnsupportedMinersDismiss,
  mockHandleManagePowerDismiss,
  mockHandleFirmwareUpdateDismiss,
  mockHandleAuthDismiss,
  mockHandleFleetAuthenticated,
  mockHandleMiningPoolError,
  mockHandleMiningPoolWarning,
  mockPushToast,
  mockRemoveToast,
  mockUpdateToast,
} = vi.hoisted(() => ({
  mockListMinerStateSnapshots: vi.fn(),
  mockUseMinerActions: vi.fn(),
  mockHandleConfirmation: vi.fn(),
  mockHandleCancel: vi.fn(),
  mockHandleManagePowerConfirm: vi.fn(),
  mockHandleFirmwareUpdateConfirm: vi.fn(),
  mockHandleMiningPoolSuccess: vi.fn(),
  mockHandleUnsupportedMinersContinue: vi.fn(),
  mockHandleUnsupportedMinersDismiss: vi.fn(),
  mockHandleManagePowerDismiss: vi.fn(),
  mockHandleFirmwareUpdateDismiss: vi.fn(),
  mockHandleAuthDismiss: vi.fn(),
  mockHandleFleetAuthenticated: vi.fn(),
  mockHandleMiningPoolError: vi.fn(),
  mockHandleMiningPoolWarning: vi.fn(),
  mockPushToast: vi.fn(),
  mockRemoveToast: vi.fn(),
  mockUpdateToast: vi.fn(),
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

vi.mock("@/shared/hooks/useClickOutside", () => ({ useClickOutside: vi.fn() }));

vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: {
    listMinerStateSnapshots: (...args: unknown[]) => mockListMinerStateSnapshots(...args),
  },
}));

// The Real Deal — mock useMinerActions so the test doesn't drag in
// the full fleet management store, useFleet, useMinerCommand, batch
// slice, capability check stack, etc. Returns the minimum shape
// FleetGroupActionsMenu consumes.
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions", () => ({
  useMinerActions: mockUseMinerActions,
}));

vi.mock("@/protoFleet/features/fleetManagement/hooks/useBatchOperations", () => ({
  useBatchActions: () => ({
    startBatchOperation: vi.fn(),
    completeBatchOperation: vi.fn(),
    removeDevicesFromBatch: vi.fn(),
  }),
}));

vi.mock(import("@/shared/features/toaster"), async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    pushToast: mockPushToast,
    removeToast: mockRemoveToast,
    updateToast: mockUpdateToast,
  };
});

// Grant every permission the menu consults so the wired entries
// render in tests. Production filtering is verified by the live
// permission catalog plus the server gate; the menu's job here is
// only to surface what the role can invoke. Partial-mock so the
// surrounding store hooks (useAuthErrors, useBatch*, ...) stay live.
vi.mock(import("@/protoFleet/store"), async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    usePermissions: () => [
      "miner:read",
      "miner:blink_led",
      "miner:download_logs",
      "miner:firmware_update",
      "miner:reboot",
      "miner:stop_mining",
      "miner:start_mining",
      "miner:delete",
      "miner:set_power_target",
      "miner:set_cooling_mode",
      "miner:rename",
      "miner:update_worker_names",
      "miner:update_password",
      "miner:update_pools",
      "pool:read",
      "rack:read",
      "rack:manage",
      "site:read",
      "site:manage",
    ],
  };
});

vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({ default: () => null }));
vi.mock("@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: () => null,
}));
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/FirmwareUpdateModal", () => ({
  default: () => null,
}));
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManagePowerModal", () => ({
  default: () => null,
}));
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManageSecurity", () => ({
  ManageSecurityModal: () => null,
  UpdateMinerPasswordModal: () => null,
}));

vi.mock("../BulkActions/BulkActionConfirmDialog", () => ({
  default: ({
    open,
    actionConfirmation,
    onConfirmation,
    testId,
  }: {
    open: boolean;
    actionConfirmation: { title: string; subtitle: string };
    onConfirmation: () => void;
    testId?: string;
  }) =>
    open ? (
      <div data-testid={testId}>
        <span>{actionConfirmation.title}</span>
        <span>{actionConfirmation.subtitle}</span>
        <button onClick={onConfirmation} data-testid="confirm-button">
          Confirm
        </button>
      </div>
    ) : null,
}));

vi.mock("../BulkActions/UnsupportedMinersModal", () => ({ default: () => null }));

// eslint-disable-next-line import-x/order -- import must come after vi.mock calls
import FleetGroupActionsMenu from "./FleetGroupActionsMenu";

const buildPopoverActions = () => [
  {
    action: deviceActions.shutdown,
    title: "Sleep",
    icon: null,
    actionHandler: vi.fn(),
    requiresConfirmation: true,
    confirmation: {
      title: "Sleep 3 miners?",
      subtitle: "Will go to sleep.",
      confirmAction: { title: "Sleep", variant: "primary" },
      testId: "shutdown-confirm-button",
    },
  },
  {
    action: deviceActions.wakeUp,
    title: "Wake up",
    icon: null,
    actionHandler: vi.fn(),
    requiresConfirmation: true,
    confirmation: {
      title: "Wake 3 miners?",
      subtitle: "Will wake up.",
      confirmAction: { title: "Wake", variant: "primary" },
      testId: "wake-confirm-button",
    },
  },
  {
    action: deviceActions.reboot,
    title: "Reboot",
    icon: null,
    actionHandler: vi.fn(),
    requiresConfirmation: true,
    confirmation: {
      title: "Reboot 3 miners?",
      subtitle: "Will reboot.",
      confirmAction: { title: "Reboot", variant: "primary" },
      testId: "reboot-confirm-button",
    },
  },
  {
    action: deviceActions.downloadLogs,
    title: "Download logs",
    icon: null,
    actionHandler: vi.fn(),
    requiresConfirmation: false,
  },
  {
    action: performanceActions.managePower,
    title: "Manage power",
    icon: null,
    actionHandler: vi.fn(),
    requiresConfirmation: false,
  },
  {
    action: deviceActions.firmwareUpdate,
    title: "Update firmware",
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
];

const makeMinerActions = () => ({
  currentAction: null as string | null,
  popoverActions: buildPopoverActions(),
  handleConfirmation: mockHandleConfirmation,
  handleCancel: mockHandleCancel,
  handleManagePowerConfirm: mockHandleManagePowerConfirm,
  handleManagePowerDismiss: mockHandleManagePowerDismiss,
  handleFirmwareUpdateConfirm: mockHandleFirmwareUpdateConfirm,
  handleFirmwareUpdateDismiss: mockHandleFirmwareUpdateDismiss,
  handleMiningPoolSuccess: mockHandleMiningPoolSuccess,
  handleMiningPoolError: mockHandleMiningPoolError,
  handleMiningPoolWarning: mockHandleMiningPoolWarning,
  handleUnsupportedMinersContinue: mockHandleUnsupportedMinersContinue,
  handleUnsupportedMinersDismiss: mockHandleUnsupportedMinersDismiss,
  handleAuthDismiss: mockHandleAuthDismiss,
  handleFleetAuthenticated: mockHandleFleetAuthenticated,
  unsupportedMinersInfo: {
    visible: false,
    unsupportedGroups: [],
    totalUnsupportedCount: 0,
    noneSupported: false,
  },
  showManagePowerModal: false,
  showFirmwareUpdateModal: false,
  showPoolSelectionPage: false,
  showAuthenticateFleetModal: false,
  authenticationPurpose: null,
  fleetCredentials: undefined,
});

beforeEach(() => {
  vi.clearAllMocks();
  mockPushToast.mockReturnValue(1);
  mockListMinerStateSnapshots.mockResolvedValue({
    miners: [{ deviceIdentifier: "miner-a" }, { deviceIdentifier: "miner-b" }, { deviceIdentifier: "miner-c" }],
    cursor: "",
  });
  mockUseMinerActions.mockReturnValue(makeMinerActions());
});

describe("FleetGroupActionsMenu", () => {
  it("renders the wired bulk actions plus extras", () => {
    render(
      <FleetGroupActionsMenu
        scopes={[{ kind: "building", id: 42n, name: "Alpha" }]}
        ariaLabel="Actions for Alpha"
        testIdPrefix="alpha"
        extraActions={[{ label: "View racks", onClick: vi.fn() }]}
      />,
    );
    fireEvent.click(screen.getByTestId("alpha-trigger"));
    for (const label of [
      "Sleep miners",
      "Wake miners",
      "Reboot miners",
      "Download logs",
      "Manage power",
      "Update firmware",
      "Edit pool",
      "View racks",
      "Manage security",
      "Unpair miners",
    ]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
  });

  it("fires the hook's Sleep handler after the device IDs land", async () => {
    render(
      <FleetGroupActionsMenu
        scopes={[{ kind: "building", id: 42n, name: "Alpha" }]}
        ariaLabel="Actions for Alpha"
        testIdPrefix="alpha"
      />,
    );
    fireEvent.click(screen.getByTestId("alpha-trigger"));
    fireEvent.click(screen.getByText("Sleep miners"));

    await waitFor(() => {
      expect(mockListMinerStateSnapshots).toHaveBeenCalledWith(
        expect.objectContaining({
          filter: expect.objectContaining({ buildingIds: [42n] }),
        }),
      );
    });

    // The hook's Sleep popoverActions entry was rebuilt once
    // selectedMiners landed, and its handler fired exactly once.
    await waitFor(() => {
      const fired = mockUseMinerActions.mock.results
        .flatMap((result) => (result.value as ReturnType<typeof makeMinerActions>).popoverActions)
        .find((entry) => entry.action === deviceActions.shutdown);
      expect(fired?.actionHandler).toHaveBeenCalledTimes(1);
    });
  });

  it("Site scope filters listMinerStateSnapshots by siteIds", async () => {
    render(
      <FleetGroupActionsMenu
        scopes={[{ kind: "site", id: 7n, name: "North" }]}
        ariaLabel="Actions for North"
        testIdPrefix="north"
      />,
    );
    fireEvent.click(screen.getByTestId("north-trigger"));
    fireEvent.click(screen.getByText("Reboot miners"));

    await waitFor(() => {
      expect(mockListMinerStateSnapshots).toHaveBeenCalledWith(
        expect.objectContaining({
          filter: expect.objectContaining({ siteIds: [7n] }),
        }),
      );
    });
  });

  it("warns when the scope has no miners and skips the dispatch", async () => {
    mockListMinerStateSnapshots.mockResolvedValueOnce({ miners: [], cursor: "" });
    render(
      <FleetGroupActionsMenu
        scopes={[{ kind: "building", id: 42n, name: "Alpha" }]}
        ariaLabel="Actions for Alpha"
        testIdPrefix="alpha"
      />,
    );
    fireEvent.click(screen.getByTestId("alpha-trigger"));
    fireEvent.click(screen.getByText("Sleep miners"));

    await waitFor(() => {
      // No handler should have fired because the dispatch effect
      // short-circuits on an empty selection.
      const fired = mockUseMinerActions.mock.results
        .flatMap((result) => (result.value as ReturnType<typeof makeMinerActions>).popoverActions)
        .find((entry) => entry.action === deviceActions.shutdown);
      expect(fired?.actionHandler).not.toHaveBeenCalled();
    });
    expect(mockPushToast).toHaveBeenLastCalledWith({
      message: "Building Alpha contains no miners.",
      status: "error",
    });
  });

  it("combines same-kind scopes into one snapshot filter and hides row-only extras", async () => {
    render(
      <FleetGroupActionsMenu
        scopes={[
          { kind: "site", id: 7n, name: "North" },
          { kind: "site", id: 8n, name: "South" },
        ]}
        ariaLabel="Bulk actions for selected sites"
        testIdPrefix="sites"
        extraActions={[{ label: "View site", onClick: vi.fn() }]}
      />,
    );
    fireEvent.click(screen.getByTestId("sites-trigger"));
    expect(screen.queryByText("View site")).not.toBeInTheDocument();

    fireEvent.click(screen.getByText("Reboot miners"));

    await waitFor(() => {
      expect(mockListMinerStateSnapshots).toHaveBeenCalledWith(
        expect.objectContaining({
          filter: expect.objectContaining({ siteIds: [7n, 8n] }),
        }),
      );
    });
  });

  it("renders Reboot and More controls in bulk presentation", async () => {
    render(
      <FleetGroupActionsMenu
        scopes={[
          { kind: "rack", id: 11n, name: "Rack A" },
          { kind: "rack", id: 12n, name: "Rack B" },
        ]}
        ariaLabel="Bulk actions for selected racks"
        testIdPrefix="racks"
        presentation="bulk"
      />,
    );

    expect(screen.getByTestId(`racks-quick-${deviceActions.reboot}`)).toHaveTextContent("Reboot");
    expect(screen.getByTestId("racks-trigger")).toHaveTextContent("More");

    fireEvent.click(screen.getByTestId(`racks-quick-${deviceActions.reboot}`));

    await waitFor(() => {
      expect(mockListMinerStateSnapshots).toHaveBeenCalledWith(
        expect.objectContaining({
          filter: expect.objectContaining({ rackIds: [11n, 12n] }),
        }),
      );
    });
  });
});
