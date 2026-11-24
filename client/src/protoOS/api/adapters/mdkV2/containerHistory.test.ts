import { describe, expect, it, vi } from "vitest";
import { normalizeContainerHistory, queryContainerHistory } from "./containerHistory";
import type { MdkV2Client } from "./MdkV2Adapter";
import { toCanonicalContainerTelemetry } from "./normalize";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";

const request = { startTime: "2026-09-15T11:00:00Z", endTime: "2026-09-15T12:00:00Z", intervalMs: 300_000 };
const sample = { unit: "kW", buckets: [{ time: request.startTime, avg: 1.5, min: 1, max: 2, count: "3" }] };

describe("container history", () => {
  it("normalizes units, zeros, empty buckets and weighted aggregates", () => {
    const series = normalizeContainerHistory(
      {
        ...sample,
        buckets: [
          ...sample.buckets,
          { time: "2026-09-15T11:05:00Z", avg: 0, min: 0, max: 0, count: "1" },
          { time: "2026-09-15T11:10:00Z", avg: 9, min: 9, max: 9, count: "0" },
        ],
      },
      "fanPower",
      300_000,
    );
    expect(series.points.map((point) => point.value)).toEqual([1500, 0, null]);
    expect(series.aggregates).toEqual({
      min: { value: 0, units: "W" },
      max: { value: 2000, units: "W" },
      avg: { value: 1125, units: "W" },
    });
  });

  it("rejects unknown units and preserves negative temperatures", () => {
    expect(() => normalizeContainerHistory({ unit: "F" }, "outsideTemperature", 60_000)).toThrow(
      "Unsupported history units",
    );
    const series = normalizeContainerHistory(
      { unit: "C", buckets: [{ time: request.startTime, avg: -4, min: -5, max: -3, count: "1" }] },
      "outsideTemperature",
      60_000,
    );
    expect(series.points[0].value).toBe(-4);
  });

  it("deduplicates component requests and retains successful siblings without board discovery", async () => {
    const query = vi
      .fn()
      .mockImplementation((params) =>
        params.identifier === "missing" ? Promise.reject({ status: 404 }) : Promise.resolve({ data: sample }),
      );
    const client = { telemetryServiceQueryTimeSeries: query } as unknown as MdkV2Client;
    const result = await queryContainerHistory(
      client,
      {
        ...request,
        selections: [
          { metric: "fanPower", identifier: "fan-7" },
          { metric: "fanPower", identifier: "fan-7" },
          { metric: "distributionPower", identifier: "missing" },
        ],
      },
      {},
    );
    expect(query).toHaveBeenCalledTimes(2);
    expect(query).toHaveBeenCalledWith(
      expect.objectContaining({ metric: "TIME_SERIES_METRIC_PLANT_FAN_POWER", identifier: "fan-7" }),
      {},
    );
    expect(result.series["fanPower:fan-7"]?.points[0].value).toBe(1500);
    expect(result.series["distributionPower:missing"]).toBeUndefined();
    expect(result.unavailable).toEqual(["distributionPower:missing"]);
  });

  it("propagates auth and cancellation instead of treating them as missing metrics", async () => {
    for (const error of [{ status: 401 }, new DOMException("Aborted", "AbortError")]) {
      const client = { telemetryServiceQueryTimeSeries: vi.fn().mockRejectedValue(error) } as unknown as MdkV2Client;
      await expect(queryContainerHistory(client, { ...request, selections: [{ metric: "power" }] }, {})).rejects.toBe(
        error,
      );
    }
  });

  it("maps distinct live indoor/outdoor sensors and gives firmware power its own provenance", () => {
    const state = toCanonicalContainerTelemetry(
      { power: 1500, hashrate: 100 },
      { plant: { insideAmbientTemperatureC: 30, outsideAmbientTemperatureC: 20 } },
      "ct1",
    );
    const plant = [...state.plant.values()][0];
    expect(plant.insideAmbientTemperature?.latest?.value).toBe(30);
    expect(plant.outsideAmbientTemperature?.latest?.value).toBe(20);
    expect(state.rollupsByEntity.get(ROOT_MODULE_KEY)?.powerCoverage?.kind).toBe("unknown");
  });
});
