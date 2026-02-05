import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { transformHashrateMetricsToChartData, transformHashrateMetricsWithUnits } from "./utils";
import {
  AggregatedValueSchema,
  AggregationType,
  MeasurementType,
  type Metric,
  MetricSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

const createMockMetric = (avgValue: number, timestampSeconds: number): Metric => {
  return create(MetricSchema, {
    measurementType: MeasurementType.HASHRATE,
    openTime: {
      seconds: BigInt(timestampSeconds),
      nanos: 0,
    },
    aggregatedValues: [
      create(AggregatedValueSchema, {
        aggregationType: AggregationType.AVERAGE,
        value: avgValue,
      }),
    ],
  });
};

describe("transformHashrateMetricsToChartData", () => {
  it("should return empty array for empty metrics", () => {
    expect(transformHashrateMetricsToChartData([])).toEqual([]);
  });

  it("should transform metrics to chart data format", () => {
    const metrics = [createMockMetric(500, 1000)];
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
    const metrics = [createMockMetric(1000, 1000)];
    const result = transformHashrateMetricsWithUnits(metrics);
    expect(result.unit).toBe("TH/S");
    expect(result.chartData[0].hashrate).toBe(1000);
  });

  it("should convert to PH/S when max value exceeds threshold (1001)", () => {
    const metrics = [createMockMetric(1001, 1000)];
    const result = transformHashrateMetricsWithUnits(metrics);
    expect(result.unit).toBe("PH/S");
    expect(result.chartData[0].hashrate).toBe(1.001);
  });

  it("should convert all values when any value exceeds threshold", () => {
    const metrics = [createMockMetric(500, 1000), createMockMetric(2000, 2000)];
    const result = transformHashrateMetricsWithUnits(metrics);
    expect(result.unit).toBe("PH/S");
    expect(result.chartData[0].hashrate).toBe(0.5);
    expect(result.chartData[1].hashrate).toBe(2);
  });
});
