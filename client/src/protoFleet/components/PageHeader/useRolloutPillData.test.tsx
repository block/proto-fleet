import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";

import {
  RolloutGroupActivity,
  RolloutGroupLifecycle,
  RolloutGroupSchema,
  RolloutGroupTerminalOutcome,
  RolloutLaneSchema,
  RolloutMemberState,
  RolloutSchema,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/rolloutEvents";
import { useRolloutPillData } from "@/protoFleet/components/PageHeader/useRolloutPillData";
import { BETWEEN_CHANNEL_STRATEGY_KEY } from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";

const {
  getRolloutLaneForRollout,
  listRolloutLanes,
  listRolloutGroups,
  listRollouts,
  getDeviceSet,
  handleAuthErrors,
  navigate,
  useHasPermission,
} = vi.hoisted(() => ({
  getRolloutLaneForRollout: vi.fn(),
  listRolloutLanes: vi.fn(),
  listRolloutGroups: vi.fn(),
  listRollouts: vi.fn(),
  getDeviceSet: vi.fn(),
  handleAuthErrors: vi.fn(),
  navigate: vi.fn(),
  useHasPermission: vi.fn(),
}));

vi.mock("react-router-dom", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router-dom")>()),
  useNavigate: () => navigate,
}));

vi.mock("@/protoFleet/api/clients", () => ({
  rolloutClient: {
    getRolloutLaneForRollout,
    listRolloutLanes,
    listRolloutGroups,
    listRollouts,
  },
  deviceSetClient: {
    getDeviceSet,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors }),
  useHasPermission,
}));

function lane(active = true, laneId = "lane-1", label = "Stable production", rolloutId = "rollout-1") {
  return create(RolloutLaneSchema, {
    laneId,
    label,
    channels: [{ channelId: 42n, releaseSetId: 8n, position: 1, rolloutId }],
    firmwareConvergence: {
      totalCount: 6,
      pendingCount: active ? 3 : 0,
      updatingCount: active ? 1 : 0,
      verifyingCount: 0,
      confirmedCount: active ? 2 : 6,
      attentionCount: 0,
    },
  });
}

function rollout(
  strategyKey = BETWEEN_CHANNEL_STRATEGY_KEY,
  state = RolloutState.RUNNING,
  memberState?: RolloutMemberState,
) {
  return create(RolloutSchema, {
    rolloutId: "rollout-1",
    name: "Stable 2.0",
    strategyKey,
    state,
    members: memberState ? [{ memberId: 1n, deviceIdentifier: "miner-1", state: memberState }] : [],
  });
}

function completedRollout(
  rolloutId: string,
  state: RolloutState.COMPLETED | RolloutState.COMPLETED_WITH_FAILURES | RolloutState.REVERTED,
  finishedAt: string,
) {
  const finishedTimestamp = timestampFromDate(new Date(finishedAt));
  return create(RolloutSchema, {
    rolloutId,
    name: `Completed ${rolloutId}`,
    strategyKey: BETWEEN_CHANNEL_STRATEGY_KEY,
    state,
    completedAt: state === RolloutState.REVERTED ? undefined : finishedTimestamp,
    revertedAt: state === RolloutState.REVERTED ? finishedTimestamp : undefined,
  });
}

function grantPermissions(...permissions: string[]) {
  useHasPermission.mockImplementation((permission: string) => permissions.includes(permission));
}

async function runInitialRefresh() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

describe("useRolloutPillData", () => {
  beforeEach(() => {
    const storedValues = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      clear: () => storedValues.clear(),
      getItem: (key: string) => storedValues.get(key) ?? null,
      setItem: (key: string, value: string) => storedValues.set(key, value),
    });
    vi.useFakeTimers();
    vi.clearAllMocks();
    localStorage.clear();
    grantPermissions("rollout:read", "channel:read");
    getRolloutLaneForRollout.mockResolvedValue({ lane: undefined });
    listRolloutLanes.mockResolvedValue({ lanes: [] });
    listRollouts.mockResolvedValue({ rollouts: [] });
    listRolloutGroups.mockImplementation(async (_request, options) => ({
      parents: [],
      legacyHistory: (
        await listRollouts(
          {
            states: [
              RolloutState.CREATED,
              RolloutState.RUNNING,
              RolloutState.PAUSED,
              RolloutState.REVIEW,
              RolloutState.ABORTED,
              RolloutState.REVERTING,
              RolloutState.COMPLETED,
              RolloutState.COMPLETED_WITH_FAILURES,
              RolloutState.REVERTED,
            ],
          },
          options,
        )
      ).rollouts,
    }));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("prefers an active persisted firmware rollout over active firmware convergence", async () => {
    listRolloutLanes.mockResolvedValue({ lanes: [lane()] });
    listRollouts.mockResolvedValue({
      rollouts: [completedRollout("completed", RolloutState.COMPLETED, "2026-08-19T01:00:00Z"), rollout()],
    });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.title).toBe("Stable 2.0");
    expect(result.current.detailsPath).toBe("/settings/firmware?tab=rolloutLanes");
    expect(result.current.hasVisiblePill).toBe(true);
    expect(getRolloutLaneForRollout).not.toHaveBeenCalled();
  });

  it("reuses aggregate legacy history without listing rollouts again", async () => {
    listRolloutGroups.mockResolvedValue({
      parents: [],
      legacyHistory: [rollout()],
    });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.title).toBe("Stable 2.0");
    expect(listRolloutGroups).toHaveBeenCalledOnce();
    expect(listRollouts).not.toHaveBeenCalled();
  });

  it("shows one aggregate pill focused on the action-needed model child", async () => {
    const running = rollout();
    running.rolloutId = "child-running";
    running.parentId = "parent-1";
    running.manufacturer = "Proto";
    running.model = "Alpha";
    const review = rollout(BETWEEN_CHANNEL_STRATEGY_KEY, RolloutState.REVIEW);
    review.rolloutId = "child-review";
    review.parentId = "parent-1";
    review.manufacturer = "Acme";
    review.model = "Beta";
    listRolloutGroups.mockResolvedValue({
      parents: [
        create(RolloutGroupSchema, {
          parentId: "parent-1",
          laneId: "lane-1",
          name: "Two model rollout",
          lifecycle: RolloutGroupLifecycle.ACTIVE,
          activity: RolloutGroupActivity.REVIEW,
          terminalOutcome: RolloutGroupTerminalOutcome.PENDING,
          children: [running, review],
        }),
      ],
    });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.title).toBe("Two model rollout");
    expect(result.current.activeEvent?.scopeLabel).toBe("2 models · 1 need attention");
    expect(result.current.detailsPath).toContain("rolloutParent=parent-1");
    expect(result.current.detailsPath).toContain("rolloutChild=child-review");
  });

  it("acknowledges only the ready parent revision and resurfaces a revised result", async () => {
    const child = completedRollout("child-completed", RolloutState.COMPLETED, "2026-08-19T01:00:00Z");
    child.parentId = "parent-ready";
    child.manufacturer = "Proto";
    child.model = "Alpha";
    let resultRevision = 4n;
    listRolloutGroups.mockImplementation(async () => ({
      parents: [
        create(RolloutGroupSchema, {
          parentId: "parent-ready",
          laneId: "lane-1",
          name: "Ready model result",
          lifecycle: RolloutGroupLifecycle.TERMINAL,
          activity: RolloutGroupActivity.SETTLED,
          terminalOutcome: RolloutGroupTerminalOutcome.SUCCESSFUL,
          resultReady: true,
          resultRevision,
          children: [child],
        }),
      ],
      legacyHistory: [],
    }));

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.hasVisiblePill).toBe(true);

    act(() => result.current.onViewRollout?.());
    expect(localStorage.getItem("protoFleet.acknowledgedRolloutGroupResult")).toBe(
      '{"parentId":"parent-ready","resultRevision":"4"}',
    );
    expect(result.current.hasVisiblePill).toBe(false);

    resultRevision = 5n;
    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await act(async () => {});
    expect(result.current.hasVisiblePill).toBe(true);
  });

  it("resolves the exact lane label for an active rollout without active convergence", async () => {
    getRolloutLaneForRollout.mockResolvedValue({
      lane: lane(false, "lane-1", "Stable production", "rollout-1"),
    });
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.scopeLabel).toBe("Stable production");
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();
    expect(getRolloutLaneForRollout.mock.calls[0]?.[0]).toMatchObject({ rolloutId: "rollout-1" });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(result.current.activeEvent?.scopeLabel).toBe("Stable production");
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();
  });

  it("resolves the exact lane label for the latest completed rollout result", async () => {
    getRolloutLaneForRollout.mockResolvedValue({
      lane: lane(false, "lane-completed", "Completed lane", "completed"),
    });
    listRollouts.mockResolvedValue({
      rollouts: [completedRollout("completed", RolloutState.COMPLETED, "2026-08-19T01:00:00Z")],
    });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.scopeLabel).toBe("Completed lane");
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();
    expect(getRolloutLaneForRollout.mock.calls[0]?.[0]).toMatchObject({ rolloutId: "completed" });
  });

  it("keeps the generic label when the exact lane relationship is not found", async () => {
    getRolloutLaneForRollout.mockRejectedValue(new ConnectError("rollout lane not found", Code.NotFound));
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();
    expect(handleAuthErrors).not.toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();

    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await act(async () => {});
    expect(getRolloutLaneForRollout).toHaveBeenCalledTimes(2);
  });

  it("drops a cached exact lane after a fresh lookup no longer finds it", async () => {
    getRolloutLaneForRollout
      .mockResolvedValueOnce({ lane: lane() })
      .mockRejectedValueOnce(new ConnectError("rollout lane not found", Code.NotFound));
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.activeEvent?.scopeLabel).toBe("Stable production");

    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await act(async () => {});

    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");
    expect(getRolloutLaneForRollout).toHaveBeenCalledTimes(2);
  });

  it("periodically revalidates and evicts an archived exact lane", async () => {
    getRolloutLaneForRollout
      .mockResolvedValueOnce({ lane: lane() })
      .mockRejectedValueOnce(new ConnectError("rollout lane not found", Code.NotFound));
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.activeEvent?.scopeLabel).toBe("Stable production");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(29_999);
    });
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(getRolloutLaneForRollout).toHaveBeenCalledTimes(2);
    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");
  });

  it("evicts a cached exact lane when forced refresh finds it archived after a list failure", async () => {
    getRolloutLaneForRollout
      .mockResolvedValueOnce({ lane: lane() })
      .mockRejectedValueOnce(new ConnectError("rollout lane not found", Code.NotFound));
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });
    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.activeEvent?.scopeLabel).toBe("Stable production");

    listRolloutLanes.mockRejectedValueOnce(new ConnectError("temporarily unavailable", Code.Unavailable));
    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await act(async () => {});

    expect(getRolloutLaneForRollout).toHaveBeenCalledTimes(2);
    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");
  });

  it("preserves a cached exact lane after a transient fresh lookup failure", async () => {
    const transientError = new ConnectError("temporarily unavailable", Code.Unavailable);
    getRolloutLaneForRollout.mockResolvedValueOnce({ lane: lane() }).mockRejectedValueOnce(transientError);
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.activeEvent?.scopeLabel).toBe("Stable production");

    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await act(async () => {});

    expect(result.current.activeEvent?.scopeLabel).toBe("Stable production");
    expect(getRolloutLaneForRollout).toHaveBeenCalledTimes(2);
    expect(handleAuthErrors).toHaveBeenCalledWith({ error: transientError });
  });

  it("does not attempt the exact lookup after channel permission is denied", async () => {
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });
    listRolloutLanes.mockRejectedValue(new ConnectError("access denied", Code.PermissionDenied));

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");
    expect(getRolloutLaneForRollout).not.toHaveBeenCalled();
    expect(handleAuthErrors).toHaveBeenCalledWith({ error: expect.any(ConnectError) });
  });

  it("falls back to the first active initial lane in server order", async () => {
    listRolloutLanes.mockResolvedValue({
      lanes: [lane(true, "lane-2", "Canary"), lane(true, "lane-1", "Stable production")],
    });
    listRollouts.mockResolvedValue({
      rollouts: [completedRollout("completed", RolloutState.COMPLETED, "2026-08-19T01:00:00Z")],
    });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.title).toBe("Firmware convergence");
    expect(result.current.activeEvent?.scopeLabel).toBe("Canary");
    expect(result.current.detailsPath).toBe("/settings/firmware?tab=rolloutLanes&setupLane=lane-2");
    expect(result.current.hasVisiblePill).toBe(true);
  });

  it("shows only the latest unacknowledged completed rollout across remounts", async () => {
    listRollouts.mockResolvedValue({
      rollouts: [
        completedRollout("older", RolloutState.COMPLETED, "2026-08-18T01:00:00Z"),
        completedRollout("latest", RolloutState.COMPLETED, "2026-08-19T01:00:00Z"),
      ],
    });

    const first = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(first.result.current.activeEvent?.title).toBe("Completed latest");
    expect(first.result.current.detailsPath).toBe("/settings/firmware?tab=rolloutLanes&rollout=latest");
    first.unmount();

    const second = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(second.result.current.activeEvent?.title).toBe("Completed latest");

    act(() => second.result.current.onViewRollout?.());

    expect(localStorage.getItem("protoFleet.acknowledgedRolloutResultId")).toBe('"latest"');
    expect(navigate).toHaveBeenCalledWith("/settings/firmware?tab=rolloutLanes&rollout=latest");
    expect(second.result.current.hasVisiblePill).toBe(false);

    second.unmount();
    const third = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(third.result.current.activeEvent).toBeNull();
  });

  it.each([
    [RolloutState.COMPLETED_WITH_FAILURES, "completedWithFailures"],
    [RolloutState.REVERTED, "reverted"],
  ] as const)("shows terminal rollout state %s", async (state, expectedState) => {
    listRollouts.mockResolvedValue({
      rollouts: [completedRollout("terminal", state, "2026-08-19T01:00:00Z")],
    });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.state).toBe(expectedState);
    expect(result.current.hasVisiblePill).toBe(true);
  });

  it("loads only the RPC allowed by each read permission", async () => {
    grantPermissions("rollout:read");
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.title).toBe("Stable 2.0");
    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");
    expect(result.current.detailsPath).toBeNull();
    expect(result.current.hasVisiblePill).toBe(true);
    expect(listRollouts).toHaveBeenCalledOnce();
    expect(listRolloutLanes).not.toHaveBeenCalled();
    expect(getRolloutLaneForRollout).not.toHaveBeenCalled();
    expect(useHasPermission).toHaveBeenCalledWith("rollout:read");
    expect(useHasPermission).toHaveBeenCalledWith("channel:read");
  });

  it("shows firmware convergence with channel read permission alone", async () => {
    grantPermissions("channel:read");
    listRolloutLanes.mockResolvedValue({ lanes: [lane()] });

    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(result.current.activeEvent?.title).toBe("Firmware convergence");
    expect(listRolloutLanes).toHaveBeenCalledOnce();
    expect(listRollouts).not.toHaveBeenCalled();
  });

  it("removes the cached lane label immediately when channel read is revoked", async () => {
    getRolloutLaneForRollout.mockResolvedValue({ lane: lane() });
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });
    const { result, rerender } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.activeEvent?.scopeLabel).toBe("Stable production");
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();

    listRollouts.mockReturnValueOnce(new Promise(() => {}));
    grantPermissions("rollout:read");
    rerender();

    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");
    expect(result.current.detailsPath).toBeNull();
    expect(result.current.hasVisiblePill).toBe(true);
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();
    await runInitialRefresh();
    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");
  });

  it("sanitizes a completed exact lane label immediately when channel read is revoked", async () => {
    getRolloutLaneForRollout.mockResolvedValue({
      lane: lane(false, "lane-completed", "Completed lane", "completed"),
    });
    listRollouts.mockResolvedValue({
      rollouts: [completedRollout("completed", RolloutState.COMPLETED, "2026-08-19T01:00:00Z")],
    });
    const { result, rerender } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.activeEvent?.scopeLabel).toBe("Completed lane");

    grantPermissions("rollout:read");
    rerender();

    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");
    expect(result.current.detailsPath).toBeNull();
  });

  it("aborts an in-flight exact lane lookup on unmount", async () => {
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });
    getRolloutLaneForRollout.mockImplementation(
      (_request: unknown, options: { signal: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          options.signal.addEventListener("abort", () => reject(new ConnectError("canceled", Code.Canceled)), {
            once: true,
          });
        }),
    );

    const hook = renderHook(() => useRolloutPillData());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();
    const options = getRolloutLaneForRollout.mock.calls[0]?.[1] as { signal: AbortSignal };
    expect(options.signal.aborted).toBe(false);

    hook.unmount();
    expect(options.signal.aborted).toBe(true);
  });

  it("does not let a stale exact lookup overwrite a newer selection", async () => {
    const staleLookup = deferred<{ lane: ReturnType<typeof lane> }>();
    getRolloutLaneForRollout
      .mockReturnValueOnce(staleLookup.promise)
      .mockResolvedValueOnce({ lane: lane(false, "lane-new", "New lane", "rollout-1") });
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });
    const { result, rerender } = renderHook(() => useRolloutPillData());

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(getRolloutLaneForRollout).toHaveBeenCalledOnce();

    grantPermissions("rollout:read");
    rerender();
    await runInitialRefresh();
    expect(result.current.activeEvent?.scopeLabel).toBe("Rollout lane");

    grantPermissions("rollout:read", "channel:read");
    rerender();
    await runInitialRefresh();
    expect(result.current.activeEvent?.scopeLabel).toBe("New lane");
    expect(getRolloutLaneForRollout).toHaveBeenCalledTimes(2);

    await act(async () => {
      staleLookup.resolve({ lane: lane(false, "lane-old", "Stale lane", "rollout-1") });
      await Promise.resolve();
    });
    expect(result.current.activeEvent?.scopeLabel).toBe("New lane");
  });

  it("does not poll when disabled or when both permissions are absent", async () => {
    grantPermissions();
    const { rerender } = renderHook(({ enabled }) => useRolloutPillData({ enabled }), {
      initialProps: { enabled: true },
    });
    await runInitialRefresh();
    expect(listRollouts).not.toHaveBeenCalled();
    expect(listRolloutLanes).not.toHaveBeenCalled();

    grantPermissions("rollout:read", "channel:read");
    rerender({ enabled: false });
    act(() => {
      vi.advanceTimersByTime(30_000);
      window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT));
    });
    expect(listRollouts).not.toHaveBeenCalled();
    expect(listRolloutLanes).not.toHaveBeenCalled();
  });

  it("does not reveal cached selection while a fresh re-enabled request is pending", async () => {
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });
    const { result, rerender } = renderHook(({ enabled }) => useRolloutPillData({ enabled }), {
      initialProps: { enabled: true },
    });
    await runInitialRefresh();
    expect(result.current.hasVisiblePill).toBe(true);

    listRollouts.mockReturnValueOnce(new Promise(() => {}));
    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await act(async () => {});
    expect(listRollouts).toHaveBeenCalledTimes(2);

    rerender({ enabled: false });
    expect(result.current.hasVisiblePill).toBe(false);

    listRollouts.mockResolvedValue({ rollouts: [] });
    rerender({ enabled: true });
    expect(result.current.hasVisiblePill).toBe(false);
    await runInitialRefresh();

    expect(listRollouts).toHaveBeenCalledTimes(3);
    expect(result.current.activeEvent).toBeNull();
  });

  it("clears cached selection after losing all read access", async () => {
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });
    const { result, rerender } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.hasVisiblePill).toBe(true);

    grantPermissions();
    rerender();
    expect(result.current.hasVisiblePill).toBe(false);

    listRollouts.mockResolvedValue({ rollouts: [] });
    grantPermissions("rollout:read");
    rerender();
    expect(result.current.hasVisiblePill).toBe(false);
    await runInitialRefresh();
    expect(result.current.activeEvent).toBeNull();
  });

  it("uses idle and active polling cadences", async () => {
    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.activeEvent).toBeNull();
    expect(result.current.hasVisiblePill).toBe(false);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(29_999);
    });
    expect(listRollouts).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(listRollouts).toHaveBeenCalledTimes(2);

    listRollouts.mockResolvedValue({ rollouts: [rollout()] });
    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await act(async () => {});
    expect(result.current.activeEvent?.title).toBe("Stable 2.0");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(4_999);
    });
    expect(listRollouts).toHaveBeenCalledTimes(3);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(listRollouts).toHaveBeenCalledTimes(4);
  });

  it("keeps completed rollout pills on the idle polling cadence", async () => {
    listRollouts.mockResolvedValue({
      rollouts: [completedRollout("completed", RolloutState.COMPLETED, "2026-08-19T01:00:00Z")],
    });
    renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(29_999);
    });
    expect(listRollouts).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(listRollouts).toHaveBeenCalledTimes(2);
  });

  it("refreshes immediately after rollout changes", async () => {
    renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(listRollouts).toHaveBeenCalledOnce();

    act(() => window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT)));
    await act(async () => {});

    expect(listRollouts).toHaveBeenCalledTimes(2);
  });

  it("reuses the selection and result when an active poll is unchanged", async () => {
    listRollouts.mockResolvedValue({ rollouts: [rollout()] });
    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    const firstResult = result.current;
    const firstEvent = result.current.activeEvent;

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(listRollouts).toHaveBeenCalledTimes(2);
    expect(result.current).toBe(firstResult);
    expect(result.current.activeEvent).toBe(firstEvent);
  });

  it("updates progress when member state changes without a rollout revision change", async () => {
    listRollouts
      .mockResolvedValueOnce({ rollouts: [rollout(undefined, undefined, RolloutMemberState.PENDING)] })
      .mockResolvedValueOnce({ rollouts: [rollout(undefined, undefined, RolloutMemberState.SUCCEEDED)] });
    const { result } = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(result.current.activeEvent?.rollups).toContainEqual({ phase: "queued", count: 1 });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(result.current.activeEvent?.rollups).toContainEqual({ phase: "done", count: 1 });
  });

  it("reports request errors, ignores aborts, and aborts requests on unmount", async () => {
    listRollouts.mockRejectedValueOnce(new Error("load failed")).mockReturnValueOnce(new Promise(() => {}));

    const first = renderHook(() => useRolloutPillData());
    await runInitialRefresh();
    expect(handleAuthErrors).toHaveBeenCalledWith({ error: expect.any(Error) });
    first.unmount();

    const second = renderHook(() => useRolloutPillData());
    act(() => {
      vi.advanceTimersByTime(0);
    });
    const options = listRollouts.mock.calls[listRollouts.mock.calls.length - 1]?.[1] as { signal: AbortSignal };
    expect(options.signal.aborted).toBe(false);
    second.unmount();
    expect(options.signal.aborted).toBe(true);
    expect(handleAuthErrors).toHaveBeenCalledTimes(1);
  });

  it("maps aggregate RPC data without hydrating device sets", async () => {
    listRolloutLanes.mockResolvedValue({ lanes: [lane()] });

    renderHook(() => useRolloutPillData());
    await runInitialRefresh();

    expect(listRolloutLanes).toHaveBeenCalledOnce();
    expect(listRolloutLanes.mock.calls[0]?.[0].activeFirmwareConvergenceOnly).toBe(true);
    expect(listRollouts.mock.calls[0]?.[0].states).toEqual([
      RolloutState.CREATED,
      RolloutState.RUNNING,
      RolloutState.PAUSED,
      RolloutState.REVIEW,
      RolloutState.ABORTED,
      RolloutState.REVERTING,
      RolloutState.COMPLETED,
      RolloutState.COMPLETED_WITH_FAILURES,
      RolloutState.REVERTED,
    ]);
    expect(getDeviceSet).not.toHaveBeenCalled();
  });
});
