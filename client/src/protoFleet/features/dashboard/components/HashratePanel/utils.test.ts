import { describe, expect, it } from "vitest";
import { transformHashrateMetricsToChartData, transformHashrateMetricsWithUnits } from "./utils";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { createMockMetric } from "@/protoFleet/features/dashboard/utils/createMockMetric";

describe("transformHashrateMetricsToChartData", () => {
  it("should return empty array for empty metrics", () => {
    expect(transformHashrateMetricsToChartData([])).toEqual([]);
  });

  it("should transform metrics to chart data format", () => {
    const metrics = [createMockMetric(MeasurementType.HASHRATE, 500, 1000)];
    const result = transformHashrateMetricsToChartData(metrics);
    expect(result).toEqual([{ datetime: 1000000, hashrate: 500 }]);
  });

  it("should normalize raw H/s values into TH/S", () => {
    const metrics = [createMockMetric(MeasurementType.HASHRATE, 500e12, 1000)];
    const result = transformHashrateMetricsToChartData(metrics);
    expect(result).toEqual([{ datetime: 1000000, hashrate: 500 }]);
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
