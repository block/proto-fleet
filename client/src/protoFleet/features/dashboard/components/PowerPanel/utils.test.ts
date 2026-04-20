import { describe, expect, it } from "vitest";
import { transformPowerMetricsToChartData } from "./utils";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { createMockMetric } from "@/protoFleet/features/dashboard/utils/createMockMetric";

describe("transformPowerMetricsToChartData", () => {
  it("returns empty array for empty metrics", () => {
    expect(transformPowerMetricsToChartData([])).toEqual([]);
  });

  it("keeps already-normalized values unchanged", () => {
    const metrics = [createMockMetric(MeasurementType.POWER, 3.2, 1000)];
    const result = transformPowerMetricsToChartData(metrics);

    expect(result).toEqual([{ datetime: 1000000, power: 3.2 }]);
  });

  it("normalizes raw watt values to kW", () => {
    const metrics = [createMockMetric(MeasurementType.POWER, 3200, 1000)];
    const result = transformPowerMetricsToChartData(metrics);

    expect(result).toEqual([{ datetime: 1000000, power: 3.2 }]);
  });
});
