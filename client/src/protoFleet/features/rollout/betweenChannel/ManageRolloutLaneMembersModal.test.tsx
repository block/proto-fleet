import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import ManageRolloutLaneMembersModal from "./ManageRolloutLaneMembersModal";
import type {
  RolloutLane,
  RolloutLaneMembershipChangePreview,
  RolloutLaneMembershipMember,
} from "@/protoFleet/features/rollout/rolloutTypes";

const pickerSelection = vi.hoisted(() => ({ current: [] as string[] }));

vi.mock("@/protoFleet/features/settings/components/Schedules/MinerSelectionModal", () => ({
  default: ({
    open,
    selectedMinerIds,
    onDismiss,
    onSave,
  }: {
    open: boolean;
    selectedMinerIds: string[];
    onDismiss: () => void;
    onSave: (selection: { selectedMinerIds: string[]; allSelected: boolean; totalMiners: number }) => void;
  }) =>
    open ? (
      <div data-testid="miner-picker">
        <span>Initially selected: {selectedMinerIds.join(", ")}</span>
        <button
          type="button"
          onClick={() =>
            onSave({
              selectedMinerIds: pickerSelection.current,
              allSelected: false,
              totalMiners: pickerSelection.current.length,
            })
          }
        >
          Save miner selection
        </button>
        <button type="button" onClick={onDismiss}>
          Cancel miner selection
        </button>
      </div>
    ) : null,
}));

vi.mock("@/shared/components/Modal", () => ({
  default: ({ open, title, children }: { open?: boolean; title?: string; children: ReactNode }) =>
    open === false ? null : (
      <div>
        <h1>{title}</h1>
        {children}
      </div>
    ),
}));

vi.mock("@/shared/components/Dialog", () => ({
  default: ({
    open,
    title,
    children,
    icon,
    buttons = [],
  }: {
    open?: boolean;
    title: string;
    children?: ReactNode;
    icon?: ReactNode;
    buttons?: Array<{ text?: string; disabled?: boolean; loading?: boolean; onClick?: () => void }>;
  }) =>
    open === false ? null : (
      <div role="dialog" aria-label={title}>
        <h2>{title}</h2>
        {icon}
        {children}
        {buttons.map((button) => (
          <button key={button.text} type="button" disabled={button.disabled || button.loading} onClick={button.onClick}>
            {button.text}
          </button>
        ))}
      </div>
    ),
  DialogIcon: ({ children, intent }: { children: ReactNode; intent?: string }) => (
    <span data-testid="dialog-icon" data-intent={intent}>
      {children}
    </span>
  ),
}));

const lane: RolloutLane = {
  id: "lane-1",
  label: "Stable production",
  description: "",
  currentChannelId: 42n,
  revision: 7n,
  channels: [],
  memberCount: 2,
  memberIdentifiers: [],
  currentReleaseTargets: [],
  firmwareConvergence: {
    totalCount: 2,
    pendingCount: 0,
    updatingCount: 0,
    verifyingCount: 0,
    confirmedCount: 2,
    attentionCount: 0,
    members: [],
  },
};

const currentMember: RolloutLaneMembershipMember = {
  deviceIdentifier: "miner-current",
  manufacturer: "Proto",
  model: "Alpha",
  observedFirmwareVersion: "2.0.0",
  channelId: 42n,
  channelPosition: 1,
  onCurrentChannel: true,
  pinnedReleaseVersion: "2.0.0",
};

const historicalMember: RolloutLaneMembershipMember = {
  deviceIdentifier: "miner-historical",
  manufacturer: "Proto",
  model: "Beta",
  observedFirmwareVersion: "1.0.0",
  channelId: 41n,
  channelPosition: 0,
  onCurrentChannel: false,
  pinnedReleaseVersion: "1.0.0",
  enforcement: {
    deviceIdentifier: "miner-historical",
    manufacturer: "Proto",
    model: "Beta",
    latestObservedFirmwareVersion: "1.0.0",
    targetFirmwareVersion: "2.0.0",
    state: "needsAttention",
    lastError: "Miner did not restart",
  },
};

const emptyPreview: RolloutLaneMembershipChangePreview = {
  targetFirmwarePreview: {
    targets: [],
    miners: [],
    matchingCount: 0,
    mismatchedCount: 0,
    unknownCount: 0,
  },
  reassignments: [],
  removals: [],
  requiresFirmwareConfirmation: false,
  requiresReassignmentConfirmation: false,
};

function props(overrides: Record<string, unknown> = {}) {
  return {
    open: true,
    lane,
    latestRollout: undefined,
    canManage: true,
    isSubmitting: false,
    error: null,
    onDismiss: vi.fn(),
    onListMembers: vi.fn().mockResolvedValue({
      members: [currentMember, historicalMember],
      nextPageToken: "",
      totalCount: 2,
    }),
    onPreview: vi.fn().mockResolvedValue(emptyPreview),
    onUpdate: vi.fn(),
    onUpdated: vi.fn(),
    ...overrides,
  };
}

describe("ManageRolloutLaneMembersModal", () => {
  beforeEach(() => {
    pickerSelection.current = [];
  });

  it("renders the first page before background pages finish for read-only users", async () => {
    let resolveSecondPage:
      | ((page: { members: RolloutLaneMembershipMember[]; nextPageToken: string; totalCount: number }) => void)
      | undefined;
    const secondPage = new Promise<{
      members: RolloutLaneMembershipMember[];
      nextPageToken: string;
      totalCount: number;
    }>((resolve) => {
      resolveSecondPage = resolve;
    });
    const onListMembers = vi
      .fn()
      .mockResolvedValueOnce({ members: [currentMember], nextPageToken: "page-2", totalCount: 2 })
      .mockReturnValueOnce(secondPage);

    render(<ManageRolloutLaneMembersModal {...props({ canManage: false, onListMembers })} />);

    expect(screen.getByText("Loading lane members...")).toBeInTheDocument();
    expect(screen.getByText("Loading lane members...").closest("[aria-live]")).toHaveAttribute("aria-live", "polite");
    expect(screen.getByText("Loading lane members...").closest("[aria-busy]")).toHaveAttribute("aria-busy", "true");
    expect(await screen.findByText("miner-current")).toBeInTheDocument();
    expect(screen.queryByText("miner-historical")).not.toBeInTheDocument();
    expect(screen.getByText("2 miners")).toBeInTheDocument();
    resolveSecondPage?.({ members: [historicalMember], nextPageToken: "", totalCount: 0 });
    expect(await screen.findByText("miner-historical")).toBeInTheDocument();
    expect(screen.getByText("Current release")).toBeInTheDocument();
    expect(screen.getByText("Historical release")).toBeInTheDocument();
    expect(screen.getByText("Needs attention")).toBeInTheDocument();
    expect(screen.getByText("Miner did not restart")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Change miners" })).not.toBeInTheDocument();
    expect(onListMembers).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ laneId: lane.id, pageSize: 1000, pageToken: "", includeTotalCount: true }),
    );
    expect(onListMembers).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ laneId: lane.id, pageSize: 1000, pageToken: "page-2", includeTotalCount: false }),
    );
    const table = screen.getByRole("table", { name: "Stable production lane miners" });
    expect(table).toBeInTheDocument();
    expect(screen.getByText("Miners assigned to Stable production")).toHaveClass("sr-only");
    for (const header of screen.getAllByRole("columnheader")) {
      expect(header).toHaveAttribute("scope", "col");
    }
  });

  it("disables membership changes until every selected identifier is loaded", async () => {
    let resolveSecondPage:
      | ((page: { members: RolloutLaneMembershipMember[]; nextPageToken: string; totalCount: number }) => void)
      | undefined;
    const secondPage = new Promise<{
      members: RolloutLaneMembershipMember[];
      nextPageToken: string;
      totalCount: number;
    }>((resolve) => {
      resolveSecondPage = resolve;
    });
    const onListMembers = vi
      .fn()
      .mockResolvedValueOnce({ members: [currentMember], nextPageToken: "page-2", totalCount: 2 })
      .mockReturnValueOnce(secondPage);

    render(<ManageRolloutLaneMembersModal {...props({ onListMembers })} />);

    expect(await screen.findByText("miner-current")).toBeInTheDocument();
    expect(screen.getByText("Loading all members…")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Change miners" })).toBeDisabled();

    resolveSecondPage?.({ members: [historicalMember], nextPageToken: "", totalCount: 0 });
    await waitFor(() => expect(screen.getByRole("button", { name: "Change miners" })).toBeEnabled());
    expect(screen.queryByText("Loading all members…")).not.toBeInTheDocument();
  });

  it("aborts background hydration when the modal closes", async () => {
    let requestSignal: AbortSignal | undefined;
    const onListMembers = vi.fn().mockImplementation(
      (options: { signal?: AbortSignal }) =>
        new Promise<never>((_resolve, reject) => {
          requestSignal = options.signal;
          options.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
        }),
    );
    const { rerender } = render(<ManageRolloutLaneMembersModal {...props({ onListMembers })} />);

    await waitFor(() => expect(requestSignal).toBeDefined());
    rerender(<ManageRolloutLaneMembersModal {...props({ open: false, onListMembers })} />);

    expect(requestSignal?.aborted).toBe(true);
  });

  it("caps rendered rows until Show more is requested while retaining every selected identifier", async () => {
    const user = userEvent.setup();
    const members = Array.from({ length: 101 }, (_, index) => ({
      ...currentMember,
      deviceIdentifier: `miner-${index.toString().padStart(3, "0")}`,
    }));
    render(
      <ManageRolloutLaneMembersModal
        {...props({
          onListMembers: vi.fn().mockResolvedValue({ members, nextPageToken: "", totalCount: members.length }),
        })}
      />,
    );

    expect(await screen.findByText("miner-099")).toBeInTheDocument();
    expect(screen.queryByText("miner-100")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Show more miners" }));
    expect(screen.getByText("miner-100")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Change miners" }));
    expect(screen.getByText(/Initially selected:/)).toHaveTextContent("miner-100");
  });

  it("shows empty and error states", async () => {
    const { rerender } = render(
      <ManageRolloutLaneMembersModal
        {...props({
          onListMembers: vi.fn().mockResolvedValue({ members: [], nextPageToken: "", totalCount: 0 }),
        })}
      />,
    );
    expect(await screen.findByText("No miners in this lane")).toBeInTheDocument();

    rerender(
      <ManageRolloutLaneMembersModal
        {...props({
          lane: { ...lane, id: "lane-2" },
          onListMembers: vi.fn().mockRejectedValue(new Error("Membership unavailable")),
        })}
      />,
    );
    expect(await screen.findByText("Membership unavailable")).toBeInTheDocument();
  });

  it("restarts pagination from page one after an in-place retry", async () => {
    const user = userEvent.setup();
    let resolveRetry:
      | ((page: { members: RolloutLaneMembershipMember[]; nextPageToken: string; totalCount: number }) => void)
      | undefined;
    const retryPage = new Promise<{
      members: RolloutLaneMembershipMember[];
      nextPageToken: string;
      totalCount: number;
    }>((resolve) => {
      resolveRetry = resolve;
    });
    const onListMembers = vi
      .fn()
      .mockResolvedValueOnce({ members: [currentMember], nextPageToken: "page-2", totalCount: 2 })
      .mockRejectedValueOnce(new Error("Second page unavailable"))
      .mockReturnValueOnce(retryPage);

    render(<ManageRolloutLaneMembersModal {...props({ onListMembers })} />);

    expect(await screen.findByText("Second page unavailable")).toBeInTheDocument();
    expect(screen.getByText("miner-current")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Change miners" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(onListMembers).toHaveBeenCalledTimes(3));
    expect(onListMembers).toHaveBeenNthCalledWith(
      3,
      expect.objectContaining({ laneId: lane.id, pageToken: "", includeTotalCount: true }),
    );
    expect(screen.queryByText("miner-current")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Change miners" })).toBeDisabled();

    resolveRetry?.({ members: [historicalMember], nextPageToken: "", totalCount: 1 });

    expect(await screen.findByText("miner-historical")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Change miners" })).toBeEnabled());
    expect(screen.queryByText("Second page unavailable")).not.toBeInTheDocument();
  });

  it("disables Change miners with the active-work reason", async () => {
    render(
      <ManageRolloutLaneMembersModal
        {...props({
          lane: {
            ...lane,
            firmwareConvergence: {
              ...lane.firmwareConvergence,
              confirmedCount: 1,
              updatingCount: 1,
            },
          },
        })}
      />,
    );

    expect(await screen.findByRole("button", { name: "Change miners" })).toBeDisabled();
    expect(screen.getByText("Wait for firmware updates to finish before changing miners.")).toBeInTheDocument();
  });

  it("preselects current members and closes a no-op selection without previewing", async () => {
    const user = userEvent.setup();
    const onPreview = vi.fn();
    pickerSelection.current = [currentMember.deviceIdentifier, historicalMember.deviceIdentifier];
    render(<ManageRolloutLaneMembersModal {...props({ onPreview })} />);

    await user.click(await screen.findByRole("button", { name: "Change miners" }));
    expect(screen.getByText("Initially selected: miner-current, miner-historical")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save miner selection" }));

    expect(onPreview).not.toHaveBeenCalled();
    expect(screen.queryByTestId("miner-picker")).not.toBeInTheDocument();
  });

  it("reviews additions, removals, reassignments, and firmware updates before confirming", async () => {
    const user = userEvent.setup();
    pickerSelection.current = ["miner-current", "miner-new", "miner-reassigned"];
    const onPreview = vi.fn().mockResolvedValue({
      ...emptyPreview,
      targetFirmwarePreview: {
        ...emptyPreview.targetFirmwarePreview,
        mismatchedCount: 1,
        unknownCount: 1,
      },
      reassignments: [
        {
          deviceIdentifier: "miner-reassigned",
          sourceLaneId: "lane-source",
          sourceLaneLabel: "Test lane",
        },
      ],
      removals: [historicalMember],
      requiresFirmwareConfirmation: true,
      requiresReassignmentConfirmation: true,
    });
    render(<ManageRolloutLaneMembersModal {...props({ onPreview })} />);

    await user.click(await screen.findByRole("button", { name: "Change miners" }));
    await user.click(screen.getByRole("button", { name: "Save miner selection" }));

    expect(await screen.findByRole("dialog", { name: "Review membership changes" })).toBeInTheDocument();
    expect(screen.getByText("Additions: 1 miner")).toBeInTheDocument();
    expect(screen.getByText(/Removals: 1 miner/)).toHaveTextContent(
      "Firmware will not change, and Fleet will stop managing it through this lane.",
    );
    expect(screen.getByText(/Reassignments: 1 miner/)).toHaveTextContent("Test lane");
    expect(screen.getByText(/2 miners have mismatched or unknown target firmware/)).toHaveTextContent(
      "Updates begin after confirmation.",
    );
    expect(screen.getByTestId("dialog-icon")).toHaveAttribute("data-intent", "critical");
    expect(onPreview).toHaveBeenCalledWith(
      expect.objectContaining({
        laneId: lane.id,
        addDeviceIdentifiers: ["miner-new", "miner-reassigned"],
        removeDeviceIdentifiers: ["miner-historical"],
      }),
    );
  });

  it("keeps management open when preview fails", async () => {
    const user = userEvent.setup();
    pickerSelection.current = ["miner-current"];
    render(
      <ManageRolloutLaneMembersModal
        {...props({ onPreview: vi.fn().mockRejectedValue(new Error("Preview unavailable")) })}
      />,
    );

    await user.click(await screen.findByRole("button", { name: "Change miners" }));
    await user.click(screen.getByRole("button", { name: "Save miner selection" }));

    expect(await screen.findByText("Preview unavailable")).toBeInTheDocument();
    expect(screen.getByText("Membership changes could not be previewed")).toBeInTheDocument();
    expect(screen.getByText("Manage Stable production miners")).toBeInTheDocument();
  });

  it("shows only nonzero benign removal copy with information intent", async () => {
    const user = userEvent.setup();
    pickerSelection.current = [currentMember.deviceIdentifier];
    render(<ManageRolloutLaneMembersModal {...props()} />);

    await user.click(await screen.findByRole("button", { name: "Change miners" }));
    await user.click(screen.getByRole("button", { name: "Save miner selection" }));

    expect(await screen.findByText(/Removals: 1 miner/)).toBeInTheDocument();
    expect(screen.queryByText(/Additions:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Reassignments:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/mismatched or unknown target firmware/)).not.toBeInTheDocument();
    expect(screen.getByTestId("dialog-icon")).toHaveAttribute("data-intent", "info");
  });

  it("uses warning intent when firmware confirmation is required", async () => {
    const user = userEvent.setup();
    pickerSelection.current = [currentMember.deviceIdentifier, historicalMember.deviceIdentifier, "miner-new"];
    render(
      <ManageRolloutLaneMembersModal
        {...props({
          onPreview: vi.fn().mockResolvedValue({
            ...emptyPreview,
            targetFirmwarePreview: { ...emptyPreview.targetFirmwarePreview, mismatchedCount: 1 },
            requiresFirmwareConfirmation: true,
          }),
        })}
      />,
    );

    await user.click(await screen.findByRole("button", { name: "Change miners" }));
    await user.click(screen.getByRole("button", { name: "Save miner selection" }));

    expect(await screen.findByText(/1 miner has mismatched or unknown target firmware/)).toBeInTheDocument();
    expect(screen.getByTestId("dialog-icon")).toHaveAttribute("data-intent", "warning");
  });

  it("reuses the idempotency key after a failed update and reports transition success", async () => {
    const user = userEvent.setup();
    pickerSelection.current = ["miner-current", "miner-new"];
    const onUpdate = vi
      .fn()
      .mockRejectedValueOnce(new Error("Update unavailable"))
      .mockResolvedValueOnce({ lane: { ...lane, revision: 8n }, transitionMembers: [currentMember] });
    const onUpdated = vi.fn();
    const baseProps = props({ onUpdate, onUpdated });
    const { rerender } = render(<ManageRolloutLaneMembersModal {...baseProps} />);

    await user.click(await screen.findByRole("button", { name: "Change miners" }));
    await user.click(screen.getByRole("button", { name: "Save miner selection" }));
    await user.click(await screen.findByRole("button", { name: "Confirm membership changes" }));
    await waitFor(() => expect(onUpdate).toHaveBeenCalledTimes(1));
    expect(screen.queryByText("Update unavailable")).not.toBeInTheDocument();
    rerender(<ManageRolloutLaneMembersModal {...baseProps} error="Update unavailable" />);
    expect(screen.getByText("Update unavailable")).toBeInTheDocument();
    expect(screen.getByText("Membership could not be updated")).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Review membership changes" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Confirm membership changes" }));

    await waitFor(() => expect(onUpdated).toHaveBeenCalled());
    expect(onUpdate).toHaveBeenCalledTimes(2);
    expect(onUpdate.mock.calls[0][0]).toMatchObject({
      expectedRevision: lane.revision,
      confirmFirmware: false,
      confirmReassign: false,
      reason: "Update rollout lane membership",
    });
    expect(onUpdate.mock.calls[1][0].idempotencyKey).toBe(onUpdate.mock.calls[0][0].idempotencyKey);
  });

  it("keeps management open and refreshes members when no firmware transition starts", async () => {
    const user = userEvent.setup();
    pickerSelection.current = [currentMember.deviceIdentifier];
    const onListMembers = vi.fn().mockResolvedValue({
      members: [currentMember, historicalMember],
      nextPageToken: "",
      totalCount: 2,
    });
    const onUpdate = vi.fn().mockResolvedValue({
      lane: { ...lane, revision: 8n },
      transitionMembers: [],
    });
    const baseProps = props({ onListMembers, onUpdate });
    const { rerender } = render(<ManageRolloutLaneMembersModal {...baseProps} />);

    await user.click(await screen.findByRole("button", { name: "Change miners" }));
    await user.click(screen.getByRole("button", { name: "Save miner selection" }));
    await user.click(await screen.findByRole("button", { name: "Confirm membership changes" }));

    await waitFor(() => expect(onUpdate).toHaveBeenCalledTimes(1));
    expect(onListMembers).toHaveBeenCalledTimes(1);
    rerender(<ManageRolloutLaneMembersModal {...baseProps} lane={{ ...lane, revision: 8n }} />);
    await waitFor(() => expect(onListMembers).toHaveBeenCalledTimes(2));
    expect(screen.getByText("Manage Stable production miners")).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "Review membership changes" })).not.toBeInTheDocument();
  });
});
