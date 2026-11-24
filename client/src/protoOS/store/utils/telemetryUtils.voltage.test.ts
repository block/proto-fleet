import { describe, expect, test } from "vitest";

import { convertAndFormatMeasurement } from "./telemetryUtils";

// Regression for the Codex security-review finding "ASIC voltage rendered 1000x
// too low". The store normalizes ASIC voltage to volts (useHashboardStatus writes
// `{ value: voltage_mv / 1000, units: "V" }`), but the diagnostic grid displays
// millivolts. Before the converter learned V<->mV, requesting "mV" left the volts
// value unscaled, so an 850 mV ASIC surfaced as 0.85 under an "mV" label.
describe("ASIC voltage unit conversion", () => {
  test("rescales volts to millivolts for the diagnostic grid", () => {
    expect(convertAndFormatMeasurement({ value: 0.85, units: "V" }, "mV", false)).toBe("850.0");
  });

  test("rescales millivolts back to volts (why the grid shows mV: one-decimal rounding collapses precision)", () => {
    expect(convertAndFormatMeasurement({ value: 850, units: "mV" }, "V", false)).toBe("0.8");
  });

  test("leaves an already-millivolt measurement unchanged", () => {
    expect(convertAndFormatMeasurement({ value: 850, units: "mV" }, "mV", false)).toBe("850.0");
  });
});
