import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { accountDashboardData } from "./accountDashboardData";
import { AccountDashboardReadingSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

describe("account dashboard readings", () => {
  it("uses authorized status distributions without inferring them from averages and preserves gaps", () => {
    const { temperatureStatusCounts, uptimeStatusCounts } = accountDashboardData(
      [
        create(AccountDashboardReadingSchema, {
          time: { seconds: 180n },
          summary: { temperatureC: 55, uptimePercent: 100 },
          temperatureStatus: { timestamp: { seconds: 180n }, hotCount: 1, coldCount: 1 },
          uptimeStatus: { timestamp: { seconds: 180n }, brokenCount: 2 },
        }),
        create(AccountDashboardReadingSchema, { time: { seconds: 270n }, summary: {} }),
      ],
      "1h",
    );
    expect(temperatureStatusCounts[0]).toMatchObject({ hotCount: 1, coldCount: 1, okCount: 0 });
    expect(uptimeStatusCounts[0]).toMatchObject({ brokenCount: 2, hashingCount: 0 });
    expect(temperatureStatusCounts[1]).toMatchObject({ timestamp: { seconds: 270n }, okCount: 0 });
    expect(uptimeStatusCounts[1]).toMatchObject({ timestamp: { seconds: 270n }, hashingCount: 0 });
  });
  it("preserves partial measured power and unavailable efficiency independently", () => {
    const { metrics, tooltipNotes } = accountDashboardData(
      [
        create(AccountDashboardReadingSchema, {
          time: { seconds: 180n },
          summary: {
            assetCount: 4n,
            moduleCount: 4n,
            hashrateHs: 400e12,
            powerW: 9000,
            hashrateReportingCount: 4n,
            powerReportingCount: 3n,
            pairedReportingCount: 3n,
          },
        }),
      ],
      "1h",
    );
    expect(metrics.find((metric) => metric.measurementType === MeasurementType.POWER)?.aggregatedValues[0].value).toBe(
      9,
    );
    expect(metrics.find((metric) => metric.measurementType === MeasurementType.EFFICIENCY)?.aggregatedValues).toEqual(
      [],
    );
    expect(tooltipNotes[MeasurementType.POWER]?.[180000]).toBe("3 of 4 modules reporting");
  });
  it("keeps measured zero and unavailable buckets distinct and charts only averaged metrics", () => {
    const { metrics } = accountDashboardData(
      [
        create(AccountDashboardReadingSchema, {
          time: { seconds: 180n },
          summary: { assetCount: 1n, minerCount: 1n, hashrateHs: 0, hashrateReportingCount: 1n },
        }),
        create(AccountDashboardReadingSchema, { time: { seconds: 270n }, summary: { assetCount: 1n, minerCount: 1n } }),
      ],
      "1h",
    );
    const hashrate = metrics.filter((metric) => metric.measurementType === MeasurementType.HASHRATE);
    expect(hashrate[0].aggregatedValues[0].value).toBe(0);
    expect(hashrate[1].aggregatedValues).toEqual([]);
    expect(new Set(metrics.map((metric) => metric.measurementType))).toEqual(
      new Set([MeasurementType.HASHRATE, MeasurementType.POWER, MeasurementType.EFFICIENCY]),
    );
  });
});

it("takes uptime coverage from the latest historical allocation, including telemetry gaps", () => {
  const { uptimeExpectedAssetCount, uptimeStatusCounts } = accountDashboardData(
    [
      create(AccountDashboardReadingSchema, {
        time: { seconds: 180n },
        summary: { assetCount: 5n },
        uptimeStatus: { hashingCount: 5 },
      }),
      create(AccountDashboardReadingSchema, { time: { seconds: 270n }, summary: { assetCount: 3n } }),
    ],
    "1h",
  );
  expect(uptimeExpectedAssetCount).toBe(3);
  expect(uptimeStatusCounts[1]).toMatchObject({ hashingCount: 0, notHashingCount: 0, brokenCount: 0 });
});
