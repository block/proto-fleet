// @vitest-environment node
import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { metricAverage } from "./metricAverage";
import { AggregationType, MetricSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

describe("metricAverage", () => {
  it("prefers a measured zero average over another aggregation", () => {
    const metric = create(MetricSchema, {
      aggregatedValues: [
        { aggregationType: AggregationType.MAX, value: 100 },
        { aggregationType: AggregationType.AVERAGE, value: 0 },
      ],
    });
    expect(metricAverage(metric)).toBe(0);
  });

  it("falls back to the first aggregation when there is no average", () => {
    const metric = create(MetricSchema, {
      aggregatedValues: [
        { aggregationType: AggregationType.MAX, value: 100 },
        { aggregationType: AggregationType.MIN, value: 20 },
      ],
    });
    expect(metricAverage(metric)).toBe(100);
  });

  it.each([NaN, Infinity, -Infinity])("keeps an invalid average (%s) as a gap", (value) => {
    const metric = create(MetricSchema, {
      aggregatedValues: [
        { aggregationType: AggregationType.MAX, value: 100 },
        { aggregationType: AggregationType.AVERAGE, value },
      ],
    });
    expect(metricAverage(metric)).toBeUndefined();
  });

  it("keeps a missing reading as a gap", () => {
    expect(metricAverage(create(MetricSchema))).toBeUndefined();
  });
});
