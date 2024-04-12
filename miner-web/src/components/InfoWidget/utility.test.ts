import { describe, expect, test } from "vitest";

import { getDisplayValue, getIntensity, separateByCommas } from "./utility";

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

describe("separateByCommas", () => {
  test("should return the same value when no commas are present", () => {
    const value = "123";
    const result = separateByCommas(value);
    expect(result).toBe("123");
  });

  test("should separate thousands with commas", () => {
    const value = "1234567";
    const result = separateByCommas(value);
    expect(result).toBe("1,234,567");
  });

  test("should handle decimal values correctly", () => {
    const value = "1234.567";
    const result = separateByCommas(value);
    expect(result).toBe("1,234.567");
  });

  test("should handle negative values correctly", () => {
    const value = "-1234567";
    const result = separateByCommas(value);
    expect(result).toBe("-1,234,567");
  });
});

describe("getDisplayValue", () => {
  test("should return the value as a string when value is provided", () => {
    const value = 5;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5");
  });

  test("should return an empty string when value is not provided", () => {
    const displayValue = getDisplayValue();
    expect(displayValue).toBeUndefined();
  });

  test("should return the value rounded down to two decimal places", () => {
    const value = 5.563;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5.56");
  });

  test("should return the value rounded up to two decimal places", () => {
    const value = 5.565;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5.57");
  });

  test("should return the value with one decimal place if second decimal is zero", () => {
    const value = 5.50;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5.5");
  });

  test("should return the value as an integer if both decimal places are zeros", () => {
    const value = 5.00;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5");
  });

  test("should return the value rounded up to two decimal places if third decimal place is non-zero", () => {
    const value = 5.106;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5.11");
  });

  test("should return the value rounded down to one decimal place if third decimal place is non-zero but below 5", () => {
    const value = 5.103;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5.1");
  });

  test("should separate thousands by commas and round down to two decimal places", () => {
    const value = 12345.671;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("12,345.67");
  });

  test("should separate thousands by commas and round up to two decimal places", () => {
    const value = 12345.678;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("12,345.68");
  });

  test("should separate thousands by commas and remove second decimal place if zero", () => {
    const value = 12345.60;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("12,345.6");
  });

  test("should separate thousands by commas and remove all decimal places if zeros", () => {
    const value = 12345.00;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("12,345");
  });
});
