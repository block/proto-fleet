import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import type { Client } from "@connectrpc/connect";

import { useDevicePoolReadings } from "./useDevicePoolReadings";
import {
  CheckConfigurationResponseSchema,
  EquipmentAssignmentSchema,
  type PoolConfigurationService,
} from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

it("withdraws a successful reading when the next refresh fails", async () => {
  const checked = create(CheckConfigurationResponseSchema, {
    state: "device_configuration",
    readbackAvailable: true,
    reported: [{ url: "stratum+tcp://old.example:3333", username: "old.worker" }],
  });
  const readDevicePools = vi
    .fn()
    .mockResolvedValueOnce({ readings: [{ target: { kind: "device", targetId: 38n }, reading: checked }] })
    .mockRejectedValueOnce(new Error("network unavailable"));
  const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
  const assignments = [create(EquipmentAssignmentSchema, { deviceId: 38n })];
  const { result } = renderHook(() => useDevicePoolReadings(client, "", assignments, true));
  await waitFor(() => expect(result.current.readings["device:38"]?.reported).toEqual(checked.reported));
  act(() => result.current.refresh());
  await waitFor(() => expect(readDevicePools).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(result.current.readings["device:38"]?.state).toBe("unavailable"));
  expect(result.current.readings["device:38"]?.reported).toEqual([]);
  expect(result.current.readings["device:38"]?.readbackAvailable).toBe(false);
});

it("keeps successful siblings while marking the failed target unavailable", async () => {
  let refresh = false;
  const readDevicePools = vi.fn(async ({ targets }: { targets: { kind: string; targetId: bigint }[] }) => ({
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
  const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
  const assignments = [38n, 39n].map((deviceId) => create(EquipmentAssignmentSchema, { deviceId }));
  const { result } = renderHook(() => useDevicePoolReadings(client, "", assignments, true));
  await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(2));
  refresh = true;
  act(() => result.current.refresh());
  await waitFor(() => expect(result.current.readings["device:38"]?.state).toBe("unavailable"));
  expect(result.current.readings["device:39"]?.reported[0].username).toBe("worker-39");
});

it("batches 150 unassigned boards, deduplicates targets, and skips canonical assignments", async () => {
  const readDevicePools = vi.fn().mockResolvedValue({ readings: [] });
  const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
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
  const readDevicePools = vi
    .fn()
    .mockImplementationOnce(async ({ targets }) => ({
      readings: targets.map((target: { kind: string; targetId: bigint }) => ({
        target,
        reading: create(CheckConfigurationResponseSchema, { readbackAvailable: true }),
      })),
    }))
    .mockRejectedValueOnce(new Error("offline"));
  const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
  const assignments = Array.from({ length: 501 }, (_, i) =>
    create(EquipmentAssignmentSchema, { deviceId: 1n, installationId: BigInt(i + 1) }),
  );
  const { result } = renderHook(() => useDevicePoolReadings(client, "", assignments, true));
  await waitFor(() => expect(Object.keys(result.current.readings)).toHaveLength(501));
  expect(readDevicePools.mock.calls.map(([request]) => request.targets.length)).toEqual([500, 1]);
  expect(Object.values(result.current.readings).filter((r) => r.readbackAvailable)).toHaveLength(500);
});

it("ignores an old response after account selection changes", async () => {
  let finish!: (value: unknown) => void;
  const readDevicePools = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValue({ readings: [] });
  const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
  const assignments = [create(EquipmentAssignmentSchema, { deviceId: 38n })];
  const { result, rerender } = renderHook(
    ({ accountId }) => useDevicePoolReadings(client, accountId, assignments, true),
    { initialProps: { accountId: "old" } },
  );
  rerender({ accountId: "new" });
  await waitFor(() => expect(result.current.readings["device:38"]?.state).toBe("unavailable"));
  await act(async () =>
    finish({
      readings: [
        {
          target: { kind: "device", targetId: 38n },
          reading: create(CheckConfigurationResponseSchema, {
            state: "device_configuration",
            readbackAvailable: true,
            reported: [{ username: "old-account" }],
          }),
        },
      ],
    }),
  );
  expect(readDevicePools.mock.calls[0][1].signal.aborted).toBe(true);
  expect(result.current.readings["device:38"].state).toBe("unavailable");
});

it("continues with healthy controllers after a slow batch times out", async () => {
  let fail!: (reason: Error) => void;
  const readDevicePools = vi
    .fn()
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
  const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
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
  const readDevicePools = vi.fn(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
  const assignments = Array.from({ length: 8 }, (_, i) =>
    create(EquipmentAssignmentSchema, { deviceId: BigInt(i + 1) }),
  );
  const { unmount } = renderHook(() => useDevicePoolReadings(client, "", assignments, true));
  unmount();
  await act(async () => finish({ readings: [] }));
  expect(readDevicePools).toHaveBeenCalledTimes(1);
  expect((readDevicePools.mock.calls[0] as unknown as [unknown, { signal: AbortSignal }])[1].signal.aborted).toBe(true);
});

it("withdraws readback and cancels the old batch when a board changes controller", async () => {
  let finish!: (value: unknown) => void;
  const readDevicePools = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValue({ readings: [] });
  const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
  const { result, rerender } = renderHook(
    ({ deviceId }) =>
      useDevicePoolReadings(client, "", [create(EquipmentAssignmentSchema, { deviceId, installationId: 10n })], true),
    { initialProps: { deviceId: 1n } },
  );
  rerender({ deviceId: 2n });
  await waitFor(() => expect(readDevicePools).toHaveBeenCalledTimes(2));
  await act(async () =>
    finish({
      readings: [
        {
          target: { kind: "hashboard_installation", targetId: 10n },
          reading: create(CheckConfigurationResponseSchema, { readbackAvailable: true }),
        },
      ],
    }),
  );
  expect(readDevicePools.mock.calls[0][1].signal.aborted).toBe(true);
  expect(result.current.readings["hashboard_installation:10"].readbackAvailable).toBe(false);
});

it("lets every batch finish when background refreshes arrive during a slow read", async () => {
  let finish!: (value: unknown) => void;
  const readDevicePools = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValue({ readings: [] });
  const client = { readDevicePools } as unknown as Client<typeof PoolConfigurationService>;
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
