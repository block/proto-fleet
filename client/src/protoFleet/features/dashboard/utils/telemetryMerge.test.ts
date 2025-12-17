import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { mergeByTimestamp, mergeMetrics, mergeStatusCounts } from "./telemetryMerge";
import {
  type Metric,
  MetricSchema,
  type TemperatureStatusCount,
  TemperatureStatusCountSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

describe("mergeByTimestamp", () => {
  it("returns existing array when incoming array is empty", () => {
    const existing = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const result = mergeByTimestamp(existing, []);
    expect(result).toEqual(existing);
  });

  it("returns incoming array when existing array is empty", () => {
    const incoming = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(2000), nanos: 0 },
      }),
    ];
    const result = mergeByTimestamp([], incoming);
    expect(result).toEqual(incoming);
  });

  it("merges arrays with unique timestamps", () => {
    const existing = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
        coldCount: 1,
      }),
    ];
    const incoming = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(2000), nanos: 0 },
        coldCount: 2,
      }),
    ];
    const result = mergeByTimestamp(existing, incoming);
    expect(result).toHaveLength(2);
    expect(result[0]).toEqual(existing[0]);
    expect(result[1]).toEqual(incoming[0]);
  });

  it("filters duplicate timestamps from incoming array", () => {
    const existing = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
        coldCount: 1,
      }),
    ];
    const incoming = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
        coldCount: 999, // Different data but same timestamp
      }),
    ];
    const result = mergeByTimestamp(existing, incoming);
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual(existing[0]); // Keeps existing, filters duplicate
  });

  it("handles items with openTime field instead of timestamp", () => {
    const existing: Metric[] = [
      create(MetricSchema, {
        openTime: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const incoming: Metric[] = [
      create(MetricSchema, {
        openTime: { seconds: BigInt(2000), nanos: 0 },
      }),
    ];
    const result = mergeByTimestamp(existing, incoming);
    expect(result).toHaveLength(2);
  });

  it("handles mixed timestamp and openTime fields", () => {
    type MixedItem =
      | { timestamp?: { seconds?: bigint }; openTime?: never }
      | { openTime?: { seconds?: bigint }; timestamp?: never };

    const existing: MixedItem[] = [{ timestamp: { seconds: BigInt(1000) } }];
    const incoming: MixedItem[] = [{ openTime: { seconds: BigInt(2000) } }];
    const result = mergeByTimestamp(existing, incoming);
    expect(result).toHaveLength(2);
  });

  it("filters items with undefined timestamps from both arrays", () => {
    const existing = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
      }),
      create(TemperatureStatusCountSchema, {
        // No timestamp
      }),
    ];
    const incoming = [
      create(TemperatureStatusCountSchema, {
        // No timestamp
      }),
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(2000), nanos: 0 },
      }),
    ];
    const result = mergeByTimestamp(existing, incoming);
    // Should only include items with defined timestamps from incoming
    expect(result).toHaveLength(3); // existing[0], existing[1], incoming[1]
    expect(result[2].timestamp?.seconds).toBe(BigInt(2000));
  });

  it("handles multiple duplicates and unique items", () => {
    const existing = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
      }),
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(2000), nanos: 0 },
      }),
    ];
    const incoming = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
      }), // Duplicate
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(3000), nanos: 0 },
      }), // Unique
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(2000), nanos: 0 },
      }), // Duplicate
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(4000), nanos: 0 },
      }), // Unique
    ];
    const result = mergeByTimestamp(existing, incoming);
    expect(result).toHaveLength(4); // 2 existing + 2 unique from incoming
    expect(result[2].timestamp?.seconds).toBe(BigInt(3000));
    expect(result[3].timestamp?.seconds).toBe(BigInt(4000));
  });
});

describe("mergeStatusCounts", () => {
  it("returns empty array when both arrays are undefined", () => {
    const result = mergeStatusCounts<TemperatureStatusCount>(undefined, undefined);
    expect(result).toEqual([]);
  });

  it("returns streaming array when historical is undefined", () => {
    const streaming = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const result = mergeStatusCounts(undefined, streaming);
    expect(result).toEqual(streaming);
  });

  it("returns historical array when streaming is undefined", () => {
    const historical = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const result = mergeStatusCounts(historical, undefined);
    expect(result).toEqual(historical);
  });

  it("returns streaming array when historical is empty", () => {
    const streaming = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const result = mergeStatusCounts([], streaming);
    expect(result).toEqual(streaming);
  });

  it("returns historical array when streaming is empty", () => {
    const historical = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const result = mergeStatusCounts(historical, []);
    expect(result).toEqual(historical);
  });

  it("merges both arrays when both have data", () => {
    const historical = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const streaming = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(2000), nanos: 0 },
      }),
    ];
    const result = mergeStatusCounts(historical, streaming);
    expect(result).toHaveLength(2);
  });

  it("filters duplicates when merging arrays with overlapping timestamps", () => {
    const historical = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
        coldCount: 1,
      }),
    ];
    const streaming = [
      create(TemperatureStatusCountSchema, {
        timestamp: { seconds: BigInt(1000), nanos: 0 },
        coldCount: 999, // Different data but same timestamp
      }),
    ];
    const result = mergeStatusCounts(historical, streaming);
    expect(result).toHaveLength(1);
    expect(result[0].coldCount).toBe(1); // Keeps historical value
  });
});

describe("mergeMetrics", () => {
  it("returns historical array when streaming is undefined", () => {
    const historical: Metric[] = [
      create(MetricSchema, {
        openTime: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const result = mergeMetrics(historical, undefined);
    expect(result).toEqual(historical);
  });

  it("returns historical array when streaming is empty", () => {
    const historical: Metric[] = [
      create(MetricSchema, {
        openTime: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const result = mergeMetrics(historical, []);
    expect(result).toEqual(historical);
  });

  it("merges historical and streaming arrays", () => {
    const historical: Metric[] = [
      create(MetricSchema, {
        openTime: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const streaming: Metric[] = [
      create(MetricSchema, {
        openTime: { seconds: BigInt(2000), nanos: 0 },
      }),
    ];
    const result = mergeMetrics(historical, streaming);
    expect(result).toHaveLength(2);
  });

  it("filters duplicate timestamps when merging", () => {
    const historical: Metric[] = [
      create(MetricSchema, {
        openTime: { seconds: BigInt(1000), nanos: 0 },
        deviceCount: 5,
      }),
    ];
    const streaming: Metric[] = [
      create(MetricSchema, {
        openTime: { seconds: BigInt(1000), nanos: 0 },
        deviceCount: 999, // Different data but same timestamp
      }),
    ];
    const result = mergeMetrics(historical, streaming);
    expect(result).toHaveLength(1);
    expect(result[0].deviceCount).toBe(5); // Keeps historical value
  });

  it("handles empty historical array", () => {
    const streaming: Metric[] = [
      create(MetricSchema, {
        openTime: { seconds: BigInt(1000), nanos: 0 },
      }),
    ];
    const result = mergeMetrics([], streaming);
    expect(result).toEqual(streaming);
  });
});
