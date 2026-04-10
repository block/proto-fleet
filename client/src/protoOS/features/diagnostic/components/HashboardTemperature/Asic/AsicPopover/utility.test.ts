import { describe, expect, test } from "vitest";

import { convertAndFormatTemperature } from "./utility";

describe("convertAndFormatTemperature", () => {
  test("formats Celsius with space before degree sign", () => {
    expect(convertAndFormatTemperature(65.2, "C")).toBe("65.2 °C");
  });

  test("converts and formats Fahrenheit with space before degree sign", () => {
    expect(convertAndFormatTemperature(100, "F")).toBe("212.0 °F");
  });

  test("returns N/A for zero temperature", () => {
    expect(convertAndFormatTemperature(0, "C")).toBe("N/A");
  });

  test("returns N/A for null temperature", () => {
    expect(convertAndFormatTemperature(null, "C")).toBe("N/A");
  });

  test("returns N/A for undefined temperature", () => {
    expect(convertAndFormatTemperature(undefined, "C")).toBe("N/A");
  });

  test("hides unit when showUnits is false", () => {
    expect(convertAndFormatTemperature(65.2, "C", false)).toBe("65.2 °");
  });

  test("uses correct degree sign (U+00B0), not ordinal indicator", () => {
    const result = convertAndFormatTemperature(50, "C");
    expect(result).toContain("°"); // U+00B0
    expect(result).not.toContain("º"); // not U+00BA
  });
});
