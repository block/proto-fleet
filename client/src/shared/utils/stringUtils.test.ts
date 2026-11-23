// @vitest-environment node
import { describe, expect, it, test } from "vitest";

import {
  convertToSentenceCase,
  getDisplayValue,
  getMacAddressDisplay,
  padLeft,
  separateByCommas,
  stripLeadingSlash,
} from "./stringUtils";

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
    expect(displayValue).toBe("5.0");
  });

  test("should return an empty string when value is not provided", () => {
    const displayValue = getDisplayValue();
    expect(displayValue).toBeUndefined();
  });

  test("should return the value rounded down to one decimal places", () => {
    const value = 5.53;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5.5");
  });

  test("should return the value rounded up to two decimal places", () => {
    const value = 5.565;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("5.6");
  });

  test("should separate thousands by commas and round down to one decimal places", () => {
    const value = 12345.671;
    const displayValue = getDisplayValue(value);
    expect(displayValue).toBe("12,345.7");
  });
});

describe("padLeft", () => {
  test("should pad the value with zeros to the left", () => {
    const value = 5;
    const length = 3;
    const result = padLeft(value, length);
    expect(result).toBe("005");
  });

  test("should not pad the value if the length is less than the value", () => {
    const value = 5;
    const length = 1;
    const result = padLeft(value, length);
    expect(result).toBe("5");
  });
});

describe("stripLeadingSlash", () => {
  test("should strip leading slash", () => {
    const path = "/foo/bar";
    expect(stripLeadingSlash(path)).toBe("foo/bar");
  });

  test("not alter string without leading slash", () => {
    const path = "foo/bar";
    expect(stripLeadingSlash(path)).toBe("foo/bar");
  });
});

describe("convertToSentenceCase", () => {
  it("should capitalize the first letter of the first word in each sentence", () => {
    const input = "hello world. how are you? i'm fine!";
    const expected = "Hello world. How are you? I'm fine!";
    expect(convertToSentenceCase(input)).toBe(expected);
  });

  it("should handle strings with no punctuation", () => {
    const input = "hello world";
    const expected = "Hello world";
    expect(convertToSentenceCase(input)).toBe(expected);
  });

  it("should handle empty strings", () => {
    const input = "";
    const expected = "";
    expect(convertToSentenceCase(input)).toBe(expected);
  });
});
