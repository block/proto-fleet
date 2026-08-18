import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { RolloutSchema, RolloutState } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const rolloutClientMock = vi.hoisted(() => ({
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
const handleAuthErrorsMock = vi.hoisted(() => vi.fn());
const permissionsMock = vi.hoisted(() => ({ current: [] as string[] }));

vi.mock("@/protoFleet/api/clients", () => ({
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
    permissionsMock.current = ["rollout:read", "rollout:manage", "rollout:control"];
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
    expect(result.current.rollouts.map((rollout) => rollout.id)).toEqual(["one"]);
    expect(result.current.rollout).toMatchObject({ id: "two", state: "paused", revision: 2n });
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
    permissionsMock.current = ["rollout:read"];
    const { result } = renderHook(() => useRolloutApi());

    expect(result.current.permissions).toEqual({
      canRead: true,
      canManage: false,
      canControl: false,
    });
  });
});
