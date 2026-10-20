import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { accountDashboardData } from "./accountDashboardData";
import { AccountDashboardReadingSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

describe("account dashboard readings", () => {
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
      "C",
    );
    expect(metrics.find((metric) => metric.measurementType === MeasurementType.POWER)?.aggregatedValues[0].value).toBe(
      9,
    );
    expect(metrics.find((metric) => metric.measurementType === MeasurementType.EFFICIENCY)?.aggregatedValues).toEqual(
      [],
    );
    expect(tooltipNotes[MeasurementType.POWER]?.[180000]).toBe("3 of 4 modules reporting");
  });
  it("keeps measured zero and unavailable buckets distinct for every metric", () => {
    const { metrics } = accountDashboardData(
      [
        create(AccountDashboardReadingSchema, {
          time: { seconds: 180n },
          summary: { assetCount: 1n, minerCount: 1n, hashrateHs: 0, hashrateReportingCount: 1n },
        }),
        create(AccountDashboardReadingSchema, { time: { seconds: 270n }, summary: { assetCount: 1n, minerCount: 1n } }),
      ],
      "1h",
      "F",
    );
    const hashrate = metrics.filter((metric) => metric.measurementType === MeasurementType.HASHRATE);
    expect(hashrate[0].aggregatedValues[0].value).toBe(0);
    expect(hashrate[1].aggregatedValues).toEqual([]);
    expect(
      metrics
        .filter((metric) => metric.measurementType === MeasurementType.TEMPERATURE)
        .every((metric) => metric.aggregatedValues.length === 0),
    ).toBe(true);
  });
});
