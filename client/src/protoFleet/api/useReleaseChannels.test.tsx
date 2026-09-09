import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";

import {
  CreateReleaseChannelRequestSchema,
  ListReleaseChannelModelGroupsResponseSchema,
  ListReleaseChannelsResponseSchema,
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
    });
  });

  it("loads channels with their scope and paged groups, rollouts and miner names together", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    expect(result.current.isLoading).toBe(true);

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.channels).toEqual([canaryView]);
    expect(mockGetReleaseChannel).toHaveBeenCalledWith({ channelId: 1n });
    expect(mockListReleaseChannelModelGroups).toHaveBeenCalledWith({ channelId: 1n, pageSize: 100, cursor: "" });
    expect(result.current.rollouts).toEqual([rollout]);
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01" });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledWith({ pageSize: 500 });
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
