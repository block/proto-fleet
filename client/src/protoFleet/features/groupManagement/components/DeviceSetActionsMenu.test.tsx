import { Fragment, type ReactNode } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeviceSetActionsMenu from "./DeviceSetActionsMenu";

// Hoisted mocks
const { mockUseMinerActions, mockBulkActionsPopover, mockListGroupMembers, mockFetchAllMinerSnapshots } = vi.hoisted(
  () => ({
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
  }),
);

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
  default: () => null,
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

  it("shows loading immediately on open when fresh data is required", () => {
    mockFetchAllMinerSnapshots.mockReturnValue(new Promise(() => {}));

    const { container } = render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(screen.queryByTestId("group-actions-popover")).not.toBeInTheDocument();
    expect(container.querySelector("svg.animate-spin")).not.toBeNull();
  });

  it("aborts the member-fetch signal on close and creates a fresh signal on reopen", async () => {
    mockFetchAllMinerSnapshots.mockReturnValue(new Promise(() => {}));

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    const button = screen.getByLabelText("Device set actions");
    fireEvent.click(button);

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(1);
    });

    const firstRequest = mockListGroupMembers.mock.calls[0][0] as { signal: AbortSignal };
    expect(firstRequest.signal.aborted).toBe(false);

    fireEvent.click(button);
    await waitFor(() => {
      expect(firstRequest.signal.aborted).toBe(true);
    });

    fireEvent.click(button);
    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(2);
    });

    const secondRequest = mockListGroupMembers.mock.calls[1][0] as { signal: AbortSignal };
    expect(firstRequest.signal.aborted).toBe(true);
    expect(secondRequest.signal.aborted).toBe(false);
  });

  it("ignores stale callbacks from a prior open", async () => {
    const memberRequests: Array<{
      signal: AbortSignal;
      onSuccess?: (ids: string[]) => void;
      onFinally?: () => void;
    }> = [];

    mockListGroupMembers.mockImplementation((request: unknown) => {
      memberRequests.push(request as (typeof memberRequests)[number]);
    });

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    const button = screen.getByLabelText("Device set actions");
    fireEvent.click(button);

    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(1);
    });

    fireEvent.click(button);
    await waitFor(() => {
      expect(memberRequests[0].signal.aborted).toBe(true);
    });

    fireEvent.click(button);
    await waitFor(() => {
      expect(mockListGroupMembers).toHaveBeenCalledTimes(2);
    });

    act(() => {
      memberRequests[0].onSuccess?.(["stale-device"]);
      memberRequests[0].onFinally?.();
    });

    expect(screen.queryByTestId("group-actions-popover")).not.toBeInTheDocument();

    act(() => {
      memberRequests[1].onSuccess?.(["fresh-device"]);
      memberRequests[1].onFinally?.();
    });

    await waitFor(() => {
      expect(screen.getByTestId("group-actions-popover")).toBeInTheDocument();
    });

    // Directly verify the version-counter guard: useMinerActions must never have been
    // handed the stale member, and its latest call must reflect the fresh member.
    expect(mockUseMinerActions).not.toHaveBeenCalledWith(
      expect.objectContaining({ selectedMiners: [{ deviceIdentifier: "stale-device" }] }),
    );
    expect(mockUseMinerActions).toHaveBeenLastCalledWith(
      expect.objectContaining({ selectedMiners: [{ deviceIdentifier: "fresh-device" }] }),
    );
  });

  it("passes a non-aborted signal to fetchAllMinerSnapshots on open", async () => {
    let capturedSignal: AbortSignal | undefined;
    mockFetchAllMinerSnapshots.mockImplementation((_filter: unknown, signal?: AbortSignal) => {
      capturedSignal = signal;
      return new Promise(() => {});
    });

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(mockFetchAllMinerSnapshots).toHaveBeenCalledTimes(1);
    });

    expect(capturedSignal).toBeDefined();
    expect(capturedSignal!.aborted).toBe(false);
  });

  it("aborts the snapshot-fetch signal on close and creates a fresh signal on reopen", async () => {
    const signals: AbortSignal[] = [];
    mockFetchAllMinerSnapshots.mockImplementation((_filter: unknown, signal?: AbortSignal) => {
      if (signal) signals.push(signal);
      return new Promise(() => {});
    });

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    const button = screen.getByLabelText("Device set actions");
    fireEvent.click(button);

    await waitFor(() => {
      expect(signals).toHaveLength(1);
    });
    expect(signals[0].aborted).toBe(false);

    fireEvent.click(button);
    await waitFor(() => {
      expect(signals[0].aborted).toBe(true);
    });

    fireEvent.click(button);
    await waitFor(() => {
      expect(signals).toHaveLength(2);
    });

    expect(signals[0].aborted).toBe(true);
    expect(signals[1].aborted).toBe(false);
  });

  it("ignores stale snapshot resolutions from a prior open", async () => {
    type SnapshotResolve = (value: Record<string, unknown>) => void;
    const resolvers: SnapshotResolve[] = [];

    mockFetchAllMinerSnapshots.mockImplementation(() => {
      return new Promise<Record<string, unknown>>((resolve) => {
        resolvers.push(resolve);
      });
    });

    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(["d1"]);
        onFinally?.();
      },
    );

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    const button = screen.getByLabelText("Device set actions");
    fireEvent.click(button);

    await waitFor(() => {
      expect(resolvers).toHaveLength(1);
    });

    fireEvent.click(button);
    fireEvent.click(button);

    await waitFor(() => {
      expect(resolvers).toHaveLength(2);
    });

    act(() => {
      resolvers[0]({ stale: {} });
    });

    act(() => {
      resolvers[1]({ fresh: {} });
    });

    await waitFor(() => {
      expect(screen.getByTestId("group-actions-popover")).toBeInTheDocument();
    });

    expect(mockUseMinerActions).toHaveBeenLastCalledWith(expect.objectContaining({ miners: { fresh: {} } }));
  });

  it("does not show spinner after close when deviceSetId becomes undefined", async () => {
    mockFetchAllMinerSnapshots.mockReturnValue(new Promise(() => {}));

    const { rerender, container } = render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    const button = screen.getByLabelText("Device set actions");
    fireEvent.click(button);

    expect(container.querySelector("svg.animate-spin")).not.toBeNull();

    fireEvent.click(button);

    rerender(<DeviceSetActionsMenu deviceSetId={undefined} onEdit={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    expect(container.querySelector("svg.animate-spin")).toBeNull();
    expect(screen.queryByTestId("group-actions-popover")).toBeInTheDocument();
  });

  it("passes a rackIds filter to fetchAllMinerSnapshots when deviceSetType is 'rack'", async () => {
    let capturedFilter: unknown;
    mockFetchAllMinerSnapshots.mockImplementation((filter: unknown) => {
      capturedFilter = filter;
      return new Promise(() => {});
    });

    render(<DeviceSetActionsMenu deviceSetId={7n} deviceSetType="rack" onEdit={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(mockFetchAllMinerSnapshots).toHaveBeenCalledTimes(1);
    });

    expect(capturedFilter).toEqual({ rackIds: [7n] });
  });

  it("aborts and re-fetches when deviceSetId changes while menu is open", async () => {
    const snapshotCalls: Array<{ filter: unknown; signal?: AbortSignal }> = [];
    mockFetchAllMinerSnapshots.mockImplementation((filter: unknown, signal?: AbortSignal) => {
      snapshotCalls.push({ filter, signal });
      return new Promise(() => {});
    });

    const { rerender } = render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(snapshotCalls).toHaveLength(1);
    });
    expect(snapshotCalls[0].filter).toEqual({ groupIds: [1n] });
    expect(snapshotCalls[0].signal?.aborted).toBe(false);
    expect(mockListGroupMembers).toHaveBeenCalledTimes(1);
    expect(mockListGroupMembers.mock.calls[0][0]).toMatchObject({ deviceSetId: 1n });

    rerender(<DeviceSetActionsMenu deviceSetId={2n} onEdit={vi.fn()} />);

    await waitFor(() => {
      expect(snapshotCalls).toHaveLength(2);
    });

    expect(snapshotCalls[0].signal?.aborted).toBe(true);
    expect(snapshotCalls[1].filter).toEqual({ groupIds: [2n] });
    expect(snapshotCalls[1].signal?.aborted).toBe(false);
    expect(mockListGroupMembers).toHaveBeenCalledTimes(2);
    expect(mockListGroupMembers.mock.calls[1][0]).toMatchObject({ deviceSetId: 2n });
  });

  it("preserves fetched data across a popover action click (programmatic close)", async () => {
    mockFetchAllMinerSnapshots.mockResolvedValueOnce({
      d1: { deviceIdentifier: "d1" },
      d2: { deviceIdentifier: "d2" },
    });
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(["d1", "d2"]);
        onFinally?.();
      },
    );

    render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    fireEvent.click(screen.getByLabelText("Device set actions"));

    await waitFor(() => {
      expect(screen.getByTestId("group-actions-popover")).toBeInTheDocument();
    });

    // Clicking a popover action triggers beforeEach → setIsOpen(false); this is the
    // same programmatic-close path used by confirmation/modal flows. The fetched
    // members/snapshots must survive so downstream handlers (captured via hook
    // closures) see the correct selection rather than an empty one.
    fireEvent.click(screen.getByTestId("edit-group-popover-button"));

    expect(mockUseMinerActions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        miners: expect.objectContaining({ d1: expect.anything(), d2: expect.anything() }),
        selectedMiners: [{ deviceIdentifier: "d1" }, { deviceIdentifier: "d2" }],
      }),
    );
  });

  it("clears stale data on close so reopening without deviceSetId shows no stale actions", async () => {
    mockFetchAllMinerSnapshots.mockResolvedValueOnce({
      stale1: { deviceIdentifier: "stale1" },
      stale2: { deviceIdentifier: "stale2" },
    });
    mockListGroupMembers.mockImplementation(
      ({ onSuccess, onFinally }: { onSuccess?: (ids: string[]) => void; onFinally?: () => void }) => {
        onSuccess?.(["stale1", "stale2"]);
        onFinally?.();
      },
    );

    const { rerender } = render(<DeviceSetActionsMenu deviceSetId={1n} onEdit={vi.fn()} />);

    const button = screen.getByLabelText("Device set actions");
    fireEvent.click(button);

    await waitFor(() => {
      expect(screen.getByTestId("group-actions-popover")).toBeInTheDocument();
    });

    // Confirm the first open surfaced the fetched data to useMinerActions
    expect(mockUseMinerActions).toHaveBeenLastCalledWith(
      expect.objectContaining({
        miners: expect.objectContaining({ stale1: expect.anything() }),
        selectedMiners: [{ deviceIdentifier: "stale1" }, { deviceIdentifier: "stale2" }],
      }),
    );

    fireEvent.click(button);

    rerender(<DeviceSetActionsMenu deviceSetId={undefined} onEdit={vi.fn()} />);
    fireEvent.click(screen.getByLabelText("Device set actions"));

    // After close + reopen without a deviceSetId, the previous fetch's data must not leak
    expect(mockUseMinerActions).toHaveBeenLastCalledWith(expect.objectContaining({ miners: {}, selectedMiners: [] }));
  });
});
