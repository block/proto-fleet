import { expect, test, vi } from "vitest";
import type { HashboardAction } from "../types";
import { MdkV2Adapter, type MdkV2Client } from "./MdkV2Adapter";

const fixture = () => {
  const result = {
    data: {
      results: [
        { macAddress: "a", applied: true },
        { macAddress: "b", applied: false, message: "disconnected" },
      ],
    },
  };
  const client = {
    miningServiceStartMiningScoped: vi.fn().mockResolvedValue(result),
    miningServiceStopMiningScoped: vi.fn().mockResolvedValue(result),
    miningServiceSetIsolation: vi.fn().mockResolvedValue(result),
    hashboardServiceSetIdentifyLed: vi.fn().mockResolvedValue({ data: { boardsUpdated: 2 } }),
  };
  return { client, adapter: new MdkV2Adapter(client as unknown as MdkV2Client) };
};

test.each([
  ["start", "miningServiceStartMiningScoped", {}],
  ["stop", "miningServiceStopMiningScoped", {}],
  ["isolate", "miningServiceSetIsolation", { isolated: true }],
  ["clearIsolation", "miningServiceSetIsolation", { isolated: false }],
  ["identify", "hashboardServiceSetIdentifyLed", { mode: "HASHBOARD_IDENTIFY_LED_MODE_BLINK" }],
] as const)("%s uses explicit board identities", async (action, method, body) => {
  const { client, adapter } = fixture();
  await adapter.runHashboardAction(action, ["a", "b", "a"]);
  expect(client[method]).toHaveBeenCalledExactlyOnceWith(
    { scope: { identifiers: ["a", "b"] }, ...body },
    expect.anything(),
  );
});

test("preserves board-level failures and never accepts an empty scope", async () => {
  const { client, adapter } = fixture();
  expect(await adapter.runHashboardAction("stop", ["a", "b"])).toEqual({
    accepted: 1,
    failures: [{ identifier: "b", message: "disconnected" }],
  });
  for (const action of ["start", "stop", "isolate", "identify"] as HashboardAction[]) {
    await expect(adapter.runHashboardAction(action, [])).rejects.toThrow("No hashboards selected");
    await expect(adapter.runHashboardAction(action, [" "])).rejects.toThrow("No hashboards selected");
  }
  expect(client.miningServiceStartMiningScoped).not.toHaveBeenCalled();
  expect(client.miningServiceSetIsolation).not.toHaveBeenCalled();
  expect(client.hashboardServiceSetIdentifyLed).not.toHaveBeenCalled();
});
