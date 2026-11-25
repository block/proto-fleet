import { expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { moduleHistoryChart } from "./moduleHistoryChart";
import { ModuleSampleSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

it("converts SI readings, keeps zero, deduplicates source timestamps and preserves gaps", () => {
  const make = (seconds: number, rate: number | undefined, collected = seconds) =>
    create(ModuleSampleSchema, {
      observedAt: timestampFromMs(seconds * 1000),
      collectedAt: timestampFromMs(collected * 1000),
      hashrateHs: rate,
      powerW: 3000,
      efficiencyJh: 30e-12,
      temperatureC: 0,
    });
  const points = moduleHistoryChart(
    [make(100, 100e12), make(100, 200e12, 110), make(200, 0), make(800, undefined)],
    "1h",
    "F",
  );
  expect(points.find((point) => point.datetime === 90_000)).toMatchObject({
    hashrate: 200,
    power: 3,
    efficiency: 30,
    temperature: 32,
  });
  expect(points.find((point) => point.datetime === 180_000)?.hashrate).toBe(0);
  expect(points.find((point) => point.datetime === 270_000)?.hashrate).toBeNull();
  expect(points.find((point) => point.datetime === 720_000)?.hashrate).toBeNull();
});
it("does not manufacture measurements from absent or non-finite values", () => {
  const sample = create(ModuleSampleSchema, {
    observedAt: timestampFromMs(100_000),
    hashrateHs: NaN,
    temperatureC: Infinity,
  });
  expect(moduleHistoryChart([sample], "1h", "C")[0]).toMatchObject({
    hashrate: null,
    power: null,
    temperature: null,
    efficiency: null,
  });
});
it("keeps long-window summaries at their retained resolution", () => {
  const points = moduleHistoryChart(
    [
      create(ModuleSampleSchema, { observedAt: timestampFromMs(86_400_000), hashrateHs: 100e12 }),
      create(ModuleSampleSchema, { observedAt: timestampFromMs(3 * 86_400_000), hashrateHs: 200e12 }),
    ],
    "30d",
    "C",
  );
  expect(points).toHaveLength(3);
  expect(points[1].hashrate).toBeNull();
});
