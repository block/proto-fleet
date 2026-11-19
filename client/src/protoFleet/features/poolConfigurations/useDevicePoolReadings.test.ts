import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import type { Client } from "@connectrpc/connect";
import { useDevicePoolReadings } from "./useDevicePoolReadings";
import {
  CheckConfigurationResponseSchema,
  EquipmentAssignmentSchema,
  type PoolConfigurationService,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useFleetStore } from "@/protoFleet/store";

const readDevicePools = vi.fn();
const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
const equipment = (id: bigint) => create(EquipmentAssignmentSchema, { deviceId: id, state: "unavailable" });
const assigned = [1n, 2n, 3n, 4n, 5n].map(equipment);
type Target = { kind: string; targetId: bigint };
const reply = (targets: Target[], state = "device_configuration") => ({
  readings: targets.map((target) => ({
    target,
    reading: create(CheckConfigurationResponseSchema, {
      state,
      readbackAvailable: state !== "unavailable",
      reported: state === "unavailable" ? [] : [{ username: "worker" }],
    }),
  })),
});
const deferred = () => {
  let resolve!: (response: ReturnType<typeof reply>) => void;
  const promise = new Promise<ReturnType<typeof reply>>((done) => {
    resolve = done;
  });
  return { promise, resolve };
};
const deviceTarget = (id: bigint): Target => ({ kind: "device", targetId: id });
beforeEach(() => {
  readDevicePools.mockReset();
  readDevicePools.mockImplementation(async ({ targets }: { targets: Target[] }) => reply(targets));
});

it("retains later batches while each device refresh is pending", async () => {
  const { result } = renderHook(() => useDevicePoolReadings(client, "", assigned, true));
  await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(5));
  const later = deferred();
  readDevicePools.mockImplementation(({ targets }: { targets: Target[] }) =>
    targets[0].targetId === 5n ? later.promise : Promise.resolve(reply(targets, "")),
  );
  act(() => result.current.refresh());
  await waitFor(() => expect(result.current.readings["device:1"].state).toBe(""));
  expect(result.current.readings["device:5"]?.state).toBe("device_configuration");
  await act(async () => later.resolve(reply([deviceTarget(5n)], "")));
  expect(result.current.readings["device:5"].state).toBe("");
});

it("keeps unaffected devices when another device gains a Fleet assignment", async () => {
  const { result, rerender } = renderHook(({ assignments }) => useDevicePoolReadings(client, "", assignments, true), {
    initialProps: { assignments: assigned },
  });
  await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(5));
  readDevicePools.mockImplementation(() => new Promise(() => {}));
  rerender({ assignments: assigned.map((a) => (a.deviceId === 1n ? { ...a, configurationId: "new-config" } : a)) });
  expect(result.current.readings["device:1"]).toBeUndefined();
  expect(result.current.readings["device:5"]?.state).toBe("device_configuration");
  // Losing the Fleet assignment again must require a new device observation.
  rerender({ assignments: assigned });
  expect(result.current.readings["device:1"]).toBeUndefined();
  expect(result.current.readings["device:5"]?.state).toBe("device_configuration");
});

it.each(["failure", "missing"])(
  "replaces old evidence after a settled %s without dropping pending batches",
  async (outcome) => {
    const { result } = renderHook(() => useDevicePoolReadings(client, "", assigned, true));
    await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(5));
    expect(result.current.readings["device:1"].reported).toHaveLength(1);
    const later = deferred();
    readDevicePools.mockImplementation(({ targets }: { targets: Target[] }) => {
      if (targets[0].targetId === 5n) return later.promise;
      return outcome === "failure" ? Promise.reject(new Error("offline")) : Promise.resolve({ readings: [] });
    });
    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.readings["device:1"].state).toBe("unavailable"));
    expect(result.current.readings["device:1"].reported).toEqual([]);
    expect(result.current.readings["device:1"].readbackAvailable).toBe(false);
    expect(result.current.readings["device:5"]?.state).toBe("device_configuration");
    await act(async () => later.resolve(reply([deviceTarget(5n)], "unavailable")));
    expect(result.current.readings["device:5"].state).toBe("unavailable");
  },
);

it.each(["account", "route", "disabled", "session", "clear"])(
  "invalidates old readings on a %s change, ignores the prior response, and reads again",
  async (change) => {
    const board = create(EquipmentAssignmentSchema, { installationId: 101n, deviceId: 1n });
    const initial = { accountId: "account-a", assignments: [board], enabled: true };
    const key = "hashboard_installation:101";
    const { result, rerender } = renderHook(
      ({ accountId, assignments, enabled }) => useDevicePoolReadings(client, accountId, assignments, enabled),
      { initialProps: initial },
    );
    await waitFor(() => expect(result.current.readings[key]?.state).toBe("device_configuration"));
    const old = deferred();
    // Later reads return a different state, so the test can tell a new reading from the old one.
    readDevicePools
      .mockImplementationOnce(() => old.promise)
      .mockImplementation(async ({ targets }: { targets: Target[] }) => reply(targets, "different"));
    act(() => result.current.refresh());
    const priorSignal = readDevicePools.mock.lastCall?.[1].signal;
    if (change === "session") {
      act(() =>
        useFleetStore.setState((s) => ({ auth: { ...s.auth, sessionGeneration: s.auth.sessionGeneration + 1 } })),
      );
    } else {
      rerender({
        ...initial,
        accountId: change === "account" ? "account-b" : initial.accountId,
        assignments:
          change === "route"
            ? [{ ...board, deviceId: 2n }]
            : change === "clear"
              ? [{ ...board, operationId: "automatic-clear", state: "pending" }]
              : initial.assignments,
        enabled: change !== "disabled",
      });
    }
    expect(result.current.readings).toEqual({});
    expect(priorSignal.aborted).toBe(true);
    // A disabled or cleared target has no read until it returns. The other changes read immediately.
    const readsNow = change !== "disabled" && change !== "clear";
    if (readsNow) await waitFor(() => expect(result.current.readings[key]?.state).toBe("different"));
    await act(async () => old.resolve(reply([{ kind: "hashboard_installation", targetId: 101n }])));
    expect(result.current.readings[key]?.state).toBe(readsNow ? "different" : undefined);
    if (!readsNow) {
      rerender(initial);
      expect(result.current.readings).toEqual({});
      await waitFor(() => expect(result.current.readings[key]?.state).toBe("different"));
    }
  },
);

it("keeps successful siblings while marking the failed target unavailable", async () => {
  let refresh = false;
  readDevicePools.mockImplementation(async ({ targets }: { targets: Target[] }) => ({
    readings: targets.map((target) => ({
      target,
      reading: create(
        CheckConfigurationResponseSchema,
        refresh && target.targetId === 38n
          ? { state: "unavailable", resultCode: "check_unavailable" }
          : {
              state: "device_configuration",
              readbackAvailable: true,
              reported: [{ username: `worker-${target.targetId}` }],
            },
      ),
    })),
  }));
  const assignments = [38n, 39n].map((deviceId) => create(EquipmentAssignmentSchema, { deviceId }));
  const { result } = renderHook(() => useDevicePoolReadings(client, "", assignments, true));
  await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(2));
  refresh = true;
  act(() => result.current.refresh());
  await waitFor(() => expect(result.current.readings["device:38"]?.state).toBe("unavailable"));
  expect(result.current.readings["device:39"]?.reported[0].username).toBe("worker-39");
});

it("batches 150 unassigned boards, deduplicates targets, and skips canonical assignments", async () => {
  readDevicePools.mockResolvedValue({ readings: [] });
  const boards = Array.from({ length: 150 }, (_, i) =>
    create(EquipmentAssignmentSchema, { deviceId: 1n, installationId: BigInt(i + 1) }),
  );
  const assignments = [
    ...boards,
    boards[0],
    create(EquipmentAssignmentSchema, { deviceId: 2n, configurationId: "canonical" }),
  ];
  const { result } = renderHook(() => useDevicePoolReadings(client, "account", assignments, true));
  await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(150));
  expect(readDevicePools).toHaveBeenCalledTimes(1);
  expect(readDevicePools.mock.calls[0][0].accountId).toBe("account");
  expect(readDevicePools.mock.calls[0][0].targets).toHaveLength(150);
  expect(result.current.readings["device:2"]).toBeUndefined();
  expect(result.current.readings["hashboard_installation:1"].state).toBe("unavailable");
});

it("chunks larger selections at 500 targets and preserves successful batches", async () => {
  readDevicePools
    .mockImplementationOnce(async ({ targets }) => ({
      readings: targets.map((target: { kind: string; targetId: bigint }) => ({
        target,
        reading: create(CheckConfigurationResponseSchema, { readbackAvailable: true }),
      })),
    }))
    .mockRejectedValueOnce(new Error("offline"));
  const assignments = Array.from({ length: 501 }, (_, i) =>
    create(EquipmentAssignmentSchema, { deviceId: 1n, installationId: BigInt(i + 1) }),
  );
  const { result } = renderHook(() => useDevicePoolReadings(client, "", assignments, true));
  await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(501));
  expect(readDevicePools.mock.calls.map(([request]) => request.targets.length)).toEqual([500, 1]);
  expect(Object.values(result.current.readings).filter((r) => r.readbackAvailable)).toHaveLength(500);
});

it("continues with healthy controllers after a slow batch times out", async () => {
  let fail!: (reason: Error) => void;
  readDevicePools
    .mockImplementationOnce(
      () =>
        new Promise((_, reject) => {
          fail = reject;
        }),
    )
    .mockImplementation(async ({ targets }) => ({
      readings: targets.map((target: { kind: string; targetId: bigint }) => ({
        target,
        reading: create(CheckConfigurationResponseSchema, { readbackAvailable: true }),
      })),
    }));
  const assignments = [9n, 7n, 3n, 1n, 5n, 8n, 6n, 4n, 2n].map((deviceId) =>
    create(EquipmentAssignmentSchema, { deviceId }),
  );
  const { result } = renderHook(() => useDevicePoolReadings(client, "", assignments, true));
  expect(readDevicePools.mock.calls[0][0].targets.map((t: { targetId: bigint }) => t.targetId)).toEqual([
    1n,
    2n,
    3n,
    4n,
  ]);
  expect(readDevicePools).toHaveBeenCalledTimes(1);
  expect(result.current.readings["device:5"]).toBeUndefined();
  await act(async () => fail(new Error("deadline exceeded")));
  await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(9));
  expect(readDevicePools.mock.calls.map(([request]) => request.targets.length)).toEqual([4, 4, 1]);
  expect(result.current.readings["device:1"].state).toBe("unavailable");
  expect(result.current.readings["device:5"].readbackAvailable).toBe(true);
  expect(result.current.readings["device:9"].readbackAvailable).toBe(true);
});

it("aborts pending readback and stops continuation when the view closes", async () => {
  let finish!: (value: unknown) => void;
  readDevicePools.mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const assignments = Array.from({ length: 8 }, (_, i) =>
    create(EquipmentAssignmentSchema, { deviceId: BigInt(i + 1) }),
  );
  const { unmount } = renderHook(() => useDevicePoolReadings(client, "", assignments, true));
  unmount();
  await act(async () => finish({ readings: [] }));
  expect(readDevicePools).toHaveBeenCalledTimes(1);
  expect(readDevicePools.mock.calls[0][1].signal.aborted).toBe(true);
});

it("lets every batch finish when background refreshes arrive during a slow read", async () => {
  let finish!: (value: unknown) => void;
  readDevicePools
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValue({ readings: [] });
  const assignments = Array.from({ length: 5 }, (_, i) =>
    create(EquipmentAssignmentSchema, { deviceId: BigInt(i + 1) }),
  );
  const { result } = renderHook(() => useDevicePoolReadings(client, "", assignments, true));
  const signal = readDevicePools.mock.calls[0][1].signal as AbortSignal;
  act(() => {
    result.current.poll();
    result.current.poll();
  });
  expect(signal.aborted).toBe(false);
  expect(readDevicePools).toHaveBeenCalledTimes(1);
  await act(async () => finish({ readings: [] }));
  await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(5));
  expect(readDevicePools).toHaveBeenCalledTimes(2);
  act(() => result.current.poll());
  await waitFor(() => expect(readDevicePools).toHaveBeenCalledTimes(4));
});
