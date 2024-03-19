import { describe, expect, test } from "vitest";

import {
  addCommas,
  getMacAddressDisplay,
  getPoolUrlDisplay,
  getSerialNumbersDisplay,
  getStandardTime,
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

describe("getPoolUrlDisplay", () => {
  test("should return the correct display format for stratum v1 url", () => {
    const url = "stratum+tcp://host.docker.internal:3333";
    expect(getPoolUrlDisplay(url)).toBe("host.docker.internal");
  });

  test("should return the correct display format for stratum v2 url", () => {
    const url =
      "stratum2+tcp://v2.stratum.braiins.com/u95GEReVMjK6k5YqiSFNqqTnKU4ypU2Wm8awa6tmbmDmk1bWt";
    expect(getPoolUrlDisplay(url)).toBe("v2.stratum.braiins.com");
  });

  test("should return the original value if the url format is not recognized", () => {
    const url = "http://example.com";
    expect(getPoolUrlDisplay(url)).toBe(url);
  });

  test("should return undefined if the value is not provided", () => {
    expect(getPoolUrlDisplay()).toBe(undefined);
  });
});

describe("getSerialNumbersDisplay", () => {
  test("should return a string with serial numbers separated by spaces", () => {
    const serialNumbers = [
      "1111111111111111111111",
      "2222222222222222222222",
      "3333333333333333333333",
    ];
    expect(getSerialNumbersDisplay(serialNumbers)).toStrictEqual([
      "1111 1111 1111 1111 1111 11",
      "2222 2222 2222 2222 2222 22",
      "3333 3333 3333 3333 3333 33",
    ]);
  });

  test("should return an empty array if the serial numbers array is empty", () => {
    const serialNumbers: string[] = [];
    expect(getSerialNumbersDisplay(serialNumbers)).toStrictEqual([]);
  });

  test("should return undefined if the serial numbers array is not provided", () => {
    expect(getSerialNumbersDisplay()).toBe(undefined);
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

describe("getStandardTime", () => {
  test("should return the standard time format", () => {
    expect(getStandardTime("00:00")).toBe("12:00 AM");
    expect(getStandardTime("12:00")).toBe("12:00 PM");
    expect(getStandardTime("13:00")).toBe("1:00 PM");
    expect(getStandardTime("23:59")).toBe("11:59 PM");
  });

  test("should return the original value if the time format is not recognized", () => {
    expect(getStandardTime("123")).toBe("123");
  });
});
