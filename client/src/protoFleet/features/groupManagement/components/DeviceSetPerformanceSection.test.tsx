import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { DeviceSetPerformanceSection } from "./DeviceSetPerformanceSection";
import { AggregationType, MeasurementType, MetricSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type { ChartData } from "@/shared/components/LineChart/types";
vi.mock("@/protoFleet/store", () => ({ useTemperatureUnit: () => "C" }));
vi.mock("@/protoFleet/components/LineChart", () => ({
  default: ({ chartData, units }: { chartData: ChartData[]; units: string }) => (
    <pre data-testid={`chart-${units}`}>{JSON.stringify(chartData)}</pre>
  ),
}));
it("preserves explicit tank units and min/average/max ordering with mixed coverage", () => {
  const metric = create(MetricSchema, {
    measurementType: MeasurementType.POWER,
    deviceCount: 1,
    openTime: { seconds: BigInt(Math.floor(Date.now() / 90000) * 90) },
    aggregatedValues: [
      { aggregationType: AggregationType.MIN, value: 18 },
      { aggregationType: AggregationType.AVERAGE, value: 153 },
      { aggregationType: AggregationType.MAX, value: 288 },
    ],
  });
  render(<DeviceSetPerformanceSection duration="1h" metrics={[metric]} inputUnits="display" />);
  const points = JSON.parse(screen.getByTestId("chart-kW").textContent!) as ChartData[];
  const point = points.find((p) => p.avg !== null)!;
  expect(point).toMatchObject({ min: 18, avg: 153, max: 288 });
  expect(point.min).toBeLessThanOrEqual(point.avg!);
  expect(point.avg).toBeLessThanOrEqual(point.max!);
});
