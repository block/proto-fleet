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
    reading: create(CheckConfigurationResponseSchema, { state, readbackAvailable: state !== "unavailable" }),
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
    const later = deferred();
    readDevicePools.mockImplementation(({ targets }: { targets: Target[] }) => {
      if (targets[0].targetId === 5n) return later.promise;
      return outcome === "failure" ? Promise.reject(new Error("offline")) : Promise.resolve({ readings: [] });
    });
    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.readings["device:1"].state).toBe("unavailable"));
    expect(result.current.readings["device:5"]?.state).toBe("device_configuration");
    await act(async () => later.resolve(reply([deviceTarget(5n)], "unavailable")));
    expect(result.current.readings["device:5"].state).toBe("unavailable");
  },
);

it.each(["account", "route", "disabled", "session", "clear"])(
  "invalidates old readings on a %s change and ignores the prior response",
  async (change) => {
    const board = create(EquipmentAssignmentSchema, { installationId: 101n, deviceId: 1n });
    const initial = { accountId: "account-a", assignments: [board], enabled: true };
    const { result, rerender } = renderHook(
      ({ accountId, assignments, enabled }) => useDevicePoolReadings(client, accountId, assignments, enabled),
      { initialProps: initial },
    );
    await waitFor(() =>
      expect(result.current.readings["hashboard_installation:101"]?.state).toBe("device_configuration"),
    );
    const old = deferred();
    readDevicePools.mockImplementationOnce(() => old.promise).mockImplementation(() => new Promise(() => {}));
    act(() => result.current.refresh());
    const priorSignal = readDevicePools.mock.calls.at(-1)?.[1].signal;
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
    await act(async () => old.resolve(reply([{ kind: "hashboard_installation", targetId: 101n }])));
    expect(result.current.readings).toEqual({});
    if (change === "disabled" || change === "clear") {
      rerender(initial);
      expect(result.current.readings).toEqual({});
    }
  },
);
