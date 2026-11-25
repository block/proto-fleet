import { beforeEach, expect, it } from "vitest";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import useMinerStore from "@/protoOS/store/useMinerStore";

const snapshot = (located: boolean) =>
  toCanonicalContainer(
    {
      containerMap: {
        tanks: [
          {
            id: located ? 1 : 0,
            modules: [
              {
                id: located ? 2 : 0,
                slots: [
                  {
                    hashboard: {
                      macAddress: "aa:bb:cc:dd:ee:01",
                      location: { hashboardPosition: located ? 1 : 0 },
                      health: { hashrate: 2.5 },
                    },
                  },
                ],
              },
            ],
          },
        ],
      },
    },
    {},
    {},
    "ct1",
  );

beforeEach(() => useMinerStore.getState().resetDeviceData());

it("moves a board between unlocated inventory and its physical module during polling", () => {
  const initial = snapshot(false);
  useMinerStore.getState().hardware.replaceHardware(initial.hardware);
  useMinerStore.getState().telemetry.mergeTelemetry(initial.telemetry, 1);
  const board = () => [...useMinerStore.getState().hardware.topology!.hashboards.values()][0];
  expect(board().moduleId).toBeUndefined();
  useMinerStore.getState().telemetry.mergeTelemetry(snapshot(true).telemetry, 3);
  expect(board().moduleId).toBeDefined();
  expect(board().location?.slot).toBe(1);
  useMinerStore.getState().telemetry.mergeTelemetry(snapshot(false).telemetry, 2);
  expect(board().moduleId).toBeDefined();
  useMinerStore.getState().telemetry.mergeTelemetry(snapshot(false).telemetry, 4);
  expect(board().moduleId).toBeUndefined();
  expect(useMinerStore.getState().hardware.topology?.modules.size).toBe(0);
  expect(useMinerStore.getState().hardware.hashboards.size).toBe(1);
});

it("keeps the post-mapping hardware refresh when an earlier poll completes later", () => {
  const initial = snapshot(false);
  const store = useMinerStore.getState();
  store.hardware.replaceHardware(initial.hardware, 1);
  store.telemetry.mergeTelemetry(initial.telemetry, 1);
  const mapped = snapshot(true);
  store.hardware.replaceHardware(mapped.hardware, 3);
  store.telemetry.mergeTelemetry(mapped.telemetry, 3);
  store.telemetry.mergeTelemetry(snapshot(false).telemetry, 2);
  expect([...useMinerStore.getState().hardware.topology!.hashboards.values()][0].moduleId).toBeDefined();
});

it("rejects an older hardware response after a newer inventory poll", () => {
  const store = useMinerStore.getState();
  store.hardware.replaceHardware(snapshot(false).hardware, 1);
  store.telemetry.mergeTelemetry(snapshot(true).telemetry, 3);
  const older = snapshot(false);
  store.hardware.replaceHardware(older.hardware, 2);
  store.telemetry.mergeTelemetry(older.telemetry, 2);
  expect([...useMinerStore.getState().hardware.topology!.hashboards.values()][0].moduleId).toBeDefined();
});
