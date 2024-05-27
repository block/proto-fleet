import { describe, expect, test } from "vitest";

import {
  addCommas,
  getDisplayValue,
  getMacAddressDisplay,
  getTimeFromEpoch,
  separateByCommas,
} from "./stringUtils";

describe("addCommas", () => {
  test("should add commas for every 3 digits in a number", () => {
    expect(addCommas(1000)).toBe("1,000");
    expect(addCommas(1000000)).toBe("1,000,000");
    expect(addCommas(1234567890)).toBe("1,234,567,890");
  });

  test("should return undefined if the value is not provided", () => {
    expect(addCommas()).toBe(undefined);
  });

  test("should not add a comma if number is less than 4 digits", () => {
    expect(addCommas(100)).toBe("100");
  });
});

describe("getMacAddressDisplay", () => {
  test("should return the mac address with colon separators", () => {
    const macAddress = "00.11.22.33.44.55";
    expect(getMacAddressDisplay(macAddress)).toBe("00:11:22:33:44:55");
  });

  test("should return undefined if the mac address is not provided", () => {
    expect(getMacAddressDisplay()).toBe(undefined);
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
    const value = 5.5;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5.5");
  });

  test("should return the value as an integer if both decimal places are zeros", () => {
    const value = 5.0;
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
    const value = 12345.6;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("12,345.6");
  });

  test("should separate thousands by commas and remove all decimal places if zeros", () => {
    const value = 12345.0;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("12,345");
  });
});

// since epoch gets converted to local timestamp, check general format rather than exact time
const expectedTimestamp = new RegExp(/^[0-9]{2}:[0-9]{2}:[0-9]{2}$/);

describe("getTimeFromEpoch", () => {
  test("should return the formatted timestamp when epoch is provided in seconds", () => {
    const epoch = 1634567890;
    const result = getTimeFromEpoch(epoch);
    expect(result).toMatch(expectedTimestamp);
  });

  test("should return the formatted timestamp when epoch is provided in miliseconds", () => {
    const epoch = 1634567890000;
    const result = getTimeFromEpoch(epoch);
    expect(result).toMatch(expectedTimestamp);
  });

  test("should return an empty string when epoch is not provided", () => {
    const result = getTimeFromEpoch();
    expect(result).toBe("");
  });
});
