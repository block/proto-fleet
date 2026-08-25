import { describe, expect, test, vi } from "vitest";
import { MdkV1Adapter } from "./MdkV1Adapter";
import { MetricUnit } from "@/protoOS/api/generatedApi";
import { getAsicId } from "@/protoOS/store";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";

describe("MdkV1Adapter.getTimeSeries", () => {
  test("adds hashboard identity context to ASIC-only history requests", async () => {
    const getTimeSeries = vi.fn().mockResolvedValue({
      data: {
        meta: { start_time: "2026-08-18T00:00:00Z", interval: "PT1M" },
        data: {
          hashboards: [{ index: 0, serial_number: "HB-1", hashrate: { unit: MetricUnit.THS, values: [50] } }],
          asics: [{ hashboard_index: 0, index: 2, temperature: { unit: MetricUnit.ValueC, values: [70] } }],
        },
      },
    });
    const adapter = new MdkV1Adapter({ getTimeSeries } as never);

    const telemetry = await adapter.getTimeSeries({
      startTime: "2026-08-18T00:00:00Z",
      interval: "PT1M",
      levels: [{ type: "asic", fields: ["temperature"] }],
    });

    expect(getTimeSeries).toHaveBeenCalledWith(
      expect.objectContaining({
        levels: [
          { type: "asic", fields: ["temperature"] },
          { type: "hashboard", fields: ["hashrate"] },
        ],
      }),
      {},
    );
    expect(telemetry.asics.get(getAsicId("HB-1", 2))?.temperature?.timeSeries?.points[0]?.value).toBe(70);
  });

  test("uses the requested interval when response metadata omits it", async () => {
    const getTimeSeries = vi.fn().mockResolvedValue({
      data: {
        data: { miner: { hashrate: { unit: MetricUnit.THS, values: [90, 100] } } },
      },
    });
    const adapter = new MdkV1Adapter({ getTimeSeries } as never);

    const telemetry = await adapter.getTimeSeries({
      startTime: "2026-08-18T00:00:00Z",
      interval: "PT5M",
      levels: [{ type: "miner", fields: ["hashrate"] }],
    });

    expect(telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries?.points).toEqual([
      { timestampMs: Date.parse("2026-08-18T00:00:00Z"), value: 90 },
      { timestampMs: Date.parse("2026-08-18T00:05:00Z"), value: 100 },
    ]);
  });

  test("rejects history when no valid interval is available", async () => {
    const getTimeSeries = vi.fn().mockResolvedValue({
      data: {
        meta: { interval: "invalid" },
        data: { miner: { hashrate: { unit: MetricUnit.THS, values: [90, 100] } } },
      },
    });
    const adapter = new MdkV1Adapter({ getTimeSeries } as never);

    await expect(
      adapter.getTimeSeries({
        startTime: "2026-08-18T00:00:00Z",
        levels: [{ type: "miner", fields: ["hashrate"] }],
      }),
    ).rejects.toThrow("valid positive interval");
  });
});

describe("MdkV1Adapter.getCurrentTelemetry", () => {
  test("joins telemetry levels into a single comma-separated value", async () => {
    const getCurrentTelemetry = vi.fn().mockResolvedValue({ data: {} });
    const adapter = new MdkV1Adapter({ getCurrentTelemetry } as never);

    await adapter.getCurrentTelemetry(["miner", "hashboard"]);

    // Must stay comma-joined (a single value): the firmware rejects repeated
    // `level=` params with 400 "duplicate field `level`" (OpenAPI explode:false,
    // contradicting the generated array type). Verified against a real rig.
    expect(getCurrentTelemetry).toHaveBeenCalledWith({ level: "miner,hashboard" }, {});
  });

  test("omits the level param entirely when no levels are requested", async () => {
    const getCurrentTelemetry = vi.fn().mockResolvedValue({ data: {} });
    const adapter = new MdkV1Adapter({ getCurrentTelemetry } as never);

    await adapter.getCurrentTelemetry([]);

    expect(getCurrentTelemetry).toHaveBeenCalledWith({ level: undefined }, {});
  });
});
