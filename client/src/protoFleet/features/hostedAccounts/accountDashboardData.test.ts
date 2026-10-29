import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { accountDashboardData } from "./accountDashboardData";
import { AccountDashboardReadingSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { transformEfficiencyMetricsToChartData } from "@/protoFleet/features/dashboard/components/EfficiencyPanel/utils";
import { transformHashrateMetricsToChartData } from "@/protoFleet/features/dashboard/components/HashratePanel/utils";
import { transformPowerMetricsToChartData } from "@/protoFleet/features/dashboard/components/PowerPanel/utils";

describe("account dashboard readings", () => {
  it.each([
    ["7d", 3600],
    ["30d", 86400],
    ["90d", 86400],
    ["1y", 86400],
  ] as const)("preserves partial values and internal/trailing gaps at %s granularity", (_duration, bucketSeconds) => {
    const latestBucket = Math.floor(1793150000 / bucketSeconds) * bucketSeconds;
    const times = Array.from({ length: 5 }, (_, index) => latestBucket - (4 - index) * bucketSeconds);
    const { metrics, tooltipNotes, temperatureStatusCounts, uptimeStatusCounts } = accountDashboardData([
      create(AccountDashboardReadingSchema, {
        time: { seconds: BigInt(times[0]) },
        summary: {
          assetCount: 4n,
          moduleCount: 4n,
          hashrateHs: 400e12,
          powerW: 10000,
          efficiencyJTh: 25,
          hashrateReportingCount: 4n,
          powerReportingCount: 4n,
          pairedReportingCount: 4n,
        },
        temperatureStatus: { timestamp: { seconds: BigInt(times[0]) }, okCount: 4 },
        uptimeStatus: { timestamp: { seconds: BigInt(times[0]) }, hashingCount: 4 },
      }),
      create(AccountDashboardReadingSchema, { time: { seconds: BigInt(times[1]) } }),
      create(AccountDashboardReadingSchema, { time: { seconds: BigInt(times[2]) }, summary: { assetCount: 4n } }),
      create(AccountDashboardReadingSchema, {
        time: { seconds: BigInt(times[3]) },
        summary: {
          assetCount: 4n,
          moduleCount: 4n,
          hashrateHs: 400e12,
          powerW: 9000,
          hashrateReportingCount: 4n,
          powerReportingCount: 3n,
          pairedReportingCount: 3n,
        },
        temperatureStatus: { timestamp: { seconds: BigInt(times[3]) }, hotCount: 1 },
        uptimeStatus: { timestamp: { seconds: BigInt(times[3]) }, brokenCount: 1 },
      }),
      create(AccountDashboardReadingSchema, { time: { seconds: BigInt(times[4]) }, summary: { assetCount: 4n } }),
    ]);
    const byType = (type: MeasurementType) => metrics.filter((metric) => metric.measurementType === type);
    const hashrate = transformHashrateMetricsToChartData(byType(MeasurementType.HASHRATE));
    const power = transformPowerMetricsToChartData(byType(MeasurementType.POWER), true);
    const efficiency = transformEfficiencyMetricsToChartData(byType(MeasurementType.EFFICIENCY));
    expect(hashrate.slice(-5).map((point) => point.hashrate)).toEqual([400, null, null, 400, null]);
    expect(power.slice(-5).map((point) => point.power)).toEqual([10, null, null, 9, null]);
    expect(efficiency.slice(-5).map((point) => point.efficiency)).toEqual([25, null, null, null, null]);
    expect(tooltipNotes[MeasurementType.POWER]?.[times[3] * 1000]).toBe("3 of 4 modules reporting");
    expect(temperatureStatusCounts.map(({ okCount, hotCount }) => okCount + hotCount)).toEqual([4, 0, 0, 1, 0]);
    expect(uptimeStatusCounts.map(({ hashingCount, brokenCount }) => hashingCount + brokenCount)).toEqual([
      4, 0, 0, 1, 0,
    ]);
  });
  it("uses authorized status distributions without inferring them from averages and preserves gaps", () => {
    const { temperatureStatusCounts, uptimeStatusCounts } = accountDashboardData([
      create(AccountDashboardReadingSchema, {
        time: { seconds: 180n },
        summary: { temperatureC: 55, uptimePercent: 100 },
        temperatureStatus: { timestamp: { seconds: 180n }, hotCount: 1, coldCount: 1 },
        uptimeStatus: { timestamp: { seconds: 180n }, brokenCount: 2 },
      }),
      create(AccountDashboardReadingSchema, { time: { seconds: 270n }, summary: {} }),
    ]);
    expect(temperatureStatusCounts[0]).toMatchObject({ hotCount: 1, coldCount: 1, okCount: 0 });
    expect(uptimeStatusCounts[0]).toMatchObject({ brokenCount: 2, hashingCount: 0 });
    expect(temperatureStatusCounts[1]).toMatchObject({ timestamp: { seconds: 270n }, okCount: 0 });
    expect(uptimeStatusCounts[1]).toMatchObject({ timestamp: { seconds: 270n }, hashingCount: 0 });
  });
  it("preserves partial measured power and unavailable efficiency independently", () => {
    const { metrics, tooltipNotes } = accountDashboardData([
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
    ]);
    expect(metrics.find((metric) => metric.measurementType === MeasurementType.POWER)?.aggregatedValues[0].value).toBe(
      9,
    );
    expect(metrics.find((metric) => metric.measurementType === MeasurementType.EFFICIENCY)?.aggregatedValues).toEqual(
      [],
    );
    expect(tooltipNotes[MeasurementType.POWER]?.[180000]).toBe("3 of 4 modules reporting");
  });
  it("keeps measured zero and unavailable buckets distinct and charts only averaged metrics", () => {
    const { metrics } = accountDashboardData([
      create(AccountDashboardReadingSchema, {
        time: { seconds: 180n },
        summary: { assetCount: 1n, minerCount: 1n, hashrateHs: 0, hashrateReportingCount: 1n },
      }),
      create(AccountDashboardReadingSchema, { time: { seconds: 270n }, summary: { assetCount: 1n, minerCount: 1n } }),
    ]);
    const hashrate = metrics.filter((metric) => metric.measurementType === MeasurementType.HASHRATE);
    expect(hashrate[0].aggregatedValues[0].value).toBe(0);
    expect(hashrate[1].aggregatedValues).toEqual([]);
    expect(new Set(metrics.map((metric) => metric.measurementType))).toEqual(
      new Set([MeasurementType.HASHRATE, MeasurementType.POWER, MeasurementType.EFFICIENCY]),
    );
  });
});

it("takes uptime coverage from the latest historical allocation, including telemetry gaps", () => {
  const { uptimeExpectedAssetCount, uptimeStatusCounts } = accountDashboardData([
    create(AccountDashboardReadingSchema, {
      time: { seconds: 180n },
      summary: { assetCount: 5n },
      uptimeStatus: { hashingCount: 5 },
    }),
    create(AccountDashboardReadingSchema, { time: { seconds: 270n }, summary: { assetCount: 3n } }),
  ]);
  expect(uptimeExpectedAssetCount).toBe(3);
  expect(uptimeStatusCounts[1]).toMatchObject({ hashingCount: 0, notHashingCount: 0, brokenCount: 0 });
});
