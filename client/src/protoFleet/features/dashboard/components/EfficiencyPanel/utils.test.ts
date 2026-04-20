import { describe, expect, it } from "vitest";
import { transformEfficiencyMetricsToChartData } from "./utils";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { createMockMetric } from "@/protoFleet/features/dashboard/utils/createMockMetric";

describe("transformEfficiencyMetricsToChartData", () => {
  it("returns empty array for empty metrics", () => {
    expect(transformEfficiencyMetricsToChartData([])).toEqual([]);
  });

  it("keeps already-normalized values unchanged", () => {
    const metrics = [createMockMetric(MeasurementType.EFFICIENCY, 24.4, 1000)];
    const result = transformEfficiencyMetricsToChartData(metrics);

    expect(result).toEqual([{ datetime: 1000000, efficiency: 24.4 }]);
  });

  it("normalizes large over-converted values back to J/TH", () => {
    const metrics = [createMockMetric(MeasurementType.EFFICIENCY, 24.4e12, 1000)];
    const result = transformEfficiencyMetricsToChartData(metrics);

    expect(result).toEqual([{ datetime: 1000000, efficiency: 24.4 }]);
  });
});
