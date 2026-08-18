import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import { ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/rolloutEvents";
import type { RolloutLane, RolloutMemberState, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";

const listFirmwareFiles = vi.hoisted(() => vi.fn());
const rolloutApi = vi.hoisted(() => ({
  lanes: [] as RolloutLane[],
  rollouts: [] as RolloutRecord[],
  isLoading: false,
  isMutating: false,
  loadError: null as string | null,
  mutationError: null as string | null,
  permissions: {
    canReadChannels: true,
    canManageChannels: false,
    canRead: true,
    canManage: false,
    canControl: false,
  },
  listRolloutLanes: vi.fn(),
  getRolloutLane: vi.fn(),
  createRolloutLane: vi.fn(),
  startRolloutLane: vi.fn(),
  listRollouts: vi.fn(),
  getRollout: vi.fn(),
  admitRollout: vi.fn(),
  continueRollout: vi.fn(),
  pauseRollout: vi.fn(),
  resumeRollout: vi.fn(),
  abortRollout: vi.fn(),
  revertRollout: vi.fn(),
  completeRollout: vi.fn(),
}));
const capturedTable = vi.hoisted(() => ({
  onStart: null as ((lane: RolloutLane) => void) | null,
}));

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({ listFirmwareFiles }),
}));

vi.mock("@/protoFleet/api/useRolloutApi", () => ({
  useRolloutApi: () => rolloutApi,
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/RolloutLanesTable", () => ({
  default: ({
    rows,
    canStart,
    isPreparingStart,
    onStart,
  }: {
    rows: Array<{ lane: RolloutLane }>;
    canStart: boolean;
    isPreparingStart?: boolean;
    onStart: (lane: RolloutLane) => void;
  }) => {
    capturedTable.onStart = onStart;
    return (
      <div data-testid="rollout-lanes-table">
        {rows.map(({ lane }) => (
          <div key={lane.id}>
            {lane.label}
            {canStart ? (
              <button type="button" disabled={isPreparingStart} onClick={() => onStart(lane)}>
                Start {lane.label}
              </button>
            ) : null}
          </div>
        ))}
      </div>
    );
  },
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/StartRolloutLaneModal", () => ({
  default: ({ lane }: { lane: RolloutLane }) => <div>Prepared {lane.label}</div>,
}));

vi.mock("@/protoFleet/features/rollout/betweenChannel/BetweenChannelRolloutStatus", () => ({
  default: ({ onCompleteWithFailures }: { onCompleteWithFailures?: () => void }) =>
    onCompleteWithFailures ? (
      <button type="button" onClick={onCompleteWithFailures}>
        Complete with failures
      </button>
    ) : null,
}));

const { default: RolloutLanesTab } = await import("./RolloutLanesTab");

function lane(id: string, label: string, rolloutId?: string, currentChannelId = 41n): RolloutLane {
  return {
    id,
    label,
    description: "",
    currentChannelId,
    revision: 1n,
    channels: rolloutId
      ? [
          {
            channelId: 42n,
            releaseSetId: 8n,
            position: 1,
            rolloutId,
            current: currentChannelId === 42n,
          },
        ]
      : [],
    memberCount: 2,
    memberIdentifiers: ["miner-1", "miner-2"],
    currentReleaseTargets: [],
  };
}

function rollout(
  id: string,
  state: RolloutRecord["state"],
  memberStates: RolloutMemberState[] = ["succeeded"],
): RolloutRecord {
  return {
    id,
    name: `Rollout ${id}`,
    strategyKey: "between_channel",
    state,
    revision: 2n,
    sourceChannelId: 41n,
    targetChannelId: 42n,
    reason: "Validated release",
    batches: [
      {
        id: 1n,
        position: 0,
        label: "Final",
        state: state === "review" ? "completed" : "admitted",
        revision: 1n,
        members: [],
      },
    ],
    members: memberStates.map((memberState, index) => ({
      id: BigInt(index + 1),
      batchId: 1n,
      deviceIdentifier: `miner-${index + 1}`,
      position: index,
      state: memberState,
      revision: 1n,
      evidence: [],
    })),
    causes: [],
    availableActions: {
      admit: false,
      continue: false,
      pause: state === "running",
      resume: false,
      abort: state === "running",
      revert: state === "aborted",
      complete: state === "review",
    },
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("RolloutLanesTab", () => {
  beforeEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    capturedTable.onStart = null;
    rolloutApi.lanes = [lane("lane-1", "Stable production")];
    rolloutApi.rollouts = [];
    rolloutApi.isLoading = false;
    rolloutApi.isMutating = false;
    rolloutApi.loadError = null;
    rolloutApi.mutationError = null;
    Object.assign(rolloutApi.permissions, {
      canReadChannels: true,
      canManageChannels: false,
      canRead: true,
      canManage: false,
      canControl: false,
    });
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    rolloutApi.getRolloutLane.mockImplementation(async ({ laneId }: { laneId: string }) => {
      const result = rolloutApi.lanes.find(({ id }) => id === laneId);
      if (!result) {
        throw new Error("Lane not found");
      }
      return result;
    });
    rolloutApi.getRollout.mockImplementation(async ({ rolloutId }: { rolloutId: string }) => {
      const result = rolloutApi.rollouts.find(({ id }) => id === rolloutId);
      if (!result) {
        throw new Error("Rollout not found");
      }
      return result;
    });
    rolloutApi.completeRollout.mockImplementation(async () => rolloutApi.rollouts[0]);
    listFirmwareFiles.mockResolvedValue([]);
  });

  it("loads history once and refreshes lane pointers after rollout events", async () => {
    const { unmount } = render(<RolloutLanesTab />);

    await waitFor(() => {
      expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1);
      expect(rolloutApi.listRollouts).toHaveBeenCalledTimes(1);
    });
    expect(await screen.findByText("Stable production")).toBeInTheDocument();

    act(() => {
      window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT));
    });
    await waitFor(() => expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2));
    expect(rolloutApi.listRollouts).toHaveBeenCalledTimes(1);

    unmount();
    act(() => {
      window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT));
    });
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2);
  });

  it("does not load firmware files for rollout managers without channel management", async () => {
    rolloutApi.permissions.canManage = true;

    render(<RolloutLanesTab />);

    await waitFor(() => expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(1));
    expect(listFirmwareFiles).not.toHaveBeenCalled();
  });

  it("loads firmware files for channel managers", async () => {
    rolloutApi.permissions.canManageChannels = true;

    render(<RolloutLanesTab />);

    await waitFor(() => expect(listFirmwareFiles).toHaveBeenCalledTimes(1));
  });

  it("polls only active and unsettled rollout IDs while preserving history", async () => {
    vi.useFakeTimers();
    rolloutApi.rollouts = [
      rollout("running", "running", ["admitted"]),
      rollout("aborted-unsettled", "aborted", ["succeeded", "admitted"]),
      rollout("history", "completed"),
    ];
    rolloutApi.lanes = [
      lane("lane-running", "Running", "running"),
      lane("lane-aborted", "Aborted", "aborted-unsettled"),
      lane("lane-history", "History", "history", 42n),
    ];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);

    render(<RolloutLanesTab />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(rolloutApi.listRollouts).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(rolloutApi.getRollout.mock.calls.map(([request]) => request.rolloutId).sort()).toEqual([
      "aborted-unsettled",
      "running",
    ]);
    expect(rolloutApi.listRollouts).toHaveBeenCalledTimes(1);
    expect(rolloutApi.listRolloutLanes).toHaveBeenCalledTimes(2);
    expect(rolloutApi.completeRollout).not.toHaveBeenCalled();
  });

  it("does not overlap bounded rollout refreshes", async () => {
    vi.useFakeTimers();
    const liveRollout = rollout("running", "running", ["admitted"]);
    const firstRefresh = deferred<RolloutRecord>();
    rolloutApi.rollouts = [liveRollout];
    rolloutApi.lanes = [lane("lane-running", "Running", liveRollout.id)];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    rolloutApi.getRollout.mockReturnValueOnce(firstRefresh.promise).mockResolvedValue(liveRollout);

    render(<RolloutLanesTab />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(rolloutApi.getRollout).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(rolloutApi.getRollout).toHaveBeenCalledTimes(1);

    await act(async () => {
      firstRefresh.resolve(liveRollout);
      await firstRefresh.promise;
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(rolloutApi.getRollout).toHaveBeenCalledTimes(2);
  });

  it("keeps the lane table visible during background and preparation loads", async () => {
    const user = userEvent.setup();
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.permissions.canManage = true;
    rolloutApi.isLoading = true;
    const preparation = deferred<RolloutLane>();
    rolloutApi.getRolloutLane.mockReturnValue(preparation.promise);

    render(<RolloutLanesTab />);
    expect(await screen.findByTestId("rollout-lanes-table")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Start Stable production" }));
    expect(screen.getByTestId("rollout-lanes-table")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Start Stable production" })).toBeDisabled();
  });

  it("ignores an out-of-order lane preparation response", async () => {
    rolloutApi.permissions.canManageChannels = true;
    rolloutApi.permissions.canManage = true;
    const firstLane = lane("lane-1", "First");
    const secondLane = lane("lane-2", "Second");
    rolloutApi.lanes = [firstLane, secondLane];
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    const first = deferred<RolloutLane>();
    const signals: AbortSignal[] = [];
    rolloutApi.getRolloutLane.mockImplementation(({ laneId, signal }: { laneId: string; signal?: AbortSignal }) => {
      if (signal) {
        signals.push(signal);
      }
      return laneId === firstLane.id ? first.promise : Promise.resolve(secondLane);
    });

    render(<RolloutLanesTab />);
    await waitFor(() => expect(capturedTable.onStart).not.toBeNull());
    act(() => {
      capturedTable.onStart?.(firstLane);
      capturedTable.onStart?.(secondLane);
    });

    expect(await screen.findByText("Prepared Second")).toBeInTheDocument();
    expect(signals[0]?.aborted).toBe(true);

    await act(async () => {
      first.resolve(firstLane);
      await first.promise;
    });
    expect(screen.queryByText("Prepared First")).not.toBeInTheDocument();
  });

  it("completes a settled failed review with explicit approval", async () => {
    const user = userEvent.setup();
    const failedReview = rollout("failed-review", "review", ["succeeded", "failed"]);
    rolloutApi.rollouts = [failedReview];
    rolloutApi.lanes = [lane("lane-1", "Stable production", failedReview.id)];
    rolloutApi.permissions.canControl = true;
    rolloutApi.listRolloutLanes.mockResolvedValue(rolloutApi.lanes);
    rolloutApi.listRollouts.mockResolvedValue(rolloutApi.rollouts);
    rolloutApi.completeRollout.mockResolvedValue({
      ...failedReview,
      state: "completedWithFailures",
      revision: 3n,
    });

    render(<RolloutLanesTab />);
    await user.click(await screen.findByRole("button", { name: "Complete with failures" }));

    expect(rolloutApi.completeRollout).toHaveBeenCalledWith(
      expect.objectContaining({
        rolloutId: failedReview.id,
        expectedRevision: failedReview.revision,
        withFailures: true,
      }),
    );
  });
});
