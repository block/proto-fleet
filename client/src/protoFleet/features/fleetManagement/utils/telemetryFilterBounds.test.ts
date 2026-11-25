import { describe, expect, it } from "vitest";

import { TELEMETRY_FILTER_BOUNDS } from "./telemetryFilterBounds";

describe("TELEMETRY_FILTER_BOUNDS", () => {
  it.each(Object.entries(TELEMETRY_FILTER_BOUNDS))("%s bounds are finite and well-ordered", (_, b) => {
    expect(Number.isFinite(b.min)).toBe(true);
    expect(Number.isFinite(b.max)).toBe(true);
    expect(b.min).toBeLessThan(b.max);
    expect(b.unit).toBeTypeOf("string");
    expect(b.unit.length).toBeGreaterThan(0);
    expect(b.label).toBeTypeOf("string");
    expect(b.label.length).toBeGreaterThan(0);
  });

  it("emits display units that match what telemetry APIs return", () => {
    expect(TELEMETRY_FILTER_BOUNDS.hashrate.unit).toBe("TH/s");
    expect(TELEMETRY_FILTER_BOUNDS.efficiency.unit).toBe("J/TH");
    expect(TELEMETRY_FILTER_BOUNDS.power.unit).toBe("kW");
    expect(TELEMETRY_FILTER_BOUNDS.temperature.unit).toBe("°C");
  });
});
