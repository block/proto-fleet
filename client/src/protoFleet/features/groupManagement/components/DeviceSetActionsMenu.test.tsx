import { Fragment, type ReactNode } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeviceSetActionsMenu from "./DeviceSetActionsMenu";

type UnsupportedMinersModalMockProps = {
  onDismiss: () => void;
};

// Hoisted mocks
const {
  mockUseMinerActions,
  mockBulkActionsPopover,
  mockListGroupMembers,
  mockFetchAllMinerSnapshots,
  mockUnsupportedMinersModal,
} = vi.hoisted(() => ({
  mockUseMinerActions: vi.fn(),
  mockBulkActionsPopover: vi.fn(
    ({
      actions,
      beforeEach: beforeEachAction,
    }: {
      actions: Array<{
        action: string;
        title: string;
        actionHandler: () => void;
        requiresConfirmation: boolean;
        disabled?: boolean;
        disabledReason?: string;
        confirmation?: {
          subtitle?: string;
        };
      }>;
      beforeEach: (requiresConfirmation: boolean) => void;
    }) => (
      <div data-testid="group-actions-popover">
        {actions.map((action) => (
          <button
            key={action.action}
            data-testid={`${action.action}-popover-button`}
            disabled={action.disabled}
            onClick={() => {
              beforeEachAction(action.requiresConfirmation);
              action.actionHandler();
            }}
          >
            {action.title}
          </button>
        ))}
      </div>
    ),
  ),
  mockListGroupMembers: vi.fn(),
  mockFetchAllMinerSnapshots: vi.fn(),
  mockUnsupportedMinersModal: vi.fn((_props: UnsupportedMinersModalMockProps) => null),
}));

const defaultMinerActions = () => ({
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
});

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/useMinerActions", () => ({
  useMinerActions: mockUseMinerActions,
}));

vi.mock("@/protoFleet/api/fetchAllMinerSnapshots", () => ({
  fetchAllMinerSnapshots: (...args: unknown[]) => mockFetchAllMinerSnapshots(...args),
}));

vi.mock("@/protoFleet/features/fleetManagement/components/BulkActions", () => ({
  BulkActionsPopover: mockBulkActionsPopover,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/BulkActions/BulkActionConfirmDialog", () => ({
  default: ({
    open,
    actionConfirmation,
    onConfirmation,
    onCancel,
    testId,
  }: {
    open: boolean;
    actionConfirmation: { subtitle?: string };
    onConfirmation: () => void;
    onCancel: () => void;
    testId: string;
  }) =>
    open ? (
      <div data-testid={testId}>
        <p>{actionConfirmation.subtitle}</p>
        <button onClick={onConfirmation}>Confirm</button>
        <button onClick={onCancel}>Cancel</button>
      </div>
    ) : null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/BulkActions/UnsupportedMinersModal", () => ({
  default: (props: UnsupportedMinersModalMockProps) => mockUnsupportedMinersModal(props),
}));

vi.mock("@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/CoolingModeModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManagePowerModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/ManageSecurity", () => ({
  ManageSecurityModal: () => null,
  UpdateMinerPasswordModal: () => null,
}));

vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({
  default: () => null,
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({ listGroupMembers: mockListGroupMembers }),
}));

vi.mock("@/shared/components/Popover", () => ({
  PopoverProvider: ({ children }: { children: ReactNode }) => <Fragment>{children}</Fragment>,
  usePopover: () => ({
    triggerRef: { current: null },
    setPopoverRenderMode: vi.fn(),
  }),
}));

vi.mock("@/shared/hooks/useClickOutside", () => ({
  useClickOutside: vi.fn(),
}));

describe("DeviceSetActionsMenu", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseMinerActions.mockImplementation(defaultMinerActions);
    mockListGroupMembers.mockImplementation(() => undefined);
    mockFetchAllMinerSnapshots.mockResolvedValue({});
    mockUnsupportedMinersModal.mockImplementation(() => null);
  });

  it("renders 'View group' action when onView is provided", () => {
    const onEdit = vi.fn();
    const onView = vi.fn();

    render(<DeviceSetActionsMenu memberDeviceIds={["d1", "d2"]} onEdit={onEdit} onView={onView} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(screen.getByTestId("view-group-popover-button")).toBeInTheDocument();
    expect(screen.getByTestId("view-group-popover-button")).toHaveTextContent("View group");
  });

  it("calls onView when 'View group' is clicked", () => {
    const onEdit = vi.fn();
    const onView = vi.fn();

    render(<DeviceSetActionsMenu memberDeviceIds={["d1", "d2"]} onEdit={onEdit} onView={onView} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));
    fireEvent.click(screen.getByTestId("view-group-popover-button"));

    expect(onView).toHaveBeenCalledTimes(1);
  });

  it("does not render 'View group' action when onView is not provided", () => {
    const onEdit = vi.fn();

    render(<DeviceSetActionsMenu memberDeviceIds={["d1", "d2"]} onEdit={onEdit} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(screen.queryByTestId("view-group-popover-button")).not.toBeInTheDocument();
  });

  it("uses custom viewLabel when provided", () => {
    const onEdit = vi.fn();
    const onView = vi.fn();

    render(
      <DeviceSetActionsMenu memberDeviceIds={["d1", "d2"]} onEdit={onEdit} onView={onView} viewLabel="View rack" />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(screen.getByTestId("view-group-popover-button")).toHaveTextContent("View rack");
  });

  it("uses site and group labels in scoped confirmation copy", async () => {
    mockUseMinerActions.mockReturnValue({
      currentAction: null,
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: vi.fn(),
          requiresConfirmation: true,
          confirmation: {
            title: "Sleep miners?",
            subtitle: "These miners will go to sleep and stop hashing.",
            confirmAction: { title: "Sleep" },
          },
        },
      ],
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
    });

    render(
      <DeviceSetActionsMenu
        memberDeviceIds={["d1", "d2", "d3", "d4", "d5", "d6"]}
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        deviceSetLabel="Group A"
        totalMemberCount={30}
      />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(mockBulkActionsPopover).toHaveBeenCalled();
    });

    const latestCall = mockBulkActionsPopover.mock.calls[mockBulkActionsPopover.mock.calls.length - 1]?.[0] as {
      actions: Array<{ action: string; confirmation?: { subtitle?: string } }>;
    };
    const sleepAction = latestCall.actions.find((action) => action.action === "shutdown");
    expect(sleepAction?.confirmation?.subtitle).toBe(
      "This action only applies to miners in Site 2, 6 of the 30 miners in Group A will go to sleep and stop hashing.",
    );
  });

  it("disables scoped bulk actions when no miners are in the active site scope", async () => {
    mockUseMinerActions.mockReturnValue({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: vi.fn(),
          requiresConfirmation: true,
          confirmation: {
            title: "Sleep miners?",
            subtitle: "These miners will go to sleep and stop hashing.",
            confirmAction: { title: "Sleep" },
          },
        },
      ],
    });

    render(
      <DeviceSetActionsMenu
        memberDeviceIds={[]}
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        deviceSetLabel="Group A"
        totalMemberCount={30}
      />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(mockBulkActionsPopover).toHaveBeenCalled();
    });

    const latestCall = mockBulkActionsPopover.mock.calls[mockBulkActionsPopover.mock.calls.length - 1]?.[0] as {
      actions: Array<{ action: string; disabled?: boolean; disabledReason?: string }>;
    };
    const sleepAction = latestCall.actions.find((action) => action.action === "shutdown");
    expect(sleepAction).toMatchObject({
      disabled: true,
      disabledReason: "No miners in Site 2.",
    });
    expect(screen.getByTestId("shutdown-popover-button")).toBeDisabled();
  });

  it("keeps scoped bulk actions enabled while scoped member ids are unknown", async () => {
    mockUseMinerActions.mockReturnValue({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: vi.fn(),
          requiresConfirmation: true,
          confirmation: {
            title: "Sleep miners?",
            subtitle: "These miners will go to sleep and stop hashing.",
            confirmAction: { title: "Sleep" },
          },
        },
      ],
    });

    render(
      <DeviceSetActionsMenu
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        deviceSetLabel="Group A"
        totalMemberCount={30}
      />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(mockBulkActionsPopover).toHaveBeenCalled();
    });

    const latestCall = mockBulkActionsPopover.mock.calls[mockBulkActionsPopover.mock.calls.length - 1]?.[0] as {
      actions: Array<{ action: string; disabled?: boolean; disabledReason?: string }>;
    };
    const sleepAction = latestCall.actions.find((action) => action.action === "shutdown");
    expect(sleepAction?.disabled).toBeUndefined();
    expect(sleepAction?.disabledReason).toBeUndefined();
    expect(screen.getByTestId("shutdown-popover-button")).toBeEnabled();
    expect(mockListGroupMembers).not.toHaveBeenCalled();
    expect(mockFetchAllMinerSnapshots).not.toHaveBeenCalled();
  });

  it("does not add scoped confirmation copy on canonical detail pages", async () => {
    mockUseMinerActions.mockReturnValue({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: vi.fn(),
          requiresConfirmation: true,
          confirmation: {
            title: "Sleep miners?",
            subtitle: "These miners will go to sleep and stop hashing.",
            confirmAction: { title: "Sleep" },
          },
        },
      ],
    });

    render(
      <DeviceSetActionsMenu
        memberDeviceIds={["d1", "d2"]}
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSiteLabel="Site 2"
        deviceSetLabel="Group A"
        totalMemberCount={30}
      />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(mockBulkActionsPopover).toHaveBeenCalled();
    });

    const latestCall = mockBulkActionsPopover.mock.calls[mockBulkActionsPopover.mock.calls.length - 1]?.[0] as {
      actions: Array<{ action: string; confirmation?: { subtitle?: string } }>;
    };
    const sleepAction = latestCall.actions.find((action) => action.action === "shutdown");
    expect(sleepAction?.confirmation?.subtitle).toBe("These miners will go to sleep and stop hashing.");
  });

  it("chains scoped confirmation after unsupported miners continuation", async () => {
    mockUseMinerActions.mockReturnValue({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: vi.fn(),
          requiresConfirmation: true,
          confirmation: {
            title: "Sleep miners?",
            subtitle: "These miners will go to sleep and stop hashing.",
            confirmAction: { title: "Sleep" },
          },
        },
      ],
    });

    const continueAction = vi.fn();
    render(
      <DeviceSetActionsMenu
        memberDeviceIds={["d1", "d2"]}
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        deviceSetLabel="Group A"
        totalMemberCount={5}
      />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(mockBulkActionsPopover).toHaveBeenCalled();
    });

    const latestHookArgs = mockUseMinerActions.mock.calls[mockUseMinerActions.mock.calls.length - 1]?.[0] as {
      onUnsupportedMinersContinue: (continuation: {
        action: string;
        filteredDeviceIdentifiers: string[];
        continueAction: () => void;
      }) => boolean;
    };

    let handled = false;
    act(() => {
      handled = latestHookArgs.onUnsupportedMinersContinue({
        action: "shutdown",
        filteredDeviceIdentifiers: ["d1"],
        continueAction,
      });
    });

    expect(handled).toBe(true);
    expect(continueAction).not.toHaveBeenCalled();
    expect(screen.getByTestId("group-actions-dialog")).toHaveTextContent(
      "This action only applies to miners in Site 2, 2 of the 5 miners in Group A will go to sleep and stop hashing.",
    );

    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    expect(continueAction).toHaveBeenCalledTimes(1);
  });

  it("clears scoped warning state when unsupported miners are dismissed", async () => {
    const handleUnsupportedMinersDismiss = vi.fn();
    const actionActiveRef = { current: false };
    mockUseMinerActions.mockReturnValue({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: vi.fn(),
          requiresConfirmation: true,
          confirmation: {
            title: "Sleep miners?",
            subtitle: "These miners will go to sleep and stop hashing.",
            confirmAction: { title: "Sleep" },
          },
        },
      ],
      handleUnsupportedMinersDismiss,
    });

    render(
      <DeviceSetActionsMenu
        memberDeviceIds={["d1", "d2"]}
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        actionActiveRef={actionActiveRef}
      />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(mockBulkActionsPopover).toHaveBeenCalled();
    });

    const latestHookArgs = mockUseMinerActions.mock.calls[mockUseMinerActions.mock.calls.length - 1]?.[0] as {
      onUnsupportedMinersContinue: (continuation: { action: string; continueAction: () => void }) => boolean;
    };

    act(() => {
      latestHookArgs.onUnsupportedMinersContinue({
        action: "shutdown",
        continueAction: vi.fn(),
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId("group-actions-dialog")).toBeInTheDocument();
      expect(actionActiveRef.current).toBe(true);
    });

    expect(mockUnsupportedMinersModal).toHaveBeenCalled();
    const latestUnsupportedProps =
      mockUnsupportedMinersModal.mock.calls[mockUnsupportedMinersModal.mock.calls.length - 1][0];
    act(() => {
      latestUnsupportedProps.onDismiss();
    });

    expect(handleUnsupportedMinersDismiss).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(screen.queryByTestId("group-actions-dialog")).not.toBeInTheDocument();
      expect(actionActiveRef.current).toBe(false);
    });
  });

  it("opens immediately without fetching group members or miner snapshots", () => {
    const onEdit = vi.fn();
    const onView = vi.fn();

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={onEdit} onView={onView} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(screen.getByTestId("group-actions-popover")).toBeInTheDocument();
    expect(screen.getByTestId("view-group-popover-button")).toHaveTextContent("View group");
    expect(screen.getByTestId("edit-group-popover-button")).toHaveTextContent("Edit group");
    expect(mockListGroupMembers).not.toHaveBeenCalled();
    expect(mockFetchAllMinerSnapshots).not.toHaveBeenCalled();
  });

  it("fetches group members and snapshots only after a miner action is chosen", async () => {
    const shutdownHandler = vi.fn();
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: shutdownHandler,
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(["d1", "d2"]);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockResolvedValue({
      d1: { deviceIdentifier: "d1" },
      d2: { deviceIdentifier: "d2" },
    });

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(mockListGroupMembers).not.toHaveBeenCalled();
    expect(mockFetchAllMinerSnapshots).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(shutdownHandler).toHaveBeenCalledTimes(1);
    });

    expect(mockListGroupMembers).toHaveBeenCalledWith(expect.objectContaining({ deviceSetId: 1n }));
    expect(mockFetchAllMinerSnapshots).toHaveBeenCalledWith({ groupIds: [1n] }, expect.any(AbortSignal));
    expect(mockUseMinerActions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        miners: expect.objectContaining({ d1: expect.anything(), d2: expect.anything() }),
        selectedMiners: [{ deviceIdentifier: "d1" }, { deviceIdentifier: "d2" }],
      }),
    );
  });

  it("replays a prop-member action against the member ids captured before snapshot fetch completes", async () => {
    type SnapshotResolve = (value: Record<string, unknown>) => void;
    let resolveSnapshots: SnapshotResolve | undefined;
    const handledDeviceIds: string[][] = [];
    mockUseMinerActions.mockImplementation(
      ({ selectedMiners }: { selectedMiners: Array<{ deviceIdentifier: string }> }) => ({
        ...defaultMinerActions(),
        popoverActions: [
          {
            action: "shutdown",
            title: "Sleep",
            actionHandler: () => {
              handledDeviceIds.push(selectedMiners.map((miner) => miner.deviceIdentifier));
            },
            requiresConfirmation: false,
          },
        ],
      }),
    );
    mockFetchAllMinerSnapshots.mockImplementation(
      () =>
        new Promise<Record<string, unknown>>((resolve) => {
          resolveSnapshots = resolve;
        }),
    );

    const { rerender } = render(
      <DeviceSetActionsMenu memberDeviceIds={["before"]} deviceSetId={1n} onEdit={vi.fn()} />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(resolveSnapshots).toBeDefined();
    });

    rerender(<DeviceSetActionsMenu memberDeviceIds={["after"]} deviceSetId={1n} onEdit={vi.fn()} />);

    await act(async () => {
      resolveSnapshots?.({
        before: { deviceIdentifier: "before" },
        after: { deviceIdentifier: "after" },
      });
    });

    await waitFor(() => {
      expect(handledDeviceIds).toEqual([["before"]]);
    });
    expect(mockListGroupMembers).not.toHaveBeenCalled();
  });

  it("keeps captured prop member ids active through scoped confirmation after prop refreshes", async () => {
    type SnapshotResolve = (value: Record<string, unknown>) => void;
    let resolveSnapshots: SnapshotResolve | undefined;
    const handledDeviceIds: string[][] = [];
    const actionActiveRef = { current: false };
    mockUseMinerActions.mockImplementation(
      ({
        selectedMiners,
        onActionComplete,
      }: {
        selectedMiners: Array<{ deviceIdentifier: string }>;
        onActionComplete?: () => void;
      }) => ({
        ...defaultMinerActions(),
        popoverActions: [
          {
            action: "shutdown",
            title: "Sleep",
            actionHandler: () => {
              handledDeviceIds.push(selectedMiners.map((miner) => miner.deviceIdentifier));
              onActionComplete?.();
            },
            requiresConfirmation: false,
          },
        ],
      }),
    );
    mockFetchAllMinerSnapshots.mockImplementation(
      () =>
        new Promise<Record<string, unknown>>((resolve) => {
          resolveSnapshots = resolve;
        }),
    );

    const { rerender } = render(
      <DeviceSetActionsMenu
        memberDeviceIds={["before"]}
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        actionActiveRef={actionActiveRef}
      />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(resolveSnapshots).toBeDefined();
      expect(actionActiveRef.current).toBe(true);
    });

    rerender(
      <DeviceSetActionsMenu
        memberDeviceIds={["after-fetch-start"]}
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        actionActiveRef={actionActiveRef}
      />,
    );

    await act(async () => {
      resolveSnapshots?.({
        before: { deviceIdentifier: "before" },
        "after-fetch-start": { deviceIdentifier: "after-fetch-start" },
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId("group-actions-dialog")).toHaveTextContent("all 1 miner");
    });
    expect(mockUseMinerActions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        selectedMiners: [{ deviceIdentifier: "before" }],
      }),
    );

    rerender(
      <DeviceSetActionsMenu
        memberDeviceIds={["after-confirm-open"]}
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        actionActiveRef={actionActiveRef}
      />,
    );

    await waitFor(() => {
      expect(mockUseMinerActions).toHaveBeenLastCalledWith(
        expect.objectContaining({
          selectedMiners: [{ deviceIdentifier: "before" }],
        }),
      );
    });

    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    expect(handledDeviceIds).toEqual([["before"]]);
    await waitFor(() => {
      expect(actionActiveRef.current).toBe(false);
    });
  });

  it("clears an open scoped confirmation when the target scope changes", async () => {
    const actionHandler = vi.fn();
    const handleCancel = vi.fn();
    const actionActiveRef = { current: false };
    mockUseMinerActions.mockReturnValue({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "blink-leds",
          title: "Blink LEDs",
          actionHandler,
          requiresConfirmation: false,
        },
      ],
      handleCancel,
    });

    const { rerender } = render(
      <DeviceSetActionsMenu
        memberDeviceIds={["site-1-miner"]}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "1", slug: "site-1" }}
        activeSiteLabel="Site 1"
        actionActiveRef={actionActiveRef}
      />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));
    fireEvent.click(screen.getByTestId("blink-leds-popover-button"));

    await waitFor(() => {
      expect(screen.getByTestId("group-actions-dialog")).toHaveTextContent("miners in Site 1");
      expect(actionActiveRef.current).toBe(true);
    });

    rerender(
      <DeviceSetActionsMenu
        memberDeviceIds={["site-2-miner"]}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        actionActiveRef={actionActiveRef}
      />,
    );

    await waitFor(() => {
      expect(screen.queryByTestId("group-actions-dialog")).not.toBeInTheDocument();
      expect(actionActiveRef.current).toBe(false);
    });
    expect(handleCancel).toHaveBeenCalledWith({ notifyComplete: false });
    expect(actionHandler).not.toHaveBeenCalled();
  });

  it("silently cancels local action state when an inactive row target changes", async () => {
    const handleCancel = vi.fn();
    const onActionComplete = vi.fn();
    mockUseMinerActions.mockReturnValue({
      ...defaultMinerActions(),
      handleCancel,
    });

    const { rerender } = render(
      <DeviceSetActionsMenu
        memberDeviceIds={["site-1-miner"]}
        onEdit={vi.fn()}
        onActionComplete={onActionComplete}
        activeSite={{ kind: "site", id: "1", slug: "site-1" }}
        activeSiteLabel="Site 1"
      />,
    );

    rerender(
      <DeviceSetActionsMenu
        memberDeviceIds={["site-2-miner"]}
        onEdit={vi.fn()}
        onActionComplete={onActionComplete}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
      />,
    );

    await waitFor(() => {
      expect(handleCancel).toHaveBeenCalledWith({ notifyComplete: false });
    });
    expect(onActionComplete).not.toHaveBeenCalled();
  });

  it("ignores stale action-complete callbacks from an older prepared action", async () => {
    const completionCallbacks: Array<() => void> = [];
    const selectedMinerCalls: string[][] = [];
    const onActionComplete = vi.fn();
    mockUseMinerActions.mockImplementation(
      ({
        selectedMiners,
        onActionComplete: completeAction,
      }: {
        selectedMiners: Array<{ deviceIdentifier: string }>;
        onActionComplete?: () => void;
      }) => {
        selectedMinerCalls.push(selectedMiners.map((miner) => miner.deviceIdentifier));
        return {
          ...defaultMinerActions(),
          popoverActions: [
            {
              action: "blink-leds",
              title: "Blink LEDs",
              actionHandler: () => {
                if (completeAction) completionCallbacks.push(completeAction);
              },
              requiresConfirmation: false,
            },
          ],
        };
      },
    );
    mockFetchAllMinerSnapshots.mockResolvedValue({ "rack-miner": { deviceIdentifier: "rack-miner" } });

    const { rerender } = render(
      <DeviceSetActionsMenu
        memberDeviceIds={["first-miner"]}
        deviceSetId={1n}
        deviceSetType="rack"
        onEdit={vi.fn()}
        onActionComplete={onActionComplete}
      />,
    );

    const menuButton = screen.getByLabelText("Device set actions");
    fireEvent.click(menuButton);
    fireEvent.click(screen.getByTestId("blink-leds-popover-button"));

    await waitFor(() => {
      expect(completionCallbacks).toHaveLength(1);
      expect(selectedMinerCalls[selectedMinerCalls.length - 1]).toEqual(["first-miner"]);
    });

    rerender(
      <DeviceSetActionsMenu
        memberDeviceIds={["second-miner"]}
        deviceSetId={1n}
        deviceSetType="rack"
        onEdit={vi.fn()}
        onActionComplete={onActionComplete}
      />,
    );
    fireEvent.click(menuButton);
    fireEvent.click(screen.getByTestId("blink-leds-popover-button"));

    await waitFor(() => {
      expect(completionCallbacks).toHaveLength(2);
      expect(selectedMinerCalls[selectedMinerCalls.length - 1]).toEqual(["second-miner"]);
    });

    rerender(
      <DeviceSetActionsMenu
        memberDeviceIds={["refreshed-prop-miner"]}
        deviceSetId={1n}
        deviceSetType="rack"
        onEdit={vi.fn()}
        onActionComplete={onActionComplete}
      />,
    );

    await act(async () => {
      completionCallbacks[0]();
    });

    expect(onActionComplete).toHaveBeenCalledTimes(1);
    expect(selectedMinerCalls[selectedMinerCalls.length - 1]).toEqual(["second-miner"]);
  });

  it("clears stale warning state when a non-confirming action supersedes a pending confirming action", async () => {
    type SnapshotResolve = (value: Record<string, unknown>) => void;
    let resolveFirstSnapshots: SnapshotResolve | undefined;
    let snapshotCall = 0;
    const actionActiveRef = { current: false };
    mockUseMinerActions.mockImplementation(({ onActionComplete }: { onActionComplete?: () => void }) => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: vi.fn(),
          requiresConfirmation: true,
          confirmation: {
            title: "Sleep miners?",
            subtitle: "These miners will go to sleep and stop hashing.",
            confirmAction: { title: "Sleep" },
          },
        },
        {
          action: "download-logs",
          title: "Download logs",
          actionHandler: () => {
            onActionComplete?.();
          },
          requiresConfirmation: false,
        },
      ],
    }));
    mockFetchAllMinerSnapshots.mockImplementation(() => {
      snapshotCall += 1;
      if (snapshotCall === 1) {
        return new Promise<Record<string, unknown>>((resolve) => {
          resolveFirstSnapshots = resolve;
        });
      }
      return Promise.resolve({ "rack-miner": { deviceIdentifier: "rack-miner" } });
    });

    render(
      <DeviceSetActionsMenu
        memberDeviceIds={["rack-miner"]}
        deviceSetId={1n}
        deviceSetType="rack"
        onEdit={vi.fn()}
        actionActiveRef={actionActiveRef}
      />,
    );

    const menuButton = screen.getByLabelText("Device set actions");
    fireEvent.click(menuButton);
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(resolveFirstSnapshots).toBeDefined();
      expect(actionActiveRef.current).toBe(true);
    });

    fireEvent.click(menuButton);
    fireEvent.click(screen.getByTestId("download-logs-popover-button"));

    await waitFor(() => {
      expect(actionActiveRef.current).toBe(false);
    });
    expect(screen.queryByTestId("group-actions-dialog")).not.toBeInTheDocument();

    await act(async () => {
      resolveFirstSnapshots?.({ "rack-miner": { deviceIdentifier: "rack-miner" } });
    });

    expect(screen.queryByTestId("group-actions-dialog")).not.toBeInTheDocument();
  });

  it("does not replay a scoped action when the lazy member fetch returns no scoped miners", async () => {
    const shutdownHandler = vi.fn();
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: shutdownHandler,
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.([]);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockResolvedValue({});

    render(
      <DeviceSetActionsMenu
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
      />,
    );

    const menuButton = screen.getByLabelText("Device set actions");
    fireEvent.click(menuButton);
    expect(screen.getByTestId("shutdown-popover-button")).toBeEnabled();

    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledWith(
        expect.objectContaining({ deviceSetId: 1n, siteIds: [2n], includeUnassigned: false }),
      );
    });
    expect(shutdownHandler).not.toHaveBeenCalled();

    fireEvent.click(menuButton);

    await waitFor(() => {
      expect(screen.getByTestId("shutdown-popover-button")).toBeDisabled();
    });
  });

  it("revalidates an empty scoped member cache when reopening the same target", async () => {
    const shutdownHandler = vi.fn();
    const memberResponses = [[], ["new-miner"], ["new-miner"]];
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: shutdownHandler,
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(memberResponses.shift() ?? []);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockResolvedValue({
      "new-miner": { deviceIdentifier: "new-miner" },
    });

    render(
      <DeviceSetActionsMenu
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
      />,
    );

    const menuButton = screen.getByLabelText("Device set actions");
    fireEvent.click(menuButton);
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(1);
    });
    expect(shutdownHandler).not.toHaveBeenCalled();

    fireEvent.click(menuButton);

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(2);
      expect(screen.getByTestId("shutdown-popover-button")).toBeEnabled();
    });

    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(3);
      expect(screen.getByTestId("group-actions-dialog")).toHaveTextContent("miners in Site 2");
    });

    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    expect(shutdownHandler).toHaveBeenCalledTimes(1);
  });

  it("ignores stale empty-cache revalidation after an action fetch starts", async () => {
    const shutdownHandler = vi.fn();
    let refreshEmptyMembers: (() => void) | undefined;
    let memberFetchCall = 0;
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: shutdownHandler,
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        memberFetchCall += 1;
        if (memberFetchCall === 1) {
          onSuccess?.([]);
          onFinally?.();
          return;
        }
        if (memberFetchCall === 2) {
          refreshEmptyMembers = () => {
            onSuccess?.([]);
            onFinally?.();
          };
          return;
        }
        onSuccess?.(["action-miner"]);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockResolvedValue({
      "action-miner": { deviceIdentifier: "action-miner" },
    });

    render(
      <DeviceSetActionsMenu
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
      />,
    );

    const menuButton = screen.getByLabelText("Device set actions");
    fireEvent.click(menuButton);
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(1);
    });
    expect(shutdownHandler).not.toHaveBeenCalled();

    fireEvent.click(menuButton);

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(2);
      expect(screen.getByTestId("shutdown-popover-button")).toBeEnabled();
    });

    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(3);
      expect(screen.getByTestId("group-actions-dialog")).toHaveTextContent("miners in Site 2");
    });

    act(() => {
      refreshEmptyMembers?.();
    });

    await waitFor(() => {
      expect(mockUseMinerActions).toHaveBeenLastCalledWith(
        expect.objectContaining({
          selectedMiners: [{ deviceIdentifier: "action-miner" }],
        }),
      );
      expect(screen.getByTestId("group-actions-dialog")).toBeInTheDocument();
    });
  });

  it("does not reuse an empty scoped member cache after the target site changes", async () => {
    const shutdownHandler = vi.fn();
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: shutdownHandler,
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({
        siteIds,
        onSuccess,
        onFinally,
      }: {
        siteIds?: bigint[];
        onSuccess?: (ids: string[]) => void;
        onFinally?: () => void;
      }) => {
        onSuccess?.(siteIds?.[0] === 1n ? [] : ["site-2-miner"]);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockImplementation((filter: { siteIds?: bigint[] }) =>
      Promise.resolve(filter.siteIds?.[0] === 1n ? {} : { "site-2-miner": { deviceIdentifier: "site-2-miner" } }),
    );

    const { rerender } = render(
      <DeviceSetActionsMenu
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "1", slug: "site-1" }}
        activeSiteLabel="Site 1"
      />,
    );

    const menuButton = screen.getByLabelText("Device set actions");
    fireEvent.click(menuButton);
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledWith(
        expect.objectContaining({ deviceSetId: 1n, siteIds: [1n], includeUnassigned: false }),
      );
    });

    fireEvent.click(menuButton);

    await waitFor(() => {
      expect(screen.getByTestId("shutdown-popover-button")).toBeDisabled();
    });

    rerender(
      <DeviceSetActionsMenu
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
      />,
    );

    await waitFor(() => {
      expect(screen.getByTestId("shutdown-popover-button")).toBeEnabled();
    });

    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledWith(
        expect.objectContaining({ deviceSetId: 1n, siteIds: [2n], includeUnassigned: false }),
      );
    });
    expect(mockUseMinerActions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        miners: expect.objectContaining({ "site-2-miner": expect.anything() }),
        selectedMiners: [{ deviceIdentifier: "site-2-miner" }],
      }),
    );

    await waitFor(() => {
      expect(screen.getByTestId("group-actions-dialog")).toHaveTextContent("miners in Site 2");
    });

    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    expect(shutdownHandler).toHaveBeenCalledTimes(1);
  });

  it("passes a rackIds filter when a rack miner action is chosen", async () => {
    const rebootHandler = vi.fn();
    let capturedFilter: unknown;
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "reboot",
          title: "Reboot",
          actionHandler: rebootHandler,
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(["rack-device"]);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockImplementation((filter: unknown) => {
      capturedFilter = filter;
      return Promise.resolve({ "rack-device": { deviceIdentifier: "rack-device" } });
    });

    render(<DeviceSetActionsMenu deviceSetId={7n} deviceSetType="rack" onEdit={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));
    fireEvent.click(screen.getByTestId("reboot-popover-button"));

    await waitFor(() => {
      expect(rebootHandler).toHaveBeenCalledTimes(1);
    });

    expect(capturedFilter).toEqual({ rackIds: [7n] });
  });

  it("aborts an in-flight action fetch when another action fetch starts", async () => {
    const signals: AbortSignal[] = [];
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: vi.fn(),
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(["d1"]);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockImplementation((_filter: unknown, signal?: AbortSignal) => {
      if (signal) signals.push(signal);
      return new Promise(() => {});
    });

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    const button = screen.getByLabelText("Device set actions");
    fireEvent.click(button);
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(signals).toHaveLength(1);
    });

    fireEvent.click(button);
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(signals).toHaveLength(2);
    });

    expect(signals[0].aborted).toBe(true);
    expect(signals[1].aborted).toBe(false);
  });

  it("ignores stale action fetch resolutions", async () => {
    type SnapshotResolve = (value: Record<string, unknown>) => void;
    const resolvers: SnapshotResolve[] = [];
    const shutdownHandler = vi.fn();
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: shutdownHandler,
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(["d1"]);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockImplementation(
      () =>
        new Promise<Record<string, unknown>>((resolve) => {
          resolvers.push(resolve);
        }),
    );

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    const button = screen.getByLabelText("Device set actions");
    fireEvent.click(button);
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(resolvers).toHaveLength(1);
    });

    fireEvent.click(button);
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(resolvers).toHaveLength(2);
    });

    act(() => {
      resolvers[0]({ stale: {} });
    });

    expect(mockUseMinerActions).not.toHaveBeenCalledWith(expect.objectContaining({ miners: { stale: {} } }));
    expect(shutdownHandler).not.toHaveBeenCalled();

    act(() => {
      resolvers[1]({ fresh: {} });
    });

    await waitFor(() => {
      expect(shutdownHandler).toHaveBeenCalledTimes(1);
    });
    expect(mockUseMinerActions).toHaveBeenLastCalledWith(expect.objectContaining({ miners: { fresh: {} } }));
  });

  it("aborts and ignores an in-flight action fetch when the target scope changes", async () => {
    type SnapshotResolve = (value: Record<string, unknown>) => void;
    const signals: AbortSignal[] = [];
    const resolvers: SnapshotResolve[] = [];
    const shutdownHandler = vi.fn();
    const actionActiveRef = { current: false };
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: shutdownHandler,
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(["old-scope-miner"]);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockImplementation((_filter: unknown, signal?: AbortSignal) => {
      if (signal) signals.push(signal);
      return new Promise<Record<string, unknown>>((resolve) => {
        resolvers.push(resolve);
      });
    });

    const { rerender } = render(
      <DeviceSetActionsMenu
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "1", slug: "site-1" }}
        activeSiteLabel="Site 1"
        actionActiveRef={actionActiveRef}
      />,
    );

    fireEvent.click(screen.getByLabelText("Device set actions"));
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(signals).toHaveLength(1);
      expect(actionActiveRef.current).toBe(true);
    });

    rerender(
      <DeviceSetActionsMenu
        deviceSetId={1n}
        onEdit={vi.fn()}
        activeSite={{ kind: "site", id: "2", slug: "site-2" }}
        activeSiteLabel="Site 2"
        actionActiveRef={actionActiveRef}
      />,
    );

    expect(signals[0].aborted).toBe(true);
    await waitFor(() => {
      expect(actionActiveRef.current).toBe(false);
    });

    await act(async () => {
      resolvers[0]({ "old-scope-miner": {} });
    });

    expect(shutdownHandler).not.toHaveBeenCalled();
    expect(mockUseMinerActions).not.toHaveBeenCalledWith(
      expect.objectContaining({ miners: { "old-scope-miner": {} } }),
    );
  });

  it("clears stale data on reopen without a deviceSetId", async () => {
    const shutdownHandler = vi.fn();
    mockUseMinerActions.mockImplementation(() => ({
      ...defaultMinerActions(),
      popoverActions: [
        {
          action: "shutdown",
          title: "Sleep",
          actionHandler: shutdownHandler,
          requiresConfirmation: false,
        },
      ],
    }));
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(["stale1", "stale2"]);
        onFinally?.();
      },
    );
    mockFetchAllMinerSnapshots.mockResolvedValueOnce({
      stale1: { deviceIdentifier: "stale1" },
      stale2: { deviceIdentifier: "stale2" },
    });

    const { rerender } = render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    const button = screen.getByLabelText("Device set actions");
    fireEvent.click(button);
    fireEvent.click(screen.getByTestId("shutdown-popover-button"));

    await waitFor(() => {
      expect(shutdownHandler).toHaveBeenCalledTimes(1);
    });
    expect(mockUseMinerActions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        miners: expect.objectContaining({ stale1: expect.anything() }),
        selectedMiners: [{ deviceIdentifier: "stale1" }, { deviceIdentifier: "stale2" }],
      }),
    );

    rerender(<DeviceSetActionsMenu deviceSetId={undefined} onEdit={vi.fn()} />);
    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(mockUseMinerActions).toHaveBeenLastCalledWith(expect.objectContaining({ miners: {}, selectedMiners: [] }));
  });
});
