import { describe, expect, test } from "vitest";

import { getIntensity } from "./utility";

describe("getIntensity", () => {
  test("should return the intensity value when both value and max are provided", () => {
    const value = 5;
    const max = 10;
    const intensity = getIntensity(value, max);
    expect(intensity).toBe(5);
  });

  test("should return 0 when value is 0", () => {
    const value = 0;
    const max = 10;
    const intensity = getIntensity(value, max);
    expect(intensity).toBe(0);
  });

  test("should return 10 when value is equal to max", () => {
    const value = 10;
    const max = 10;
    const intensity = getIntensity(value, max);
    expect(intensity).toBe(10);
  });

  test("should return 0 when value is not provided", () => {
    const max = 10;
    const intensity = getIntensity(undefined, max);
    expect(intensity).toBe(0);
  });

  test("should return 0 when max is not provided", () => {
    const value = 5;
    const intensity = getIntensity(value);
    expect(intensity).toBe(0);
  });
});
