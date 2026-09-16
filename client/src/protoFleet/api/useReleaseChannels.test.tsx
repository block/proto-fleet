import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import {
  CreateReleaseChannelRequestSchema,
  ListReleaseChannelModelGroupsResponseSchema,
  ListReleaseChannelsResponseSchema,
  type ListRolloutsResponse,
  ListRolloutsResponseSchema,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
  ReleaseChannelScopeSchema,
  ReleaseChannelSummarySchema,
  RolloutBehaviorSchema,
  RolloutMethod,
  RolloutOrder,
  RolloutSchema,
  RolloutStatus,
  UpdateReleaseChannelRequestSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useReleaseChannels } from "@/protoFleet/api/useReleaseChannels";
import { defaultBehavior } from "@/protoFleet/features/settings/components/ReleaseChannels/behaviorUtils";

const {
  mockListReleaseChannels,
  mockGetReleaseChannel,
  mockListReleaseChannelModelGroups,
  mockListRollouts,
  mockListMinerStateSnapshots,
  mockCreateReleaseChannel,
  mockUpdateReleaseChannel,
  mockDeleteReleaseChannel,
  mockPreviewReleaseChannelScope,
  mockApplyReleaseChannelFirmware,
  mockCancelRollout,
  mockRetryFailedRolloutDevices,
  mockListReleaseChannelMiners,
  mockListRolloutDevices,
  mockHandleAuthErrors,
} = vi.hoisted(() => ({
  mockListReleaseChannels: vi.fn(),
  mockGetReleaseChannel: vi.fn(),
  mockListReleaseChannelModelGroups: vi.fn(),
  mockListRollouts: vi.fn(),
  mockListMinerStateSnapshots: vi.fn(),
  mockCreateReleaseChannel: vi.fn(),
  mockUpdateReleaseChannel: vi.fn(),
  mockDeleteReleaseChannel: vi.fn(),
  mockPreviewReleaseChannelScope: vi.fn(),
  mockApplyReleaseChannelFirmware: vi.fn(),
  mockCancelRollout: vi.fn(),
  mockRetryFailedRolloutDevices: vi.fn(),
  mockListReleaseChannelMiners: vi.fn(),
  mockListRolloutDevices: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mockHandleAuthErrors }),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  rolloutClient: {
    listReleaseChannels: mockListReleaseChannels,
    getReleaseChannel: mockGetReleaseChannel,
    listReleaseChannelModelGroups: mockListReleaseChannelModelGroups,
    listRollouts: mockListRollouts,
    createReleaseChannel: mockCreateReleaseChannel,
    updateReleaseChannel: mockUpdateReleaseChannel,
    deleteReleaseChannel: mockDeleteReleaseChannel,
    previewReleaseChannelScope: mockPreviewReleaseChannelScope,
    applyReleaseChannelFirmware: mockApplyReleaseChannelFirmware,
    cancelRollout: mockCancelRollout,
    retryFailedRolloutDevices: mockRetryFailedRolloutDevices,
    listReleaseChannelMiners: mockListReleaseChannelMiners,
    listRolloutDevices: mockListRolloutDevices,
  },
  fleetManagementClient: {
    listMinerStateSnapshots: mockListMinerStateSnapshots,
  },
}));

const canarySummary = create(ReleaseChannelSummarySchema, {
  id: 1n,
  name: "Canary",
  minerCount: 3,
  modelGroupCount: 1,
});
const canary = create(ReleaseChannelSchema, {
  id: 1n,
  name: "Canary",
  minerCount: 3,
  modelGroupCount: 1,
  scope: { rackIds: [40n] },
});
const rigGroup = create(ReleaseChannelModelGroupSchema, { manufacturer: "Proto", model: "Rig", minerCount: 3 });
// What the hook hands components: the channel with its groups drained.
const canaryView = { ...canary, modelGroups: [rigGroup] };
const rollout = create(RolloutSchema, {
  id: 9n,
  channelId: 1n,
  manufacturer: "Proto",
  model: "Rig",
  status: RolloutStatus.ACTIVE,
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function capturePollingTimer() {
  const setInterval = globalThis.setInterval;
  let poll = () => {};
  vi.spyOn(globalThis, "setInterval").mockImplementation((handler, delay, ...args) => {
    if (delay !== 5000) return setInterval(handler, delay, ...args);
    poll = () => {
      if (typeof handler === "function") handler();
    };
    return -1 as unknown as ReturnType<typeof setInterval>;
  });
  return () => poll();
}

describe("useReleaseChannels", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListReleaseChannels.mockResolvedValue(create(ListReleaseChannelsResponseSchema, { channels: [canarySummary] }));
    mockGetReleaseChannel.mockResolvedValue({ channel: canary });
    mockListReleaseChannelModelGroups.mockResolvedValue(
      create(ListReleaseChannelModelGroupsResponseSchema, { modelGroups: [rigGroup], cursor: "" }),
    );
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [rollout] }));
    mockListMinerStateSnapshots.mockResolvedValue({
      miners: [{ deviceIdentifier: "rig-001", name: "Rig A01" }],
      cursor: "",
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("handles expired authentication on initial load and interval refreshes without restarting on render", async () => {
    const poll = capturePollingTimer();
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const initialError = new ConnectError("session expired", Code.Unauthenticated);
    mockListRollouts.mockRejectedValueOnce(initialError);
    const { result, rerender } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(mockHandleAuthErrors).toHaveBeenCalledWith({ error: initialError });

    rerender();
    expect(mockListReleaseChannels).toHaveBeenCalledTimes(1);
    await act(async () => {
      poll();
    });
    expect(result.current.channels).toEqual([canaryView]);
    const previous = result.current.channels;

    const pollError = new ConnectError("session revoked", Code.Unauthenticated);
    mockListMinerStateSnapshots.mockRejectedValueOnce(pollError);
    await act(async () => {
      poll();
    });
    expect(mockHandleAuthErrors).toHaveBeenCalledTimes(2);
    expect(mockHandleAuthErrors).toHaveBeenLastCalledWith({ error: pollError });
    expect(result.current.channels).toBe(previous);
    expect(result.current.isLoading).toBe(false);
  });

  it("skips interval ticks while a refresh is pending without queueing extra requests", async () => {
    const poll = capturePollingTimer();
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    const pendingRollouts = deferred<ListRolloutsResponse>();
    mockListRollouts.mockReturnValueOnce(pendingRollouts.promise);

    await act(async () => {
      poll();
      poll();
      poll();
      poll();
    });
    expect(mockListRollouts).toHaveBeenCalledTimes(2);
    await act(async () => {
      pendingRollouts.resolve(create(ListRolloutsResponseSchema, { rollouts: [rollout] }));
      await pendingRollouts.promise;
    });
    expect(mockListRollouts).toHaveBeenCalledTimes(2);
    await act(async () => {
      poll();
    });
    expect(mockListRollouts).toHaveBeenCalledTimes(3);
  });

  it.each(["succeeds", "fails"])(
    "waits for a fresh complete snapshot after a mutation even when the already pending poll %s",
    async (priorPollResult) => {
      const poll = capturePollingTimer();
      vi.spyOn(console, "error").mockImplementation(() => undefined);
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      const oldSnapshot = create(ListReleaseChannelsResponseSchema, { channels: [canarySummary] });
      const oldPoll = deferred<typeof oldSnapshot>();
      mockListReleaseChannels.mockReturnValueOnce(oldPoll.promise);
      await act(async () => {
        poll();
      });
      expect(mockListReleaseChannels).toHaveBeenCalledTimes(2);

      const stable = create(ReleaseChannelSchema, { id: 2n, name: "Stable" });
      const freshMiners = { miners: [{ deviceIdentifier: "rig-002", name: "New miner" }], cursor: "" };
      const freshMinerPage = deferred<typeof freshMiners>();
      mockGetReleaseChannel.mockImplementation(({ channelId }) =>
        Promise.resolve({ channel: channelId === 2n ? stable : canary }),
      );
      mockListReleaseChannelModelGroups.mockImplementation(({ channelId }) =>
        Promise.resolve(
          create(ListReleaseChannelModelGroupsResponseSchema, { modelGroups: channelId === 2n ? [] : [rigGroup] }),
        ),
      );
      mockCreateReleaseChannel.mockImplementation(async () => {
        mockListReleaseChannels.mockResolvedValue(
          create(ListReleaseChannelsResponseSchema, {
            channels: [canarySummary, create(ReleaseChannelSummarySchema, { id: 2n, name: "Stable" })],
          }),
        );
        mockListMinerStateSnapshots.mockReturnValueOnce(freshMinerPage.promise);
        return { channel: stable };
      });

      let creationCompleted = false;
      let creation!: Promise<unknown>;
      await act(async () => {
        creation = result.current
          .createChannel({
            name: "Stable",
            description: "",
            scope: create(ReleaseChannelScopeSchema),
            behavior: defaultBehavior(),
          })
          .then((channel) => {
            creationCompleted = true;
            return channel;
          });
      });
      expect(mockGetReleaseChannel).toHaveBeenCalledWith({ channelId: 2n });
      expect(creationCompleted).toBe(false);
      expect(mockListReleaseChannels).toHaveBeenCalledTimes(2);

      await act(async () => {
        if (priorPollResult === "fails") {
          oldPoll.reject(new Error("previous poll unavailable"));
        } else {
          oldPoll.resolve(oldSnapshot);
        }
      });
      await waitFor(() => expect(mockListReleaseChannels).toHaveBeenCalledTimes(3));
      expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(3);
      expect(creationCompleted).toBe(false);
      expect(result.current.channels).toEqual([canaryView]);

      await act(async () => {
        freshMinerPage.resolve(freshMiners);
        await creation;
      });
      expect(creationCompleted).toBe(true);
      expect(result.current.channels).toEqual([canaryView, { ...stable, modelGroups: [] }]);
      expect(result.current.minerNames).toEqual({ "rig-002": "New miner" });
      expect(mockListReleaseChannels).toHaveBeenCalledTimes(3);
    },
  );

  it("loads channels with their scope and paged groups, rollouts and miner names together", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    expect(result.current.isLoading).toBe(true);

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.channels).toEqual([canaryView]);
    expect(mockGetReleaseChannel).toHaveBeenCalledWith({ channelId: 1n });
    expect(mockListReleaseChannelModelGroups).toHaveBeenCalledWith({ channelId: 1n, pageSize: 100, cursor: "" });
    expect(result.current.rollouts).toEqual([rollout]);
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01" });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledWith({ pageSize: 1000, cursor: "" });
  });

  it("drains rollout history and miner names on initial load and every explicit refresh", async () => {
    const completed = create(RolloutSchema, { ...rollout, id: 10n, status: RolloutStatus.COMPLETED });
    mockListRollouts.mockImplementation(({ cursor = "" }) =>
      Promise.resolve(
        create(ListRolloutsResponseSchema, {
          rollouts: cursor === "" ? [completed] : cursor === "history-3" ? [rollout] : [],
          cursor: cursor === "" ? "history-2" : cursor === "history-2" ? "history-3" : "",
        }),
      ),
    );
    mockListMinerStateSnapshots.mockImplementation(({ cursor = "" }) =>
      Promise.resolve({
        miners:
          cursor === ""
            ? [{ deviceIdentifier: "rig-001", name: "Rig A01" }]
            : cursor === "miners-3"
              ? [{ deviceIdentifier: "rig-999", name: "Rig Z99" }]
              : [],
        cursor: cursor === "" ? "miners-2" : cursor === "miners-2" ? "miners-3" : "",
      }),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.rollouts).toEqual([completed, rollout]);
    expect(result.current.rollouts.filter((r) => r.status === RolloutStatus.ACTIVE)).toEqual([rollout]);
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01", "rig-999": "Rig Z99" });

    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.rollouts).toEqual([completed, rollout]);
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01", "rig-999": "Rig Z99" });
    expect(mockListRollouts.mock.calls.map(([request]) => request)).toEqual(
      ["", "history-2", "history-3", "", "history-2", "history-3"].map((cursor) => ({ pageSize: 1000, cursor })),
    );
    expect(mockListMinerStateSnapshots.mock.calls.map(([request]) => request)).toEqual(
      ["", "miners-2", "miners-3", "", "miners-2", "miners-3"].map((cursor) => ({ pageSize: 1000, cursor })),
    );
  });

  it.each(["rollouts", "miner snapshots"])(
    "retains the completed snapshot while a later %s page is pending or fails, then retries from the beginning",
    async (pagedList) => {
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      const previous = {
        channels: result.current.channels,
        rollouts: result.current.rollouts,
        minerNames: result.current.minerNames,
      };
      const renamedChannel = create(ReleaseChannelSchema, { ...canary, name: "Renamed channel" });
      const completed = create(RolloutSchema, { ...rollout, id: 10n, status: RolloutStatus.COMPLETED });
      const nextMiner = { deviceIdentifier: "rig-002", name: "Rig B02" };
      const lastMiner = { deviceIdentifier: "rig-999", name: "Rig Z99" };
      let rejectPage!: (reason: Error) => void;
      const failedPage = new Promise((_, reject) => {
        rejectPage = reject;
      });
      const listPages = vi.fn().mockImplementationOnce(() => failedPage);
      mockGetReleaseChannel.mockResolvedValue({ channel: renamedChannel });
      mockListRollouts.mockImplementation(({ cursor = "" }) =>
        cursor !== ""
          ? listPages()
          : Promise.resolve(
              create(ListRolloutsResponseSchema, {
                rollouts: pagedList === "rollouts" ? [completed] : [completed, rollout],
                cursor: pagedList === "rollouts" ? "later-page" : "",
              }),
            ),
      );
      mockListMinerStateSnapshots.mockImplementation(({ cursor = "" }) =>
        cursor !== ""
          ? listPages()
          : Promise.resolve({
              miners: pagedList === "miner snapshots" ? [nextMiner] : [nextMiner, lastMiner],
              cursor: pagedList === "miner snapshots" ? "later-page" : "",
            }),
      );
      let refresh!: Promise<void>;
      await act(async () => {
        refresh = result.current.refresh();
      });
      await waitFor(() => expect(listPages).toHaveBeenCalledTimes(1));
      expect(result.current.channels).toBe(previous.channels);
      expect(result.current.rollouts).toBe(previous.rollouts);
      expect(result.current.minerNames).toBe(previous.minerNames);

      const rejection = expect(refresh).rejects.toThrow("later page unavailable");
      await act(async () => {
        rejectPage(new Error("later page unavailable"));
        await rejection;
      });
      expect(result.current.channels).toBe(previous.channels);
      expect(result.current.rollouts).toBe(previous.rollouts);
      expect(result.current.minerNames).toBe(previous.minerNames);
      expect(result.current.isLoading).toBe(false);

      listPages.mockResolvedValue(
        pagedList === "rollouts"
          ? create(ListRolloutsResponseSchema, { rollouts: [rollout], cursor: "" })
          : { miners: [lastMiner], cursor: "" },
      );
      await act(async () => {
        await result.current.refresh();
      });
      expect(result.current.channels).toEqual([{ ...renamedChannel, modelGroups: [rigGroup] }]);
      expect(result.current.rollouts).toEqual([completed, rollout]);
      expect(result.current.minerNames).toEqual({ "rig-002": "Rig B02", "rig-999": "Rig Z99" });
      const mockPagedList = pagedList === "rollouts" ? mockListRollouts : mockListMinerStateSnapshots;
      expect(mockPagedList.mock.calls.slice(1).map(([request]) => request.cursor)).toEqual([
        "",
        "later-page",
        "",
        "later-page",
      ]);
    },
  );

  it("loads every channel page with each channel's scope and model groups on refresh", async () => {
    const stableSummary = create(ReleaseChannelSummarySchema, { id: 2n, name: "Stable" });
    const stable = create(ReleaseChannelSchema, { id: 2n, name: "Stable", scope: { rackIds: [80n] } });
    const stableGroup = create(ReleaseChannelModelGroupSchema, {
      manufacturer: "Proto",
      model: "Rig 2",
      minerCount: 5,
    });
    mockListReleaseChannels.mockImplementation(({ cursor = "" }) =>
      Promise.resolve(
        create(ListReleaseChannelsResponseSchema, {
          channels: cursor === "" ? [canarySummary] : [stableSummary],
          cursor: cursor === "" ? "page-2" : "",
        }),
      ),
    );
    mockGetReleaseChannel.mockImplementation(({ channelId }) =>
      Promise.resolve({ channel: channelId === 1n ? canary : stable }),
    );
    mockListReleaseChannelModelGroups.mockImplementation(({ channelId }) =>
      Promise.resolve(
        create(ListReleaseChannelModelGroupsResponseSchema, {
          modelGroups: channelId === 1n ? [rigGroup] : [stableGroup],
        }),
      ),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    const expectedViews = [canaryView, { ...stable, modelGroups: [stableGroup] }];
    expect(result.current.channels).toEqual(expectedViews);
    expect(mockListReleaseChannels).toHaveBeenNthCalledWith(1, { pageSize: 1000, cursor: "" });
    expect(mockListReleaseChannels).toHaveBeenNthCalledWith(2, { pageSize: 1000, cursor: "page-2" });
    expect(mockGetReleaseChannel).toHaveBeenCalledWith({ channelId: 2n });
    expect(mockListReleaseChannelModelGroups).toHaveBeenCalledWith({ channelId: 2n, pageSize: 100, cursor: "" });

    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.channels).toEqual(expectedViews);
    expect(mockListReleaseChannels).toHaveBeenCalledTimes(4);
    expect(mockListReleaseChannels).toHaveBeenNthCalledWith(3, { pageSize: 1000, cursor: "" });
    expect(mockListReleaseChannels).toHaveBeenNthCalledWith(4, { pageSize: 1000, cursor: "page-2" });
  });

  it("creates a channel from a draft and refreshes", async () => {
    mockCreateReleaseChannel.mockResolvedValue({ channel: canary });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    const draft = {
      name: "Canary",
      description: "First wave",
      scope: create(ReleaseChannelScopeSchema, { rackIds: [40n] }),
      behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.PILOT_THEN_CONTINUE, pilotSize: 2 }),
    };
    let created;
    await act(async () => {
      created = await result.current.createChannel(draft);
    });
    expect(created).toEqual(canaryView);
    expect(mockCreateReleaseChannel).toHaveBeenCalledWith(draft);
    expect(mockListReleaseChannels).toHaveBeenCalledTimes(2);
  });

  it("creates an empty channel without sending inactive form defaults or changing the draft", async () => {
    mockCreateReleaseChannel.mockResolvedValue({ channel: canary });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    const draft = {
      name: "Empty channel",
      description: "",
      scope: create(ReleaseChannelScopeSchema),
      behavior: defaultBehavior(),
    };
    const draftBeforeSave = toJson(CreateReleaseChannelRequestSchema, create(CreateReleaseChannelRequestSchema, draft));

    await act(async () => {
      await result.current.createChannel(draft);
    });

    const request = create(CreateReleaseChannelRequestSchema, mockCreateReleaseChannel.mock.calls[0][0]);
    expect(toJson(CreateReleaseChannelRequestSchema, request)).toEqual({
      name: "Empty channel",
      scope: {},
      behavior: { method: "ROLLOUT_METHOD_ALL_AT_ONCE", order: "ROLLOUT_ORDER_LEAST_EFFICIENT_FIRST" },
    });
    expect(toJson(CreateReleaseChannelRequestSchema, create(CreateReleaseChannelRequestSchema, draft))).toEqual(
      draftBeforeSave,
    );
  });

  it("omits present but empty thresholds when saving a fetched all-at-once channel", async () => {
    const fetchedChannel = create(ReleaseChannelSchema, {
      ...canary,
      behavior: create(RolloutBehaviorSchema, { method: RolloutMethod.ALL_AT_ONCE, thresholds: {} }),
    });
    mockGetReleaseChannel.mockResolvedValue({ channel: fetchedChannel });
    mockUpdateReleaseChannel.mockResolvedValue({ channel: fetchedChannel });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    const loadedChannel = result.current.channels[0];

    await act(async () => {
      await result.current.updateChannel(loadedChannel.id, {
        name: loadedChannel.name,
        description: loadedChannel.description,
        scope: create(ReleaseChannelScopeSchema, loadedChannel.scope),
        behavior: create(RolloutBehaviorSchema, loadedChannel.behavior),
      });
    });

    const request = create(UpdateReleaseChannelRequestSchema, mockUpdateReleaseChannel.mock.calls[0][0]);
    expect(toJson(UpdateReleaseChannelRequestSchema, request)).toEqual({
      channelId: "1",
      name: "Canary",
      scope: { rackIds: ["40"] },
      behavior: { method: "ROLLOUT_METHOD_ALL_AT_ONCE" },
    });
    expect(loadedChannel.behavior?.thresholds).toBeDefined();
  });

  it.each([
    {
      name: "all at once",
      changes: { method: RolloutMethod.ALL_AT_ONCE },
      expectedBehavior: {
        method: "ROLLOUT_METHOD_ALL_AT_ONCE",
        order: "ROLLOUT_ORDER_RANDOM",
        maxConcurrentOffline: 4,
      },
    },
    {
      name: "batches without review",
      changes: { reviewAfterEachBatch: false, waitBetweenBatchesSeconds: 90 },
      expectedBehavior: {
        method: "ROLLOUT_METHOD_BATCHED",
        order: "ROLLOUT_ORDER_RANDOM",
        batchSize: 7,
        waitBetweenBatchesSeconds: 90,
        maxConcurrentOffline: 4,
      },
    },
    {
      name: "reviewed batches without automation",
      changes: { autoContinueOnHealthyTelemetry: false },
      expectedBehavior: {
        method: "ROLLOUT_METHOD_BATCHED",
        order: "ROLLOUT_ORDER_RANDOM",
        batchSize: 7,
        reviewAfterEachBatch: true,
        maxConcurrentOffline: 4,
      },
    },
  ])(
    "clears inactive settings when switching reviewed automatic batches to $name",
    async ({ changes, expectedBehavior }) => {
      const fetchedChannel = create(ReleaseChannelSchema, {
        ...canary,
        behavior: create(RolloutBehaviorSchema, {
          method: RolloutMethod.BATCHED,
          order: RolloutOrder.RANDOM,
          batchSize: 7,
          reviewAfterEachBatch: true,
          autoContinueOnHealthyTelemetry: true,
          stabilizationSeconds: 600,
          thresholds: { maxHashrateDropPercent: 10, maxNewErrors: 0 },
          maxConcurrentOffline: 4,
        }),
      });
      mockGetReleaseChannel.mockResolvedValue({ channel: fetchedChannel });
      mockUpdateReleaseChannel.mockResolvedValue({ channel: fetchedChannel });
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      const loadedChannel = result.current.channels[0];
      const draft = {
        name: loadedChannel.name,
        description: loadedChannel.description,
        scope: create(ReleaseChannelScopeSchema, loadedChannel.scope),
        behavior: create(RolloutBehaviorSchema, {
          ...create(RolloutBehaviorSchema, loadedChannel.behavior),
          ...changes,
        }),
      };
      const draftBeforeSave = toJson(RolloutBehaviorSchema, draft.behavior);

      await act(async () => {
        await result.current.updateChannel(loadedChannel.id, draft);
      });

      const request = create(UpdateReleaseChannelRequestSchema, mockUpdateReleaseChannel.mock.calls[0][0]);
      expect(toJson(UpdateReleaseChannelRequestSchema, request)).toEqual({
        channelId: "1",
        name: "Canary",
        scope: { rackIds: ["40"] },
        behavior: expectedBehavior,
      });
      expect(toJson(RolloutBehaviorSchema, draft.behavior)).toEqual(draftBeforeSave);
    },
  );

  it("updates and deletes by channel id", async () => {
    mockUpdateReleaseChannel.mockResolvedValue({ channel: canary });
    mockDeleteReleaseChannel.mockResolvedValue({});
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      await result.current.updateChannel(1n, { name: "Canary 2", description: "", scope: {}, behavior: {} } as never);
      await result.current.deleteChannel(1n);
    });
    expect(mockUpdateReleaseChannel).toHaveBeenCalledWith(expect.objectContaining({ channelId: 1n, name: "Canary 2" }));
    expect(mockDeleteReleaseChannel).toHaveBeenCalledWith({ channelId: 1n });
  });

  it("previews a scope without touching the polled state", async () => {
    mockPreviewReleaseChannelScope.mockResolvedValue({ minerCount: 4, models: [], conflicts: [] });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    const listCalls = mockListReleaseChannels.mock.calls.length;

    const preview = await result.current.previewScope({ siteIds: [1n] } as never, 7n);
    expect(preview.minerCount).toBe(4);
    expect(mockPreviewReleaseChannelScope).toHaveBeenCalledWith({ scope: { siteIds: [1n] }, channelId: 7n });
    expect(mockListReleaseChannels).toHaveBeenCalledTimes(listCalls);
  });

  it("applies firmware, cancels and retries by id", async () => {
    mockApplyReleaseChannelFirmware.mockResolvedValue({ startedRollouts: [rollout] });
    mockCancelRollout.mockResolvedValue({ rollout });
    mockRetryFailedRolloutDevices.mockResolvedValue({ rollout });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      const started = await result.current.applyFirmware(1n, [
        { manufacturer: "Proto", model: "Rig", firmwareFileId: "fw-2" },
      ]);
      expect(started).toEqual([rollout]);
      await result.current.cancelRollout(9n);
      const retried = await result.current.retryFailedDevices(9n);
      expect(retried).toEqual(rollout);
    });
    expect(mockApplyReleaseChannelFirmware).toHaveBeenCalledWith({
      channelId: 1n,
      assignments: [{ manufacturer: "Proto", model: "Rig", firmwareFileId: "fw-2" }],
    });
    expect(mockCancelRollout).toHaveBeenCalledWith({ rolloutId: 9n });
    expect(mockRetryFailedRolloutDevices).toHaveBeenCalledWith({ rolloutId: 9n });
  });

  it("walks every page of the detail lists", async () => {
    mockListReleaseChannelMiners
      .mockResolvedValueOnce({ miners: [{ deviceIdentifier: "rig-001" }], cursor: "p2" })
      .mockResolvedValueOnce({ miners: [{ deviceIdentifier: "rig-002" }], cursor: "" });
    mockListRolloutDevices
      .mockResolvedValueOnce({ devices: [{ deviceIdentifier: "rig-001" }], cursor: "d2" })
      .mockResolvedValueOnce({ devices: [{ deviceIdentifier: "rig-002" }], cursor: "d3" })
      .mockResolvedValueOnce({ devices: [], cursor: "" });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      const miners = await result.current.listChannelMiners(1n, "Proto", "Rig");
      expect(miners.map((m) => m.deviceIdentifier)).toEqual(["rig-001", "rig-002"]);
      const devices = await result.current.listRolloutDevices(9n);
      expect(devices.map((d) => d.deviceIdentifier)).toEqual(["rig-001", "rig-002"]);
    });
    expect(mockListReleaseChannelMiners).toHaveBeenNthCalledWith(1, {
      channelId: 1n,
      manufacturer: "Proto",
      model: "Rig",
      pageSize: 1000,
      cursor: "",
    });
    expect(mockListReleaseChannelMiners).toHaveBeenNthCalledWith(2, {
      channelId: 1n,
      manufacturer: "Proto",
      model: "Rig",
      pageSize: 1000,
      cursor: "p2",
    });
    expect(mockListRolloutDevices).toHaveBeenCalledTimes(3);
    expect(mockListRolloutDevices).toHaveBeenLastCalledWith({ rolloutId: 9n, pageSize: 1000, cursor: "d3" });
  });
});
