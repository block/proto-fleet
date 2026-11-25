import { describe, expect, test } from "vitest";

import { convertAndFormatMeasurement } from "./telemetryUtils";

// The store holds temperatures as canonical contract measurements ({ units: "°C" }).
// After deleting the legacy projection these flow straight into the display helpers,
// so the degree glyph must survive a round-trip without being doubled (the old
// formatValue prepended "°" to a bare "C") or dropped.
describe("temperature measurement formatting", () => {
  test("formats a contract °C measurement verbatim when Celsius is preferred", () => {
    expect(convertAndFormatMeasurement({ value: 62, units: "°C" }, "°C")).toBe("62.0 °C");
  });

  test("converts a contract °C measurement to Fahrenheit", () => {
    expect(convertAndFormatMeasurement({ value: 100, units: "°C" }, "°F")).toBe("212.0 °F");
  });

  test("converts a 0 °C reading to Fahrenheit — a falsy 0 must not skip conversion", () => {
    // Regression: convertValueUnits used to short-circuit on `!value`, returning
    // "0 °C" under a Fahrenheit request. Temperature is affine, so 0°C is 32°F.
    expect(convertAndFormatMeasurement({ value: 0, units: "°C" }, "°F")).toBe("32.0 °F");
  });

  test("returns N/A for a null reading and undefined for a missing value", () => {
    expect(convertAndFormatMeasurement({ value: null, units: "°C" }, "°F")).toBe("N/A");
    expect(convertAndFormatMeasurement({ value: undefined as unknown as number, units: "°C" }, "°F")).toBeUndefined();
  });
});
