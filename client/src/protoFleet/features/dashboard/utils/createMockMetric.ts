import { create } from "@bufbuild/protobuf";
import {
  AggregatedValueSchema,
  AggregationType,
  type MeasurementType,
  type Metric,
  MetricSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

export const createMockMetric = (
  measurementType: MeasurementType,
  avgValue: number,
  timestampSeconds: number,
): Metric => {
  return create(MetricSchema, {
    measurementType,
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
    deviceCount: 1,
  });
};
