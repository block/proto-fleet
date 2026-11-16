import { describe, expect, it, vi } from "vitest";

import type { MdkV2Client } from "./connectClient";
import type { V2StreamASICHealthResponse } from "./generated/generatedApi";
import { followHashboardStatus } from "./hashboardStream";
import { MdkV2Adapter } from "./MdkV2Adapter";

const collect = async <T>(items: AsyncIterable<T>) => {
  const out: T[] = [];
  for await (const item of items) out.push(item);
  return out;
};

const MAC = "02:00:00:00:00:0a";
const layout = {
  stackCount: 1,
  asicsPerStack: 2,
  totalAsics: 2,
  entries: [
    { index: 0, stackId: 0, stackIndex: 0, x: 0, y: 0 },
    { index: 1, stackId: 0, stackIndex: 1, x: 1, y: 0 },
  ],
};
const asics = (temperatureDc: number) => [
  { index: 0, temperatureDc, hashrateGhs10: 125 },
  { index: 1, temperatureDc: temperatureDc + 10, hashrateGhs10: 130 },
];

const follow = (frames: V2StreamASICHealthResponse[]) =>
  vi.fn(async function* () {
    yield* frames;
  });

describe("followHashboardStatus", () => {
  it("keeps the last layout for frames that omit it", async () => {
    const stream = follow([
      { macAddress: MAC, layout, asics: asics(700), timestamp: "2026-11-15T08:00:00Z" },
      { macAddress: MAC, asics: asics(710) },
    ]);

    const statuses = await collect(followHashboardStatus(stream, MAC.toUpperCase(), { headers: { a: "b" } }));

    expect(stream).toHaveBeenCalledWith({ identifier: MAC.toUpperCase() }, { headers: { a: "b" } });
    expect(statuses).toHaveLength(2);
    for (const status of statuses) {
      expect(status.serial).toBe(MAC.toUpperCase());
      expect(status.asics.map((asic) => [asic.row, asic.column])).toEqual([
        [0, 0],
        [0, 1],
      ]);
    }
    expect(statuses.map((status) => status.observedAt)).toEqual(["2026-11-15T08:00:00Z", undefined]);
    expect(statuses[1].asicTelemetry[0]).toMatchObject({
      temperature: { value: 71, units: "°C" },
      hashrate: { value: 12.5, units: "GH/s" },
    });
  });

  it("rejects readings for a different board", async () => {
    const stream = follow([{ macAddress: "02:00:00:00:00:0b", layout, asics: asics(700) }]);

    await expect(collect(followHashboardStatus(stream, MAC, {}))).rejects.toThrow(
      "streamed readings for 02:00:00:00:00:0b instead of 02:00:00:00:00:0a",
    );
  });

  it("accepts an R2 serial identity", async () => {
    const stream = follow([{ serial: "R2-0001", slot: 2, layout, asics: asics(700) }]);

    const [status] = await collect(followHashboardStatus(stream, "R2-0001", {}));

    expect(status.asicIds).toEqual(["serial:R2-0001:asic:0", "serial:R2-0001:asic:1"]);
  });
});

describe("MdkV2Adapter.streamHashboardStatus", () => {
  it("is offered only by clients that can follow the stream", async () => {
    const base = {} as MdkV2Client;
    expect(new MdkV2Adapter(base).streamHashboardStatus).toBeUndefined();

    const stream = follow([{ macAddress: MAC, layout, asics: asics(700) }]);
    const adapter = new MdkV2Adapter({ ...base, telemetryServiceFollowAsicHealth: stream });
    const statuses = await collect(adapter.streamHashboardStatus!(MAC));

    expect(statuses).toHaveLength(1);
    expect(stream).toHaveBeenCalledWith(
      { identifier: MAC },
      expect.objectContaining({ headers: expect.objectContaining({ "X-Frigate-Actor": "proto-os" }) }),
    );
  });

  it("is not offered over the REST gateway", () => {
    expect(
      MdkV2Adapter.fromConfig({ baseUrl: "http://ct1.test", protocol: "rest" }).streamHashboardStatus,
    ).toBeUndefined();
    expect(
      MdkV2Adapter.fromConfig({ baseUrl: "http://ct1.test", protocol: "connect" }).streamHashboardStatus,
    ).toBeDefined();
  });
});
