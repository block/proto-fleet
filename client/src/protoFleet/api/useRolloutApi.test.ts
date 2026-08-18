import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import {
  InitialFirmwareMatchStatus,
  RolloutLaneChannelSchema,
  RolloutLanePreviewMinerSchema,
  RolloutLanePreviewSchema,
  RolloutLaneSchema,
  RolloutSchema,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const rolloutClientMock = vi.hoisted(() => ({
  previewRolloutLane: vi.fn(),
  createRolloutLane: vi.fn(),
  getRolloutLane: vi.fn(),
  listRolloutLanes: vi.fn(),
  startRolloutLane: vi.fn(),
  createRollout: vi.fn(),
  getRollout: vi.fn(),
  listRollouts: vi.fn(),
  admitRollout: vi.fn(),
  continueRollout: vi.fn(),
  pauseRollout: vi.fn(),
  resumeRollout: vi.fn(),
  abortRollout: vi.fn(),
  revertRollout: vi.fn(),
  completeRollout: vi.fn(),
}));
const deviceSetClientMock = vi.hoisted(() => ({
  getDeviceSet: vi.fn(),
  listDeviceSetMembers: vi.fn(),
}));
const handleAuthErrorsMock = vi.hoisted(() => vi.fn());
const permissionsMock = vi.hoisted(() => ({ current: [] as string[] }));

vi.mock("@/protoFleet/api/clients", () => ({
  deviceSetClient: deviceSetClientMock,
  rolloutClient: rolloutClientMock,
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: handleAuthErrorsMock }),
  useHasPermission: (permission: string) => permissionsMock.current.includes(permission),
}));

const { useRolloutApi } = await import("@/protoFleet/api/useRolloutApi");

function protoRollout(id: string, state = RolloutState.RUNNING, revision = 1n) {
  return create(RolloutSchema, {
    rolloutId: id,
    name: `Rollout ${id}`,
    strategyKey: "fixture-strategy",
    state,
    revision,
  });
}

function protoLane(id = "15bc6181-07d8-45ac-8424-50b5e938b871") {
  return create(RolloutLaneSchema, {
    laneId: id,
    label: "Stable production",
    currentChannelId: 41n,
    revision: 2n,
    channels: [
      create(RolloutLaneChannelSchema, {
        channelId: 41n,
        releaseSetId: 7n,
        position: 0,
        current: true,
      }),
    ],
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("useRolloutApi", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    permissionsMock.current = ["channel:read", "channel:manage", "rollout:read", "rollout:manage", "rollout:control"];
    deviceSetClientMock.getDeviceSet.mockResolvedValue({
      deviceSet: {
        id: 41n,
        deviceCount: 2,
        typeDetails: {
          case: "channelInfo",
          value: {
            releaseSetId: 7n,
            releaseTargets: [
              {
                firmwareFileId: "file-alpha",
                targetManufacturer: "Proto",
                targetModel: "Alpha",
                firmwareVersion: "1.0.0",
                sha256: "abc",
              },
            ],
          },
        },
      },
    });
    deviceSetClientMock.listDeviceSetMembers.mockResolvedValue({
      members: [{ deviceIdentifier: "miner-1" }, { deviceIdentifier: "miner-2" }],
      nextPageToken: "",
    });
  });

  it("lists, creates, and loads hydrated rollout lanes through real RPCs", async () => {
    rolloutClientMock.listRolloutLanes.mockResolvedValue({ lanes: [protoLane()] });
    rolloutClientMock.createRolloutLane.mockResolvedValue({ lane: protoLane("f209bd52-d1c8-46a2-b76a-07ecf8426476") });
    rolloutClientMock.getRolloutLane.mockResolvedValue({ lane: protoLane() });
    const { result } = renderHook(() => useRolloutApi());

    await act(async () => {
      await result.current.listRolloutLanes();
    });
    await act(async () => {
      await result.current.createRolloutLane({
        label: "Stable production",
        description: "Production firmware lane",
        firmwareFileIds: ["file-alpha"],
        deviceIdentifiers: ["miner-1", "miner-2"],
        idempotencyKey: "create-lane-one",
      });
    });
    await act(async () => {
      await result.current.getRolloutLane({ laneId: "15bc6181-07d8-45ac-8424-50b5e938b871" });
    });

    expect(rolloutClientMock.listRolloutLanes).toHaveBeenCalledTimes(1);
    expect(rolloutClientMock.createRolloutLane.mock.calls[0][0]).toMatchObject({
      label: "Stable production",
      description: "Production firmware lane",
      firmwareFileIds: ["file-alpha"],
      deviceIdentifiers: ["miner-1", "miner-2"],
      idempotencyKey: "create-lane-one",
    });
    expect(rolloutClientMock.getRolloutLane.mock.calls[0][0]).toMatchObject({
      laneId: "15bc6181-07d8-45ac-8424-50b5e938b871",
    });
    expect(result.current.lane).toMatchObject({
      label: "Stable production",
      memberCount: 2,
      memberIdentifiers: ["miner-1", "miner-2"],
      currentReleaseTargets: [{ firmwareFileId: "file-alpha", firmwareVersion: "1.0.0" }],
    });
    expect(deviceSetClientMock.listDeviceSetMembers).toHaveBeenCalledTimes(1);
  });

  it("previews initial convergence and sends confirmation only on confirmed create", async () => {
    rolloutClientMock.previewRolloutLane.mockResolvedValue({
      preview: create(RolloutLanePreviewSchema, {
        matchingCount: 1,
        mismatchedCount: 1,
        unknownCount: 1,
        miners: [
          create(RolloutLanePreviewMinerSchema, {
            deviceIdentifier: "miner-1",
            manufacturer: "Proto",
            model: "Alpha",
            currentFirmwareVersion: "1.0.0",
            targetFirmwareVersion: "2.0.0",
            targetFirmwareFileId: "file-alpha",
            status: InitialFirmwareMatchStatus.MISMATCHED,
          }),
        ],
      }),
    });
    rolloutClientMock.createRolloutLane.mockResolvedValue({ lane: protoLane() });
    const { result } = renderHook(() => useRolloutApi());
    const selection = {
      firmwareFileIds: ["file-alpha"],
      deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
    };

    let preview!: Awaited<ReturnType<typeof result.current.previewRolloutLane>>;
    await act(async () => {
      preview = await result.current.previewRolloutLane(selection);
    });
    await act(async () => {
      await result.current.createRolloutLane({
        label: "Stable production",
        description: "",
        ...selection,
        idempotencyKey: "create-confirmed",
        confirmInitialEnforcement: true,
      });
    });

    expect(rolloutClientMock.previewRolloutLane.mock.calls[0][0]).toMatchObject(selection);
    expect(preview).toMatchObject({
      matchingCount: 1,
      mismatchedCount: 1,
      unknownCount: 1,
      miners: [
        {
          deviceIdentifier: "miner-1",
          currentFirmwareVersion: "1.0.0",
          targetFirmwareVersion: "2.0.0",
          status: "mismatched",
        },
      ],
    });
    expect(rolloutClientMock.createRolloutLane.mock.calls[0][0]).toMatchObject({
      idempotencyKey: "create-confirmed",
      confirmInitialEnforcement: true,
    });
  });

  it("refreshes a lane pointer and its current release count without remounting", async () => {
    const advancedLane = create(RolloutLaneSchema, {
      laneId: "15bc6181-07d8-45ac-8424-50b5e938b871",
      label: "Stable production",
      currentChannelId: 42n,
      revision: 3n,
      channels: [
        create(RolloutLaneChannelSchema, {
          channelId: 41n,
          releaseSetId: 7n,
          position: 0,
        }),
        create(RolloutLaneChannelSchema, {
          channelId: 42n,
          releaseSetId: 8n,
          position: 1,
          current: true,
        }),
      ],
    });
    rolloutClientMock.listRolloutLanes
      .mockResolvedValueOnce({ lanes: [protoLane()] })
      .mockResolvedValueOnce({ lanes: [advancedLane] });
    deviceSetClientMock.getDeviceSet.mockImplementation(async ({ deviceSetId }: { deviceSetId: bigint }) => ({
      deviceSet: {
        id: deviceSetId,
        deviceCount: deviceSetId === 42n ? 3 : 2,
        typeDetails: {
          case: "channelInfo",
          value: {
            releaseTargets: [
              {
                firmwareFileId: deviceSetId === 42n ? "file-beta" : "file-alpha",
                targetManufacturer: "Proto",
                targetModel: "Alpha",
                firmwareVersion: deviceSetId === 42n ? "2.0.0" : "1.0.0",
                sha256: deviceSetId === 42n ? "def" : "abc",
              },
            ],
          },
        },
      },
    }));
    const { result } = renderHook(() => useRolloutApi());

    await act(async () => {
      await result.current.listRolloutLanes();
      await result.current.listRolloutLanes();
    });

    expect(result.current.lanes[0]).toMatchObject({
      currentChannelId: 42n,
      revision: 3n,
      memberCount: 3,
      currentReleaseTargets: [{ firmwareFileId: "file-beta", firmwareVersion: "2.0.0" }],
    });
  });

  it("starts the selected lane with exact target files and frozen batch assignments", async () => {
    rolloutClientMock.startRolloutLane.mockResolvedValue({
      lane: protoLane(),
      rollout: protoRollout("created", RolloutState.CREATED),
    });
    const { result } = renderHook(() => useRolloutApi());

    await act(async () => {
      await result.current.startRolloutLane({
        laneId: "15bc6181-07d8-45ac-8424-50b5e938b871",
        name: "Production 2.0.0",
        firmwareFileIds: ["file-alpha-2", "file-beta-2"],
        batches: [
          {
            label: "Pilot",
            members: [{ deviceIdentifier: "miner-2" }],
          },
          {
            label: "Remaining",
            members: [{ deviceIdentifier: "miner-1" }, { deviceIdentifier: "miner-3" }],
          },
        ],
        idempotencyKey: "start-lane-one",
        reason: "Deploy validated release",
      });
    });

    expect(rolloutClientMock.startRolloutLane.mock.calls[0][0]).toMatchObject({
      laneId: "15bc6181-07d8-45ac-8424-50b5e938b871",
      name: "Production 2.0.0",
      firmwareFileIds: ["file-alpha-2", "file-beta-2"],
      batches: [
        {
          label: "Pilot",
          members: [{ deviceIdentifier: "miner-2" }],
        },
        {
          label: "Remaining",
          members: [{ deviceIdentifier: "miner-1" }, { deviceIdentifier: "miner-3" }],
        },
      ],
      idempotencyKey: "start-lane-one",
      reason: "Deploy validated release",
    });
    expect(result.current.rollout).toMatchObject({ id: "created", state: "created" });
  });

  it("lists and gets mapped rollout records", async () => {
    rolloutClientMock.listRollouts.mockResolvedValue({ rollouts: [protoRollout("one")] });
    rolloutClientMock.getRollout.mockResolvedValue({ rollout: protoRollout("two", RolloutState.PAUSED, 2n) });
    const { result } = renderHook(() => useRolloutApi());

    await act(async () => {
      await result.current.listRollouts({ states: ["running"] });
    });
    await act(async () => {
      await result.current.getRollout({ rolloutId: "two" });
    });

    expect(rolloutClientMock.listRollouts.mock.calls[0][0].states).toEqual([RolloutState.RUNNING]);
    expect(result.current.rollouts.map((rollout) => rollout.id)).toEqual(["two", "one"]);
    expect(result.current.rollout).toMatchObject({ id: "two", state: "paused", revision: 2n });
  });

  it("merges a bounded get refresh into existing rollout history", async () => {
    rolloutClientMock.listRollouts.mockResolvedValue({
      rollouts: [protoRollout("history", RolloutState.COMPLETED), protoRollout("live", RolloutState.RUNNING, 1n)],
    });
    rolloutClientMock.getRollout.mockResolvedValue({
      rollout: protoRollout("live", RolloutState.REVIEW, 2n),
    });
    const { result } = renderHook(() => useRolloutApi());

    await act(async () => {
      await result.current.listRollouts();
      await result.current.getRollout({ rolloutId: "live" });
    });

    expect(result.current.rollouts.map(({ id }) => id)).toEqual(["history", "live"]);
    expect(result.current.rollouts.find(({ id }) => id === "live")).toMatchObject({
      state: "review",
      revision: 2n,
    });
  });

  it("merges concurrent get refreshes independently by rollout ID", async () => {
    const first = deferred<{ rollout: ReturnType<typeof protoRollout> }>();
    rolloutClientMock.getRollout.mockImplementation(({ rolloutId }: { rolloutId: string }) =>
      rolloutId === "one" ? first.promise : Promise.resolve({ rollout: protoRollout("two", RolloutState.PAUSED, 2n) }),
    );
    const { result } = renderHook(() => useRolloutApi());
    let firstRequest!: Promise<unknown>;
    let secondRequest!: Promise<unknown>;

    act(() => {
      firstRequest = result.current.getRollout({ rolloutId: "one" });
      secondRequest = result.current.getRollout({ rolloutId: "two" });
    });
    await act(async () => {
      await secondRequest;
    });
    await act(async () => {
      first.resolve({ rollout: protoRollout("one", RolloutState.RUNNING, 2n) });
      await firstRequest;
    });

    expect(result.current.rollouts.map(({ id }) => id).sort()).toEqual(["one", "two"]);
  });

  it("forwards revision and idempotency fields to distinct abort and revert controls", async () => {
    const aborted = protoRollout("one", RolloutState.ABORTED, 5n);
    const reverting = protoRollout("one", RolloutState.REVERTING, 6n);
    rolloutClientMock.abortRollout.mockResolvedValue({ rollout: aborted });
    rolloutClientMock.revertRollout.mockResolvedValue({ rollout: reverting });
    const { result } = renderHook(() => useRolloutApi());

    await act(async () => {
      await result.current.abortRollout({
        rolloutId: "one",
        expectedRevision: 4n,
        idempotencyKey: "abort-one",
        reason: "Stop new work",
      });
    });
    await act(async () => {
      await result.current.revertRollout({
        rolloutId: "one",
        expectedRevision: 5n,
        idempotencyKey: "revert-one",
        reason: "Restore prior state",
      });
    });

    expect(rolloutClientMock.abortRollout.mock.calls[0][0]).toMatchObject({
      rolloutId: "one",
      expectedRevision: 4n,
      idempotencyKey: "abort-one",
      reason: "Stop new work",
    });
    expect(rolloutClientMock.revertRollout.mock.calls[0][0]).toMatchObject({
      rolloutId: "one",
      expectedRevision: 5n,
      idempotencyKey: "revert-one",
      reason: "Restore prior state",
    });
    expect(result.current.rollout).toMatchObject({ state: "reverting", revision: 6n });
  });

  it("provides every generic control with revision and idempotency guards", async () => {
    const response = { rollout: protoRollout("one", RolloutState.RUNNING, 8n) };
    rolloutClientMock.admitRollout.mockResolvedValue(response);
    rolloutClientMock.continueRollout.mockResolvedValue(response);
    rolloutClientMock.pauseRollout.mockResolvedValue(response);
    rolloutClientMock.resumeRollout.mockResolvedValue(response);
    rolloutClientMock.completeRollout.mockResolvedValue(response);
    const { result } = renderHook(() => useRolloutApi());
    const control = {
      rolloutId: "one",
      expectedRevision: 7n,
      idempotencyKey: "control-one",
      reason: "Operator action",
    };

    await act(async () => {
      await result.current.admitRollout({ ...control, batchId: 3n });
      await result.current.continueRollout(control);
      await result.current.pauseRollout(control);
      await result.current.resumeRollout(control);
      await result.current.completeRollout({ ...control, withFailures: true });
    });

    expect(rolloutClientMock.admitRollout.mock.calls[0][0]).toMatchObject({ ...control, batchId: 3n });
    expect(rolloutClientMock.continueRollout.mock.calls[0][0]).toMatchObject(control);
    expect(rolloutClientMock.pauseRollout.mock.calls[0][0]).toMatchObject(control);
    expect(rolloutClientMock.resumeRollout.mock.calls[0][0]).toMatchObject(control);
    expect(rolloutClientMock.completeRollout.mock.calls[0][0]).toMatchObject({
      ...control,
      withFailures: true,
    });
  });

  it("creates model-neutral rollout requests without adding lane fields", async () => {
    rolloutClientMock.createRollout.mockResolvedValue({
      rollout: protoRollout("created", RolloutState.CREATED),
    });
    const { result } = renderHook(() => useRolloutApi());

    await act(async () => {
      await result.current.createRollout({
        name: "Firmware rollout",
        strategyKey: "strategy-a",
        batches: [
          {
            label: "Batch 1",
            members: [{ deviceIdentifier: "miner-1", targetSnapshot: { version: "1.2.3" } }],
          },
        ],
        idempotencyKey: "create-one",
        reason: "Controlled deployment",
      });
    });

    expect(rolloutClientMock.createRollout.mock.calls[0][0]).toMatchObject({
      name: "Firmware rollout",
      strategyKey: "strategy-a",
      idempotencyKey: "create-one",
      reason: "Controlled deployment",
      batches: [
        {
          label: "Batch 1",
          members: [{ deviceIdentifier: "miner-1", targetSnapshot: { version: "1.2.3" } }],
        },
      ],
    });
  });

  it("keeps mutation state active until concurrent rollout and lane mutations settle", async () => {
    const rolloutResponse = deferred<{ rollout: ReturnType<typeof protoRollout> }>();
    const laneResponse = deferred<{ lane: ReturnType<typeof protoLane> }>();
    rolloutClientMock.createRollout.mockReturnValue(rolloutResponse.promise);
    rolloutClientMock.createRolloutLane.mockReturnValue(laneResponse.promise);
    const { result } = renderHook(() => useRolloutApi());

    let rolloutRequest!: Promise<unknown>;
    let laneRequest!: Promise<unknown>;
    act(() => {
      rolloutRequest = result.current.createRollout({
        name: "Firmware rollout",
        strategyKey: "strategy-a",
        batches: [],
        idempotencyKey: "create-one",
        reason: "Controlled deployment",
      });
      laneRequest = result.current.createRolloutLane({
        label: "Stable production",
        description: "Production firmware lane",
        firmwareFileIds: ["file-alpha"],
        deviceIdentifiers: ["miner-1", "miner-2"],
        idempotencyKey: "create-lane-one",
      });
    });
    expect(result.current.isMutating).toBe(true);

    await act(async () => {
      rolloutResponse.resolve({ rollout: protoRollout("created", RolloutState.CREATED) });
      await rolloutRequest;
    });
    expect(result.current.isMutating).toBe(true);

    await act(async () => {
      laneResponse.resolve({ lane: protoLane() });
      await laneRequest;
    });
    expect(result.current.isMutating).toBe(false);
  });

  it("preserves mutation auth errors and cancellation handling", async () => {
    const permissionError = new ConnectError("permission denied", Code.PermissionDenied);
    rolloutClientMock.createRollout.mockRejectedValueOnce(permissionError);
    const pendingResponse = deferred<{ rollout: ReturnType<typeof protoRollout> }>();
    rolloutClientMock.createRollout.mockReturnValueOnce(pendingResponse.promise);
    const controller = new AbortController();
    const { result } = renderHook(() => useRolloutApi());
    const input = {
      name: "Firmware rollout",
      strategyKey: "strategy-a",
      batches: [],
      idempotencyKey: "create-one",
      reason: "Controlled deployment",
    };

    await act(async () => {
      await expect(result.current.createRollout(input)).rejects.toThrow("permission denied");
    });
    expect(handleAuthErrorsMock).toHaveBeenCalledWith({ error: permissionError });
    expect(result.current.mutationError).toBe("permission denied");

    let request!: Promise<unknown>;
    act(() => {
      request = result.current.createRollout({ ...input, signal: controller.signal });
    });
    controller.abort();
    pendingResponse.resolve({ rollout: protoRollout("cancelled") });
    await act(async () => {
      await expect(request).rejects.toMatchObject({ name: "AbortError" });
    });
    expect(result.current.mutationError).toBeNull();
    expect(handleAuthErrorsMock).toHaveBeenCalledTimes(1);
    expect(result.current.isMutating).toBe(false);
  });

  it("does not let a stale list response replace newer state", async () => {
    const stale = deferred<{ rollouts: ReturnType<typeof protoRollout>[] }>();
    rolloutClientMock.listRollouts
      .mockReturnValueOnce(stale.promise)
      .mockResolvedValueOnce({ rollouts: [protoRollout("fresh")] });
    const { result } = renderHook(() => useRolloutApi());

    let staleRequest!: Promise<unknown>;
    act(() => {
      staleRequest = result.current.listRollouts();
    });
    await act(async () => {
      await result.current.listRollouts();
    });
    await act(async () => {
      stale.resolve({ rollouts: [protoRollout("stale")] });
      await staleRequest;
    });

    expect(result.current.rollouts.map((rollout) => rollout.id)).toEqual(["fresh"]);
  });

  it("does not let a stale history list overwrite a newer per-ID refresh", async () => {
    const staleList = deferred<{ rollouts: ReturnType<typeof protoRollout>[] }>();
    rolloutClientMock.listRollouts.mockReturnValue(staleList.promise);
    rolloutClientMock.getRollout.mockResolvedValue({
      rollout: protoRollout("live", RolloutState.REVIEW, 2n),
    });
    const { result } = renderHook(() => useRolloutApi());
    let listRequest!: Promise<unknown>;

    act(() => {
      listRequest = result.current.listRollouts();
    });
    await act(async () => {
      await result.current.getRollout({ rolloutId: "live" });
    });
    await act(async () => {
      staleList.resolve({ rollouts: [protoRollout("live", RolloutState.RUNNING, 1n)] });
      await listRequest;
    });

    expect(result.current.rollouts).toEqual([expect.objectContaining({ id: "live", state: "review", revision: 2n })]);
  });

  it("does not update state or report auth errors after cancellation", async () => {
    const response = deferred<{ rollout: ReturnType<typeof protoRollout> }>();
    rolloutClientMock.getRollout.mockReturnValue(response.promise);
    const controller = new AbortController();
    const { result } = renderHook(() => useRolloutApi());

    let request!: Promise<unknown>;
    act(() => {
      request = result.current.getRollout({ rolloutId: "one", signal: controller.signal });
    });
    controller.abort();
    response.resolve({ rollout: protoRollout("one") });

    await act(async () => {
      await expect(request).rejects.toMatchObject({ name: "AbortError" });
    });

    expect(result.current.rollout).toBeNull();
    expect(result.current.loadError).toBeNull();
    expect(handleAuthErrorsMock).not.toHaveBeenCalled();
  });

  it("routes permission failures through the shared auth error handler", async () => {
    const error = new ConnectError("permission denied", Code.PermissionDenied);
    rolloutClientMock.getRollout.mockResolvedValueOnce({ rollout: protoRollout("one") }).mockRejectedValueOnce(error);
    const { result } = renderHook(() => useRolloutApi());

    await act(async () => {
      await result.current.getRollout({ rolloutId: "one" });
    });
    expect(result.current.rollout?.id).toBe("one");

    await act(async () => {
      await expect(result.current.getRollout({ rolloutId: "one" })).rejects.toThrow("permission denied");
    });

    expect(handleAuthErrorsMock).toHaveBeenCalledWith({ error });
    expect(result.current.rollout).toBeNull();
    expect(result.current.rollouts).toEqual([]);
    expect(result.current.loadError).toBe("permission denied");
  });

  it("exposes organization-scoped rollout permissions", () => {
    permissionsMock.current = ["channel:read", "rollout:read"];
    const { result } = renderHook(() => useRolloutApi());

    expect(result.current.permissions).toEqual({
      canReadChannels: true,
      canManageChannels: false,
      canRead: true,
      canManage: false,
      canControl: false,
    });
  });
});
