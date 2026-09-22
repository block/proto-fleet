import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create, toJson } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import {
  CancelRolloutRequestSchema,
  ContinueRolloutRequestSchema,
  CreateReleaseChannelRequestSchema,
  ListReleaseChannelModelGroupsResponseSchema,
  ListReleaseChannelsResponseSchema,
  type ListRolloutsResponse,
  ListRolloutsResponseSchema,
  PauseRolloutRequestSchema,
  PreviewReleaseChannelScopeResponseSchema,
  ReleaseChannelMinerSchema,
  ReleaseChannelModelGroupSchema,
  ReleaseChannelSchema,
  ReleaseChannelScopeSchema,
  ReleaseChannelSummarySchema,
  ResumeRolloutRequestSchema,
  RetryFailedRolloutDevicesRequestSchema,
  RollbackReleaseChannelFirmwareRequestSchema,
  RolloutBehaviorSchema,
  RolloutCancelReason,
  RolloutDeviceSchema,
  RolloutMethod,
  RolloutOrder,
  RolloutSchema,
  RolloutService,
  RolloutState,
  RolloutStatus,
  UpdateReleaseChannelRequestSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { type ReleaseChannelsApi, useReleaseChannels } from "@/protoFleet/api/useReleaseChannels";
import { defaultBehavior } from "@/protoFleet/features/settings/components/ReleaseChannels/behaviorUtils";
import ModelMinersModal from "@/protoFleet/features/settings/components/ReleaseChannels/ModelMinersModal";

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
  mockContinueRollout,
  mockPauseRollout,
  mockResumeRollout,
  mockRollbackReleaseChannelFirmware,
  mockRetryFailedRolloutDevices,
  mockListReleaseChannelMiners,
  mockListRolloutDevices,
  mockHandleAuthErrors,
  mockAuth,
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
  mockContinueRollout: vi.fn(),
  mockPauseRollout: vi.fn(),
  mockResumeRollout: vi.fn(),
  mockRollbackReleaseChannelFirmware: vi.fn(),
  mockRetryFailedRolloutDevices: vi.fn(),
  mockListReleaseChannelMiners: vi.fn(),
  mockListRolloutDevices: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockAuth: { sessionGeneration: 1, isAuthenticated: true, username: "operator" },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mockHandleAuthErrors }),
  useSessionGeneration: () => mockAuth.sessionGeneration,
  useIsAuthenticated: () => mockAuth.isAuthenticated,
  useUsername: () => mockAuth.username,
  useFleetStore: { getState: () => ({ auth: mockAuth }) },
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
    continueRollout: mockContinueRollout,
    pauseRollout: mockPauseRollout,
    resumeRollout: mockResumeRollout,
    rollbackReleaseChannelFirmware: mockRollbackReleaseChannelFirmware,
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
  channelName: "Canary",
  manufacturer: "Proto",
  model: "Rig",
  status: RolloutStatus.ACTIVE,
});
const rollbackSource = create(RolloutSchema, {
  ...rollout,
  revision: 7n,
  assignmentGeneration: 4n,
  firmwareChecksum: "b".repeat(64),
  firmwareVersion: "2.0.0",
  previousFirmwareChecksum: "a".repeat(64),
  previousFirmwareVersion: "1.0.0",
  createdAt: create(TimestampSchema, { seconds: 100n }),
  updatedAt: create(TimestampSchema, { seconds: 200n }),
});
const rollbackGroup = create(ReleaseChannelModelGroupSchema, {
  ...rigGroup,
  assignmentGeneration: rollbackSource.assignmentGeneration,
  activeRolloutId: rollbackSource.id,
  firmwareChecksum: rollbackSource.firmwareChecksum,
  firmwareVersion: rollbackSource.firmwareVersion,
  onTargetCount: 1,
});
const rollbackSuccessor = create(RolloutSchema, {
  ...rollbackSource,
  id: 10n,
  revision: 1n,
  assignmentGeneration: 5n,
  firmwareChecksum: rollbackSource.previousFirmwareChecksum,
  firmwareVersion: rollbackSource.previousFirmwareVersion,
  previousFirmwareChecksum: rollbackSource.firmwareChecksum,
  previousFirmwareVersion: rollbackSource.firmwareVersion,
  createdAt: create(TimestampSchema, { seconds: 300n }),
  updatedAt: create(TimestampSchema, { seconds: 300n }),
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

const pollOptions = { timeoutMs: 30_000, signal: expect.any(AbortSignal) };
const channelLoadOptions = pollOptions;

function expireMinerNames() {
  const now = Date.now();
  vi.spyOn(Date, "now").mockReturnValue(now + 5 * 60 * 1000 + 1);
}

function stalledRolloutTransport(firstPage?: ListRolloutsResponse | Response) {
  const signals: AbortSignal[] = [];
  const client = createClient(
    RolloutService,
    createConnectTransport({
      baseUrl: "https://fleet.invalid",
      fetch: async (_input, init) => {
        if (firstPage) {
          const page = firstPage;
          firstPage = undefined;
          if (page instanceof Response) return page;
          return new Response(JSON.stringify(toJson(ListRolloutsResponseSchema, page)), {
            headers: { "Content-Type": "application/json" },
          });
        }
        const signal = init?.signal;
        if (!signal) throw new Error("Expected the transport's AbortSignal");
        signals.push(signal);
        return new Promise<Response>((_resolve, reject) => {
          const abort = () => reject(signal.reason);
          if (signal.aborted) abort();
          else signal.addEventListener("abort", abort, { once: true });
        });
      },
    }),
  );
  return { client, signals };
}

const revisionActions = [
  ["rollbackFirmware", mockRollbackReleaseChannelFirmware, RollbackReleaseChannelFirmwareRequestSchema],
  ["continueRollout", mockContinueRollout, ContinueRolloutRequestSchema],
  ["pauseRollout", mockPauseRollout, PauseRolloutRequestSchema],
  ["resumeRollout", mockResumeRollout, ResumeRolloutRequestSchema],
  ["cancelRollout", mockCancelRollout, CancelRolloutRequestSchema],
  ["retryFailedDevices", mockRetryFailedRolloutDevices, RetryFailedRolloutDevicesRequestSchema],
] as const;

function callRevisionAction(
  api: ReleaseChannelsApi,
  action: (typeof revisionActions)[number][0],
  source: typeof rollout,
) {
  return action === "rollbackFirmware" ? api.rollbackFirmware(source) : api[action](source.id, source.revision);
}

const mutationDraft = {
  name: "Canary",
  description: "",
  scope: create(ReleaseChannelScopeSchema),
  behavior: defaultBehavior(),
};
const startedRollouts = [rollout];
const mutationCases: {
  name: string;
  rpc: typeof mockCreateReleaseChannel;
  call: (api: ReleaseChannelsApi) => Promise<unknown>;
  response: object;
  expected: unknown;
}[] = [
  {
    name: "create",
    rpc: mockCreateReleaseChannel,
    call: (api) => api.createChannel(mutationDraft),
    response: { channel: canary },
    expected: canary,
  },
  {
    name: "update",
    rpc: mockUpdateReleaseChannel,
    call: (api) => api.updateChannel(1n, mutationDraft),
    response: { channel: canary },
    expected: canary,
  },
  {
    name: "delete",
    rpc: mockDeleteReleaseChannel,
    call: (api) => api.deleteChannel(1n),
    response: {},
    expected: undefined,
  },
  {
    name: "apply",
    rpc: mockApplyReleaseChannelFirmware,
    call: (api) => api.applyFirmware(1n, [{ manufacturer: "Proto", model: "Rig", firmwareFileId: "fw-2" }]),
    response: { startedRollouts },
    expected: startedRollouts,
  },
  {
    name: "rollback",
    rpc: mockRollbackReleaseChannelFirmware,
    call: (api) => api.rollbackFirmware(create(RolloutSchema, { ...rollout, revision: 1n })),
    response: { startedRollouts },
    expected: startedRollouts,
  },
  {
    name: "continue",
    rpc: mockContinueRollout,
    call: (api) => api.continueRollout(9n, 1n),
    response: {},
    expected: undefined,
  },
  { name: "pause", rpc: mockPauseRollout, call: (api) => api.pauseRollout(9n, 1n), response: {}, expected: undefined },
  {
    name: "resume",
    rpc: mockResumeRollout,
    call: (api) => api.resumeRollout(9n, 1n),
    response: {},
    expected: undefined,
  },
  {
    name: "cancel",
    rpc: mockCancelRollout,
    call: (api) => api.cancelRollout(9n, 1n),
    response: {},
    expected: undefined,
  },
  {
    name: "retry",
    rpc: mockRetryFailedRolloutDevices,
    call: (api) => api.retryFailedDevices(9n, 1n),
    response: { rollout },
    expected: rollout,
  },
];

const paginatedActionCases = [
  {
    name: "channel miners",
    rpc: mockListReleaseChannelMiners,
    call: (api: ReleaseChannelsApi, signal?: AbortSignal) => api.listChannelMiners(1n, "Proto", "Rig", signal),
    firstPage: { miners: [], cursor: "details-2" },
  },
  {
    name: "rollout devices",
    rpc: mockListRolloutDevices,
    call: (api: ReleaseChannelsApi, signal?: AbortSignal) => api.listRolloutDevices(9n, signal),
    firstPage: { devices: [], cursor: "details-2" },
  },
];

describe("useReleaseChannels", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuth.sessionGeneration = 1;
    mockAuth.isAuthenticated = true;
    mockAuth.username = "operator";
    mockListReleaseChannels.mockResolvedValue(create(ListReleaseChannelsResponseSchema, { channels: [canarySummary] }));
    mockGetReleaseChannel.mockResolvedValue({ channel: canary });
    mockCreateReleaseChannel.mockResolvedValue({ channel: canary });
    mockUpdateReleaseChannel.mockResolvedValue({ channel: canary });
    mockListReleaseChannelMiners.mockResolvedValue({ miners: [], cursor: "" });
    mockListRolloutDevices.mockResolvedValue({ devices: [], cursor: "" });
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
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it.each(
    (["delta", "active", "channel list", "miner names"] as const).flatMap((stage) =>
      (["unmount", "new login", "new login before rerender"] as const).map((change) => ({ stage, change })),
    ),
  )("stops $stage pagination after $change", async ({ stage, change }) => {
    capturePollingTimer();
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "saved" }),
    );
    const { result, rerender, unmount } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    if (stage === "miner names") expireMinerNames();
    const rpc =
      stage === "channel list"
        ? mockListReleaseChannels
        : stage === "miner names"
          ? mockListMinerStateSnapshots
          : mockListRollouts;
    const page = deferred<object>();
    const original = rpc.getMockImplementation()!;
    let signal: AbortSignal | undefined;
    rpc.mockImplementation((request, options) => {
      const matches =
        stage === "delta"
          ? request.status !== RolloutStatus.ACTIVE
          : stage === "active"
            ? request.status === RolloutStatus.ACTIVE
            : true;
      if (matches && !signal) {
        signal = options.signal;
        return page.promise;
      }
      return original(request, options);
    });
    let refresh!: Promise<void>;
    await act(async () => {
      refresh = result.current.refresh();
    });
    expect(signal).toBeInstanceOf(AbortSignal);
    if (change === "unmount") unmount();
    else {
      mockAuth.sessionGeneration += 1;
      if (change === "new login") {
        rerender();
        await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      }
    }
    if (change !== "new login before rerender") expect(signal?.aborted).toBe(true);
    await act(async () => {
      page.resolve({
        cursor: "must-not-load",
        channels: [canarySummary],
        rollouts: [rollout],
        pollCursor: "must-not-commit",
        miners: [{ deviceIdentifier: "old-login", name: "Stale name" }],
      });
      await refresh;
    });
    expect(signal?.aborted).toBe(true);
    expect(rpc.mock.calls.some(([request]) => request.cursor === "must-not-load")).toBe(false);
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    if (change === "new login before rerender") {
      rerender();
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    }
    if (change !== "unmount") {
      expect(result.current.channels).toEqual([canaryView]);
      expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01" });
      expect(result.current.error).toBeNull();
    }
  });

  it.each(["delta", "active"] as const)(
    "aborts and drains core siblings after a %s failure without abandoning cached names",
    async (failedRead) => {
      const poll = capturePollingTimer();
      mockListRollouts.mockResolvedValue(
        create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "saved" }),
      );
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const previous = {
        channels: result.current.channels,
        rollouts: result.current.rollouts,
        names: result.current.minerNames,
      };
      expireMinerNames();
      const delta = deferred<ListRolloutsResponse>();
      const active = deferred<ListRolloutsResponse>();
      const names = deferred<{ miners: { deviceIdentifier: string; name: string }[]; cursor: string }>();
      const coreSignals: AbortSignal[] = [];
      let namesSignal: AbortSignal | undefined;
      mockListRollouts.mockImplementation(({ status }, { signal }) => {
        coreSignals.push(signal);
        return status === RolloutStatus.ACTIVE ? active.promise : delta.promise;
      });
      mockListMinerStateSnapshots.mockImplementationOnce((_request, { signal }) => {
        namesSignal = signal;
        return names.promise;
      });
      const failure = new ConnectError("polling read failed", Code.Unavailable);
      let rejection!: Promise<void>;
      await act(async () => {
        rejection = expect(result.current.refresh()).rejects.toBe(failure);
      });
      expect(coreSignals).toHaveLength(2);
      await act(async () => {
        if (failedRead === "delta") delta.reject(failure);
        else active.reject(failure);
      });
      expect(coreSignals.every((signal) => signal.aborted)).toBe(true);
      expect(namesSignal?.aborted).toBe(false);
      expect(result.current.error).toBeNull();
      const calls = mockListRollouts.mock.calls.length;
      await act(async () => poll());
      expect(mockListRollouts).toHaveBeenCalledTimes(calls);
      await act(async () => {
        const latePage = create(ListRolloutsResponseSchema, {
          rollouts: [rollout],
          cursor: "must-not-load",
          pollCursor: "must-not-commit",
        });
        delta.resolve(latePage);
        active.resolve(latePage);
        await rejection;
      });
      expect(result.current.error).toBe(failure);
      expect(result.current.channels).toBe(previous.channels);
      expect(result.current.rollouts).toBe(previous.rollouts);
      expect(result.current.minerNames).toBe(previous.names);
      expect(mockHandleAuthErrors).toHaveBeenCalledExactlyOnceWith({ error: failure });
      expect(mockListRollouts.mock.calls.some(([request]) => request.cursor === "must-not-load")).toBe(false);
      await act(async () => {
        names.resolve({ miners: [{ deviceIdentifier: "rig-001", name: "Fresh name" }], cursor: "" });
      });
      mockListRollouts.mockResolvedValue(
        create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "next" }),
      );
      await act(async () => {
        await result.current.refresh();
      });
      expect(mockListRollouts).toHaveBeenCalledWith({ pageSize: 1000, cursor: "", pollCursor: "saved" }, pollOptions);
      expect(result.current.error).toBeNull();
      expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
      expect(result.current.minerNames).toEqual({ "rig-001": "Fresh name" });
    },
  );

  it("aborts an initial polling request through Connect on unmount without waiting for its deadline", async () => {
    const stalled = stalledRolloutTransport();
    mockListRollouts.mockImplementation(stalled.client.listRollouts);
    const { unmount } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(stalled.signals).toHaveLength(1));
    await act(async () => unmount());
    expect(stalled.signals[0].aborted).toBe(true);
    expect(mockListReleaseChannels).not.toHaveBeenCalled();
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
  });

  it("ignores an abandoned names scan's late authentication failure after a new login", async () => {
    const names = deferred<never>();
    let oldSignal: AbortSignal | undefined;
    mockListMinerStateSnapshots.mockImplementationOnce((_request, { signal }) => {
      oldSignal = signal;
      return names.promise;
    });
    const { result, rerender } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(oldSignal).toBeInstanceOf(AbortSignal));
    mockAuth.sessionGeneration += 1;
    rerender();
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    expect(oldSignal?.aborted).toBe(true);
    await act(async () => names.reject(new ConnectError("old login expired", Code.Unauthenticated)));
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01" });
    expect(result.current.error).toBeNull();
  });

  it("bounds channel hydration across summary pages, retains paged groups, and preserves list order", async () => {
    capturePollingTimer();
    const channels = Array.from({ length: 9 }, (_, index) =>
      create(ReleaseChannelSchema, { id: BigInt(index + 1), name: `Channel ${index + 1}` }),
    );
    mockListReleaseChannels.mockImplementation(({ cursor }) =>
      Promise.resolve({ channels: cursor ? channels.slice(5) : channels.slice(0, 5), cursor: cursor ? "" : "next" }),
    );
    const details = new Map<bigint, ReturnType<typeof deferred<{ channel: typeof canary }>>>();
    const groups = new Map<bigint, ReturnType<typeof deferred<{ modelGroups: (typeof rigGroup)[]; cursor: string }>>>();
    let active = 0;
    let maximum = 0;
    const track = <T,>(request: Promise<T>) => {
      active += 1;
      maximum = Math.max(maximum, active);
      return request.finally(() => {
        active -= 1;
      });
    };
    mockGetReleaseChannel.mockImplementation(({ channelId }) => {
      const pending = deferred<{ channel: typeof canary }>();
      details.set(channelId, pending);
      return track(pending.promise);
    });
    mockListReleaseChannelModelGroups.mockImplementation(({ channelId, cursor }) => {
      if (!cursor) return track(Promise.resolve({ modelGroups: [rigGroup], cursor: "groups-2" }));
      const pending = deferred<{ modelGroups: (typeof rigGroup)[]; cursor: string }>();
      groups.set(channelId, pending);
      return track(pending.promise);
    });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(groups.size).toBe(4));
    expect(details.size).toBe(4);
    expect(maximum).toBeLessThanOrEqual(8);
    await act(async () => details.get(2n)!.resolve({ channel: channels[1] }));
    expect(details.size).toBe(4); // Its paginated groups still own the worker slot.
    await act(async () => groups.get(2n)!.resolve({ modelGroups: [rigGroup], cursor: "" }));
    expect(details.size).toBe(5);
    expect(result.current.channels).toEqual([]);
    expect(result.current.hasLoaded).toBe(false);
    for (let pass = 0; pass < 4 && !result.current.hasLoaded; pass += 1) {
      await act(async () => {
        for (const [id, pending] of details) pending.resolve({ channel: channels[Number(id) - 1] });
        for (const pending of groups.values()) pending.resolve({ modelGroups: [rigGroup], cursor: "" });
      });
    }
    expect(result.current.hasLoaded).toBe(true);
    expect(result.current.channels.map(({ id }) => id)).toEqual(channels.map(({ id }) => id));
    expect(result.current.channels.every(({ modelGroups }) => modelGroups.length === 2)).toBe(true);
    expect(maximum).toBeLessThanOrEqual(8);
    expect(active).toBe(0);
    expect(mockListReleaseChannels.mock.calls.map(([request]) => request.cursor)).toEqual(["", "next"]);
  });

  it.each(["channel detail", "model groups"])(
    "drains a failed %s read before retrying and preserves the snapshot and watermark",
    async (failedRead) => {
      const poll = capturePollingTimer();
      mockListRollouts.mockResolvedValue(
        create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "saved" }),
      );
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const previous = { channels: result.current.channels, rollouts: result.current.rollouts };
      const channels = Array.from({ length: 9 }, (_, index) =>
        create(ReleaseChannelSummarySchema, { id: BigInt(index + 1) }),
      );
      mockListReleaseChannels.mockResolvedValue({ channels, cursor: "" });
      mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { pollCursor: "uncommitted" }));
      const details = Array.from({ length: 4 }, () => deferred<{ channel: typeof canary }>());
      const groups = Array.from({ length: 4 }, () => deferred<{ modelGroups: (typeof rigGroup)[]; cursor: string }>());
      const signals: AbortSignal[] = [];
      mockGetReleaseChannel.mockImplementation(({ channelId }, { signal }) => {
        signals.push(signal);
        return details[Number(channelId) - 1].promise;
      });
      mockListReleaseChannelModelGroups.mockImplementation(({ channelId }, { signal }) => {
        signals.push(signal);
        return groups[Number(channelId) - 1].promise;
      });
      const failure = new ConnectError("channel detail failed", Code.Unavailable);
      let rejected!: Promise<void>;
      await act(async () => {
        rejected = expect(result.current.refresh()).rejects.toBe(failure);
      });
      expect(signals).toHaveLength(8);
      await act(async () => {
        if (failedRead === "channel detail") details[0].reject(failure);
        else groups[0].reject(failure);
      });
      expect(signals.every((signal) => signal.aborted)).toBe(true);
      const polls = mockListRollouts.mock.calls.length;
      await act(async () => poll());
      expect(mockListRollouts).toHaveBeenCalledTimes(polls);
      expect(result.current.error).toBeNull();
      await act(async () => {
        for (const pending of details) pending.resolve({ channel: canary });
        for (const pending of groups) pending.resolve({ modelGroups: [], cursor: "must-not-load" });
        await rejected;
      });
      expect(signals).toHaveLength(8);
      expect(result.current.error).toBe(failure);
      expect(result.current.channels).toBe(previous.channels);
      expect(result.current.rollouts).toBe(previous.rollouts);
      expect(mockHandleAuthErrors).toHaveBeenCalledExactlyOnceWith({ error: failure });
      mockListReleaseChannels.mockResolvedValue({ channels: [canarySummary], cursor: "" });
      mockGetReleaseChannel.mockResolvedValue({ channel: canary });
      mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [rigGroup], cursor: "" });
      await act(async () => {
        await result.current.refresh();
      });
      expect(mockListRollouts).toHaveBeenCalledWith({ pageSize: 1000, cursor: "", pollCursor: "saved" }, pollOptions);
      expect(result.current.error).toBeNull();
      expect(result.current.channels).toEqual([canaryView]);
    },
  );

  it.each(["unmount", "new login"])(
    "cancels queued channel work after %s and ignores a late authentication error",
    async (change) => {
      capturePollingTimer();
      const channels = Array.from({ length: 9 }, (_, index) =>
        create(ReleaseChannelSummarySchema, { id: BigInt(index + 1) }),
      );
      mockListReleaseChannels.mockResolvedValue({ channels, cursor: "" });
      const details = Array.from({ length: 4 }, () => deferred<{ channel: typeof canary }>());
      const groups = Array.from({ length: 4 }, () => deferred<{ modelGroups: (typeof rigGroup)[]; cursor: string }>());
      const signals: AbortSignal[] = [];
      mockGetReleaseChannel.mockImplementation(({ channelId }, { signal }) => {
        signals.push(signal);
        return details[Number(channelId) - 1].promise;
      });
      mockListReleaseChannelModelGroups.mockImplementation(({ channelId }, { signal }) => {
        signals.push(signal);
        return groups[Number(channelId) - 1].promise;
      });
      const { result, rerender, unmount } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(signals).toHaveLength(8));
      if (change === "unmount") unmount();
      else {
        mockListReleaseChannels.mockResolvedValue({ channels: [canarySummary], cursor: "" });
        mockGetReleaseChannel.mockResolvedValue({ channel: canary });
        mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [rigGroup], cursor: "" });
        mockAuth.sessionGeneration += 1;
        rerender();
        await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      }
      expect(signals.every((signal) => signal.aborted)).toBe(true);
      await act(async () => {
        details[0].reject(new ConnectError("old login expired", Code.Unauthenticated));
        for (const pending of details) pending.resolve({ channel: canary });
        for (const pending of groups) pending.resolve({ modelGroups: [], cursor: "must-not-load" });
      });
      expect(mockGetReleaseChannel).toHaveBeenCalledTimes(change === "unmount" ? 4 : 5);
      expect(mockListReleaseChannelModelGroups).toHaveBeenCalledTimes(change === "unmount" ? 4 : 5);
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
      if (change === "new login") expect(result.current.channels).toEqual([canaryView]);
    },
  );

  it("times out a stalled delta page through Connect, preserves its watermark, and recovers on the next poll", async () => {
    const poll = capturePollingTimer();
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "saved-token" }),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    const previous = {
      channels: result.current.channels,
      rollouts: result.current.rollouts,
      names: result.current.minerNames,
    };
    const changed = create(RolloutSchema, { ...rollout, revision: 2n });
    const stalled = stalledRolloutTransport(
      create(ListRolloutsResponseSchema, {
        rollouts: [changed],
        cursor: "stalled-page",
        pollCursor: "uncommitted-token",
      }),
    );
    mockListRollouts.mockImplementation((request, options) =>
      request.status === RolloutStatus.ACTIVE
        ? Promise.resolve(create(ListRolloutsResponseSchema))
        : stalled.client.listRollouts(request, options),
    );
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    await act(async () => {
      poll();
    });
    expect(stalled.signals).toHaveLength(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(29_999);
    });
    expect(result.current.error).toBeNull();
    expect(stalled.signals[0].aborted).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(stalled.signals[0].aborted).toBe(true);
    expect(result.current.error).toMatchObject({ code: Code.DeadlineExceeded });
    expect(result.current.hasLoaded).toBe(true);
    expect(result.current.channels).toBe(previous.channels);
    expect(result.current.rollouts).toBe(previous.rollouts);
    expect(result.current.minerNames).toBe(previous.names);
    mockListRollouts.mockImplementation(({ status }) =>
      Promise.resolve(
        create(ListRolloutsResponseSchema, {
          rollouts: status === RolloutStatus.ACTIVE ? [] : [changed],
          pollCursor: "recovered-token",
        }),
      ),
    );
    await act(async () => {
      poll();
    });
    expect(result.current.error).toBeNull();
    expect(result.current.rollouts).toEqual([changed]);
    const deltaCalls = mockListRollouts.mock.calls.filter(([request]) => request.status === undefined);
    expect(deltaCalls[deltaCalls.length - 1]?.[0].pollCursor).toBe("saved-token");
  });

  it("releases a committed mutation after timed-out polling and refresh without resending the write", async () => {
    const poll = capturePollingTimer();
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    const stalled = stalledRolloutTransport();
    mockListRollouts.mockImplementation(stalled.client.listRollouts);
    mockCancelRollout.mockResolvedValue({});
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    await act(async () => {
      poll();
    });
    let completed = false;
    let mutation!: Promise<void>;
    await act(async () => {
      mutation = result.current.cancelRollout(9n, 1n).then(() => {
        completed = true;
      });
    });
    expect(stalled.signals).toHaveLength(1);
    expect(completed).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(stalled.signals[0].aborted).toBe(true);
    expect(stalled.signals).toHaveLength(2);
    expect(completed).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
      await mutation;
    });
    expect(completed).toBe(true);
    expect(stalled.signals.every((signal) => signal.aborted)).toBe(true);
    expect(mockCancelRollout).toHaveBeenCalledExactlyOnceWith({ rolloutId: 9n, expectedRevision: 1n });
    expect(result.current.error).toMatchObject({ code: Code.DeadlineExceeded });
    expect(result.current.hasLoaded).toBe(true);
    expect(result.current.rollouts).toEqual([rollout]);
  });

  it("bounds every page of the snapshot reads, including active rollouts and channel details", async () => {
    mockListRollouts.mockImplementation(({ cursor }) =>
      Promise.resolve(
        create(ListRolloutsResponseSchema, {
          rollouts: cursor ? [] : [rollout],
          cursor: cursor ? "" : "rollouts-2",
          pollCursor: "next-cycle",
        }),
      ),
    );
    mockListReleaseChannels.mockImplementation(({ cursor }) =>
      Promise.resolve(
        create(ListReleaseChannelsResponseSchema, {
          channels: cursor ? [] : [canarySummary],
          cursor: cursor ? "" : "channels-2",
        }),
      ),
    );
    mockListReleaseChannelModelGroups.mockImplementation(({ cursor }) =>
      Promise.resolve(
        create(ListReleaseChannelModelGroupsResponseSchema, {
          modelGroups: cursor ? [] : [rigGroup],
          cursor: cursor ? "" : "groups-2",
        }),
      ),
    );
    mockListMinerStateSnapshots.mockImplementation(({ cursor }) =>
      Promise.resolve({
        miners: cursor ? [] : [{ deviceIdentifier: "rig-001", name: "Rig A01" }],
        cursor: cursor ? "" : "miners-2",
      }),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await act(async () => {
      await result.current.refresh();
    });
    for (const rpc of [
      mockListRollouts,
      mockListReleaseChannels,
      mockListMinerStateSnapshots,
      mockGetReleaseChannel,
      mockListReleaseChannelModelGroups,
    ]) {
      expect(rpc.mock.calls.length).toBeGreaterThan(1);
      expect(rpc.mock.calls.every(([, options]) => options?.timeoutMs === 30_000)).toBe(true);
    }
    expect(mockListRollouts).toHaveBeenCalledWith(
      { pageSize: 1000, cursor: "rollouts-2", status: RolloutStatus.ACTIVE },
      pollOptions,
    );
  });

  it.each(mutationCases)(
    "$name preserves a successful write result when refresh fails, but propagates write failures",
    async ({ name, rpc, call, response, expected }) => {
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      const previous = {
        channels: result.current.channels,
        rollouts: result.current.rollouts,
        miners: result.current.minerNames,
      };
      rpc.mockResolvedValue(response);
      const refreshError = new ConnectError("refresh unavailable after write", Code.Unavailable);
      mockListRollouts.mockRejectedValueOnce(refreshError);
      const detailReads = mockGetReleaseChannel.mock.calls.length;
      await act(async () => {
        expect(await call(result.current)).toBe(expected);
      });
      expect(rpc).toHaveBeenCalledTimes(1);
      expect(result.current.error).toBe(refreshError);
      expect(result.current.hasLoaded).toBe(true);
      if (name === "rollback") {
        expect(result.current.channels[0].modelGroups[0].rollbackPending).toBe(true);
        expect(result.current.rollouts[0]).toMatchObject({ revision: 1n, status: RolloutStatus.CANCELED });
      } else {
        expect(result.current.channels).toBe(previous.channels);
        expect(result.current.rollouts).toEqual(previous.rollouts);
      }
      expect(result.current.minerNames).toBe(previous.miners);
      expect(mockGetReleaseChannel).toHaveBeenCalledTimes(detailReads);
      expect(mockHandleAuthErrors).toHaveBeenCalledExactlyOnceWith({ error: refreshError });

      const acknowledged = { channels: result.current.channels, rollouts: result.current.rollouts };
      rpc.mockClear();
      const writeError = new ConnectError("write rejected", Code.FailedPrecondition);
      rpc.mockRejectedValueOnce(writeError);
      const readsAfterFailure = mockListRollouts.mock.calls.length;
      await act(async () => {
        await expect(call(result.current)).rejects.toBe(writeError);
      });
      expect(rpc).toHaveBeenCalledTimes(1);
      expect(mockListRollouts).toHaveBeenCalledTimes(readsAfterFailure);
      expect(mockGetReleaseChannel).toHaveBeenCalledTimes(detailReads);
      expect(result.current.error).toBe(refreshError);
      expect(result.current.channels).toBe(acknowledged.channels);
      expect(result.current.rollouts).toBe(acknowledged.rollouts);
      expect(result.current.minerNames).toBe(previous.miners);
    },
  );

  it.each(mutationCases)(
    "$name handles an authentication failure and rethrows the original write error",
    async ({ rpc, call }) => {
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const error = new ConnectError("session expired during write", Code.Unauthenticated);
      rpc.mockRejectedValueOnce(error);
      await act(async () => {
        await expect(call(result.current)).rejects.toBe(error);
      });
      expect(rpc).toHaveBeenCalledTimes(1);
      expect(mockHandleAuthErrors).toHaveBeenCalledExactlyOnceWith({ error });
      expect(mockListRollouts).toHaveBeenCalledTimes(1);
      expect(mockListReleaseChannels).toHaveBeenCalledTimes(1);
      expect(mockGetReleaseChannel).toHaveBeenCalledTimes(1);
      expect(result.current.error).toBeNull();
      expect(result.current.hasLoaded).toBe(true);
    },
  );

  it.each(["replaced session", "replaced session before rerender", "unmounted hook"])(
    "does not handle a delayed mutation authentication failure from a %s",
    async (oldContext) => {
      const { result, rerender, unmount } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const pendingWrite = deferred<object>();
      mockCancelRollout.mockReturnValueOnce(pendingWrite.promise);
      let mutation!: Promise<void>;
      await act(async () => {
        mutation = result.current.cancelRollout(9n, 1n);
      });
      expect(mockCancelRollout).toHaveBeenCalledTimes(1);
      if (oldContext === "unmounted hook") {
        unmount();
      } else {
        mockAuth.sessionGeneration += 1;
        if (oldContext === "replaced session") {
          rerender();
          await waitFor(() => expect(result.current.hasLoaded).toBe(true));
        }
      }
      const readsBeforeRejection = mockListRollouts.mock.calls.length;
      const error = new ConnectError("previous session expired during write", Code.Unauthenticated);
      const rejection = expect(mutation).rejects.toBe(error);
      await act(async () => {
        pendingWrite.reject(error);
        await rejection;
      });
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
      expect(mockListRollouts).toHaveBeenCalledTimes(readsBeforeRejection);
      expect(mockCancelRollout).toHaveBeenCalledTimes(1);
    },
  );

  it("handles a follow-up read authentication failure once without rejecting or resending a committed mutation", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    mockCancelRollout.mockResolvedValue({});
    const error = new ConnectError("session expired after write", Code.Unauthenticated);
    mockListRollouts.mockRejectedValueOnce(error);
    await act(async () => {
      await expect(result.current.cancelRollout(9n, 1n)).resolves.toBeUndefined();
    });
    expect(mockCancelRollout).toHaveBeenCalledTimes(1);
    expect(mockListRollouts).toHaveBeenCalledTimes(2);
    expect(mockHandleAuthErrors).toHaveBeenCalledExactlyOnceWith({ error });
    expect(result.current.error).toBe(error);
  });

  it("does not dispatch saved mutation callbacks after session replacement", async () => {
    const { result, rerender } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const previousApi = result.current;
    mockAuth.sessionGeneration += 1;
    rerender();
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    for (const { call, rpc } of mutationCases) {
      await expect(call(previousApi)).rejects.toThrow("Your session changed. Refresh the page before trying again.");
      expect(rpc).not.toHaveBeenCalled();
    }
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    expect(mockListRollouts).toHaveBeenCalledTimes(2);

    mockCancelRollout.mockResolvedValue({});
    await act(async () => {
      await result.current.cancelRollout(9n, 1n);
    });
    expect(mockCancelRollout).toHaveBeenCalledExactlyOnceWith({ rolloutId: 9n, expectedRevision: 1n });
    expect(mockListRollouts).toHaveBeenCalledTimes(3);
  });

  it("retains every applied pair after a failed refresh without regressing a newer polled rollout", async () => {
    const existing = create(RolloutSchema, { ...rollout, model: "Existing", revision: 3n, assignmentGeneration: 1n });
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [existing], pollCursor: "baseline" }),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const first = create(RolloutSchema, { ...rollout, id: 10n, revision: 1n, assignmentGeneration: 1n });
    const second = create(RolloutSchema, {
      ...rollout,
      id: 11n,
      manufacturer: "Other",
      revision: 1n,
      assignmentGeneration: 1n,
    });
    const started = [first, second];
    const assignments = started.map(({ manufacturer, model }) => ({
      manufacturer,
      model,
      firmwareFileId: `${manufacturer}-fw`,
    }));
    const response = deferred<object>();
    mockApplyReleaseChannelFirmware.mockReturnValueOnce(response.promise);
    const mutation = result.current.applyFirmware(canary.id, assignments);
    const newer = create(RolloutSchema, { ...first, revision: 3n, state: RolloutState.PAUSED });
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [newer], pollCursor: "newer" }));
    await act(async () => result.current.refresh());
    const error = new ConnectError("refresh unavailable after apply", Code.Unavailable);
    mockListRollouts.mockRejectedValueOnce(error);
    await act(async () => {
      response.resolve({ channel: canary, startedRollouts: started });
      await expect(mutation).resolves.toBe(started);
    });
    expect(mockApplyReleaseChannelFirmware).toHaveBeenCalledExactlyOnceWith({ channelId: canary.id, assignments });
    expect(result.current.rollouts).toEqual([second, newer, existing]);
    expect(result.current.error).toBe(error);
    expect(result.current.acknowledgedRollbacks).toEqual([]);
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { pollCursor: "recovered" }));
    await act(async () => result.current.refresh());
    expect(mockListRollouts).toHaveBeenCalledWith({ pageSize: 1000, cursor: "", pollCursor: "newer" }, pollOptions);
    expect(result.current.rollouts).toEqual([second, newer, existing]);
    expect(result.current.error).toBeNull();
  });

  it("retires the replaced active rollout from an apply acknowledgment before channel polling recovers", async () => {
    const previous = create(RolloutSchema, { ...rollbackSource, manufacturer: " PROTO ", model: "rig" });
    const successor = create(RolloutSchema, {
      ...rollbackSuccessor,
      firmwareChecksum: "c".repeat(64),
      firmwareVersion: "3.0.0",
    });
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [previous], pollCursor: "baseline" }),
    );
    mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [rollbackGroup], cursor: "" });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const channels = result.current.channels;
    mockApplyReleaseChannelFirmware.mockResolvedValueOnce({ channel: canary, startedRollouts: [successor] });
    const error = new ConnectError("channel refresh unavailable", Code.Unavailable);
    mockListReleaseChannels.mockRejectedValueOnce(error);
    await act(async () =>
      result.current.applyFirmware(canary.id, [{ manufacturer: "Proto", model: "Rig", firmwareFileId: "firmware-3" }]),
    );
    expect(result.current.error).toBe(error);
    expect(result.current.channels).toBe(channels);
    expect(result.current.channels[0].modelGroups[0].assignmentGeneration).toBe(previous.assignmentGeneration);
    expect(result.current.acknowledgedRollbacks).toEqual([]);
    expect(result.current.rollouts.filter((row) => row.status === RolloutStatus.ACTIVE)).toEqual([successor]);
    expect(result.current.rollouts.find((row) => row.id === previous.id)).toEqual({
      ...previous,
      status: RolloutStatus.CANCELED,
      state: RolloutState.CANCELED,
      cancelReason: RolloutCancelReason.SUPERSEDED,
    });
    // A delayed active header cannot revive the old generation or fabricate
    // a new revision while the authoritative cancellation read is pending.
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [previous], pollCursor: "replayed" }),
    );
    await act(async () => result.current.refresh());
    expect(result.current.rollouts.filter((row) => row.status === RolloutStatus.ACTIVE)).toEqual([successor]);
    expect(result.current.rollouts.find((row) => row.id === previous.id)).toMatchObject({
      status: RolloutStatus.CANCELED,
      revision: previous.revision,
    });
  });

  it("does not retain applied rollouts when the successful response belongs to a replaced session", async () => {
    const { result, rerender } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const response = deferred<object>();
    mockApplyReleaseChannelFirmware.mockReturnValueOnce(response.promise);
    const mutation = result.current.applyFirmware(canary.id, [
      { manufacturer: "Proto", model: "Rig", firmwareFileId: "new-firmware" },
    ]);
    mockAuth.sessionGeneration += 1;
    rerender();
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const currentRows = result.current.rollouts;
    const reads = mockListRollouts.mock.calls.length;
    const started = [create(RolloutSchema, { ...rollout, id: 10n, revision: 1n })];
    await act(async () => {
      response.resolve({ channel: canary, startedRollouts: started });
      await expect(mutation).resolves.toBe(started);
    });
    expect(result.current.rollouts).toBe(currentRows);
    expect(mockListRollouts).toHaveBeenCalledTimes(reads);
    await act(async () => result.current.refresh());
    expect(result.current.rollouts).toEqual([rollout]);
  });

  it.each(revisionActions.filter(([action]) => action !== "rollbackFirmware"))(
    "%s retains the acknowledged rollout and its revision when the follow-up read fails",
    async (action, rpc) => {
      const initial = create(RolloutSchema, { ...rollout, revision: 1n });
      mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [initial] }));
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const channels = result.current.channels;
      const acknowledged = create(RolloutSchema, {
        ...initial,
        revision: 2n,
        status: action === "cancelRollout" ? RolloutStatus.CANCELED : RolloutStatus.ACTIVE,
        state:
          action === "cancelRollout"
            ? RolloutState.CANCELED
            : action === "pauseRollout"
              ? RolloutState.PAUSED
              : RolloutState.IN_PROGRESS,
      });
      rpc.mockResolvedValueOnce({ rollout: acknowledged });
      const error = new ConnectError("read unavailable after write", Code.Unavailable);
      mockListRollouts.mockRejectedValueOnce(error);
      await act(async () => {
        await callRevisionAction(result.current, action, initial);
      });
      expect(result.current.rollouts).toEqual([acknowledged]);
      expect(result.current.channels).toBe(channels);
      expect(result.current.error).toBe(error);
      expect(result.current.hasLoaded).toBe(true);
      expect(rpc).toHaveBeenCalledExactlyOnceWith({ rolloutId: initial.id, expectedRevision: 1n });

      // The next action uses the committed revision, even before polling recovers.
      const [nextAction, nextRpc] =
        action === "pauseRollout"
          ? (["resumeRollout", mockResumeRollout] as const)
          : action === "cancelRollout"
            ? (["retryFailedDevices", mockRetryFailedRolloutDevices] as const)
            : (["pauseRollout", mockPauseRollout] as const);
      nextRpc.mockResolvedValueOnce({});
      await act(async () => {
        await result.current[nextAction](initial.id, result.current.rollouts[0].revision);
      });
      expect(nextRpc).toHaveBeenLastCalledWith({ rolloutId: initial.id, expectedRevision: 2n });
      expect(result.current.rollouts).toEqual([acknowledged]);
    },
  );

  it.each([
    { outcome: "starts a successor", source: rollbackSource, started: [rollbackSuccessor] },
    { outcome: "restores an assignment with no mismatched miners", source: rollbackSource, started: [] },
    {
      outcome: "clears the first assignment",
      source: create(RolloutSchema, { ...rollbackSource, previousFirmwareChecksum: "", previousFirmwareVersion: "" }),
      started: [],
    },
  ])(
    "retains a rollback that $outcome through refresh failure and reconciles authoritative recovery",
    async ({ source, started }) => {
      const unrelated = create(RolloutSchema, { ...source, id: 11n, manufacturer: "Other" });
      const unrelatedGroup = create(ReleaseChannelModelGroupSchema, {
        ...rollbackGroup,
        manufacturer: "Other",
        activeRolloutId: unrelated.id,
      });
      // Observed keys use the same normalization as assignment writes.
      const observedGroup = create(ReleaseChannelModelGroupSchema, {
        ...rollbackGroup,
        manufacturer: " PROTO ",
        model: "rig",
      });
      mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [observedGroup, unrelatedGroup], cursor: "" });
      mockListRollouts.mockResolvedValue(
        create(ListRolloutsResponseSchema, { rollouts: [source, unrelated], pollCursor: "baseline" }),
      );
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      mockRollbackReleaseChannelFirmware.mockResolvedValueOnce({ channel: canary, startedRollouts: started });
      const error = new ConnectError("read unavailable after rollback", Code.Unavailable);
      mockListRollouts.mockRejectedValueOnce(error);
      await act(async () => {
        await expect(result.current.rollbackFirmware(source)).resolves.toBe(started);
      });
      expect(mockRollbackReleaseChannelFirmware).toHaveBeenCalledExactlyOnceWith({
        rolloutId: source.id,
        expectedRevision: source.revision,
      });
      expect(result.current.error).toBe(error);
      expect(result.current.acknowledgedRollbacks).toEqual([source]);
      expect(result.current.rollouts.find((row) => row.id === source.id)).toEqual({
        ...source,
        status: RolloutStatus.CANCELED,
        state: RolloutState.CANCELED,
        cancelReason: RolloutCancelReason.ROLLED_BACK,
      });
      expect(result.current.rollouts.find((row) => row.id === unrelated.id)).toEqual(unrelated);
      expect(result.current.rollouts.filter((row) => row.status === RolloutStatus.ACTIVE)).toEqual(
        expect.arrayContaining([unrelated, ...started]),
      );
      expect(result.current.channels[0].modelGroups).toEqual([
        { ...observedGroup, rollbackPending: true },
        unrelatedGroup,
      ]);

      const authoritative = create(RolloutSchema, {
        ...source,
        revision: 8n,
        status: RolloutStatus.CANCELED,
        state: RolloutState.CANCELED,
        cancelReason: RolloutCancelReason.ROLLED_BACK,
        updatedAt: create(TimestampSchema, { seconds: 310n }),
        finishedAt: create(TimestampSchema, { seconds: 310n }),
      });
      const recoveredGroup = create(ReleaseChannelModelGroupSchema, {
        ...observedGroup,
        assignmentGeneration: 5n,
        firmwareChecksum: source.previousFirmwareChecksum,
        firmwareVersion: source.previousFirmwareVersion,
        activeRolloutId: started[0]?.id ?? 0n,
        onTargetCount: 0,
      });
      mockListReleaseChannelModelGroups.mockResolvedValue({
        modelGroups: [recoveredGroup, unrelatedGroup],
        cursor: "",
      });
      mockListRollouts.mockImplementation(({ status }) =>
        Promise.resolve(
          create(ListRolloutsResponseSchema, {
            rollouts: status === RolloutStatus.ACTIVE ? [unrelated, ...started] : [authoritative, ...started],
            pollCursor: "recovered",
          }),
        ),
      );
      await act(async () => result.current.refresh());
      expect(result.current.acknowledgedRollbacks).toEqual([]);
      expect(result.current.channels[0].modelGroups).toEqual([recoveredGroup, unrelatedGroup]);
      expect(result.current.rollouts.find((row) => row.id === source.id)).toEqual(authoritative);
      expect(result.current.error).toBeNull();
    },
  );

  it("retains a historical rollback source and retires its generation's different active reconciliation run", async () => {
    const historical = create(RolloutSchema, {
      ...rollbackSource,
      status: RolloutStatus.COMPLETED,
      state: RolloutState.COMPLETED,
      finishedAt: create(TimestampSchema, { seconds: 210n }),
    });
    const reconciliation = create(RolloutSchema, { ...rollbackSource, id: 12n, revision: 3n });
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [reconciliation], pollCursor: "baseline" }),
    );
    mockListReleaseChannelModelGroups.mockResolvedValue({
      modelGroups: [{ ...rollbackGroup, activeRolloutId: reconciliation.id }],
      cursor: "",
    });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    mockRollbackReleaseChannelFirmware.mockResolvedValueOnce({ channel: canary, startedRollouts: [] });
    mockListRollouts.mockRejectedValueOnce(new ConnectError("refresh unavailable", Code.Unavailable));
    await act(async () => result.current.rollbackFirmware(historical));
    expect(result.current.acknowledgedRollbacks).toEqual([historical]);
    expect(result.current.rollouts.find((row) => row.id === historical.id)).toEqual(historical);
    expect(result.current.rollouts.find((row) => row.id === reconciliation.id)).toEqual({
      ...reconciliation,
      status: RolloutStatus.CANCELED,
      state: RolloutState.CANCELED,
      cancelReason: RolloutCancelReason.ROLLED_BACK,
    });
    expect(result.current.channels[0].modelGroups[0].rollbackPending).toBe(true);
  });

  it("preserves a rollback through a pre-write poll and late control acknowledgment without fabricating revisions", async () => {
    const poll = capturePollingTimer();
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollbackSource], pollCursor: "baseline" }),
    );
    mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [rollbackGroup], cursor: "" });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const control = deferred<object>();
    mockPauseRollout.mockReturnValueOnce(control.promise);
    const pause = result.current.pauseRollout(rollbackSource.id, 6n);
    const oldPoll = deferred<ListRolloutsResponse>();
    const postWrite = deferred<ListRolloutsResponse>();
    const responses = [oldPoll.promise, postWrite.promise];
    const cursors: string[] = [];
    mockListRollouts.mockImplementation(({ status, pollCursor }) => {
      if (status === RolloutStatus.ACTIVE)
        return Promise.resolve(create(ListRolloutsResponseSchema, { rollouts: [rollbackSource] }));
      cursors.push(pollCursor);
      return responses.shift() ?? Promise.reject(new ConnectError("still unavailable", Code.Unavailable));
    });
    await act(async () => poll());
    mockRollbackReleaseChannelFirmware.mockResolvedValueOnce({ channel: canary, startedRollouts: [rollbackSuccessor] });
    let rollback!: Promise<(typeof rollout)[]>;
    await act(async () => {
      rollback = result.current.rollbackFirmware(rollbackSource);
    });
    expect(result.current.acknowledgedRollbacks).toEqual([rollbackSource]);
    expect(result.current.rollouts.find((row) => row.id === rollbackSource.id)?.status).toBe(RolloutStatus.CANCELED);
    await act(async () => {
      oldPoll.resolve(create(ListRolloutsResponseSchema, { rollouts: [rollbackSource], pollCursor: "old-poll" }));
    });
    await waitFor(() => expect(cursors).toEqual(["baseline", "old-poll"]));
    expect(result.current.acknowledgedRollbacks).toEqual([rollbackSource]);
    expect(result.current.rollouts.find((row) => row.id === rollbackSuccessor.id)).toEqual(rollbackSuccessor);
    await act(async () => {
      postWrite.reject(new ConnectError("refresh unavailable", Code.Unavailable));
      await rollback;
      control.resolve({ rollout: { ...rollbackSource, state: RolloutState.PAUSED } });
      await pause;
    });
    expect(cursors).toEqual(["baseline", "old-poll", "old-poll"]);
    expect(result.current.rollouts.find((row) => row.id === rollbackSource.id)).toMatchObject({
      status: RolloutStatus.CANCELED,
      state: RolloutState.CANCELED,
      revision: 7n,
    });
    expect(result.current.rollouts.find((row) => row.id === rollbackSuccessor.id)).toEqual(rollbackSuccessor);
    expect(result.current.acknowledgedRollbacks).toEqual([rollbackSource]);
  });

  it("keeps rollback acknowledgment until a fresh scan confirms both assignment and canceled rows", async () => {
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [rollbackSource] }));
    mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [rollbackGroup], cursor: "" });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    mockRollbackReleaseChannelFirmware.mockResolvedValueOnce({ channel: canary, startedRollouts: [] });
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { pollCursor: "first-watermark" }));
    mockListReleaseChannelModelGroups.mockResolvedValue({
      modelGroups: [{ ...rollbackGroup, assignmentGeneration: 5n, activeRolloutId: 0n }],
      cursor: "",
    });
    await act(async () => result.current.rollbackFirmware(rollbackSource));
    // The active-only baseline omits terminal rows; it cannot replace the
    // source's real revision, even though the new assignment is already known.
    expect(result.current.acknowledgedRollbacks).toEqual([rollbackSource]);
    expect(result.current.channels[0].modelGroups[0].rollbackPending).toBeUndefined();
    expect(result.current.rollouts[0]).toMatchObject({ revision: 7n, status: RolloutStatus.CANCELED });
    const terminal = create(RolloutSchema, {
      ...rollbackSource,
      revision: 8n,
      status: RolloutStatus.CANCELED,
      state: RolloutState.CANCELED,
      cancelReason: RolloutCancelReason.ROLLED_BACK,
    });
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [terminal], pollCursor: "confirmed" }),
    );
    await act(async () => result.current.refresh());
    expect(result.current.acknowledgedRollbacks).toEqual([]);
    expect(result.current.rollouts).toEqual([terminal]);
  });

  it("keeps the newest invalidated generation when rollback acknowledgments arrive out of order", async () => {
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollbackSource], pollCursor: "baseline" }),
    );
    mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [rollbackGroup], cursor: "" });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const earlier = deferred<object>();
    mockRollbackReleaseChannelFirmware.mockReturnValueOnce(earlier.promise);
    const pendingEarlier = result.current.rollbackFirmware(rollbackSource);
    // Another view can observe and roll back the first successor while the
    // original request's successful response is still delayed in transport.
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollbackSuccessor], pollCursor: "successor" }),
    );
    mockListReleaseChannelModelGroups.mockResolvedValue({
      modelGroups: [{ ...rollbackGroup, assignmentGeneration: 5n, activeRolloutId: rollbackSuccessor.id }],
      cursor: "",
    });
    await act(async () => result.current.refresh());
    const latest = create(RolloutSchema, { ...rollbackSuccessor, id: 13n, assignmentGeneration: 6n, revision: 3n });
    mockRollbackReleaseChannelFirmware.mockResolvedValueOnce({ channel: canary, startedRollouts: [latest] });
    mockListRollouts.mockImplementation(({ status }) =>
      status === RolloutStatus.ACTIVE
        ? Promise.resolve(create(ListRolloutsResponseSchema))
        : Promise.reject(new ConnectError("refresh unavailable", Code.Unavailable)),
    );
    await act(async () => result.current.rollbackFirmware(rollbackSuccessor));
    expect(result.current.acknowledgedRollbacks).toEqual([rollbackSuccessor]);
    await act(async () => {
      earlier.resolve({ channel: canary, startedRollouts: [rollbackSuccessor] });
      await pendingEarlier;
    });
    expect(result.current.acknowledgedRollbacks).toEqual([rollbackSuccessor]);
    expect(result.current.rollouts.filter((row) => row.status === RolloutStatus.ACTIVE)).toEqual([latest]);
    expect(result.current.rollouts.find((row) => row.id === rollbackSuccessor.id)).toMatchObject({
      revision: 1n,
      status: RolloutStatus.CANCELED,
    });
    expect(result.current.rollouts.find((row) => row.id === rollbackSource.id)).toMatchObject({
      revision: 7n,
      status: RolloutStatus.CANCELED,
    });
    expect(result.current.channels[0].modelGroups[0]).toMatchObject({
      assignmentGeneration: 5n,
      rollbackPending: true,
    });
  });

  it("does not reactivate a late rollback successor after a newer assignment was polled", async () => {
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollbackSource], pollCursor: "baseline" }),
    );
    mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [rollbackGroup], cursor: "" });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const response = deferred<object>();
    mockRollbackReleaseChannelFirmware.mockReturnValueOnce(response.promise);
    const pending = result.current.rollbackFirmware(rollbackSource);
    const newest = create(RolloutSchema, { ...rollbackSuccessor, id: 13n, assignmentGeneration: 6n, revision: 3n });
    const currentGroup = create(ReleaseChannelModelGroupSchema, {
      ...rollbackGroup,
      manufacturer: " PROTO ",
      model: "rig",
      assignmentGeneration: 6n,
      activeRolloutId: newest.id,
    });
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [newest], pollCursor: "newer-assignment" }),
    );
    mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [currentGroup], cursor: "" });
    await act(async () => result.current.refresh());
    expect(result.current.acknowledgedRollbacks).toEqual([]);
    expect(result.current.rollouts.filter((row) => row.status === RolloutStatus.ACTIVE)).toEqual([newest]);
    mockListRollouts.mockRejectedValueOnce(new ConnectError("refresh unavailable", Code.Unavailable));
    await act(async () => {
      response.resolve({ channel: canary, startedRollouts: [rollbackSuccessor] });
      await pending;
    });
    expect(result.current.acknowledgedRollbacks).toEqual([rollbackSource]);
    expect(result.current.rollouts.filter((row) => row.status === RolloutStatus.ACTIVE)).toEqual([newest]);
    expect(result.current.rollouts.find((row) => row.id === rollbackSource.id)?.cancelReason).toBe(
      RolloutCancelReason.ROLLED_BACK,
    );
    expect(result.current.rollouts.find((row) => row.id === rollbackSuccessor.id)).toEqual({
      ...rollbackSuccessor,
      status: RolloutStatus.CANCELED,
      state: RolloutState.CANCELED,
      cancelReason: RolloutCancelReason.SUPERSEDED,
    });
    expect(result.current.channels[0].modelGroups).toEqual([currentGroup]);
    // A repeated stale header must not undo the known newer assignment, even
    // when its revision has advanced independently of the delayed response.
    const staleSuccessor = create(RolloutSchema, { ...rollbackSuccessor, revision: 2n });
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [staleSuccessor, newest], pollCursor: "replayed" }),
    );
    await act(async () => result.current.refresh());
    expect(result.current.rollouts.filter((row) => row.status === RolloutStatus.ACTIVE)).toEqual([newest]);
    expect(result.current.rollouts.find((row) => row.id === rollbackSuccessor.id)).toMatchObject({
      revision: 2n,
      status: RolloutStatus.CANCELED,
      cancelReason: RolloutCancelReason.SUPERSEDED,
    });
  });

  it.each(["replaced session", "replaced session before rerender", "logged out", "unmounted hook"])(
    "does not retain a rollback or successor from a %s",
    async (oldContext) => {
      mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [rollbackSource] }));
      mockListReleaseChannelModelGroups.mockResolvedValue({ modelGroups: [rollbackGroup], cursor: "" });
      const { result, rerender, unmount } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const response = deferred<object>();
      mockRollbackReleaseChannelFirmware.mockReturnValueOnce(response.promise);
      const mutation = result.current.rollbackFirmware(rollbackSource);
      if (oldContext === "unmounted hook") unmount();
      else if (oldContext === "logged out") {
        mockAuth.isAuthenticated = false;
        rerender();
      } else {
        mockAuth.sessionGeneration += 1;
        if (oldContext === "replaced session") {
          rerender();
          await waitFor(() => expect(result.current.hasLoaded).toBe(true));
        }
      }
      const reads = mockListRollouts.mock.calls.length;
      const before = result.current.rollouts;
      await act(async () => {
        response.resolve({ channel: canary, startedRollouts: [rollbackSuccessor] });
        await mutation;
      });
      expect(result.current.rollouts).toBe(before);
      expect(result.current.acknowledgedRollbacks).toEqual([]);
      expect(
        result.current.channels.every((channel) => channel.modelGroups.every((group) => !group.rollbackPending)),
      ).toBe(true);
      expect(mockListRollouts).toHaveBeenCalledTimes(reads);
    },
  );

  it.each(["", "baseline-token"])(
    "preserves acknowledged controls through an older poll with cursor %j and a failed refresh",
    async (pollCursor) => {
      const poll = capturePollingTimer();
      const initial = create(RolloutSchema, { ...rollout, revision: 1n });
      mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [initial], pollCursor }));
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const oldPoll = deferred<ListRolloutsResponse>();
      const followUp = deferred<ListRolloutsResponse>();
      const responses = [oldPoll.promise, followUp.promise];
      const cursors: string[] = [];
      mockListRollouts.mockImplementation((request) => {
        if (request.status === RolloutStatus.ACTIVE && request.pollCursor === undefined) {
          return Promise.resolve(create(ListRolloutsResponseSchema, { rollouts: [initial] }));
        }
        cursors.push(request.pollCursor);
        return responses.shift() ?? Promise.resolve(create(ListRolloutsResponseSchema, { pollCursor: "recovered" }));
      });
      await act(async () => poll());
      expect(cursors).toEqual([pollCursor]);
      const acknowledged = create(RolloutSchema, { ...initial, revision: 2n, state: RolloutState.PAUSED });
      mockPauseRollout.mockResolvedValueOnce({ rollout: acknowledged });
      let mutation!: Promise<void>;
      await act(async () => {
        mutation = result.current.pauseRollout(initial.id, initial.revision);
      });
      expect(result.current.rollouts).toEqual([acknowledged]);
      expect(cursors).toEqual([pollCursor]);
      await act(async () => {
        oldPoll.resolve(create(ListRolloutsResponseSchema, { rollouts: [initial], pollCursor: "old-poll-token" }));
      });
      await waitFor(() => expect(cursors).toEqual([pollCursor, "old-poll-token"]));
      expect(result.current.rollouts).toEqual([acknowledged]);
      const error = new ConnectError("follow-up unavailable", Code.Unavailable);
      await act(async () => {
        followUp.reject(error);
        await mutation;
      });
      expect(result.current.rollouts).toEqual([acknowledged]);
      expect(result.current.error).toBe(error);
      await act(async () => result.current.refresh());
      expect(cursors).toEqual([pollCursor, "old-poll-token", "old-poll-token"]);
      expect(result.current.rollouts).toEqual([acknowledged]);
      expect(result.current.error).toBeNull();
    },
  );

  it("keeps a newer polled revision when an earlier control response arrives late", async () => {
    const initial = create(RolloutSchema, { ...rollout, revision: 1n });
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [initial] }));
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const response = deferred<object>();
    mockPauseRollout.mockReturnValueOnce(response.promise);
    const mutation = result.current.pauseRollout(initial.id, initial.revision);
    const newer = create(RolloutSchema, { ...initial, revision: 3n, state: RolloutState.IN_PROGRESS });
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [newer] }));
    await act(async () => result.current.refresh());
    const error = new ConnectError("follow-up unavailable", Code.Unavailable);
    mockListRollouts.mockRejectedValueOnce(error);
    await act(async () => {
      response.resolve({ rollout: create(RolloutSchema, { ...initial, revision: 2n, state: RolloutState.PAUSED }) });
      await mutation;
    });
    expect(result.current.rollouts).toEqual([newer]);
    expect(result.current.error).toBe(error);
  });

  it("retains a historical retry's successor outside the initial active snapshot when its refresh fails", async () => {
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { pollCursor: "baseline-token" }));
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    expect(result.current.rollouts).toEqual([]);
    const successor = create(RolloutSchema, { ...rollout, id: 10n, revision: 1n });
    mockRetryFailedRolloutDevices.mockResolvedValueOnce({ rollout: successor });
    const error = new ConnectError("follow-up unavailable", Code.Unavailable);
    mockListRollouts.mockRejectedValueOnce(error);
    await act(async () => {
      await expect(result.current.retryFailedDevices(9n, 5n)).resolves.toEqual(successor);
    });
    expect(result.current.rollouts).toEqual([successor]);
    expect(result.current.error).toBe(error);
    await act(async () => result.current.refresh());
    expect(result.current.rollouts).toEqual([successor]);
    expect(result.current.error).toBeNull();
  });

  it.each(["replaced session", "replaced session before rerender", "logged out", "unmounted hook"])(
    "does not retain a delayed rollout acknowledgment from a %s",
    async (oldContext) => {
      const initial = create(RolloutSchema, { ...rollout, revision: 1n });
      mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [initial] }));
      const { result, rerender, unmount } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const response = deferred<object>();
      mockCancelRollout.mockReturnValueOnce(response.promise);
      const mutation = result.current.cancelRollout(initial.id, initial.revision);
      if (oldContext === "unmounted hook") unmount();
      else if (oldContext === "logged out") {
        mockAuth.isAuthenticated = false;
        rerender();
      } else {
        mockAuth.sessionGeneration += 1;
        if (oldContext === "replaced session") {
          rerender();
          await waitFor(() => expect(result.current.hasLoaded).toBe(true));
        }
      }
      const readsBeforeResponse = mockListRollouts.mock.calls.length;
      const rowsBeforeResponse = result.current.rollouts;
      await act(async () => {
        response.resolve({
          rollout: create(RolloutSchema, { ...initial, revision: 2n, status: RolloutStatus.CANCELED }),
        });
        await mutation;
      });
      expect(result.current.rollouts).toBe(rowsBeforeResponse);
      expect(mockListRollouts).toHaveBeenCalledTimes(readsBeforeResponse);
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
      if (oldContext === "replaced session") {
        await act(async () => result.current.refresh());
        expect(result.current.rollouts).toEqual([initial]);
      }
    },
  );

  it("handles scope-preview authentication failures without refreshing or hiding the original error", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const error = new ConnectError("preview authentication expired", Code.Unauthenticated);
    mockPreviewReleaseChannelScope.mockRejectedValueOnce(error);
    await expect(result.current.previewScope(create(ReleaseChannelScopeSchema), 1n)).rejects.toBe(error);
    expect(mockPreviewReleaseChannelScope).toHaveBeenCalledTimes(1);
    expect(mockHandleAuthErrors).toHaveBeenCalledExactlyOnceWith({ error });
    expect(mockListRollouts).toHaveBeenCalledTimes(1);
    expect(result.current.error).toBeNull();
  });

  it("does not send an already-aborted scope preview", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const controller = new AbortController();
    const reason = new Error("Scope preview was replaced");
    controller.abort(reason);
    await expect(
      result.current.previewScope(create(ReleaseChannelScopeSchema), undefined, controller.signal),
    ).rejects.toBe(reason);
    expect(mockPreviewReleaseChannelScope).not.toHaveBeenCalled();
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
  });

  it("times out a stalled scope preview through Connect and allows retry without refreshing the snapshot", async () => {
    capturePollingTimer();
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const previous = { channels: result.current.channels, rollouts: result.current.rollouts };
    const stalled = stalledRolloutTransport();
    mockPreviewReleaseChannelScope.mockImplementation(stalled.client.previewReleaseChannelScope);
    const scope = create(ReleaseChannelScopeSchema, { siteIds: [1n] });
    const controller = new AbortController();
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    let rejected!: Promise<void>;
    await act(async () => {
      rejected = expect(result.current.previewScope(scope, 1n, controller.signal)).rejects.toMatchObject({
        code: Code.DeadlineExceeded,
      });
    });
    expect(stalled.signals).toHaveLength(1);
    await act(async () => vi.advanceTimersByTimeAsync(29_999));
    expect(stalled.signals[0].aborted).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
      await rejected;
    });
    expect(stalled.signals[0].aborted).toBe(true);
    expect(controller.signal.aborted).toBe(false);
    const preview = create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 5 });
    mockPreviewReleaseChannelScope.mockResolvedValue(preview);
    await expect(result.current.previewScope(scope, 1n, controller.signal)).resolves.toEqual(preview);
    expect(mockPreviewReleaseChannelScope).toHaveBeenCalledTimes(2);
    expect(mockPreviewReleaseChannelScope).toHaveBeenLastCalledWith(
      { scope, channelId: 1n },
      { timeoutMs: 30_000, signal: controller.signal },
    );
    expect(mockListRollouts).toHaveBeenCalledOnce();
    expect(result.current.channels).toBe(previous.channels);
    expect(result.current.rollouts).toBe(previous.rollouts);
    expect(result.current.error).toBeNull();
  });

  it.each(
    (["canceled", "new login", "new login before rerender", "unmounted"] as const).flatMap((context) =>
      (["success", "401"] as const).map((outcome) => ({ context, outcome })),
    ),
  )(
    "rejects a late scope preview $outcome when $context without logging out the current session",
    async ({ context, outcome }) => {
      const { result, rerender, unmount } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const oldPreview = create(PreviewReleaseChannelScopeResponseSchema, { minerCount: 99 });
      const response = deferred<typeof oldPreview>();
      mockPreviewReleaseChannelScope.mockReturnValueOnce(response.promise);
      const controller = new AbortController();
      const reason = new Error("Scope preview was replaced");
      const authenticationError = new ConnectError("Old preview login expired", Code.Unauthenticated);
      const request = result.current.previewScope(create(ReleaseChannelScopeSchema), 1n, controller.signal);
      const rejected =
        context === "canceled"
          ? expect(request).rejects.toBe(reason)
          : outcome === "401"
            ? expect(request).rejects.toBe(authenticationError)
            : expect(request).rejects.toThrow("Your session changed");
      if (context === "canceled") controller.abort(reason);
      else if (context === "unmounted") unmount();
      else {
        mockAuth.sessionGeneration += 1;
        if (context === "new login") {
          rerender();
          await waitFor(() => expect(result.current.hasLoaded).toBe(true));
        }
      }
      if (outcome === "401") response.reject(authenticationError);
      else response.resolve(oldPreview);
      await rejected;
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    },
  );

  it.each(paginatedActionCases)(
    "times out a stalled $name page through Connect and makes the modal retry usable",
    async ({ name, rpc }) => {
      capturePollingTimer();
      const previousMiner = create(ReleaseChannelMinerSchema, { deviceIdentifier: "previous", firmwareVersion: "1.0" });
      mockListReleaseChannelMiners.mockResolvedValue({ miners: [previousMiner], cursor: "" });
      mockListRolloutDevices.mockResolvedValue({
        devices: [create(RolloutDeviceSchema, { deviceIdentifier: "previous" })],
        cursor: "",
      });
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const props = {
        channelId: 1n,
        channelName: "Canary",
        group: rigGroup,
        activeRollout: rollout,
        minerNames: {},
        listChannelMiners: result.current.listChannelMiners,
        listRolloutDevices: result.current.listRolloutDevices,
        onClose: vi.fn(),
      };
      const { rerender } = render(<ModelMinersModal {...props} />);
      await screen.findByTestId("channel-miner-previous");
      const key = name === "channel miners" ? "miners" : "devices";
      const stalled = stalledRolloutTransport(
        Response.json({ [key]: [{ deviceIdentifier: "partial" }], cursor: "stalled-page" }),
      );
      rpc.mockImplementation(
        name === "channel miners" ? stalled.client.listReleaseChannelMiners : stalled.client.listRolloutDevices,
      );
      vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
      await act(async () => rerender(<ModelMinersModal {...props} group={{ ...rigGroup }} />));
      expect(stalled.signals).toHaveLength(1);
      expect(screen.getByTestId("channel-miner-previous")).toBeInTheDocument();
      expect(screen.queryByTestId("channel-miner-partial")).not.toBeInTheDocument();
      await act(async () => vi.advanceTimersByTimeAsync(30_000));
      expect(stalled.signals[0].aborted).toBe(true);
      expect(screen.getByRole("alert")).toHaveTextContent("Showing the last loaded data");
      expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled();
      expect(screen.getByTestId("channel-miner-previous")).toBeInTheDocument();
      expect(screen.queryByTestId("channel-miner-partial")).not.toBeInTheDocument();
      mockListReleaseChannelMiners.mockResolvedValue({
        miners: [create(ReleaseChannelMinerSchema, { deviceIdentifier: "recovered" })],
        cursor: "",
      });
      mockListRolloutDevices.mockResolvedValue({ devices: [], cursor: "" });
      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Retry" })));
      expect(screen.getByTestId("channel-miner-recovered")).toBeInTheDocument();
      expect(screen.queryByTestId("channel-miner-previous")).not.toBeInTheDocument();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(result.current.error).toBeNull();
    },
  );

  it.each(paginatedActionCases)(
    "aborts the active $name page through Connect without returning partial details",
    async ({ name, rpc, call }) => {
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const key = name === "channel miners" ? "miners" : "devices";
      const stalled = stalledRolloutTransport(
        Response.json({ [key]: [{ deviceIdentifier: "partial" }], cursor: "next-page" }),
      );
      rpc.mockImplementation(
        name === "channel miners" ? stalled.client.listReleaseChannelMiners : stalled.client.listRolloutDevices,
      );
      const controller = new AbortController();
      const pending = call(result.current, controller.signal);
      const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" });
      await waitFor(() => expect(stalled.signals).toHaveLength(1));
      controller.abort();
      await rejected;
      expect(stalled.signals[0].aborted).toBe(true);
      expect(rpc).toHaveBeenCalledTimes(2);
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    },
  );

  it.each(paginatedActionCases)(
    "does not dispatch already-canceled $name scans or advance after cancellation",
    async ({ rpc, call, firstPage }) => {
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const preCanceled = new AbortController();
      preCanceled.abort();
      await expect(call(result.current, preCanceled.signal)).rejects.toMatchObject({ name: "AbortError" });
      expect(rpc).not.toHaveBeenCalled();
      const page = deferred<typeof firstPage>();
      rpc.mockReturnValueOnce(page.promise);
      const controller = new AbortController();
      const pending = call(result.current, controller.signal);
      const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" });
      controller.abort();
      page.resolve(firstPage);
      await rejected;
      expect(rpc).toHaveBeenCalledOnce();
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    },
  );

  it.each(paginatedActionCases)(
    "does not log out when an abandoned $name scan returns a late 401",
    async ({ rpc, call }) => {
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const page = deferred<never>();
      rpc.mockReturnValueOnce(page.promise);
      const controller = new AbortController();
      const pending = call(result.current, controller.signal);
      const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" });
      controller.abort();
      page.reject(new ConnectError("Abandoned authentication failure", Code.Unauthenticated));
      await rejected;
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    },
  );

  it.each(paginatedActionCases)(
    "handles authentication failures on later $name pages without returning partial details",
    async ({ rpc, call, firstPage }) => {
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const error = new ConnectError("details authentication expired", Code.Unauthenticated);
      rpc.mockResolvedValueOnce(firstPage).mockRejectedValueOnce(error);
      await expect(call(result.current)).rejects.toBe(error);
      expect(rpc.mock.calls.map(([request]) => request.cursor)).toEqual(["", "details-2"]);
      expect(mockHandleAuthErrors).toHaveBeenCalledExactlyOnceWith({ error });
      expect(mockListRollouts).toHaveBeenCalledTimes(1);
      expect(result.current.error).toBeNull();
    },
  );

  it.each(paginatedActionCases)(
    "stops paging $name when the session changes between responses",
    async ({ rpc, call, firstPage }) => {
      const { result, rerender } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const page = deferred<typeof firstPage>();
      rpc.mockReturnValueOnce(page.promise);
      const details = call(result.current);
      expect(rpc).toHaveBeenCalledTimes(1);
      mockAuth.sessionGeneration += 1;
      rerender();
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const rejection = expect(details).rejects.toThrow("Your session changed. Refresh the page before trying again.");
      await act(async () => {
        page.resolve(firstPage);
        await rejection;
      });
      expect(rpc).toHaveBeenCalledTimes(1);
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
      expect(mockListRollouts).toHaveBeenCalledTimes(2);
    },
  );

  it("distinguishes a failed first load from stale loaded data and clears errors after recovery", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const initialError = new ConnectError("initial read unavailable", Code.Unavailable);
    mockListRollouts.mockRejectedValueOnce(initialError);
    const { result } = renderHook(() => useReleaseChannels());
    expect(result.current.isLoading).toBe(true);
    expect(result.current.hasLoaded).toBe(false);
    expect(result.current.error).toBeNull();
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.error).toBe(initialError);
    expect(result.current.hasLoaded).toBe(false);
    expect(result.current.channels).toEqual([]);
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.error).toBeNull();
    expect(result.current.hasLoaded).toBe(true);
    const channels = result.current.channels;
    const laterError = new Error("later read unavailable");
    mockListRollouts.mockRejectedValueOnce(laterError);
    await act(async () => {
      await expect(result.current.refresh()).rejects.toBe(laterError);
    });
    expect(result.current.error).toBe(laterError);
    expect(result.current.hasLoaded).toBe(true);
    expect(result.current.channels).toBe(channels);
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.error).toBeNull();
    expect(result.current.hasLoaded).toBe(true);
  });

  it("clears previous-session load errors while the new session is still loading", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const error = new ConnectError("old session expired", Code.Unauthenticated);
    mockListRollouts.mockRejectedValueOnce(error);
    const { result, rerender } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.error).toBe(error);
    expect(result.current.hasLoaded).toBe(false);
    expect(mockHandleAuthErrors).toHaveBeenCalledWith({ error });
    const baseline = deferred<ListRolloutsResponse>();
    mockListRollouts.mockReturnValueOnce(baseline.promise);
    mockAuth.sessionGeneration += 1;
    rerender();
    expect(result.current.error).toBeNull();
    expect(result.current.hasLoaded).toBe(false);
    expect(result.current.isLoading).toBe(true);
    await act(async () => {
      baseline.resolve(create(ListRolloutsResponseSchema));
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.error).toBeNull();
    expect(result.current.hasLoaded).toBe(true);
  });

  it("serializes two mutation refreshes behind an old poll without regressing state or the polling cursor", async () => {
    const poll = capturePollingTimer();
    const first = create(RolloutSchema, { ...rollout, revision: 1n });
    const second = create(RolloutSchema, { ...rollout, id: 10n, revision: 1n });
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [first, second], pollCursor: "initial-token" }),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    const oldPoll = deferred<ListRolloutsResponse>();
    const firstRefresh = deferred<ListRolloutsResponse>();
    const secondRefresh = deferred<ListRolloutsResponse>();
    const responses = [oldPoll, firstRefresh, secondRefresh];
    const cursors: string[] = [];
    mockListRollouts.mockImplementation(({ status, pollCursor }) => {
      if (status === RolloutStatus.ACTIVE) return Promise.resolve(create(ListRolloutsResponseSchema));
      cursors.push(pollCursor);
      return (
        responses[cursors.length - 1]?.promise ??
        Promise.resolve(create(ListRolloutsResponseSchema, { pollCursor: "last-token" }))
      );
    });
    await act(async () => {
      poll();
    });
    expect(cursors).toEqual(["initial-token"]);

    const cancel = deferred<object>();
    const pause = deferred<object>();
    mockCancelRollout.mockReturnValueOnce(cancel.promise);
    mockPauseRollout.mockReturnValueOnce(pause.promise);
    let firstDone = false;
    let secondDone = false;
    let firstMutation!: Promise<void>;
    let secondMutation!: Promise<void>;
    await act(async () => {
      firstMutation = result.current.cancelRollout(9n, 1n).then(() => {
        firstDone = true;
      });
      secondMutation = result.current.pauseRollout(10n, 1n).then(() => {
        secondDone = true;
      });
    });
    expect(mockCancelRollout).toHaveBeenCalledExactlyOnceWith({ rolloutId: 9n, expectedRevision: 1n });
    expect(mockPauseRollout).toHaveBeenCalledExactlyOnceWith({ rolloutId: 10n, expectedRevision: 1n });
    await act(async () => {
      cancel.resolve({});
      pause.resolve({});
    });
    expect(firstDone).toBe(false);
    expect(secondDone).toBe(false);
    expect(cursors).toEqual(["initial-token"]);

    await act(async () => {
      oldPoll.resolve(create(ListRolloutsResponseSchema, { rollouts: [first, second], pollCursor: "old-poll-token" }));
    });
    await waitFor(() => expect(cursors).toHaveLength(2));
    expect(cursors).toEqual(["initial-token", "old-poll-token"]);
    expect(firstDone).toBe(false);
    expect(secondDone).toBe(false);
    const canceled = create(RolloutSchema, { ...first, revision: 2n, status: RolloutStatus.CANCELED });
    await act(async () => {
      firstRefresh.resolve(
        create(ListRolloutsResponseSchema, { rollouts: [canceled], pollCursor: "first-mutation-token" }),
      );
      await firstMutation;
    });
    await waitFor(() => expect(cursors).toHaveLength(3));
    expect(cursors[2]).toBe("first-mutation-token");
    expect(firstDone).toBe(true);
    expect(secondDone).toBe(false);
    expect(result.current.rollouts.map((r) => [r.id, r.revision])).toEqual([
      [10n, 1n],
      [9n, 2n],
    ]);

    const paused = create(RolloutSchema, { ...second, revision: 2n });
    await act(async () => {
      secondRefresh.resolve(
        create(ListRolloutsResponseSchema, { rollouts: [paused], pollCursor: "second-mutation-token" }),
      );
      await secondMutation;
    });
    expect(result.current.rollouts.map((r) => [r.id, r.revision])).toEqual([
      [10n, 2n],
      [9n, 2n],
    ]);
    expect(secondDone).toBe(true);
    await act(async () => {
      await result.current.refresh();
    });
    expect(cursors[3]).toBe("second-mutation-token");
    expect(result.current.rollouts.map((r) => [r.id, r.revision])).toEqual([
      [10n, 2n],
      [9n, 2n],
    ]);
  });

  it.each(revisionActions)(
    "%s sends the caller's observed revision and propagates a stale response without retrying",
    async (action, rpc, schema) => {
      const observed = create(RolloutSchema, { ...rollout, revision: 3n });
      mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [observed] }));
      rpc.mockResolvedValue({ startedRollouts: [observed], rollout: observed });
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      const revisionFromOpenDialog = result.current.rollouts[0].revision;

      const newer = create(RolloutSchema, { ...observed, revision: 9n });
      mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [newer] }));
      await act(async () => {
        await result.current.refresh();
      });
      expect(result.current.rollouts[0].revision).toBe(9n);
      await act(async () => {
        await callRevisionAction(result.current, action, { ...observed, revision: revisionFromOpenDialog });
      });
      expect(rpc).toHaveBeenCalledExactlyOnceWith({ rolloutId: 9n, expectedRevision: 3n });
      expect(toJson(schema, create(schema, rpc.mock.calls[0][0]))).toEqual({ rolloutId: "9", expectedRevision: "3" });

      rpc.mockClear();
      const stale = new ConnectError("stale rollout revision", Code.FailedPrecondition);
      rpc.mockRejectedValueOnce(stale);
      const readsBeforeFailure = mockListRollouts.mock.calls.length;
      await act(async () => {
        await expect(
          callRevisionAction(result.current, action, { ...observed, revision: revisionFromOpenDialog }),
        ).rejects.toBe(stale);
      });
      expect(rpc).toHaveBeenCalledExactlyOnceWith({ rolloutId: 9n, expectedRevision: 3n });
      expect(mockListRollouts).toHaveBeenCalledTimes(readsBeforeFailure);
      expect(result.current.rollouts[0].revision).toBe(9n);
    },
  );

  it.each(revisionActions)("%s rejects nonpositive revisions before sending any mutation", async (action, rpc) => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    for (const expectedRevision of [0n, -1n]) {
      await expect(
        callRevisionAction(result.current, action, { ...rollout, revision: expectedRevision }),
      ).rejects.toThrow("Refresh the rollout before taking this action.");
    }
    expect(rpc).not.toHaveBeenCalled();
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    expect(mockListRollouts).toHaveBeenCalledTimes(1);
  });

  it("loads and mutates rollout data when miner display names are forbidden", async () => {
    mockListMinerStateSnapshots.mockRejectedValue(new ConnectError("miner:read is required", Code.PermissionDenied));
    const initial = create(RolloutSchema, { ...rollout, revision: 2n });
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [initial] }));
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.channels).toEqual([canaryView]);
    expect(result.current.rollouts).toEqual([initial]);
    expect(result.current.minerNames).toEqual({});
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();

    const canceled = create(RolloutSchema, { ...initial, status: RolloutStatus.CANCELED, revision: 3n });
    mockCancelRollout.mockResolvedValue({ rollout: canceled });
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [canceled] }));
    await act(async () => {
      await result.current.cancelRollout(9n, initial.revision);
    });
    expect(mockCancelRollout).toHaveBeenCalledExactlyOnceWith({ rolloutId: 9n, expectedRevision: 2n });
    expect(result.current.rollouts).toEqual([canceled]);
    expect(result.current.minerNames).toEqual({});
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(1);
  });

  it("reuses fully paged names across polls and mutations until five minutes after the scan completes", async () => {
    const poll = capturePollingTimer();
    let now = Date.now();
    vi.spyOn(Date, "now").mockImplementation(() => now);
    const initialLastPage = deferred<{ miners: { deviceIdentifier: string; name: string }[]; cursor: string }>();
    let updatedNames = false;
    mockListMinerStateSnapshots.mockImplementation(({ cursor }) => {
      if (cursor && !updatedNames) return initialLastPage.promise;
      return Promise.resolve({
        miners: cursor
          ? [{ deviceIdentifier: "rig-999", name: "Renamed Z99" }]
          : [{ deviceIdentifier: "rig-001", name: updatedNames ? "Renamed A01" : "Rig A01" }],
        cursor: cursor ? "" : "names-2",
      });
    });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2));
    now += 60_000;
    await act(async () => {
      initialLastPage.resolve({ miners: [{ deviceIdentifier: "rig-999", name: "Rig Z99" }], cursor: "" });
    });
    expect(result.current.hasLoaded).toBe(true);
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01", "rig-999": "Rig Z99" });

    updatedNames = true;
    now += 5 * 60_000 - 1;
    await act(async () => {
      poll();
    });
    await act(async () => {
      await result.current.refresh();
    });
    mockCancelRollout.mockResolvedValue({});
    await act(async () => {
      await result.current.cancelRollout(9n, 1n);
    });
    expect(mockListRollouts).toHaveBeenCalledTimes(4);
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01", "rig-999": "Rig Z99" });

    now += 1;
    await act(async () => {
      poll();
    });
    expect(result.current.minerNames).toEqual({ "rig-001": "Renamed A01", "rig-999": "Renamed Z99" });
    expect(mockListMinerStateSnapshots.mock.calls).toEqual(
      ["", "names-2", "", "names-2"].map((cursor) => [{ pageSize: 1000, cursor }, pollOptions]),
    );
    await act(async () => {
      await result.current.refresh();
    });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(4);
  });

  it("loads and mutates channels while a paged names scan is stalled, then publishes only complete names", async () => {
    const poll = capturePollingTimer();
    const lastNames = deferred<{ miners: { deviceIdentifier: string; name: string }[]; cursor: string }>();
    mockListMinerStateSnapshots
      .mockResolvedValueOnce({ miners: [{ deviceIdentifier: "rig-001", name: "First name" }], cursor: "names-2" })
      .mockReturnValueOnce(lastNames.promise);
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "saved" }),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    expect(result.current.channels).toEqual([canaryView]);
    expect(result.current.minerNames).toEqual({});
    const finished = create(RolloutSchema, { ...rollout, status: RolloutStatus.CANCELED, revision: 2n });
    mockCancelRollout.mockResolvedValue({ rollout: finished });
    mockListRollouts.mockImplementation(({ status }) =>
      Promise.resolve(
        create(ListRolloutsResponseSchema, {
          rollouts: status === RolloutStatus.ACTIVE ? [] : [finished],
          pollCursor: "updated",
        }),
      ),
    );
    await act(async () => {
      poll();
    });
    await act(async () => {
      await result.current.cancelRollout(9n, 1n);
    });
    expect(mockCancelRollout).toHaveBeenCalledExactlyOnceWith({ rolloutId: 9n, expectedRevision: 1n });
    expect(result.current.rollouts).toEqual([finished]);
    expect(result.current.minerNames).toEqual({});
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
    const coreSnapshot = { channels: result.current.channels, rollouts: result.current.rollouts };
    await act(async () => {
      lastNames.resolve({ miners: [{ deviceIdentifier: "rig-002", name: "Second name" }], cursor: "" });
    });
    expect(result.current.minerNames).toEqual({ "rig-001": "First name", "rig-002": "Second name" });
    expect(result.current.channels).toBe(coreSnapshot.channels);
    expect(result.current.rollouts).toBe(coreSnapshot.rollouts);
    expect(result.current.error).toBeNull();
  });

  it("keeps complete names after a later background page fails and retries without replaying core data", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const completeNames = result.current.minerNames;
    expireMinerNames();
    const laterNames = deferred<{ miners: { deviceIdentifier: string; name: string }[]; cursor: string }>();
    mockListMinerStateSnapshots
      .mockResolvedValueOnce({ miners: [{ deviceIdentifier: "rig-002", name: "Partial" }], cursor: "names-2" })
      .mockReturnValueOnce(laterNames.promise);
    const updated = create(RolloutSchema, { ...rollout, revision: 2n });
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [updated] }));
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.rollouts).toEqual([updated]);
    expect(result.current.minerNames).toBe(completeNames);
    await act(async () => {
      laterNames.reject(new ConnectError("names unavailable", Code.Unavailable));
    });
    expect(result.current.minerNames).toBe(completeNames);
    expect(result.current.rollouts).toEqual([updated]);
    expect(result.current.error).toBeNull();
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    mockListMinerStateSnapshots.mockResolvedValue({
      miners: [{ deviceIdentifier: "rig-002", name: "Recovered" }],
      cursor: "",
    });
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.minerNames).toEqual({ "rig-002": "Recovered" });
    expect(mockListMinerStateSnapshots.mock.calls.map(([request]) => request.cursor)).toEqual(["", "", "names-2", ""]);
  });

  it("handles an independent names scan's authentication failure once across overlapping refreshes", async () => {
    const names = deferred<never>();
    mockListMinerStateSnapshots.mockReturnValueOnce(names.promise);
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    await act(async () => {
      await Promise.all([result.current.refresh(), result.current.refresh()]);
    });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledOnce();
    const error = new ConnectError("name scan session expired", Code.Unauthenticated);
    await act(async () => {
      names.reject(error);
    });
    expect(mockHandleAuthErrors).toHaveBeenCalledExactlyOnceWith({ error });
    expect(result.current.hasLoaded).toBe(true);
    expect(result.current.error).toBeNull();
  });

  it("shares a pending names scan across failed core reads and publishes complete names independently", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const previous = {
      channels: result.current.channels,
      rollouts: result.current.rollouts,
      names: result.current.minerNames,
    };
    expireMinerNames();
    const names = deferred<{ miners: { deviceIdentifier: string; name: string }[]; cursor: string }>();
    mockListMinerStateSnapshots.mockReturnValueOnce(names.promise);
    const coreError = new ConnectError("rollout service unavailable", Code.Unavailable);
    for (let attempt = 0; attempt < 2; attempt += 1) {
      mockListRollouts.mockRejectedValueOnce(coreError);
      await act(async () => {
        await expect(result.current.refresh()).rejects.toBe(coreError);
      });
      expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
    }
    await act(async () => {
      names.resolve({ miners: [{ deviceIdentifier: "rig-001", name: "Updated name" }], cursor: "" });
    });
    expect(result.current.error).toBe(coreError);
    expect(result.current.channels).toBe(previous.channels);
    expect(result.current.rollouts).toBe(previous.rollouts);
    expect(result.current.minerNames).toEqual({ "rig-001": "Updated name" });

    await act(async () => {
      await result.current.refresh();
    });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
    expect(result.current.minerNames).toEqual({ "rig-001": "Updated name" });
    expect(result.current.error).toBeNull();
  });

  it("isolates the name cache from a previous session's late scan", async () => {
    const oldNames = deferred<{ miners: { deviceIdentifier: string; name: string }[]; cursor: string }>();
    mockListMinerStateSnapshots.mockReturnValueOnce(oldNames.promise).mockResolvedValue({
      miners: [{ deviceIdentifier: "rig-002", name: "New session name" }],
      cursor: "",
    });
    const { result, rerender } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    expect(result.current.minerNames).toEqual({});
    mockAuth.sessionGeneration += 1;
    rerender();
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    expect(result.current.minerNames).toEqual({ "rig-002": "New session name" });
    await act(async () => {
      oldNames.resolve({ miners: [{ deviceIdentifier: "rig-001", name: "Old session name" }], cursor: "" });
    });
    expect(result.current.minerNames).toEqual({ "rig-002": "New session name" });
    await act(async () => {
      await result.current.refresh();
    });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(2);
    expect(result.current.minerNames).toEqual({ "rig-002": "New session name" });
  });

  it("discards stale and partial miner names when a later snapshot page is forbidden", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01" });
    expireMinerNames();
    const updated = create(RolloutSchema, { ...rollout, revision: 2n });
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [updated] }));
    mockListMinerStateSnapshots.mockImplementation(({ cursor }) =>
      cursor
        ? Promise.reject(new ConnectError("permission revoked", Code.PermissionDenied))
        : Promise.resolve({ miners: [{ deviceIdentifier: "rig-002", name: "Partial name" }], cursor: "next-page" }),
    );
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.rollouts).toEqual([updated]);
    expect(result.current.channels).toEqual([canaryView]);
    expect(result.current.minerNames).toEqual({});
    expect(mockListMinerStateSnapshots).toHaveBeenLastCalledWith({ pageSize: 1000, cursor: "next-page" }, pollOptions);
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    mockListMinerStateSnapshots.mockResolvedValue({
      miners: [{ deviceIdentifier: "rig-001", name: "Restored name" }],
      cursor: "",
    });
    await act(async () => {
      await result.current.refresh();
    });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(3);
    expect(result.current.minerNames).toEqual({});
    expireMinerNames();
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.minerNames).toEqual({ "rig-001": "Restored name" });
  });

  it.each([
    ["authentication", new ConnectError("expired", Code.Unauthenticated), true],
    ["availability", new ConnectError("offline", Code.Unavailable), false],
    ["untyped", Object.assign(new Error("denied"), { code: Code.PermissionDenied }), false],
  ] as const)(
    "keeps complete names after a miner %s failure without blocking core data",
    async (_, error, handlesAuth) => {
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const names = result.current.minerNames;
      expireMinerNames();
      mockListMinerStateSnapshots.mockRejectedValueOnce(error);
      const updated = create(RolloutSchema, { ...rollout, revision: 2n });
      mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [updated] }));
      await act(async () => {
        await result.current.refresh();
      });
      expect(result.current.rollouts).toEqual([updated]);
      expect(result.current.minerNames).toBe(names);
      expect(result.current.error).toBeNull();
      expect(mockHandleAuthErrors).toHaveBeenCalledTimes(handlesAuth ? 1 : 0);
      if (handlesAuth) expect(mockHandleAuthErrors).toHaveBeenCalledWith({ error });
    },
  );

  it("keeps firmware permission failures blocking and delegates them to auth handling", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const previous = result.current.rollouts;
    const error = new ConnectError("firmware permission revoked", Code.PermissionDenied);
    mockListRollouts.mockRejectedValueOnce(error);
    await act(async () => {
      await expect(result.current.refresh()).rejects.toBe(error);
    });
    expect(result.current.rollouts).toBe(previous);
    expect(result.current.error).toBe(error);
    expect(mockHandleAuthErrors).toHaveBeenCalledExactlyOnceWith({ error });
  });

  it.each(["success", "unauthenticated"])(
    "discards the old session's late %s without clearing the new session's pending refresh",
    async (lateResponse) => {
      const poll = capturePollingTimer();
      mockListRollouts.mockResolvedValue(
        create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "old-session-token" }),
      );
      const { result, rerender } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      const oldDelta = deferred<ListRolloutsResponse>();
      const newBaseline = deferred<ListRolloutsResponse>();
      let newBaselinePending = true;
      mockListRollouts.mockImplementation(({ status, pollCursor }) => {
        if (status === RolloutStatus.ACTIVE && pollCursor === undefined)
          return Promise.resolve(create(ListRolloutsResponseSchema));
        if (mockAuth.sessionGeneration === 1) return oldDelta.promise;
        if (newBaselinePending) {
          newBaselinePending = false;
          return newBaseline.promise;
        }
        return Promise.resolve(create(ListRolloutsResponseSchema, { pollCursor: "next-session-token" }));
      });
      let oldRefresh!: Promise<void>;
      await act(async () => {
        oldRefresh = result.current.refresh();
      });
      mockAuth.sessionGeneration = 2;
      rerender();
      expect(result.current.rollouts).toEqual([]);
      expect(result.current.channels).toEqual([]);
      expect(result.current.minerNames).toEqual({});
      expect(mockListRollouts).toHaveBeenLastCalledWith(
        { pageSize: 1000, cursor: "", pollCursor: "", status: RolloutStatus.ACTIVE },
        pollOptions,
      );
      const requestsWithNewBaselinePending = mockListRollouts.mock.calls.length;
      await act(async () => {
        if (lateResponse === "success") {
          oldDelta.resolve(create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "stale-token" }));
        } else {
          oldDelta.reject(new ConnectError("old session expired", Code.Unauthenticated));
        }
        await oldRefresh.catch(() => undefined);
        poll();
      });
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
      expect(mockListRollouts).toHaveBeenCalledTimes(requestsWithNewBaselinePending);
      expect(result.current.rollouts).toEqual([]);
      expect(result.current.isLoading).toBe(true);
      const currentRollout = create(RolloutSchema, { ...rollout, id: 99n });
      await act(async () => {
        newBaseline.resolve(
          create(ListRolloutsResponseSchema, { rollouts: [currentRollout], pollCursor: "new-session-token" }),
        );
      });
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      expect(result.current.rollouts).toEqual([currentRollout]);
      await act(async () => {
        await result.current.refresh();
      });
      expect(mockListRollouts).toHaveBeenCalledWith(
        { pageSize: 1000, cursor: "", pollCursor: "new-session-token" },
        pollOptions,
      );
      expect(result.current.rollouts).toEqual([currentRollout]);
    },
  );

  it("clears session data and skips polling after logout, then starts an active baseline on login", async () => {
    const poll = capturePollingTimer();
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "old-session-token" }),
    );
    const { result, rerender } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    mockAuth.isAuthenticated = false;
    mockAuth.sessionGeneration = 2;
    rerender();
    expect(result.current.channels).toEqual([]);
    expect(result.current.rollouts).toEqual([]);
    expect(result.current.minerNames).toEqual({});
    await act(async () => {
      poll();
    });
    expect(mockListRollouts).toHaveBeenCalledTimes(1);
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { pollCursor: "login-token" }));
    mockAuth.isAuthenticated = true;
    mockAuth.sessionGeneration = 3;
    rerender();
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(mockListRollouts).toHaveBeenCalledTimes(2);
    expect(mockListRollouts).toHaveBeenLastCalledWith(
      { pageSize: 1000, cursor: "", pollCursor: "", status: RolloutStatus.ACTIVE },
      pollOptions,
    );
    expect(result.current.rollouts).toEqual([]);
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
    expect(mockListRollouts).toHaveBeenCalledTimes(1);
    await act(async () => {
      poll();
    });
    expect(result.current.channels).toEqual([canaryView]);
    const previous = result.current.channels;

    const pollError = new ConnectError("session revoked", Code.Unauthenticated);
    mockListRollouts.mockRejectedValueOnce(pollError);
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
      const freshRollout = create(RolloutSchema, { ...rollout, revision: 2n });
      const freshRolloutPage = deferred<ListRolloutsResponse>();
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
        mockListRollouts.mockReturnValueOnce(freshRolloutPage.promise);
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
      expect(mockGetReleaseChannel).not.toHaveBeenCalledWith({ channelId: 2n }, channelLoadOptions);
      expect(creationCompleted).toBe(false);
      expect(mockListReleaseChannels).toHaveBeenCalledTimes(2);

      await act(async () => {
        if (priorPollResult === "fails") {
          oldPoll.reject(new Error("previous poll unavailable"));
        } else {
          oldPoll.resolve(oldSnapshot);
        }
      });
      await waitFor(() => expect(mockListRollouts).toHaveBeenCalledTimes(3));
      expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(1);
      expect(creationCompleted).toBe(false);
      expect(result.current.channels).toEqual([canaryView]);

      await act(async () => {
        freshRolloutPage.resolve(create(ListRolloutsResponseSchema, { rollouts: [freshRollout] }));
        await creation;
      });
      expect(creationCompleted).toBe(true);
      expect(result.current.channels).toEqual([canaryView, { ...stable, modelGroups: [] }]);
      expect(result.current.rollouts).toEqual([freshRollout]);
      expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01" });
      expect(mockListReleaseChannels).toHaveBeenCalledTimes(3);
    },
  );

  it("loads channels with their scope and paged groups, rollouts and miner names together", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    expect(result.current.isLoading).toBe(true);

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.channels).toEqual([canaryView]);
    expect(mockGetReleaseChannel).toHaveBeenCalledWith({ channelId: 1n }, channelLoadOptions);
    expect(mockListReleaseChannelModelGroups).toHaveBeenCalledWith(
      { channelId: 1n, pageSize: 100, cursor: "" },
      channelLoadOptions,
    );
    expect(result.current.rollouts).toEqual([rollout]);
    expect(result.current.minerNames).toEqual({ "rig-001": "Rig A01" });
    expect(mockListMinerStateSnapshots).toHaveBeenCalledWith({ pageSize: 1000, cursor: "" }, pollOptions);
  });

  it("retains newly completed rollouts across paged deltas and empty polls while refreshing active evidence", async () => {
    const active = create(RolloutSchema, {
      ...rollout,
      id: 1n,
      revision: 5n,
      createdAt: create(TimestampSchema, { seconds: 10n, nanos: 10 }),
    });
    const completed = create(RolloutSchema, {
      ...rollout,
      id: 2n,
      revision: 2n,
      status: RolloutStatus.COMPLETED,
      createdAt: create(TimestampSchema, { seconds: 10n, nanos: 30 }),
    });
    const tied = create(RolloutSchema, { ...completed, id: 3n });
    const finished = create(RolloutSchema, { ...active, revision: 6n, status: RolloutStatus.COMPLETED });
    const started = create(RolloutSchema, {
      ...rollout,
      id: 4n,
      revision: 1n,
      createdAt: create(TimestampSchema, { seconds: 11n, nanos: 0 }),
    });
    let cycle = 0;
    mockListRollouts.mockImplementation(({ cursor, status }) => {
      if (cycle === 0) {
        return Promise.resolve(
          create(ListRolloutsResponseSchema, {
            rollouts: status === RolloutStatus.ACTIVE ? (cursor ? [] : [active]) : [completed, tied],
            cursor: cursor ? "" : "baseline-2",
            pollCursor: "baseline-token",
          }),
        );
      }
      if (status === RolloutStatus.ACTIVE) {
        return Promise.resolve(
          create(ListRolloutsResponseSchema, {
            rollouts: cursor ? [active] : [create(RolloutSchema, { ...started, deviceCount: cycle === 1 ? 10 : 11 })],
            cursor: cycle === 1 && !cursor ? "active-2" : "",
            pollCursor: "ignore-active-token",
          }),
        );
      }
      return Promise.resolve(
        create(ListRolloutsResponseSchema, {
          rollouts: cycle === 1 ? (cursor ? [started] : [finished, completed, tied]) : [],
          cursor: cycle === 1 && !cursor ? "delta-2" : "",
          pollCursor: cycle === 1 ? "delta-token" : "empty-token",
        }),
      );
    });
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.rollouts.map((r) => r.id)).toEqual([1n]);
    cycle = 1;
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.rollouts.map((r) => r.id)).toEqual([4n, 3n, 2n, 1n]);
    expect(result.current.rollouts.find((r) => r.id === 1n)).toEqual(finished);
    expect(result.current.rollouts[0].deviceCount).toBe(10);
    cycle = 2;
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.rollouts.map((r) => r.id)).toEqual([4n, 3n, 2n, 1n]);
    expect(result.current.rollouts[0].deviceCount).toBe(11);
    expect(result.current.rollouts.find((r) => r.id === 1n)).toEqual(finished);
    expect(mockListRollouts.mock.calls.map(([request]) => request)).toEqual(
      expect.arrayContaining([
        { pageSize: 1000, cursor: "", pollCursor: "", status: RolloutStatus.ACTIVE },
        { pageSize: 1000, cursor: "baseline-2", pollCursor: "", status: RolloutStatus.ACTIVE },
        { pageSize: 1000, cursor: "", pollCursor: "baseline-token" },
        { pageSize: 1000, cursor: "delta-2", pollCursor: "baseline-token" },
        { pageSize: 1000, cursor: "", status: RolloutStatus.ACTIVE },
        { pageSize: 1000, cursor: "active-2", status: RolloutStatus.ACTIVE },
        { pageSize: 1000, cursor: "", pollCursor: "delta-token" },
      ]),
    );
    expect(mockListRollouts).toHaveBeenCalledTimes(8);
    cycle = 3;
    await act(async () => {
      await result.current.refresh();
    });
    expect(mockListRollouts).toHaveBeenCalledWith(
      { pageSize: 1000, cursor: "", pollCursor: "empty-token" },
      pollOptions,
    );
  });

  it("reads current channel membership after rollout scans, pruning deleted channels and overlaying renamed channels", async () => {
    const removed = create(ReleaseChannelSchema, { id: 2n, name: "Removed" });
    const removedRollout = create(RolloutSchema, { ...rollout, id: 20n, channelId: 2n, channelName: removed.name });
    let channels = [canary, removed];
    mockListReleaseChannels.mockImplementation(() =>
      Promise.resolve(
        create(ListReleaseChannelsResponseSchema, {
          channels: channels.map((channel) =>
            create(ReleaseChannelSummarySchema, { id: channel.id, name: channel.name }),
          ),
        }),
      ),
    );
    mockGetReleaseChannel.mockImplementation(({ channelId }) =>
      Promise.resolve({ channel: channels.find((c) => c.id === channelId) }),
    );
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollout, removedRollout], pollCursor: "initial" }),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.rollouts).toHaveLength(2);
    const delta = deferred<ListRolloutsResponse>();
    mockListRollouts.mockImplementation(({ status }) =>
      status === RolloutStatus.ACTIVE
        ? Promise.resolve(create(ListRolloutsResponseSchema, { pollCursor: "unused" }))
        : delta.promise,
    );
    let refresh!: Promise<void>;
    await act(async () => {
      refresh = result.current.refresh();
    });
    expect(mockListReleaseChannels).toHaveBeenCalledTimes(1);
    const added = create(ReleaseChannelSchema, { id: 3n, name: "New channel" });
    channels = [create(ReleaseChannelSchema, { ...canary, name: "Renamed" }), added];
    const newRollout = create(RolloutSchema, { ...rollout, id: 30n, channelId: 3n, channelName: "Old channel label" });
    await act(async () => {
      delta.resolve(create(ListRolloutsResponseSchema, { rollouts: [newRollout], pollCursor: "next" }));
      await refresh;
    });
    expect(result.current.rollouts.map((r) => [r.id, r.channelName])).toEqual([
      [30n, "New channel"],
      [9n, "Renamed"],
    ]);
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { pollCursor: "empty" }));
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.rollouts.map((r) => r.id)).toEqual([30n, 9n]);
  });

  it.each(["delta page", "active page", "channel list", "channel detail"])(
    "preserves the rollout watermark and complete snapshot when a later %s fails",
    async (failurePoint) => {
      const initial = create(RolloutSchema, { ...rollout, revision: 1n });
      mockListRollouts.mockResolvedValue(
        create(ListRolloutsResponseSchema, { rollouts: [initial], pollCursor: "saved-token" }),
      );
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      const previous = {
        channels: result.current.channels,
        rollouts: result.current.rollouts,
        miners: result.current.minerNames,
      };
      const updated = create(RolloutSchema, { ...initial, revision: 2n, status: RolloutStatus.COMPLETED });
      const added = create(RolloutSchema, { ...rollout, id: 10n, revision: 1n });
      const error = new Error("refresh interrupted");
      let shouldFail = true;
      mockListRollouts.mockImplementation(({ cursor, status }) => {
        if (status === RolloutStatus.ACTIVE) {
          if (cursor && failurePoint === "active page" && shouldFail) return Promise.reject(error);
          return Promise.resolve(
            create(ListRolloutsResponseSchema, {
              cursor: failurePoint === "active page" && !cursor ? "active-2" : "",
              pollCursor: "unused",
            }),
          );
        }
        if (cursor && failurePoint === "delta page" && shouldFail) return Promise.reject(error);
        return Promise.resolve(
          create(ListRolloutsResponseSchema, {
            rollouts: cursor ? [added] : [updated],
            cursor: cursor ? "" : "delta-2",
            pollCursor: "advanced-token",
          }),
        );
      });
      if (failurePoint === "channel list") mockListReleaseChannels.mockRejectedValueOnce(error);
      if (failurePoint === "channel detail") mockGetReleaseChannel.mockRejectedValueOnce(error);
      await act(async () => {
        await expect(result.current.refresh()).rejects.toBe(error);
      });
      expect(result.current.channels).toBe(previous.channels);
      expect(result.current.rollouts).toBe(previous.rollouts);
      expect(result.current.minerNames).toBe(previous.miners);
      shouldFail = false;
      await act(async () => {
        await result.current.refresh();
      });
      expect(result.current.rollouts).toEqual([added, updated]);
      const deltaRequests = mockListRollouts.mock.calls
        .slice(1)
        .map(([request]) => request)
        .filter((request) => request.status === undefined);
      expect(deltaRequests).toHaveLength(4);
      expect(deltaRequests.map((request) => [request.cursor, request.pollCursor])).toEqual([
        ["", "saved-token"],
        ["delta-2", "saved-token"],
        ["", "saved-token"],
        ["delta-2", "saved-token"],
      ]);
      await act(async () => {
        await result.current.refresh();
      });
      expect(mockListRollouts).toHaveBeenCalledWith(
        { pageSize: 1000, cursor: "", pollCursor: "advanced-token" },
        pollOptions,
      );
    },
  );

  it("drains all active baseline pages, including empty pages, without requesting historical rollouts", async () => {
    const second = create(RolloutSchema, { ...rollout, id: 10n });
    const historic = create(RolloutSchema, { ...rollout, id: 8n, status: RolloutStatus.COMPLETED });
    mockListRollouts.mockImplementation(({ cursor, status }) =>
      Promise.resolve(
        create(ListRolloutsResponseSchema, {
          rollouts:
            status !== RolloutStatus.ACTIVE
              ? [historic]
              : cursor === ""
                ? [rollout]
                : cursor === "active-3"
                  ? [second]
                  : [],
          cursor: cursor === "" ? "active-2" : cursor === "active-2" ? "active-3" : "",
          pollCursor: "active-baseline-token",
        }),
      ),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    expect(result.current.rollouts).toEqual([second, rollout]);
    expect(mockListRollouts.mock.calls).toEqual(
      ["", "active-2", "active-3"].map((cursor) => [
        { pageSize: 1000, cursor, pollCursor: "", status: RolloutStatus.ACTIVE },
        pollOptions,
      ]),
    );
  });

  it.each(["rollouts"])(
    "retains the completed snapshot while a later %s page is pending or fails, then retries from the beginning",
    async (pagedList) => {
      const { result } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      const previous = {
        channels: result.current.channels,
        rollouts: result.current.rollouts,
        minerNames: result.current.minerNames,
      };
      expireMinerNames();
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
      expect(result.current.minerNames).toEqual({ "rig-002": "Rig B02", "rig-999": "Rig Z99" });

      const rejection = expect(refresh).rejects.toThrow("later page unavailable");
      await act(async () => {
        rejectPage(new Error("later page unavailable"));
        await rejection;
      });
      expect(result.current.channels).toBe(previous.channels);
      expect(result.current.rollouts).toBe(previous.rollouts);
      expect(result.current.minerNames).toEqual({ "rig-002": "Rig B02", "rig-999": "Rig Z99" });
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
      expect(result.current.rollouts).toEqual(
        [completed, rollout].map((r) => ({ ...r, channelName: renamedChannel.name })),
      );
      expect(result.current.minerNames).toEqual({ "rig-002": "Rig B02", "rig-999": "Rig Z99" });
      const mockPagedList = pagedList === "rollouts" ? mockListRollouts : mockListMinerStateSnapshots;
      expect(mockPagedList.mock.calls.slice(1).map(([request]) => request.cursor)).toEqual([
        "",
        "later-page",
        "",
        "later-page",
      ]);
      const completedNameReads = mockListMinerStateSnapshots.mock.calls.length;
      await act(async () => {
        await result.current.refresh();
      });
      expect(mockListMinerStateSnapshots).toHaveBeenCalledTimes(completedNameReads);
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
    expect(mockListReleaseChannels).toHaveBeenNthCalledWith(1, { pageSize: 1000, cursor: "" }, pollOptions);
    expect(mockListReleaseChannels).toHaveBeenNthCalledWith(2, { pageSize: 1000, cursor: "page-2" }, pollOptions);
    expect(mockGetReleaseChannel).toHaveBeenCalledWith({ channelId: 2n }, channelLoadOptions);
    expect(mockListReleaseChannelModelGroups).toHaveBeenCalledWith(
      { channelId: 2n, pageSize: 100, cursor: "" },
      channelLoadOptions,
    );

    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.channels).toEqual(expectedViews);
    expect(mockListReleaseChannels).toHaveBeenCalledTimes(4);
    expect(mockListReleaseChannels).toHaveBeenNthCalledWith(3, { pageSize: 1000, cursor: "" }, pollOptions);
    expect(mockListReleaseChannels).toHaveBeenNthCalledWith(4, { pageSize: 1000, cursor: "page-2" }, pollOptions);
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
    expect(created).toEqual(canary);
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

    const controller = new AbortController();
    const preview = await result.current.previewScope({ siteIds: [1n] } as never, 7n, controller.signal);
    expect(preview.minerCount).toBe(4);
    expect(mockPreviewReleaseChannelScope).toHaveBeenCalledWith(
      { scope: { siteIds: [1n] }, channelId: 7n },
      { timeoutMs: 30_000, signal: controller.signal },
    );
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
      await result.current.cancelRollout(9n, 4n);
      const retried = await result.current.retryFailedDevices(9n, 5n);
      expect(retried).toEqual(rollout);
    });
    expect(mockApplyReleaseChannelFirmware).toHaveBeenCalledWith({
      channelId: 1n,
      assignments: [{ manufacturer: "Proto", model: "Rig", firmwareFileId: "fw-2" }],
    });
    expect(mockCancelRollout).toHaveBeenCalledWith({ rolloutId: 9n, expectedRevision: 4n });
    expect(mockRetryFailedRolloutDevices).toHaveBeenCalledWith({ rolloutId: 9n, expectedRevision: 5n });
  });

  it("walks every page of the detail lists", async () => {
    const controller = new AbortController();
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
      const miners = await result.current.listChannelMiners(1n, "Proto", "Rig", controller.signal);
      expect(miners.map((m) => m.deviceIdentifier)).toEqual(["rig-001", "rig-002"]);
      const devices = await result.current.listRolloutDevices(9n, controller.signal);
      expect(devices.map((d) => d.deviceIdentifier)).toEqual(["rig-001", "rig-002"]);
    });
    const options = { timeoutMs: 30_000, signal: controller.signal };
    expect(mockListReleaseChannelMiners).toHaveBeenNthCalledWith(
      1,
      {
        channelId: 1n,
        manufacturer: "Proto",
        model: "Rig",
        pageSize: 1000,
        cursor: "",
      },
      options,
    );
    expect(mockListReleaseChannelMiners).toHaveBeenNthCalledWith(
      2,
      {
        channelId: 1n,
        manufacturer: "Proto",
        model: "Rig",
        pageSize: 1000,
        cursor: "p2",
      },
      options,
    );
    expect(mockListRolloutDevices).toHaveBeenCalledTimes(3);
    expect(mockListRolloutDevices).toHaveBeenLastCalledWith({ rolloutId: 9n, pageSize: 1000, cursor: "d3" }, options);
    expect(
      mockListRolloutDevices.mock.calls.every(
        ([, options]) => options.timeoutMs === 30_000 && options.signal === controller.signal,
      ),
    ).toBe(true);
  });

  it("reads a channel's complete history only on demand without changing its live snapshot or watermark", async () => {
    mockListRollouts.mockResolvedValue(
      create(ListRolloutsResponseSchema, { rollouts: [rollout], pollCursor: "live-token" }),
    );
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const previous = { channels: result.current.channels, rollouts: result.current.rollouts };
    const historical = create(RolloutSchema, { ...rollout, id: 3n, status: RolloutStatus.COMPLETED });
    const oldest = create(RolloutSchema, { ...historical, id: 2n, status: RolloutStatus.CANCELED });
    mockListRollouts.mockImplementation(({ channelId, cursor }) =>
      Promise.resolve(
        create(ListRolloutsResponseSchema, {
          rollouts: channelId ? (cursor === "" ? [historical] : cursor === "history-3" ? [oldest] : []) : [],
          cursor: channelId ? (cursor === "" ? "history-2" : cursor === "history-2" ? "history-3" : "") : "",
          pollCursor: "must-not-be-used",
        }),
      ),
    );
    const controller = new AbortController();
    await expect(result.current.listChannelRollouts(1n, controller.signal)).resolves.toEqual([historical, oldest]);
    expect(mockListRollouts.mock.calls.slice(1)).toEqual(
      ["", "history-2", "history-3"].map((cursor) => [
        { channelId: 1n, pageSize: 1000, cursor },
        { timeoutMs: 30_000, signal: controller.signal },
      ]),
    );
    expect(result.current.channels).toBe(previous.channels);
    expect(result.current.rollouts).toBe(previous.rollouts);
    expect(result.current.error).toBeNull();
    await act(async () => {
      await result.current.refresh();
    });
    expect(mockListRollouts).toHaveBeenCalledWith(
      { pageSize: 1000, cursor: "", pollCursor: "live-token" },
      pollOptions,
    );
  });

  it("times out a later history page through Connect and retries the complete channel scan", async () => {
    capturePollingTimer();
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const previous = result.current.rollouts;
    const historical = create(RolloutSchema, { ...rollout, status: RolloutStatus.COMPLETED });
    const stalled = stalledRolloutTransport(
      create(ListRolloutsResponseSchema, { rollouts: [historical], cursor: "stalled-page" }),
    );
    mockListRollouts.mockImplementation(stalled.client.listRollouts);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const request = result.current.listChannelRollouts(1n);
    const rejected = expect(request).rejects.toMatchObject({ code: Code.DeadlineExceeded });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(stalled.signals).toHaveLength(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
      await rejected;
    });
    expect(stalled.signals[0].aborted).toBe(true);
    expect(result.current.rollouts).toBe(previous);
    expect(result.current.error).toBeNull();
    mockListRollouts.mockResolvedValue(create(ListRolloutsResponseSchema, { rollouts: [historical] }));
    await expect(result.current.listChannelRollouts(1n)).resolves.toEqual([historical]);
    expect(mockListRollouts).toHaveBeenLastCalledWith(
      { channelId: 1n, pageSize: 1000, cursor: "" },
      { timeoutMs: 30_000, signal: undefined },
    );
  });

  it("aborts a pending history transport without publishing its partial first page", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const stalled = stalledRolloutTransport(
      create(ListRolloutsResponseSchema, { rollouts: [rollout], cursor: "next-page" }),
    );
    mockListRollouts.mockImplementation(stalled.client.listRollouts);
    const controller = new AbortController();
    const rejected = expect(result.current.listChannelRollouts(1n, controller.signal)).rejects.toMatchObject({
      name: "AbortError",
    });
    await waitFor(() => expect(stalled.signals).toHaveLength(1));
    controller.abort();
    await rejected;
    expect(stalled.signals[0].aborted).toBe(true);
    expect(mockListRollouts).toHaveBeenCalledTimes(3);
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    expect(result.current.error).toBeNull();
  });

  it("does not dispatch a pre-aborted history request", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const controller = new AbortController();
    const reason = new Error("history closed");
    controller.abort(reason);
    await expect(result.current.listChannelRollouts(1n, controller.signal)).rejects.toBe(reason);
    expect(mockListRollouts).toHaveBeenCalledOnce();
  });

  it.each(["canceled", "new login", "new login before rerender", "unmounted"] as const)(
    "rejects a late final history page after the read is %s without dispatching more pages",
    async (change) => {
      const { result, rerender, unmount } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const page = deferred<ListRolloutsResponse>();
      mockListRollouts.mockReturnValueOnce(page.promise);
      const controller = new AbortController();
      const pending = result.current.listChannelRollouts(1n, controller.signal);
      const rejected =
        change === "canceled"
          ? expect(pending).rejects.toMatchObject({ name: "AbortError" })
          : expect(pending).rejects.toThrow("Your session changed");
      if (change === "canceled") controller.abort();
      else if (change === "unmounted") unmount();
      else {
        mockAuth.sessionGeneration += 1;
        if (change === "new login") rerender();
      }
      await act(async () => {
        page.resolve(create(ListRolloutsResponseSchema, { rollouts: [rollout] }));
        await rejected;
      });
      expect(mockListRollouts.mock.calls.filter(([request]) => request.channelId === 1n)).toHaveLength(1);
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    },
  );

  it("stops channel history pagination when authentication changes before React rerenders", async () => {
    const { result } = renderHook(() => useReleaseChannels());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const page = deferred<ListRolloutsResponse>();
    mockListRollouts.mockReturnValueOnce(page.promise);
    const rejected = expect(result.current.listChannelRollouts(1n)).rejects.toThrow("Your session changed");
    mockAuth.sessionGeneration += 1;
    page.resolve(create(ListRolloutsResponseSchema, { cursor: "must-not-load" }));
    await rejected;
    expect(mockListRollouts).toHaveBeenCalledTimes(2);
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
  });

  it.each(["current", "canceled", "new login before rerender", "unmounted"] as const)(
    "handles a later history-page authentication failure only for a %s request",
    async (change) => {
      const { result, unmount } = renderHook(() => useReleaseChannels());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      const page = deferred<never>();
      mockListRollouts
        .mockResolvedValueOnce(create(ListRolloutsResponseSchema, { rollouts: [rollout], cursor: "history-2" }))
        .mockReturnValueOnce(page.promise);
      const controller = new AbortController();
      const error = new ConnectError("history session expired", Code.Unauthenticated);
      const pending = result.current.listChannelRollouts(1n, controller.signal);
      const rejected =
        change === "canceled"
          ? expect(pending).rejects.toMatchObject({ name: "AbortError" })
          : expect(pending).rejects.toBe(error);
      await waitFor(() => expect(mockListRollouts).toHaveBeenCalledTimes(3));
      if (change === "canceled") controller.abort();
      else if (change === "unmounted") unmount();
      else if (change === "new login before rerender") mockAuth.sessionGeneration += 1;
      page.reject(error);
      await rejected;
      expect(mockHandleAuthErrors).toHaveBeenCalledTimes(change === "current" ? 1 : 0);
      expect(result.current.error).toBeNull();
    },
  );
});
