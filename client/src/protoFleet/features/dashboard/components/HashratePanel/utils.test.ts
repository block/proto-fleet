// @vitest-environment node
import { describe, expect, it } from "vitest";
import { transformHashrateMetricsToChartData, transformHashrateMetricsWithUnits } from "./utils";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { transformEfficiencyMetricsToChartData } from "@/protoFleet/features/dashboard/components/EfficiencyPanel/utils";
import { transformPowerMetricsToChartData } from "@/protoFleet/features/dashboard/components/PowerPanel/utils";
import { createMockMetric } from "@/protoFleet/features/dashboard/utils/createMockMetric";

describe.each([
  {
    name: "transformEfficiencyMetricsToChartData",
    transform: transformEfficiencyMetricsToChartData,
    measurementType: MeasurementType.EFFICIENCY,
    key: "efficiency",
    gapValue: 24.4,
    normalizedValue: 24.4,
    rawValue: 24.4e12,
    rawTitle: "normalizes large over-converted values back to J/TH",
  },
  {
    name: "transformPowerMetricsToChartData",
    transform: transformPowerMetricsToChartData,
    measurementType: MeasurementType.POWER,
    key: "power",
    gapValue: 3200,
    normalizedValue: 3.2,
    rawValue: 3200,
    rawTitle: "normalizes raw watt values to kW",
  },
  {
    name: "transformHashrateMetricsToChartData",
    transform: transformHashrateMetricsToChartData,
    measurementType: MeasurementType.HASHRATE,
    key: "hashrate",
    gapValue: 500,
    normalizedValue: 500,
    rawValue: 500e12,
    rawTitle: "normalizes raw H/s values into TH/S",
  },
])("$name", ({ transform, measurementType, key, gapValue, normalizedValue, rawValue, rawTitle }) => {
  it("keeps an unavailable bucket as a gap instead of zero", () => {
    const metric = createMockMetric(measurementType, gapValue, 1000);
    metric.aggregatedValues = [];
    expect(transform([metric])[0][key]).toBeNull();
  });

  it("returns empty array for empty metrics", () => {
    expect(transform([])).toEqual([]);
  });

  it("keeps already-normalized values unchanged", () => {
    const metrics = [createMockMetric(measurementType, normalizedValue, 1000)];
    expect(transform(metrics)).toEqual([{ datetime: 1000000, [key]: normalizedValue }]);
  });

  it(rawTitle, () => {
    const metrics = [createMockMetric(measurementType, rawValue, 1000)];
    expect(transform(metrics)).toEqual([{ datetime: 1000000, [key]: normalizedValue }]);
  });
});

describe("transformHashrateMetricsWithUnits", () => {
  it("should return TH/S for empty metrics", () => {
    const result = transformHashrateMetricsWithUnits([]);
    expect(result).toEqual({ chartData: [], unit: "TH/S" });
  });

  it("should use TH/S when max value is at threshold (1000)", () => {
    const metrics = [createMockMetric(MeasurementType.HASHRATE, 1000, 1000)];
    const result = transformHashrateMetricsWithUnits(metrics);
    expect(result.unit).toBe("TH/S");
    expect(result.chartData[0].hashrate).toBe(1000);
  });

  it("should convert to PH/S when max value exceeds threshold (1001)", () => {
    const metrics = [createMockMetric(MeasurementType.HASHRATE, 1001, 1000)];
    const result = transformHashrateMetricsWithUnits(metrics);
    expect(result.unit).toBe("PH/S");
    expect(result.chartData[0].hashrate).toBe(1.001);
  });

  it("should convert all values when any value exceeds threshold", () => {
    const metrics = [
      createMockMetric(MeasurementType.HASHRATE, 500, 1000),
      createMockMetric(MeasurementType.HASHRATE, 2000, 2000),
    ];
    const result = transformHashrateMetricsWithUnits(metrics);
    expect(result.unit).toBe("PH/S");
    expect(result.chartData[0].hashrate).toBe(0.5);
    expect(result.chartData[1].hashrate).toBe(2);
  });
});
